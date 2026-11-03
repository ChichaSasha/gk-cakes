//go:build integration

package notifications_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/notifications"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/jackc/pgx/v5"
)

// AC1: a rolled-back domain transaction leaves neither the business change
// nor any notice, job or audit event; a committed one leaves exactly one of
// each, linked.
func TestRollbackLeavesNeitherBusinessChangeNorNotice(t *testing.T) {
	h := newHarness(t)
	h.owned("CREATE TABLE business.synthetic_order (id uuid PRIMARY KEY, status text NOT NULL)")
	order := nextUUID()
	h.owned("INSERT INTO business.synthetic_order VALUES ($1::uuid, 'pending')", order)
	confirm := func(tx pgx.Tx, n notifications.Notice) error {
		if _, err := tx.Exec(h.ctx, "UPDATE business.synthetic_order SET status = 'confirmed' WHERE id = $1::uuid", order); err != nil {
			return err
		}
		return notifications.Enqueue(h.ctx, tx, n, "staff", nextUUID())
	}
	status := func() string {
		var s string
		if err := h.admin.QueryRow(h.ctx, "SELECT status FROM business.synthetic_order WHERE id = $1::uuid", order).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	totals := func() [3]int {
		return [3]int{h.count("SELECT count(*) FROM business.notification_intent"),
			h.count("SELECT count(*) FROM business.job"), h.count("SELECT count(*) FROM business.audit_event")}
	}

	failLater := errors.New("a later statement in the transaction failed")
	err := h.ownerTx(func(tx pgx.Tx) error {
		if err := confirm(tx, newNotice(t, "test")); err != nil {
			return err
		}
		return failLater
	})
	if !errors.Is(err, failLater) {
		t.Fatalf("transaction: %v", err)
	}
	if got := status(); got != "pending" || totals() != [3]int{} {
		t.Fatalf("after rollback: order %s, intents/jobs/audit %v; want pending and nothing", got, totals())
	}

	n := newNotice(t, "test")
	if err := h.ownerTx(func(tx pgx.Tx) error { return confirm(tx, n) }); err != nil {
		t.Fatal(err)
	}
	if got := status(); got != "confirmed" || totals() != [3]int{1, 1, 1} {
		t.Fatalf("after commit: order %s, intents/jobs/audit %v; want confirmed and one of each", got, totals())
	}
	row := h.intent(n.IntentID)
	if row.State != "queued" || row.FirstDispatch != nil || !bytes.Equal(row.Payload, n.Payload) {
		t.Fatalf("intent = %+v, want queued with the exact payload", row)
	}
	if h.count("SELECT count(*) FROM business.job WHERE id = $1::uuid AND operation_id = $2::uuid AND state = 'queued'", row.JobID, n.IntentID) != 1 {
		t.Fatal("the intent's job is not a queued job for this intent")
	}
	var detail string
	if err := h.admin.QueryRow(h.ctx, `SELECT detail::text FROM business.audit_event WHERE action = 'notification.queued'
		AND subject_id = $1::uuid AND transition_id = $2::uuid AND actor_kind = 'staff'`, n.IntentID, n.TransitionID).Scan(&detail); err != nil {
		t.Fatalf("audit event: %v", err)
	}
	if strings.Contains(detail, recipient) || strings.Contains(detail, subject) {
		t.Fatalf("audit detail carries personal data: %s", detail)
	}
}

// AC1: a committed notice survives a worker stopping mid-call and is sent
// once, every request carrying the intent's stable key and stored body.
func TestCommittedNoticeIsSentOnceAcrossAWorkerRestart(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "test")
	h.enqueue(n)
	h.provider.queue("hang")

	w1 := h.worker(1, jobs.Options{ShutdownTimeout: 200 * time.Millisecond})
	ctx, cancel := context.WithCancel(h.ctx)
	done := make(chan error, 1)
	go func() { done <- w1.Run(ctx) }()
	select {
	case <-h.provider.arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the first worker never called the provider")
	}
	cancel() // the provider has sent it, but its response never reaches the worker
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if row := h.intent(n.IntentID); row.State != "unknown" || len(row.Attempts) != 1 || row.Attempts[0] != "unknown" {
		t.Fatalf("after the stop: intent %+v, want unknown with one unknown attempt", row)
	}
	if got := h.recoveryState(n.IdempotencyKey); got != "prepared" {
		t.Fatalf("recovery operation %s, want prepared", got)
	}

	h.runOnce(2) // a new worker process
	row := h.intent(n.IntentID)
	if row.State != "accepted" || row.ProviderMessageID == nil || *row.ProviderMessageID != "re-msg-1" {
		t.Fatalf("after restart: intent %+v, want accepted as the original message", row)
	}
	if strings.Join(row.Attempts, ",") != "unknown,accepted" {
		t.Fatalf("attempts %v, want unknown then accepted", row.Attempts)
	}
	if sends, reqs := h.provider.counts(); sends != 1 || reqs != 2 {
		t.Fatalf("provider sends %d, requests %d; want one send from two requests", sends, reqs)
	}
	sameKeyAndBody(t, h.provider.allRequests(), n.IdempotencyKey, storedRequest(t, n, row.Payload))
	if j := h.job(row.JobID); j.State != "completed" {
		t.Fatalf("job %+v, want completed", j)
	}
	if got := h.recoveryState(n.IdempotencyKey); got != "secured" {
		t.Fatalf("recovery operation %s, want secured", got)
	}
}

