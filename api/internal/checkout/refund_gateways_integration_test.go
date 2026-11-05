//go:build integration

package checkout_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
)

// GK-025 A3c2: the lease-fenced refund gateway of migration 0016, driven
// call by call as the refund worker will drive it.

type refundOp struct {
	ID, Order, Collection, PI string
	Amount                    int64
}

// reservedRefund is the late capture's reserved refund operation.
func (h *payHarness) reservedRefund(t *testing.T, coll string) refundOp {
	t.Helper()
	var r refundOp
	if err := h.admin.QueryRow(h.ctx, `SELECT ro.id::text, ro.order_id::text, ro.collection_id::text,
		coalesce(ro.payment_intent_id, pc.payment_intent_id), ro.amount_minor
		FROM business.refund_operation ro JOIN business.payment_collection pc ON pc.id = ro.collection_id
		WHERE ro.collection_id = $1::uuid AND ro.state = 'reserved'`, coll).Scan(&r.ID, &r.Order, &r.Collection, &r.PI, &r.Amount); err != nil {
		t.Fatal(err)
	}
	return r
}

func (h *payHarness) refundJob(t *testing.T, purpose, target string) string {
	t.Helper()
	col := "refund_operation_id"
	if purpose == "reconcile" {
		col = "collection_id"
	}
	var id string
	if err := h.admin.QueryRow(h.ctx, `SELECT j.id::text FROM business.refund_job rj JOIN business.job j ON j.id = rj.job_id
		WHERE rj.purpose = $1 AND rj.`+col+` = $2::uuid AND j.state IN ('queued', 'retry')
		ORDER BY j.created_at DESC LIMIT 1`, purpose, target).Scan(&id); err != nil {
		t.Fatalf("%s job of %s: %v", purpose, target, err)
	}
	return id
}

// refundPayload is the exact dispatch payload the refund worker builds.
func refundPayload(r refundOp, amount int64) string {
	v := url.Values{}
	v.Set("payment_intent", r.PI)
	v.Set("amount", strconv.FormatInt(amount, 10))
	v.Set("reason", "requested_by_customer")
	v.Set("metadata[order_id]", r.Order)
	v.Set("metadata[collection_id]", r.Collection)
	v.Set("metadata[refund_operation_id]", r.ID)
	v.Set("metadata[environment]", "test")
	b, _ := json.Marshal(map[string]any{"v": 1, "provider": "stripe", "kind": "refund", "environment": "test",
		"account": stripeAccount, "livemode": false, "idempotency_key": r.ID, "refund_operation_id": r.ID,
		"order_id": r.Order, "collection_id": r.Collection, "payment_intent_id": r.PI,
		"amount_minor": amount, "request": v.Encode()})
	return string(b)
}

func sha(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }

type refundBegin struct {
	Status, Purpose, State, PI, Payload string
	Amount                              int64
	DBNow                               time.Time
}

func (h *payHarness) refundBegin(t *testing.T, job jobs.Job) (refundBegin, error) {
	t.Helper()
	var (
		b                            refundBegin
		purpose, state, pi, payload  *string
		amount                       *int64
		now                          *time.Time
		coll, order, op, refund, env *string
		account                      *string
		until, started               *time.Time
		live                         *bool
	)
	err := h.worker.QueryRow(h.ctx, `SELECT status, purpose, collection_id::text, order_id::text, operation_id::text,
		operation_state, amount_minor, payment_intent_id, provider_refund_id, payload, replay_until, dispatch_started_at,
		db_now, environment, account, livemode FROM business.refund_job_begin($1::uuid, $2::uuid, $3)`,
		job.ID, job.Worker, job.Generation).Scan(&b.Status, &purpose, &coll, &order, &op, &state, &amount, &pi, &refund,
		&payload, &until, &started, &now, &env, &account, &live)
	s := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	b.Purpose, b.State, b.PI, b.Payload = s(purpose), s(state), s(pi), s(payload)
	if amount != nil {
		b.Amount = *amount
	}
	if now != nil {
		b.DBNow = *now
	}
	return b, err
}