// A worker that secured the dispatch but stopped before completing the job:
// the next lease holder finds the recorded outcome and sends nothing.
func TestSecuredDispatchIsNotRepeatedAfterALostLease(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "test")
	h.enqueue(n)
	var job jobs.Job
	if err := h.pool.QueryRow(h.ctx, "SELECT job_id::text, job_operation_id::text, job_generation FROM business.job_claim($1::uuid, interval '60 seconds')",
		workerUUID(1)).Scan(&job.ID, &job.OperationID, &job.Generation); err != nil {
		t.Fatal(err)
	}
	job.Worker = workerUUID(1)
	if err := h.dispatcher().Handle(h.ctx, job); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// The process dies before job_complete; its lease runs out.
	if _, err := h.admin.Exec(h.ctx, "UPDATE business.job SET lease_until = now() - interval '1 second' WHERE id = $1::uuid", job.ID); err != nil {
		t.Fatal(err)
	}
	h.runOnce(2)
	if j := h.job(job.ID); j.State != "completed" || j.Generation != 2 {
		t.Fatalf("job %+v, want completed by the second lease", j)
	}
	if sends, reqs := h.provider.counts(); sends != 1 || reqs != 1 {
		t.Fatalf("provider sends %d, requests %d; want exactly one", sends, reqs)
	}
}

// AC2: a lost response leaves the intent unknown; the retry replays the
// original key and body and records the original provider message.
func TestLostResponseIsReconciledWithTheOriginalKey(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "test")
	h.enqueue(n)
	h.provider.queue("lose")
	h.runOnce(1)
	row := h.intent(n.IntentID)
	if row.State != "unknown" || strings.Join(row.Attempts, ",") != "unknown" {
		t.Fatalf("after the lost response: intent %+v, want unknown", row)
	}
	if j := h.job(row.JobID); j.State != "retry" || j.Failures != 1 || j.Next < 50*time.Second || j.Next > 70*time.Second {
		t.Fatalf("job %+v, want the first retry about 1m ahead", j)
	}

	h.due(row.JobID)
	h.runOnce(1)
	row = h.intent(n.IntentID)
	if row.State != "accepted" || *row.ProviderMessageID != "re-msg-1" || strings.Join(row.Attempts, ",") != "unknown,accepted" {
		t.Fatalf("after the replay: intent %+v, want accepted as re-msg-1", row)
	}
	if sends, reqs := h.provider.counts(); sends != 1 || reqs != 2 {
		t.Fatalf("provider sends %d, requests %d; want one send", sends, reqs)
	}
	sameKeyAndBody(t, h.provider.allRequests(), n.IdempotencyKey, storedRequest(t, n, row.Payload))
}

// AC2: past the provider idempotency window an unknown outcome goes to
// manager reconciliation; no further provider request is made, even after a
// manager requeues the job.
func TestUnknownPastTheProviderWindowIsNeverResentBlindly(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "test")
	h.enqueue(n)
	h.provider.queue("lose")
	h.runOnce(1)
	row := h.intent(n.IntentID)

	h.advance(23*time.Hour + time.Minute)
	h.due(row.JobID)
	h.runOnce(1)
	j := h.job(row.JobID)
	if j.State != "attention" || j.Reason == nil || !strings.Contains(*j.Reason, "reconciliation") {
		t.Fatalf("job %+v, want attention for reconciliation", j)
	}
	if got := h.intent(n.IntentID).State; got != "unknown" {
		t.Fatalf("intent %s, want unknown", got)
	}
	if got := h.recoveryState(n.IdempotencyKey); got != "reconcile" {
		t.Fatalf("recovery operation %s, want reconcile", got)
	}
	h.owned("SELECT business.job_requeue($1::uuid)", row.JobID)
	h.runOnce(1)
	if j := h.job(row.JobID); j.State != "attention" {
		t.Fatalf("after requeue: job %+v, want attention again", j)
	}
	if _, reqs := h.provider.counts(); reqs != 1 {
		t.Fatalf("provider requests %d, want only the original one", reqs)
	}
}

// A definitive provider rejection fails the intent without retrying and
// tells a manager; evidence keeps the status but not the provider's message
// (which echoes recipients).
func TestDefinitiveRejectionFailsWithoutRetry(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "test")
	h.enqueue(n)
	h.provider.queue("reject")
	h.runOnce(1)
	row := h.intent(n.IntentID)
	if row.State != "failed" || strings.Join(row.Attempts, ",") != "rejected" {
		t.Fatalf("intent %+v, want failed after one rejected attempt", row)
	}
	if j := h.job(row.JobID); j.State != "attention" || j.Reason == nil || !strings.Contains(*j.Reason, "rejected") {
		t.Fatalf("job %+v, want attention for the rejection", j)
	}
	h.owned("SELECT business.job_requeue($1::uuid)", row.JobID)
	h.runOnce(1)
	if _, reqs := h.provider.counts(); reqs != 1 {
		t.Fatalf("provider requests %d after a requeue, want still 1 (the outcome is recorded)", reqs)
	}
	var status int
	var code string
	if err := h.admin.QueryRow(h.ctx, "SELECT http_status, provider_code FROM business.notification_attempt WHERE intent_id = $1::uuid",
		n.IntentID).Scan(&status, &code); err != nil || status != 422 || code != "validation_error" {
		t.Fatalf("attempt evidence %d %q (%v), want 422 validation_error", status, code, err)
	}
	if got := h.recoveryState(n.IdempotencyKey); got != "secured" {
		t.Fatalf("recovery operation %s, want secured", got)
	}
}

// A refused credential needs a manager; after the fix and a requeue the same
// key and body are sent.
func TestRefusedCredentialNeedsAttentionThenResumesWithTheSameKey(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "test")
	h.enqueue(n)
	h.provider.queue("refuse")
	h.runOnce(1)
	row := h.intent(n.IntentID)
	j := h.job(row.JobID)
	if j.State != "attention" || j.Reason == nil || !strings.Contains(*j.Reason, "API key") {
		t.Fatalf("job %+v, want attention about the API key", j)
	}
	if row.State != "sending" || strings.Join(row.Attempts, ",") != "refused" {
		t.Fatalf("intent %+v, want sending with one refused attempt", row)
	}
	h.owned("SELECT business.job_requeue($1::uuid)", row.JobID)
	h.runOnce(1)
	if got := h.intent(n.IntentID); got.State != "accepted" {
		t.Fatalf("after requeue: intent %+v, want accepted", got)
	}
	sameKeyAndBody(t, h.provider.allRequests(), n.IdempotencyKey, storedRequest(t, n, row.Payload))
}

// Deferred and failing calls retry after 1m, 5m, 15m and 1h, then need a
// manager; a deferral alone never makes the outcome unknown.
func TestRetriesBackOffThenNeedAttention(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "test")
	h.enqueue(n)
	h.provider.queue("ratelimit", "ratelimit", "ratelimit", "ratelimit", "ratelimit")
	jobID := ""
	for i, want := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour} {
		h.runOnce(1)
		jobID = h.intent(n.IntentID).JobID
		j := h.job(jobID)
		if j.State != "retry" || j.Failures != i+1 || j.Next < want-10*time.Second || j.Next > want+10*time.Second {
			t.Fatalf("after failure %d: job %+v, want retry about %s ahead", i+1, j, want)
		}
		h.due(jobID)
	}
	h.runOnce(1)
	if j := h.job(jobID); j.State != "attention" || j.Failures != 5 || j.Reason == nil {
		t.Fatalf("after the fifth failure: job %+v, want attention", j)
	}
	row := h.intent(n.IntentID)
	if row.State != "sending" || len(row.Attempts) != 5 {
		t.Fatalf("intent %+v, want sending with five deferred attempts", row)
	}
	if sends, _ := h.provider.counts(); sends != 0 {
		t.Fatalf("provider sends %d, want 0", sends)
	}
}

// A rejection after an unknown attempt proves only that the replay was not
// sent: the intent stays unknown.
func TestRejectionAfterAnUnknownAttemptStaysUnknown(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "test")
	h.enqueue(n)
	h.provider.queue("fail500", "reject")
	h.runOnce(1)
	h.due(h.intent(n.IntentID).JobID)
	h.runOnce(1)
	row := h.intent(n.IntentID)
	if row.State != "unknown" || strings.Join(row.Attempts, ",") != "unknown,rejected" {
		t.Fatalf("intent %+v, want unknown after unknown then rejected", row)
	}
	if j := h.job(row.JobID); j.State != "attention" {
		t.Fatalf("job %+v, want attention", j)
	}
}