func (h *payHarness) refundFix(t *testing.T, job jobs.Job, op, payload string, at time.Time) (string, string, error) {
	t.Helper()
	var status string
	var stored *string
	var until *time.Time
	err := h.worker.QueryRow(h.ctx, `SELECT status, payload, replay_until FROM business.refund_dispatch_fix($1::uuid, $2::uuid, $3, $4::uuid, $5, $6)`,
		job.ID, job.Worker, job.Generation, op, payload, at).Scan(&status, &stored, &until)
	if stored != nil {
		return status, *stored, err
	}
	return status, "", err
}

func (h *payHarness) refundText(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var s string
	if err := h.worker.QueryRow(h.ctx, sql, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return s
}

// refundPrepared fixes the dispatch and walks it to K11 prepared; it
// returns the fixed payload.
func (h *payHarness) refundPrepared(t *testing.T, job jobs.Job, r refundOp) string {
	t.Helper()
	b, err := h.refundBegin(t, job)
	if err != nil || b.Status != "ready" {
		t.Fatalf("begin %+v %v", b, err)
	}
	status, payload, err := h.refundFix(t, job, r.ID, refundPayload(r, r.Amount), b.DBNow)
	if err != nil || status != "ready" {
		t.Fatalf("fix %s %v", status, err)
	}
	var until time.Time
	if err := h.admin.QueryRow(h.ctx, "SELECT replay_until FROM business.refund_operation WHERE id = $1::uuid", r.ID).Scan(&until); err != nil {
		t.Fatal(err)
	}
	var result string
	if err := h.worker.QueryRow(h.ctx, `SELECT result FROM business.refund_recovery_reserve($1::uuid, $2::uuid, $3, $4::uuid,
		'dispatch', $4::uuid, 0, $5, $6, $7)`, job.ID, job.Worker, job.Generation, r.ID, sha(payload),
		"recovery/test/op/"+r.ID+"/0000000000-"+r.ID, until).Scan(&result); err != nil || result != "reserved" {
		t.Fatalf("reserve %s %v", result, err)
	}
	if got := h.refundText(t, "SELECT business.refund_recovery_prepared($1::uuid, $2::uuid, $3, $4::uuid)",
		job.ID, job.Worker, job.Generation, r.ID); got != "prepared" {
		t.Fatalf("prepared %s", got)
	}
	return payload
}

func (h *payHarness) refundAdmit(t *testing.T, job jobs.Job, op string) string {
	t.Helper()
	return h.refundText(t, "SELECT business.refund_dispatch_admit($1::uuid, $2::uuid, $3, $4::uuid)", job.ID, job.Worker, job.Generation, op)
}

func (h *payHarness) refundAttempt(t *testing.T, job jobs.Job, op, result string, status int) {
	t.Helper()
	var no int
	var code *int
	if status != 0 {
		code = &status
	}
	if err := h.worker.QueryRow(h.ctx, "SELECT business.refund_attempt_record($1::uuid, $2::uuid, $3, $4::uuid, $5, $6, NULL)",
		job.ID, job.Worker, job.Generation, op, result, code).Scan(&no); err != nil {
		t.Fatal(err)
	}
}

func refundOutcome(status, refundID string, r refundOp, amount int64) string {
	b, _ := json.Marshal(map[string]any{"status": status, "evidence": map[string]any{"refund_id": refundID, "status": status,
		"amount_minor": amount, "currency": "gbp", "payment_intent_id": r.PI, "livemode": false}})
	return string(b)
}

func (h *payHarness) refundCommit(t *testing.T, job jobs.Job, op, outcome string) (string, bool) {
	t.Helper()
	var state string
	var changed bool
	if err := h.worker.QueryRow(h.ctx, `SELECT op_state, changed FROM business.refund_recovery_committed($1::uuid, $2::uuid, $3, $4::uuid, $5::json, $6)`,
		job.ID, job.Worker, job.Generation, op, outcome, sha(outcome)).Scan(&state, &changed); err != nil {
		t.Fatal(err)
	}
	return state, changed
}

type refundRow struct {
	State, RefundID, Mismatch string
	Started                   bool
}

func (h *payHarness) refundRow(t *testing.T, op string) refundRow {
	t.Helper()
	var r refundRow
	if err := h.admin.QueryRow(h.ctx, `SELECT state, coalesce(provider_refund_id, ''), coalesce(mismatch_reason, ''),
		dispatch_started_at IS NOT NULL FROM business.refund_operation WHERE id = $1::uuid`, op).Scan(&r.State, &r.RefundID,
		&r.Mismatch, &r.Started); err != nil {
		t.Fatal(err)
	}
	return r
}

func (h *payHarness) observe(t *testing.T, job jobs.Job, coll, refundID string, amount int64, status, pi string, metaOp *string) (string, bool, string) {
	t.Helper()
	var obs, result string
	var matched, reason *string
	var quarantined bool
	if err := h.worker.QueryRow(h.ctx, `SELECT observation_id::text, matched_operation_id::text, quarantined, quarantine_reason, result
		FROM business.refund_observation_record($1::uuid, $2::uuid, $3, $4::uuid, $5, $6, 'gbp', $7, $8, $9::uuid, now())`,
		job.ID, job.Worker, job.Generation, coll, refundID, amount, status, pi, metaOp).Scan(&obs, &matched, &quarantined, &reason, &result); err != nil {
		t.Fatal(err)
	}
	_ = obs
	return result, quarantined, deref(reason)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (h *payHarness) reconcileRefund(t *testing.T, job jobs.Job, op, verdict string, refundID *string, reason *string) (string, bool) {
	t.Helper()
	var state string
	var ended bool
	if err := h.worker.QueryRow(h.ctx, `SELECT operation_state, window_ended FROM business.refund_reconcile_record(
		$1::uuid, $2::uuid, $3, $4::uuid, $5, $6, $7, '{"source":"test"}'::json, $8)`,
		job.ID, job.Worker, job.Generation, op, verdict, refundID, nil, reason).Scan(&state, &ended); err != nil {
		t.Fatal(err)
	}
	return state, ended
}

// Late capture -> fixed request -> admitted call -> a lost answer (unknown)
// -> the same key again -> succeeded and secured: the order stays cancelled
// and its view says returned; nothing more is available.
func TestRefundDispatchGatewayReturnsTheLateCapture(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "gw")
	r := h.reservedRefund(t, coll)
	job := h.claimOnly(t, h.refundJob(t, "dispatch", r.ID))

	b, err := h.refundBegin(t, job)
	if err != nil || b.Status != "ready" || b.Purpose != "dispatch" || b.Payload != "" || b.State != "reserved" ||
		b.Amount != 10000 || b.PI != r.PI {
		t.Fatalf("begin %+v %v", b, err)
	}
	// A request that is not this operation's is refused.
	if _, _, err := h.refundFix(t, job, r.ID, refundPayload(r, 9999), b.DBNow); dbtest.SQLState(err) != "22023" {
		t.Fatalf("foreign payload: %v", err)
	}
	payload := h.refundPrepared(t, job, r)
	// Fixed once: another payload is ignored.
	if status, again, err := h.refundFix(t, job, r.ID, refundPayload(r, 1), time.Now()); err != nil || status != "ready" || again != payload {
		t.Fatalf("refix %s %v", status, err)
	}
	if got := h.refundAdmit(t, job, r.ID); got != "ok" {
		t.Fatalf("admit %s", got)
	}
	if row := h.refundRow(t, r.ID); row.State != "submitting" || !row.Started {
		t.Fatalf("admitted %+v", row)
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "pending" {
		t.Fatalf("view while submitting %s", raw)
	}
	h.refundAttempt(t, job, r.ID, "unknown", 0)
	if row := h.refundRow(t, r.ID); row.State != "unknown" {
		t.Fatalf("after a lost answer %+v", row)
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "pending" {
		t.Fatalf("view while unknown %s", raw)
	}
	if got := h.refundAdmit(t, job, r.ID); got != "ok" {
		t.Fatalf("readmit %s", got)
	}
	h.refundAttempt(t, job, r.ID, "succeeded", 200)
	outcome := refundOutcome("succeeded", "re_fake_gw", r, 10000)
	if state, changed := h.refundCommit(t, job, r.ID, outcome); state != "committed" || !changed {
		t.Fatalf("committed %s %v", state, changed)
	}
	if got := h.refundText(t, "SELECT business.refund_recovery_secured($1::uuid, $2::uuid, $3, $4::uuid)",
		job.ID, job.Worker, job.Generation, r.ID); got != "secured" {
		t.Fatalf("secured %s", got)
	}
	h.finish(t, job, nil)
	if row := h.refundRow(t, r.ID); row.State != "succeeded" || row.RefundID != "re_fake_gw" {
		t.Fatalf("settled %+v", row)
	}
	v, raw := h.view(t, order, d)
	if v.Lifecycle != "cancelled" || v.Refund == nil || v.Refund.State != "returned" || v.Refund.TargetMinor != 10000 ||
		v.Refund.SucceededMinor != 10000 || v.Refund.InProgress != 0 || v.Refund.Currency != "GBP" {
		t.Fatalf("view %s", raw)
	}
	if b := h.balance(t, coll); b.Succeeded != 10000 || b.Room != 0 || b.Available != 0 || b.Unresolved {
		t.Fatalf("balance %+v", b)
	}
	if h.count(t, "SELECT business.refund_reserve($1::uuid)", order) != 0 ||
		h.count(t, "SELECT count(*) FROM business.refund_attempt WHERE refund_operation_id = $1::uuid", r.ID) != 2 ||
		h.count(t, "SELECT count(*) FROM business.audit_event WHERE subject_id = $1::uuid AND action = 'refund.succeeded'", order) != 1 {
		t.Fatal("after the return")
	}
	h.noPersonalDataInLogs(t)
}

// Every refund gateway is fenced by the job lease: another generation or
// worker, or a finished job, gets GK001; and the worker cannot run the
// owner-internal accounting or reservation.
func TestRefundGatewaysAreLeaseFenced(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	_, _, coll := h.lateCapture(t, f, pol, "fence")
	r := h.reservedRefund(t, coll)
	job := h.claimOnly(t, h.refundJob(t, "dispatch", r.ID))
	stale := job
	stale.Generation++
	other := job
	other.Worker = "00000000-0000-4000-8000-999999999999"
	for _, j := range []jobs.Job{stale, other} {
		if _, err := h.refundBegin(t, j); dbtest.SQLState(err) != "GK001" {
			t.Errorf("begin %+v: %v", j, err)
		}
		if _, _, err := h.refundFix(t, j, r.ID, refundPayload(r, r.Amount), time.Now()); dbtest.SQLState(err) != "GK001" {
			t.Errorf("fix: %v", err)
		}
		for _, sql := range []string{
			"SELECT business.refund_dispatch_admit($1::uuid, $2::uuid, $3, $4::uuid)",
			"SELECT business.refund_recovery_prepared($1::uuid, $2::uuid, $3, $4::uuid)",
			"SELECT business.refund_recovery_secured($1::uuid, $2::uuid, $3, $4::uuid)",
			"SELECT business.refund_recovery_reconcile($1::uuid, $2::uuid, $3, $4::uuid, 'test')",
		} {
			var s string
			if err := h.worker.QueryRow(h.ctx, sql, j.ID, j.Worker, j.Generation, r.ID).Scan(&s); dbtest.SQLState(err) != "GK001" {
				t.Errorf("%s: %v", sql, err)
			}
		}
	}
	// The dispatch job is not the reconcile of the collection.
	var n int
	if err := h.worker.QueryRow(h.ctx, "SELECT count(*) FROM business.refund_reconcile_operations($1::uuid, $2::uuid, $3, $4::uuid)",
		job.ID, job.Worker, job.Generation, coll).Scan(&n); dbtest.SQLState(err) != "22023" {
		t.Errorf("reconcile operations on a dispatch job: %v", err)
	}
	h.finish(t, job, nil)
	if _, err := h.refundBegin(t, job); dbtest.SQLState(err) != "GK001" {
		t.Errorf("begin after completion: %v", err)
	}
	worker := h.db.LoginConn(t, "gk_worker")
	for _, sql := range []string{
		"SELECT business.refund_reserve(gen_random_uuid())",
		"SELECT business.refund_late_capture(gen_random_uuid())",
		"SELECT * FROM business.refund_balance(gen_random_uuid())",
		"SELECT count(*) FROM business.refund_operation",
		"SELECT count(*) FROM business.refund_observation",
	} {
		if _, err := worker.Exec(h.ctx, sql); dbtest.SQLState(err) != "42501" {
			t.Errorf("gk_worker %s: %v", sql, err)
		}
	}
}

// Pending at Stripe -> reconcile: the listed refund is matched (counted
// once however often it is seen), the operation succeeds.
func TestPendingRefundIsReconciledOnce(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "pend")
	r := h.reservedRefund(t, coll)
	job := h.claimOnly(t, h.refundJob(t, "dispatch", r.ID))
	h.refundPrepared(t, job, r)
	if got := h.refundAdmit(t, job, r.ID); got != "ok" {
		t.Fatal(got)
	}
	h.refundAttempt(t, job, r.ID, "pending", 200)
	h.refundCommit(t, job, r.ID, refundOutcome("pending", "re_fake_pend", r, 10000))
	h.finish(t, job, nil)
	if row := h.refundRow(t, r.ID); row.State != "pending" || row.RefundID != "re_fake_pend" {
		t.Fatalf("pending %+v", row)
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "pending" {
		t.Fatalf("view %s", raw)
	}
	rec := h.claimOnly(t, h.refundJob(t, "reconcile", coll))
	if b, err := h.refundBegin(t, rec); err != nil || b.Status != "ready" || b.Purpose != "reconcile" || b.PI != r.PI {
		t.Fatalf("reconcile begin %+v %v", b, err)
	}
	var listed int
	if err := h.worker.QueryRow(h.ctx, "SELECT count(*) FROM business.refund_reconcile_operations($1::uuid, $2::uuid, $3, $4::uuid)",
		rec.ID, rec.Worker, rec.Generation, coll).Scan(&listed); err != nil || listed != 1 {
		t.Fatalf("operations to reconcile %d %v", listed, err)
	}
	for i, status := range []string{"pending", "succeeded", "succeeded"} {
		result, quarantined, _ := h.observe(t, rec, coll, "re_fake_pend", 10000, status, r.PI, &r.ID)
		if quarantined || result != []string{"recorded", "updated", "unchanged"}[i] {
			t.Fatalf("observation %d: %s %v", i, result, quarantined)
		}
	}
	id := "re_fake_pend"
	if state, _ := h.reconcileRefund(t, rec, r.ID, "succeeded", &id, nil); state != "succeeded" {
		t.Fatalf("reconciled %s", state)
	}
	h.finish(t, rec, nil)
	if b := h.balance(t, coll); b.Succeeded != 10000 || b.External != 0 || b.Exposure != 10000 || b.Quarantined {
		t.Fatalf("balance %+v", b)
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "returned" {
		t.Fatalf("view %s", raw)
	}
}

// An external (dashboard) refund seen at Stripe is quarantined once: it
// blocks fixing and admitting the dispatch and counts as exposure until a
// manager resolves it; a restore freeze also defers the dispatch.
func TestExternalRefundQuarantineBlocksDispatch(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "ext")
	r := h.reservedRefund(t, coll)
	h.owner(t, "SELECT business.refund_schedule_reconcile($1::uuid, now())", coll)
	rec := h.claimOnly(t, h.refundJob(t, "reconcile", coll))
	for i := range 2 {
		result, quarantined, reason := h.observe(t, rec, coll, "re_dashboard", 5000, "succeeded", r.PI, nil)
		if !quarantined || reason != "unmatched" || result != []string{"recorded", "unchanged"}[i] {
			t.Fatalf("observation %d: %s %v %s", i, result, quarantined, reason)
		}
	}
	// Metadata naming our operation with another amount is quarantined too.
	if _, quarantined, reason := h.observe(t, rec, coll, "re_wrong_amount", 3000, "pending", r.PI, &r.ID); !quarantined || reason != "amount_mismatch" {
		t.Fatalf("mismatched observation %v %s", quarantined, reason)
	}
	h.finish(t, rec, nil)
	if b := h.balance(t, coll); b.Succeeded != 5000 || b.Reserved != 13000 || b.External != 8000 || b.Exposure != 18000 ||
		b.Room != 0 || !b.Quarantined {
		t.Fatalf("balance %+v", b)
	}
	if h.count(t, "SELECT count(*) FROM business.refund_observation WHERE collection_id = $1::uuid", coll) != 2 ||
		h.count(t, "SELECT count(*) FROM business.audit_event WHERE subject_id = $1::uuid AND action = 'refund.observation_quarantined'", order) != 2 {
		t.Fatal("observations recorded more than once")
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "needs_attention" {
		t.Fatalf("view %s", raw)
	}

	job := h.claimOnly(t, h.refundJob(t, "dispatch", r.ID))
	b, err := h.refundBegin(t, job)
	if err != nil || b.Status != "ready" {
		t.Fatalf("begin %+v %v", b, err)
	}
	if status, _, err := h.refundFix(t, job, r.ID, refundPayload(r, r.Amount), b.DBNow); err != nil || status != "blocked" {
		t.Fatalf("fix under quarantine %s %v", status, err)
	}
	h.owner(t, `UPDATE business.refund_observation SET resolved_at = now(), resolution_note = 'checked'
		WHERE collection_id = $1::uuid`, coll)
	h.refundPrepared(t, job, r)
	// A new quarantined observation after the request was fixed still
	// stops the call at admission.
	h.owner(t, `INSERT INTO business.refund_observation (id, collection_id, provider_account_id, livemode, provider_refund_id,
		amount_minor, currency, status, quarantined, quarantine_reason, source)
		VALUES (gen_random_uuid(), $1::uuid, $2, false, 're_second_dashboard', 100, 'gbp', 'succeeded', true, 'unmatched', 'reconcile')`,
		coll, stripeAccount)
	if got := h.refundAdmit(t, job, r.ID); got != "blocked" {
		t.Fatalf("admit under quarantine %s", got)
	}
	h.owner(t, `UPDATE business.refund_observation SET resolved_at = now(), resolution_note = 'checked'
		WHERE provider_refund_id = 're_second_dashboard'`)
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if got := h.refundAdmit(t, job, r.ID); got != "frozen" {
		t.Fatalf("admit while frozen %s", got)
	}
	if b, err := h.refundBegin(t, job); err != nil || b.Status != "frozen" {
		t.Fatalf("begin while frozen %+v %v", b, err)
	}
	var frozen bool
	if err := h.worker.QueryRow(h.ctx, "SELECT business.refund_recovery_frozen($1::uuid, $2::uuid, $3)", job.ID, job.Worker,
		job.Generation).Scan(&frozen); err != nil || !frozen {
		t.Fatalf("frozen %v %v", frozen, err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	if got := h.refundAdmit(t, job, r.ID); got != "ok" {
		t.Fatalf("admit after the freeze %s", got)
	}
	if row := h.refundRow(t, r.ID); row.State != "submitting" {
		t.Fatalf("row %+v", row)
	}
}

// A lost answer never becomes a failure on a timer: past the window the
// dispatch goes to reconciliation, the complete list has no refund for it
// and the operation stays unknown (manager); nothing new is reserved. One
// never admitted is withdrawn instead and the remainder reopens.
func TestUnknownRefundPastTheWindowStaysUnknown(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "lost")
	r := h.reservedRefund(t, coll)
	job := h.claimOnly(t, h.refundJob(t, "dispatch", r.ID))
	h.refundPrepared(t, job, r)
	if got := h.refundAdmit(t, job, r.ID); got != "ok" {
		t.Fatal(got)
	}
	h.refundAttempt(t, job, r.ID, "unknown", 502)
	h.owner(t, `UPDATE business.refund_operation SET first_dispatch_at = first_dispatch_at - interval '24 hours',
		replay_until = replay_until - interval '24 hours', dispatch_started_at = dispatch_started_at - interval '24 hours'
		WHERE id = $1::uuid`, r.ID)
	if got := h.refundAdmit(t, job, r.ID); got != "expired" {
		t.Fatalf("admit past the window %s", got)
	}
	if got := h.refundText(t, "SELECT business.refund_recovery_reconcile($1::uuid, $2::uuid, $3, $4::uuid, 'window ended')",
		job.ID, job.Worker, job.Generation, r.ID); got != "reconcile" {
		t.Fatalf("recovery %s", got)
	}
	h.finish(t, job, jobs.Attention("refund outcome unknown", nil))
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "needs_attention" {
		t.Fatalf("view %s", raw)
	}
	rec := h.claimOnly(t, h.refundJob(t, "reconcile", coll))
	state, ended := h.reconcileRefund(t, rec, r.ID, "not_found", nil, nil)
	if state != "unknown" || !ended {
		t.Fatalf("not found: %s ended=%v", state, ended)
	}
	h.finish(t, rec, nil)
	if h.count(t, "SELECT business.refund_reserve($1::uuid)", order) != 0 || h.refundRow(t, r.ID).State != "unknown" {
		t.Fatal("an unknown refund was released")
	}

	// Never admitted: withdrawn when its window ends; the remainder reopens.
	order2, _, coll2 := h.lateCapture(t, f, pol, "never")
	r2 := h.reservedRefund(t, coll2)
	job2 := h.claimOnly(t, h.refundJob(t, "dispatch", r2.ID))
	h.refundPrepared(t, job2, r2)
	h.owner(t, `UPDATE business.refund_operation SET first_dispatch_at = first_dispatch_at - interval '24 hours',
		replay_until = replay_until - interval '24 hours' WHERE id = $1::uuid`, r2.ID)
	if got := h.refundAdmit(t, job2, r2.ID); got != "expired" {
		t.Fatalf("admit %s", got)
	}
	h.refundText(t, "SELECT business.refund_recovery_reconcile($1::uuid, $2::uuid, $3, $4::uuid, 'window ended')",
		job2.ID, job2.Worker, job2.Generation, r2.ID)
	h.finish(t, job2, nil)
	if row := h.refundRow(t, r2.ID); row.State != "cancelled" || row.Started {
		t.Fatalf("never admitted %+v", row)
	}
	if got := h.count(t, "SELECT business.refund_reserve($1::uuid)", order2); got != 1 {
		t.Fatalf("reserved %d after the withdrawal", got)
	}
}

// Stripe's answers settle the operation only when they match it; a
// contradicting answer blocks any new reservation for the collection until
// a manager clears it; a failed refund is never re-sent automatically and a
// later contradiction never reopens or pays out; once cleared, the
// remainder of the definitive failure reopens.
func TestRefundOutcomesAreVerified(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "verify")
	r := h.reservedRefund(t, coll)
	job := h.claimOnly(t, h.refundJob(t, "dispatch", r.ID))
	h.refundPrepared(t, job, r)
	h.refundAdmit(t, job, r.ID)
	h.refundAttempt(t, job, r.ID, "succeeded", 200)
	h.refundCommit(t, job, r.ID, refundOutcome("succeeded", "re_fake_half", r, 5000))
	if got := h.refundAdmit(t, job, r.ID); got != "blocked" {
		t.Fatalf("admit after a mismatched answer %s", got)
	}
	h.finish(t, job, nil)
	if row := h.refundRow(t, r.ID); row.State != "unknown" || !strings.Contains(row.Mismatch, "amount") || row.RefundID != "" {
		t.Fatalf("mismatched answer %+v", row)
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "needs_attention" {
		t.Fatalf("view %s", raw)
	}
	rec := h.claimOnly(t, h.refundJob(t, "reconcile", coll))
	id := "re_fake_verify"
	reason := "expired_or_canceled_card"
	if state, _ := h.reconcileRefund(t, rec, r.ID, "failed", &id, &reason); state != "failed" {
		t.Fatalf("failed %s", state)
	}
	if b := h.balance(t, coll); b.Room != 10000 || b.Available != 0 {
		t.Fatalf("balance with a contradiction on record %+v", b)
	}
	// Stripe later says succeeded: recorded beside it, never reopened.
	if state, _ := h.reconcileRefund(t, rec, r.ID, "succeeded", &id, nil); state != "failed" {
		t.Fatalf("contradiction %s", state)
	}
	h.finish(t, rec, nil)
	if h.count(t, "SELECT business.refund_reserve($1::uuid)", order) != 0 ||
		h.count(t, "SELECT count(*) FROM business.refund_operation WHERE order_id = $1::uuid", order) != 1 ||
		h.count(t, "SELECT count(*) FROM business.audit_event WHERE subject_id = $1::uuid AND action = 'refund.mismatch'", order) != 2 {
		t.Fatal("a contradicted refund was re-sent or not recorded")
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "needs_attention" {
		t.Fatalf("view %s", raw)
	}
	// A manager clears the contradiction (tooling: Not yet).
	h.owner(t, "UPDATE business.refund_operation SET mismatch_reason = NULL, mismatch_evidence = NULL WHERE id = $1::uuid", r.ID)
	if b := h.balance(t, coll); b.Succeeded != 0 || b.Reserved != 0 || b.Available != 10000 {
		t.Fatalf("balance %+v", b)
	}
	if got := h.count(t, "SELECT business.refund_reserve($1::uuid)", order); got != 1 {
		t.Fatalf("reserved %d after a definitive failure", got)
	}
}

// Refund events for a succeeded collection bring one refund reconcile
// forward, however many arrive.
func TestRefundEventsScheduleOneReconcile(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, coll, _ := h.paidCollection(t, f, pol, "evt")
	for i, typ := range []string{"charge.refunded", "refund.updated", "refund.created", "charge.refunded"} {
		obj, kind := "ch_fake_x", "charge"
		if strings.HasPrefix(typ, "refund.") {
			obj, kind = fmt.Sprintf("re_fake_%d", i), "refund"
		}
		h.deliver(t, fmt.Sprintf("evt_refund_%d", i), typ, kind, obj, coll, order)
	}
	if got := h.count(t, `SELECT count(*) FROM business.refund_job rj JOIN business.job j ON j.id = rj.job_id
		WHERE rj.collection_id = $1::uuid AND rj.purpose = 'reconcile' AND j.state IN ('queued', 'retry')`, coll); got != 1 {
		t.Fatalf("%d waiting refund reconciles", got)
	}
}