// A revocation committed while the dispatch is being prepared stops it at
// the admission check right before the provider call.
func TestRevocationDuringTheDispatchStopsTheCall(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "test")
	h.enqueue(n)
	var once sync.Once
	h.journal.Fail = func(op, name string) error {
		if op == "put" && strings.HasSuffix(name, ".prepared") {
			once.Do(func() {
				h.owned("SELECT business.notification_suspend($1::uuid, 'recipient deleted their account', 'system', NULL)", n.IntentID)
			})
		}
		return nil
	}
	h.runOnce(1)
	row := h.intent(n.IntentID)
	if _, reqs := h.provider.counts(); reqs != 0 || len(row.Attempts) != 0 {
		t.Fatalf("provider requests %d, attempts %v; want no call", reqs, row.Attempts)
	}
	if j := h.job(row.JobID); j.State != "attention" || j.Reason == nil || !strings.Contains(*j.Reason, "revoked") {
		t.Fatalf("job %+v, want attention for the revocation", j)
	}
	var detail string
	if err := h.admin.QueryRow(h.ctx, "SELECT detail::text FROM business.audit_event WHERE action = 'notification.suspended' AND subject_id = $1::uuid",
		n.IntentID).Scan(&detail); err != nil || !strings.Contains(detail, `"intent_state"`) {
		t.Fatalf("suspension audit detail %q (%v), want the intent state recorded", detail, err)
	}
}

// The admission check uses the database clock: a dispatch whose window has
// ended there is not sent even when the worker's clock is behind.
func TestWindowEndOnTheDatabaseClockStopsTheCall(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "test")
	h.enqueue(n)
	// The window was anchored long ago (as after an earlier start whose
	// reservation never happened); the worker's clock runs two hours slow.
	if _, err := h.admin.Exec(h.ctx, `UPDATE business.notification_intent SET first_dispatch_at = now() - interval '23 hours 1 minute',
		replay_until = now() - interval '1 minute' WHERE id = $1::uuid`, n.IntentID); err != nil {
		t.Fatal(err)
	}
	h.advance(-2 * time.Hour)
	h.runOnce(1)
	row := h.intent(n.IntentID)
	if _, reqs := h.provider.counts(); reqs != 0 || len(row.Attempts) != 0 {
		t.Fatalf("provider requests %d, attempts %v; want no call", reqs, row.Attempts)
	}
	if j := h.job(row.JobID); j.State != "attention" || j.Reason == nil || !strings.Contains(*j.Reason, "reconciliation") {
		t.Fatalf("job %+v, want attention for reconciliation", j)
	}
	if got := h.recoveryState(n.IdempotencyKey); got != "reconcile" || row.State != "unknown" {
		t.Fatalf("recovery %s, intent %s; want reconcile and unknown", got, row.State)
	}
}

// After a primary loss, the journal lists the pending dispatch with the
// exact stored payload (HTML, '<', '&' and all), so a replay sends the
// identical request.
func TestJournalHoldsTheExactNoticePayload(t *testing.T) {
	h := newHarness(t)
	msg := message()
	msg.HTML = `<p>Dear Ms Example, Tom &amp; Jerry's cake &lt;3 is confirmed — £42.50</p>`
	n, err := notifications.NewNotice("test", nextUUID(), nextUUID(), "order-confirmed.v1", msg)
	if err != nil {
		t.Fatal(err)
	}
	h.enqueue(n)
	h.provider.queue("lose")
	h.runOnce(1)
	pending, err := h.coord.Pending(h.ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("Pending = %+v, %v", pending, err)
	}
	if !bytes.Equal(pending[0].Payload, n.Payload) || pending[0].ID != n.IdempotencyKey || pending[0].AggregateID != n.IntentID {
		t.Fatalf("journal copy %s, want the exact stored payload %s", pending[0].Payload, n.Payload)
	}
	d, err := notifications.ParseDispatch(pending[0].Payload, "test", n.IntentID, n.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	sameKeyAndBody(t, h.provider.allRequests(), n.IdempotencyKey, d.Request)
}

// A revoked recipient is never sent to, including the replay of an unknown
// dispatch; the notice is not retargeted.
func TestSuspendedRecipientIsNeverSent(t *testing.T) {
	h := newHarness(t)
	queued, unknown := newNotice(t, "test"), newNotice(t, "test")
	h.enqueue(queued)
	h.owned("SELECT business.notification_suspend($1::uuid, 'recipient deleted their account', 'system', NULL)", queued.IntentID)
	h.runOnce(1)
	row := h.intent(queued.IntentID)
	if j := h.job(row.JobID); j.State != "attention" || j.Reason == nil || !strings.Contains(*j.Reason, "revoked") {
		t.Fatalf("job %+v, want attention for the revoked recipient", j)
	}
	if row.State != "queued" || row.FirstDispatch != nil {
		t.Fatalf("intent %+v, want queued without a dispatch window", row)
	}
	if h.count("SELECT count(*) FROM business.audit_event WHERE action = 'notification.suspended' AND subject_id = $1::uuid", queued.IntentID) != 1 {
		t.Fatal("suspension was not audited")
	}

	h.enqueue(unknown)
	h.provider.queue("lose")
	h.runOnce(1)
	row = h.intent(unknown.IntentID)
	h.owned("SELECT business.notification_suspend($1::uuid, 'contact revoked', 'system', NULL)", unknown.IntentID)
	h.due(row.JobID)
	h.runOnce(1)
	if _, reqs := h.provider.counts(); reqs != 1 {
		t.Fatalf("provider requests %d, want only the first (lost) one", reqs)
	}
	if got := h.intent(unknown.IntentID).State; got != "unknown" {
		t.Fatalf("intent %s, want unknown (no claim either way)", got)
	}

	// A settled notice keeps its evidence; suspending it changes nothing.
	sent := newNotice(t, "test")
	h.enqueue(sent)
	h.runOnce(1)
	var changed bool
	if err := h.ownerTx(func(tx pgx.Tx) error {
		return tx.QueryRow(h.ctx, "SELECT business.notification_suspend($1::uuid, 'late revocation', 'system', NULL)", sent.IntentID).Scan(&changed)
	}); err != nil || changed {
		t.Fatalf("suspending a sent notice: changed %v, %v; want no change", changed, err)
	}
	if got := h.intent(sent.IntentID).State; got != "accepted" {
		t.Fatalf("sent notice %s, want accepted", got)
	}
}

// A restore freeze defers dispatch without counting a failure or starting
// the provider window; after unfreezing it is sent.
func TestRestoreFreezeDefersWithoutStartingTheWindow(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "test")
	h.enqueue(n)
	h.owned("SELECT business.recovery_set_freeze(true, 'restore in progress')")
	h.runOnce(1)
	row := h.intent(n.IntentID)
	j := h.job(row.JobID)
	if j.State != "retry" || j.Failures != 0 || j.Next < 4*time.Minute {
		t.Fatalf("frozen: job %+v, want a deferral about 5m ahead without a failure", j)
	}
	if row.State != "queued" || row.FirstDispatch != nil {
		t.Fatalf("frozen: intent %+v, want queued without a dispatch window", row)
	}
	h.owned("SELECT business.recovery_set_freeze(false, NULL)")
	h.due(row.JobID)
	h.runOnce(1)
	if got := h.intent(n.IntentID).State; got != "accepted" {
		t.Fatalf("after unfreezing: intent %s, want accepted", got)
	}
}

// The prepared envelope must be in the journal before the provider call: a
// journal outage sends nothing and is retried.
func TestJournalOutageSendsNothing(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "test")
	h.enqueue(n)
	h.journal.Fail = func(op, _ string) error {
		if op == "put" {
			return errors.New("b2 unavailable")
		}
		return nil
	}
	h.runOnce(1)
	row := h.intent(n.IntentID)
	if _, reqs := h.provider.counts(); reqs != 0 || row.State != "queued" {
		t.Fatalf("journal outage: requests %d, intent %s; want no request and queued", reqs, row.State)
	}
	if j := h.job(row.JobID); j.State != "retry" || j.Failures != 1 {
		t.Fatalf("job %+v, want a counted retry", j)
	}
	h.journal.Fail = nil
	h.due(row.JobID)
	h.runOnce(1)
	if got := h.intent(n.IntentID).State; got != "accepted" {
		t.Fatalf("after the outage: intent %s, want accepted", got)
	}
}

// A payload stored for another environment (for example data restored from
// production into staging) is refused before any provider call.
func TestPayloadOfAnotherEnvironmentIsRefused(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "production")
	h.enqueue(n)
	h.runOnce(1)
	row := h.intent(n.IntentID)
	if j := h.job(row.JobID); j.State != "attention" || j.Reason == nil || !strings.Contains(*j.Reason, "payload") {
		t.Fatalf("job %+v, want attention for the payload", j)
	}
	if _, reqs := h.provider.counts(); reqs != 0 {
		t.Fatalf("provider requests %d, want 0", reqs)
	}
}

// Concurrent workers send each notice exactly once.
func TestConcurrentWorkersSendEachNoticeOnce(t *testing.T) {
	h := newHarness(t)
	const notices, workers = 12, 4
	for range notices {
		h.enqueue(newNotice(t, "test"))
	}
	ctx, cancel := context.WithCancel(h.ctx)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		w := h.worker(10+i, jobs.Options{})
		go func() { defer wg.Done(); _ = w.Run(ctx) }()
	}
	deadline := time.Now().Add(20 * time.Second)
	for h.count("SELECT count(*) FROM business.notification_intent WHERE state = 'accepted'") < notices && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	wg.Wait()
	sends, reqs := h.provider.counts()
	if sends != notices || reqs != notices {
		t.Fatalf("provider sends %d, requests %d; want %d each", sends, reqs, notices)
	}
	if got := h.count("SELECT count(*) FROM business.job WHERE state = 'completed'"); got != notices {
		t.Fatalf("completed jobs %d, want %d", got, notices)
	}
}

// Logs carry IDs and provider status, never the recipient, content, the
// provider's message or the API key.
func TestWorkerLogsCarryNoPersonalData(t *testing.T) {
	h := newHarness(t)
	rejected, lost := newNotice(t, "test"), newNotice(t, "test")
	h.enqueue(rejected)
	h.enqueue(lost)
	h.provider.queue("reject", "lose")
	h.runOnce(1)
	h.runOnce(1)
	logs := h.logs.String()
	if !strings.Contains(logs, rejected.IntentID) || !strings.Contains(logs, `"http_status":422`) {
		t.Fatalf("logs lack the operational evidence:\n%s", logs)
	}
	for _, secret := range []string{recipient, subject, bodyText, echoedMessage, "Ms Example", apiKey} {
		if strings.Contains(logs, secret) {
			t.Errorf("logs contain %q", secret)
		}
	}
	if len(lines(logs)) == 0 {
		t.Fatal("no log lines")
	}
}

// Every gateway call is fenced by the live lease and bound to the leased
// intent's own dispatch.
func TestGatewayIsFencedByTheLiveLease(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "test")
	h.enqueue(n)
	claim := func(worker string) int64 {
		var gen int64
		if err := h.pool.QueryRow(h.ctx, "SELECT job_generation FROM business.job_claim($1::uuid, interval '60 seconds')", worker).Scan(&gen); err != nil {
			t.Fatal(err)
		}
		return gen
	}
	jobID := h.intent(n.IntentID).JobID
	a, b := workerUUID(1), workerUUID(2)
	expire := func() {
		if _, err := h.admin.Exec(h.ctx, "UPDATE business.job SET lease_until = now() - interval '1 second' WHERE id = $1::uuid", jobID); err != nil {
			t.Fatal(err)
		}
	}
	genA := claim(a)
	var (
		status string
		replay time.Time
	)
	if err := h.pool.QueryRow(h.ctx, "SELECT status, replay_until FROM business.notification_dispatch_begin($1::uuid, $2::uuid, $3)", jobID, a, genA).Scan(&status, &replay); err != nil || status != "ready" {
		t.Fatalf("begin with the live lease: %q, %v", status, err)
	}
	row := h.intent(n.IntentID)
	key, hash := n.IdempotencyKey, recovery.HashHex(row.Payload)
	envelopeKey := "recovery/test/op/" + n.IntentID + "/0000000000-" + key
	// A's lease expires (not yet reclaimed): the gateway refuses it already.
	// Every state-changing call carries otherwise valid arguments.
	expire()
	stale := []string{
		"SELECT status FROM business.notification_dispatch_begin($1::uuid, $2::uuid, $3)",
		"SELECT business.notification_recovery_frozen($1::uuid, $2::uuid, $3)::text",
		fmt.Sprintf("SELECT result FROM business.notification_recovery_reserve($1::uuid, $2::uuid, $3, '%s'::uuid, 'dispatch', '%s'::uuid, 0, '%s', '%s', '%s'::timestamptz)",
			key, n.IntentID, hash, envelopeKey, replay.UTC().Format(time.RFC3339Nano)),
		"SELECT business.notification_recovery_prepared($1::uuid, $2::uuid, $3, '" + key + "'::uuid)",
		"SELECT business.notification_dispatch_admit($1::uuid, $2::uuid, $3, '" + key + "'::uuid)",
		"SELECT op_state FROM business.notification_recovery_committed($1::uuid, $2::uuid, $3, '" + key +
			`'::uuid, '{"status":"accepted","evidence":{"provider_message_id":"re-1"}}'::json, repeat('a', 64))`,
		"SELECT business.notification_recovery_secured($1::uuid, $2::uuid, $3, '" + key + "'::uuid)",
		"SELECT business.notification_recovery_reconcile($1::uuid, $2::uuid, $3, '" + key + "'::uuid, 'r')",
	}
	for _, sql := range stale {
		var out string
		err := h.pool.QueryRow(h.ctx, sql, jobID, a, genA).Scan(&out)
		if got := dbtest.SQLState(err); got != "GK001" {
			t.Errorf("expired lease: %s: SQLSTATE %q (%v), want GK001", sql, got, err)
		}
	}
	// A reclaims its own job: the old generation alone is fenced.
	genA2 := claim(a)
	for _, sql := range stale {
		var out string
		if got := dbtest.SQLState(h.pool.QueryRow(h.ctx, sql, jobID, a, genA).Scan(&out)); got != "GK001" {
			t.Errorf("same worker, older generation: %s: SQLSTATE %q, want GK001", sql, got)
		}
	}
	// Holding no lease, B cannot use A's current generation either.
	for _, sql := range stale {
		var out string
		if got := dbtest.SQLState(h.pool.QueryRow(h.ctx, sql, jobID, b, genA2).Scan(&out)); got != "GK001" {
			t.Errorf("another worker with the current generation: %s: SQLSTATE %q, want GK001", sql, got)
		}
	}
	expire()
	genB := claim(b)
	for _, sql := range stale {
		var out string
		if got := dbtest.SQLState(h.pool.QueryRow(h.ctx, sql, jobID, a, genA2).Scan(&out)); got != "GK001" {
			t.Errorf("reclaimed by another worker: %s: SQLSTATE %q, want GK001", sql, got)
		}
	}

	// The lease holder can reserve only the intent's own dispatch: each case
	// changes one field of the valid reservation.
	valid := []any{key, "dispatch", n.IntentID, 0, hash, envelopeKey, replay}
	with := func(i int, v any) []any { out := append([]any{}, valid...); out[i] = v; return out }
	reserve := func(args []any) (string, error) {
		var result string
		err := h.pool.QueryRow(h.ctx, `SELECT result FROM business.notification_recovery_reserve($1::uuid, $2::uuid, $3, $4::uuid, $5, $6::uuid, $7, $8, $9, $10)`,
			append([]any{jobID, b, genB}, args...)...).Scan(&result)
		return result, err
	}
	for name, args := range map[string][]any{
		"another operation":     with(0, nextUUID()),
		"another aggregate":     with(2, nextUUID()),
		"a later prior version": with(3, 1),
		"another payload":       with(4, strings.Repeat("a", 64)),
		"another envelope key":  with(5, "recovery/test/op/x"),
		"another deadline":      with(6, replay.Add(time.Hour)),
	} {
		if _, err := reserve(args); dbtest.SQLState(err) != "22023" {
			t.Errorf("%s: %v, want 22023", name, err)
		}
	}
	if result, err := reserve(valid); err != nil || result != "reserved" {
		t.Fatalf("the intent's own dispatch: %q, %v; want reserved", result, err)
	}

	// Evidence is kept from a worker that lost its lease (its call happened),
	// but only for an issued generation of this job and its own operation.
	var prepared string
	if err := h.pool.QueryRow(h.ctx, "SELECT business.notification_recovery_prepared($1::uuid, $2::uuid, $3, $4::uuid)", jobID, b, genB, key).Scan(&prepared); err != nil {
		t.Fatal(err)
	}
	record := func(worker string, gen int64, op string) error {
		var no int
		return h.pool.QueryRow(h.ctx, "SELECT business.notification_attempt_record($1::uuid, $2::uuid, $3, $4::uuid, 'unknown', NULL, NULL, NULL)",
			jobID, worker, gen, op).Scan(&no)
	}
	if err := record(a, genA, key); err != nil {
		t.Fatalf("attempt from the lost lease: %v, want it recorded", err)
	}
	if got := h.intent(n.IntentID); strings.Join(got.Attempts, ",") != "unknown" || got.State != "unknown" {
		t.Fatalf("intent %+v, want the stale unknown attempt recorded", got)
	}
	for name, err := range map[string]error{
		"a generation never issued": record(a, genB+1, key),
		"another operation":         record(b, genB, nextUUID()),
	} {
		if got := dbtest.SQLState(err); got != "22023" {
			t.Errorf("attempt with %s: SQLSTATE %q (%v), want 22023", name, got, err)
		}
	}
}

// A job that is not a notification job is left to other handlers.
func TestOtherJobsAreNotHandled(t *testing.T) {
	h := newHarness(t)
	h.owned("SELECT business.job_enqueue(gen_random_uuid(), NULL)")
	h.runOnce(1)
	var state string
	if err := h.admin.QueryRow(h.ctx, "SELECT state FROM business.job").Scan(&state); err != nil || state != "retry" {
		t.Fatalf("other job: %q (%v), want retry", state, err)
	}
	if _, reqs := h.provider.counts(); reqs != 0 {
		t.Fatalf("provider requests %d, want 0", reqs)
	}
}

func TestEnqueueRefusesDuplicatesAndForeignPayloads(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "test")
	h.enqueue(n)
	again, err := notifications.NewNotice("test", n.TransitionID, n.RecipientID, n.TemplateRevision, message())
	if err != nil {
		t.Fatal(err)
	}
	enqueue := func(n notifications.Notice) error {
		return h.ownerTx(func(tx pgx.Tx) error { return notifications.Enqueue(h.ctx, tx, n, "system", "") })
	}
	if got := dbtest.SQLState(enqueue(again)); got != "23505" {
		t.Fatalf("second notice for the same transition, recipient and template: SQLSTATE %q, want 23505", got)
	}
	otherKey := newNotice(t, "test")
	otherKey.IdempotencyKey = nextUUID()
	otherTag := newNotice(t, "test")
	otherTag.IntentID = nextUUID()
	for name, bad := range map[string]notifications.Notice{"another key": otherKey, "another intent tag": otherTag} {
		if got := dbtest.SQLState(enqueue(bad)); got != "22023" {
			t.Errorf("%s: SQLSTATE %q, want 22023", name, got)
		}
	}
	if got := h.count("SELECT count(*) FROM business.notification_intent"); got != 1 {
		t.Fatalf("intents %d, want 1", got)
	}
}

// Runtime roles reach notifications only through the worker gateway; no
// runtime role reads intents, attempts or the audit trail, and only the
// owner enqueues, suspends or requeues.
func TestRuntimeRolesUseOnlyTheNotificationGateway(t *testing.T) {
	h := newHarness(t)
	worker := []string{
		"business.job_fail(uuid, uuid, bigint, text)",
		"business.job_attention(uuid, uuid, bigint, text)",
		"business.notification_dispatch_begin(uuid, uuid, bigint)",
		"business.notification_recovery_reserve(uuid, uuid, bigint, uuid, text, uuid, integer, text, text, timestamp with time zone)",
		"business.notification_recovery_prepared(uuid, uuid, bigint, uuid)",
		"business.notification_dispatch_admit(uuid, uuid, bigint, uuid)",
		"business.notification_attempt_record(uuid, uuid, bigint, uuid, text, integer, text, text)",
		"business.notification_recovery_committed(uuid, uuid, bigint, uuid, json, text)",
		"business.notification_recovery_secured(uuid, uuid, bigint, uuid)",
		"business.notification_recovery_reconcile(uuid, uuid, bigint, uuid, text)",
		"business.notification_recovery_frozen(uuid, uuid, bigint)",
	}
	ownerOnly := []string{
		"business.audit_record(text, uuid, text, text, uuid, uuid, json)",
		"business.notification_enqueue(uuid, uuid, uuid, text, uuid, json, text, uuid)",
		"business.notification_suspend(uuid, text, text, uuid)",
		"business.job_requeue(uuid)",
		"business.notification_leased_intent(uuid, uuid, bigint)",
		"business.notification_leased_operation(uuid, uuid, bigint, uuid)",
	}
	tables := []string{"business.audit_event", "business.notification_intent", "business.notification_attempt"}
	for _, group := range []string{"gk_api", "gk_worker", "gk_backup"} {
		for _, fn := range append(append([]string{}, worker...), ownerOnly...) {
			var can bool
			if err := h.admin.QueryRow(h.ctx, "SELECT has_function_privilege($1, $2, 'EXECUTE')", group, fn).Scan(&can); err != nil {
				t.Fatal(err)
			}
			want := group == "gk_worker" && contains(worker, fn)
			if can != want {
				t.Errorf("%s EXECUTE %s = %v, want %v", group, fn, can, want)
			}
		}
		for _, table := range tables {
			for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
				var can bool
				if err := h.admin.QueryRow(h.ctx, "SELECT has_table_privilege($1, $2, $3)", group, table, priv).Scan(&can); err != nil {
					t.Fatal(err)
				}
				if want := group == "gk_backup" && priv == "SELECT"; can != want {
					t.Errorf("%s %s on %s = %v, want %v", group, priv, table, can, want)
				}
			}
		}
	}
	// Exactly these, plus the GK-020 lease functions, for the worker.
	rows, err := h.admin.Query(h.ctx, `SELECT replace(p.oid::regprocedure::text, ',', ', ') FROM pg_proc p
		JOIN pg_namespace ns ON ns.oid = p.pronamespace
		WHERE ns.nspname = 'business' AND has_function_privilege('gk_worker', p.oid, 'EXECUTE') ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string{"business.job_claim(uuid, interval)", "business.job_complete(uuid, uuid, bigint)",
		"business.job_heartbeat(uuid, uuid, bigint, interval)", "business.job_release(uuid, uuid, bigint, interval)"}, worker...)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("gk_worker can execute %v, want exactly %v", got, want)
	}
	// And in practice: the worker login cannot enqueue or read intents.
	conn := h.db.LoginConn(t, "gk_worker")
	for _, sql := range []string{"SELECT count(*) FROM business.notification_intent",
		"SELECT business.job_requeue(gen_random_uuid())"} {
		if _, err := conn.Exec(h.ctx, sql); dbtest.SQLState(err) != "42501" {
			t.Errorf("gk_worker: %s: %v, want 42501", sql, err)
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
