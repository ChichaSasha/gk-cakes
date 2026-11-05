//go:build integration

package checkout_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
)

// GK-026 B2 part 2: manager recovery tools (migration 0025; K05, K00, K11).

type attentionItem struct {
	Kind              string
	OrderID           string  `json:"order_id"`
	RefundOperationID *string `json:"refund_operation_id"`
	ObservationID     *string `json:"observation_id"`
	AmountMinor       *int64  `json:"amount_minor"`
	WindowEnded       *bool   `json:"window_ended"`
	Reason            *string
}

func (h *payHarness) attention(t *testing.T, subject, order string) []attentionItem {
	t.Helper()
	raw, err := h.co.AttentionList(h.ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	var all, out []attentionItem
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	for _, i := range all {
		if i.OrderID == order {
			out = append(out, i)
		}
	}
	return out
}

func findKind(items []attentionItem, kind string) *attentionItem {
	for i := range items {
		if items[i].Kind == kind {
			return &items[i]
		}
	}
	return nil
}

func (h *payHarness) version(t *testing.T, order string, d []byte) int {
	t.Helper()
	v, _ := h.cancelView(t, order, d)
	return v.Version
}

// unknownPastWindow makes the late capture's refund dispatch unknown (Stripe
// answers 503 and creates nothing), then moves its replay window into the
// past (the coordinator clock follows).
func (h *payHarness) unknownPastWindow(t *testing.T, coll string) refundOp {
	t.Helper()
	r := h.reservedRefund(t, coll)
	h.withRefunds()
	h.stripe.mu.Lock()
	h.stripe.refunds.status = 503
	h.stripe.mu.Unlock()
	h.due(t)
	if errs := h.drain(t); len(errs) != 1 || h.operations(t, coll)[0].State != "unknown" {
		t.Fatalf("503:\n%s", reasons(errs))
	}
	h.stripe.mu.Lock()
	h.stripe.refunds.status = 0
	h.stripe.mu.Unlock()
	h.owner(t, `UPDATE business.refund_operation SET first_dispatch_at = first_dispatch_at - interval '24 hours',
		replay_until = replay_until - interval '24 hours', dispatch_started_at = dispatch_started_at - interval '24 hours'
		WHERE id = $1::uuid`, r.ID)
	h.owner(t, `UPDATE business.recovery_operation SET replay_until = replay_until - interval '24 hours' WHERE id = $1::uuid`, r.ID)
	h.coord.Now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	return r
}

// K05 card fixture resolved by evidence: a 10000p collection, a local
// unknown 10000p refund and an external (dashboard) 5000p return. Matching
// the external refund to the operation is refused (identities unproven);
// accepting it counts its 5000p as returned without any new refund; the
// unknown refund is failed only with the complete-list evidence after its
// window; the remainder re-reserved is exactly 5000p, sent once.
func TestCardFixtureResolvedByEvidence(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d, coll := h.lateCapture(t, f, pol, "fix")
	r := h.reservedRefund(t, coll)
	h.withRefunds()
	h.stripe.mu.Lock()
	h.stripe.refunds.status = 503
	h.stripe.mu.Unlock()
	h.due(t)
	h.drain(t)
	h.stripe.mu.Lock()
	h.stripe.refunds.status = 0
	h.stripe.mu.Unlock()
	// Inside the window nothing can be decided.
	if _, err := h.co.DecideOperation(h.ctx, chef, "early", order, r.ID, checkout.DecideOperationInput{
		ExpectedVersion: h.version(t, order, d), Decision: "failed", Note: "Too early"}); conflict(err) != "window_open" {
		t.Fatalf("inside the window: %v", err)
	}
	h.owner(t, `UPDATE business.refund_operation SET first_dispatch_at = first_dispatch_at - interval '24 hours',
		replay_until = replay_until - interval '24 hours', dispatch_started_at = dispatch_started_at - interval '24 hours'
		WHERE id = $1::uuid`, r.ID)
	h.owner(t, `UPDATE business.recovery_operation SET replay_until = replay_until - interval '24 hours' WHERE id = $1::uuid`, r.ID)
	h.coord.Now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	h.stripe.dashboardRefund(r.PI, 5000, "succeeded")
	h.due(t)
	h.drain(t) // the complete list: the external refund quarantined, the operation absent
	if b := h.balance(t, coll); b.Succeeded != 5000 || b.Reserved != 10000 || b.Exposure != 15000 || b.Available != 0 {
		t.Fatalf("fixture balance %+v", b)
	}
	var obs string
	if err := h.admin.QueryRow(h.ctx, `SELECT id::text FROM business.refund_observation WHERE collection_id = $1::uuid`, coll).Scan(&obs); err != nil {
		t.Fatal(err)
	}

	// The attention list: amounts for the chef only; support reads are audited.
	items := h.attention(t, chef, order)
	q, u := findKind(items, "quarantined_observation"), findKind(items, "refund_unknown")
	if q == nil || q.AmountMinor == nil || *q.AmountMinor != 5000 || u == nil || u.WindowEnded == nil || !*u.WindowEnded {
		t.Fatalf("chef attention %+v", items)
	}
	for _, i := range h.attention(t, support, order) {
		if i.AmountMinor != nil {
			t.Fatalf("support sees an amount: %+v", i)
		}
	}
	if h.count(t, "SELECT count(*) FROM business.audit_event WHERE action = 'attention.viewed'") != 1 {
		t.Fatal("support read not audited")
	}
	if _, err := h.co.AttentionList(h.ctx, fulfilment); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("fulfillment attention: %v", err)
	}
	if _, err := h.co.RecordRefund(h.ctx, chef, "x", order, refund(h.version(t, order, d), 100, "x", alloc(coll, 100))); conflict(err) != "reconciliation_required" {
		t.Fatalf("exception while quarantined: %v", err)
	}

	v := h.version(t, order, d)
	if _, err := h.co.ResolveObservation(h.ctx, fulfilment, "m", order, obs, checkout.ResolveObservationInput{ExpectedVersion: v,
		Resolution: "accept_external", Note: "x"}); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("fulfillment resolve: %v", err)
	}
	if _, err := h.co.ResolveObservation(h.ctx, chef, "m", order, obs, checkout.ResolveObservationInput{ExpectedVersion: v,
		Resolution: "match", RefundOperationID: r.ID, Note: "Looks like it"}); conflict(err) != "identity_unproven" {
		t.Fatalf("match without proof: %v", err)
	}
	accept := checkout.ResolveObservationInput{ExpectedVersion: v, Resolution: "accept_external", Note: "Dashboard refund by the chef"}
	res, err := h.co.ResolveObservation(h.ctx, chef, "a", order, obs, accept)
	if err != nil || res.Replayed {
		t.Fatalf("accept: %+v %v", res, err)
	}
	if again, err := h.co.ResolveObservation(h.ctx, chef, "a", order, obs, accept); err != nil || !again.Replayed || again.ID != res.ID {
		t.Fatalf("replay %+v %v", again, err)
	}
	if b := h.balance(t, coll); b.Succeeded != 5000 || b.Reserved != 10000 || b.Quarantined || b.Available != 0 {
		t.Fatalf("after accepting %+v", b)
	}
	if n := h.count(t, "SELECT count(*) FROM business.refund_observation WHERE id = $1::uuid AND quarantined AND resolved_at IS NOT NULL", obs); n != 1 {
		t.Fatal("the accepted observation is not kept")
	}
	if len(h.stripe.refundCreates()) != 1 || h.count(t, "SELECT count(*) FROM business.refund_operation WHERE collection_id = $1::uuid", coll) != 1 {
		t.Fatal("accepting sent or reserved money")
	}

	// Decide the unknown refund: stale version, then failed with the
	// complete-list evidence.
	if _, err := h.co.DecideOperation(h.ctx, chef, "d0", order, r.ID, checkout.DecideOperationInput{ExpectedVersion: v,
		Decision: "failed", Note: "x"}); conflict(err) != "stale_version" {
		t.Fatalf("stale: %v", err)
	}
	v = h.version(t, order, d)
	if _, err := h.co.DecideOperation(h.ctx, chef, "d1", order, r.ID, checkout.DecideOperationInput{ExpectedVersion: v,
		Decision: "failed", Note: "Absent from Stripe's complete list after the window"}); err != nil {
		t.Fatal(err)
	}
	if row := h.refundRow(t, r.ID); row.State != "failed" {
		t.Fatalf("decided %+v", row)
	}
	if b := h.balance(t, coll); b.Succeeded != 5000 || b.Reserved != 0 || b.Room != 5000 {
		t.Fatalf("after the decision %+v", b)
	}
	if fo := findKind(h.attention(t, chef, order), "refund_failed_owed"); fo == nil || fo.AmountMinor == nil || *fo.AmountMinor != 5000 {
		t.Fatalf("failed owed %+v", h.attention(t, chef, order))
	}
	if len(h.stripe.refundCreates()) != 1 {
		t.Fatal("the decision sent money")
	}
	h.coord.Now = nil
	v = h.version(t, order, d)
	if _, err := h.co.ReserveRemainder(h.ctx, chef, "rr", order, r.ID, checkout.RemainderInput{ExpectedVersion: v}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.co.ReserveRemainder(h.ctx, chef, "rr2", order, r.ID, checkout.RemainderInput{ExpectedVersion: v + 1}); conflict(err) != "refund_in_progress" {
		t.Fatalf("second re-reserve: %v", err)
	}
	h.drain(t)
	creates := h.stripe.refundCreates()
	if len(creates) != 2 || h.stripe.refundForm(creates[1]).Get("amount") != "5000" {
		t.Fatalf("creates %v", creates)
	}
	if b := h.balance(t, coll); b.Succeeded != 10000 || b.Available != 0 {
		t.Fatalf("returned %+v", b)
	}
	if view, raw := h.view(t, order, d); view.Refund == nil || view.Refund.State != "returned" {
		t.Fatalf("view %s", raw)
	}
	// The decision is journaled for the dispatch's K11 record (outside any
	// transaction, by the worker) and its attention job is settled.
	var recState, recStatus, recDecision string
	if err := h.admin.QueryRow(h.ctx, `SELECT state, outcome ->> 'status', outcome -> 'evidence' ->> 'decision_id'
		FROM business.recovery_operation WHERE id = $1::uuid`, r.ID).Scan(&recState, &recStatus, &recDecision); err != nil ||
		recState != "secured" || recStatus != "manager_failed" || recDecision == "" {
		t.Fatalf("dispatch record %s %s %s %v", recState, recStatus, recDecision, err)
	}
	for _, i := range h.attention(t, chef, order) {
		if i.Kind == "job_attention" && i.RefundOperationID != nil && *i.RefundOperationID == r.ID {
			t.Fatalf("settled job still listed %+v", i)
		}
	}
	if h.count(t, `SELECT count(*) FROM business.job j JOIN business.refund_job rj ON rj.job_id = j.id
		WHERE rj.refund_operation_id = $1::uuid AND j.state = 'attention'`, r.ID) != 0 {
		t.Fatal("the decided refund's dispatch job is still in attention")
	}
	if h.count(t, "SELECT count(*) FROM business.refund_recovery_decision WHERE order_id = $1::uuid", order) != 3 ||
		h.auditCount(t, order, "refund.recovery_decided", "Dashboard") != 0 {
		t.Fatal("decisions not recorded (or the note in the audit)")
	}
}

// An unknown refund that did happen at Stripe: "succeeded" with its refund
// ID is verified by the reconcile (list, matching the recorded ID) before
// it is decided; failing it is refused while evidence names it.
func TestUnknownRefundSucceededIsVerifiedAtStripe(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "ver")
	r := h.unknownPastWindow(t, coll)
	// The chef made the refund in the dashboard meanwhile (no metadata).
	refundID := h.stripe.dashboardRefund(r.PI, 10000, "succeeded")
	v := h.version(t, order, d)
	if _, err := h.co.DecideOperation(h.ctx, chef, "bad", order, r.ID, checkout.DecideOperationInput{ExpectedVersion: v,
		Decision: "failed", Note: "x"}); conflict(err) != "evidence_required" {
		t.Fatalf("failed without a complete list after the window: %v", err)
	}
	res, err := h.co.DecideOperation(h.ctx, chef, "ok", order, r.ID, checkout.DecideOperationInput{ExpectedVersion: v,
		Decision: "succeeded", ProviderRefundID: refundID, Note: "Made in the dashboard"})
	if err != nil {
		t.Fatal(err)
	}
	if row := h.refundRow(t, r.ID); row.State != "unknown" || row.RefundID != refundID {
		t.Fatalf("before verification %+v", row)
	}
	h.due(t)
	h.drain(t)
	if row := h.refundRow(t, r.ID); row.State != "succeeded" {
		t.Fatalf("after verification %+v", row)
	}
	if b := h.balance(t, coll); b.Succeeded != 10000 || b.Exposure != 10000 || b.Quarantined {
		t.Fatalf("counted once %+v", b)
	}
	if len(h.stripe.refundCreates()) != 1 {
		t.Fatalf("creates %v", h.stripe.refundCreates())
	}
	var action string
	if err := h.admin.QueryRow(h.ctx, "SELECT action FROM business.refund_recovery_decision WHERE id = $1::uuid", res.ID).Scan(&action); err != nil ||
		action != "operation_verification" {
		t.Fatalf("decision %q %v", action, err)
	}
}

// A refund observed before the decision is matched by the manager's refund
// ID (same amount) and settles the operation by its own status.
func TestUnknownRefundMatchedToAnObservedRefund(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "obs")
	r := h.unknownPastWindow(t, coll)
	refundID := h.stripe.dashboardRefund(r.PI, 10000, "succeeded")
	h.due(t)
	h.drain(t) // observed and quarantined (no metadata)
	if b := h.balance(t, coll); !b.Quarantined || b.Exposure != 20000 {
		t.Fatalf("observed %+v", b)
	}
	v := h.version(t, order, d)
	// The manager's ID alone does not prove the identity (the observed
	// refund carries no metadata): it is recorded and verified by the
	// reconcile, which matches the observation to the operation.
	if _, err := h.co.DecideOperation(h.ctx, chef, "ok", order, r.ID, checkout.DecideOperationInput{ExpectedVersion: v,
		Decision: "succeeded", ProviderRefundID: refundID, Note: "Same refund"}); err != nil {
		t.Fatal(err)
	}
	if row := h.refundRow(t, r.ID); row.State != "unknown" || row.RefundID != refundID {
		t.Fatalf("before verification %+v", row)
	}
	h.due(t)
	h.drain(t)
	if row := h.refundRow(t, r.ID); row.State != "succeeded" || row.RefundID != refundID {
		t.Fatalf("verified %+v", row)
	}
	if n := h.count(t, "SELECT count(*) FROM business.refund_observation WHERE matched_operation_id = $1::uuid", r.ID); n != 1 {
		t.Fatalf("%d observations matched", n)
	}
	if b := h.balance(t, coll); b.Succeeded != 10000 || b.Exposure != 10000 || b.Quarantined {
		t.Fatalf("counted once %+v", b)
	}
	if view, raw := h.view(t, order, d); view.Refund.State != "returned" {
		t.Fatalf("view %s", raw)
	}
}

// A succeeded refund Stripe later reports failed: the chef corrects it to
// failed (no payout), then re-reserves the remainder once; a succeeded
// correction needs a succeeded matched refund. A decided payment
// collection's contradiction is cleared with a recorded decision.
func TestContradictionClearedAndRemainderReserved(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "cc")
	r := h.reservedRefund(t, coll)
	h.withRefunds()
	h.due(t)
	h.drain(t)
	h.refundEvent(t, "evt_re_created", "refund.created", "re_fake1")
	h.drain(t)
	h.stripe.setRefund("re_fake1", "failed", "expired_or_canceled_card")
	h.refundEvent(t, "evt_re_updated", "refund.updated", "re_fake1")
	h.drain(t)
	if row := h.refundRow(t, r.ID); row.State != "succeeded" || row.Mismatch == "" {
		t.Fatalf("contradiction %+v", row)
	}
	if c := findKind(h.attention(t, chef, order), "refund_contradiction"); c == nil {
		t.Fatal("contradiction not listed")
	}
	v := h.version(t, order, d)
	if _, err := h.co.ReserveRemainder(h.ctx, chef, "early", order, r.ID, checkout.RemainderInput{ExpectedVersion: v}); conflict(err) != "reconciliation_required" {
		t.Fatalf("re-reserve under a contradiction: %v", err)
	}
	if _, err := h.co.ClearRefundMismatch(h.ctx, chef, "s", order, r.ID, checkout.ClearMismatchInput{ExpectedVersion: v,
		Decision: "succeeded", Note: "x"}); conflict(err) != "state_conflict" {
		t.Fatalf("succeeded on a succeeded refund: %v", err)
	}
	if _, err := h.co.ClearRefundMismatch(h.ctx, chef, "f", order, r.ID, checkout.ClearMismatchInput{ExpectedVersion: v,
		Decision: "failed", Note: "Stripe reports the refund failed (card expired)"}); err != nil {
		t.Fatal(err)
	}
	if row := h.refundRow(t, r.ID); row.State != "failed" || row.Mismatch != "" {
		t.Fatalf("corrected %+v", row)
	}
	h.due(t)
	h.drain(t)
	if len(h.stripe.refundCreates()) != 1 || len(h.operations(t, coll)) != 1 {
		t.Fatal("paid out automatically after the correction")
	}
	v = h.version(t, order, d)
	if _, err := h.co.ReserveRemainder(h.ctx, chef, "rr", order, r.ID, checkout.RemainderInput{ExpectedVersion: v, Note: "Card replaced"}); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	creates := h.stripe.refundCreates()
	if len(creates) != 2 || h.stripe.refundForm(creates[1]).Get("amount") != "10000" {
		t.Fatalf("creates %v", creates)
	}
	if b := h.balance(t, coll); b.Succeeded != 10000 {
		t.Fatalf("returned %+v", b)
	}
	if _, err := h.co.ReserveRemainder(h.ctx, chef, "rr3", order, r.ID, checkout.RemainderInput{ExpectedVersion: h.version(t, order, d)}); conflict(err) != "state_conflict" {
		t.Fatalf("re-reserve a superseded failure: %v", err)
	}

	// A contradiction on the decided payment collection.
	h.owner(t, `UPDATE business.payment_collection SET mismatch_reason = 'charge amount differs', mismatch_evidence = '{}' WHERE id = $1::uuid`, coll)
	v = h.version(t, order, d)
	if _, err := h.co.ClearPaymentMismatch(h.ctx, support, "p", order, coll, checkout.NoteInput{ExpectedVersion: v, Note: "x"}); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("support clears: %v", err)
	}
	if _, err := h.co.ClearPaymentMismatch(h.ctx, chef, "p", order, coll, checkout.NoteInput{ExpectedVersion: v, Note: "Checked in Stripe"}); err != nil {
		t.Fatal(err)
	}
	if h.count(t, "SELECT count(*) FROM business.payment_collection WHERE id = $1::uuid AND mismatch_reason IS NULL", coll) != 1 {
		t.Fatal("payment mismatch not cleared")
	}
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if _, err := h.co.ClearPaymentMismatch(h.ctx, chef, "pz", order, coll, checkout.NoteInput{ExpectedVersion: h.version(t, order, d), Note: "x"}); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("frozen: %v", err)
	}
	if again, err := h.co.ClearPaymentMismatch(h.ctx, chef, "p", order, coll, checkout.NoteInput{ExpectedVersion: v, Note: "Checked in Stripe"}); err != nil || !again.Replayed {
		t.Fatalf("replay during the freeze: %+v %v", again, err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
}

// M1: a dispatch-time contradiction (Stripe answered with another amount)
// leaves the refund unknown with contradicting evidence. Clearing it as
// failed needs exactly the proof of a failed decision: refused inside the
// window, refused after it without a complete listing, and once the
// listing shows the refund (it did happen) the operation is decided by
// Stripe's record and "failed" needs a failed record of that refund. No
// second POST is ever sent.
func TestDispatchMismatchClearedAsFailedNeedsTheProof(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "mm")
	r := h.reservedRefund(t, coll)
	h.withRefunds()
	h.stripe.mu.Lock()
	h.stripe.refunds.lie = 9000
	h.stripe.refunds.listFail = 503
	h.stripe.mu.Unlock()
	h.due(t)
	h.drain(t)
	row := h.refundRow(t, r.ID)
	if row.State != "unknown" || row.Mismatch == "" || len(h.stripe.refundCreates()) != 1 {
		t.Fatalf("dispatch mismatch %+v %v", row, h.stripe.refundCreates())
	}
	clear := func(key string) error {
		_, err := h.co.ClearRefundMismatch(h.ctx, chef, key, order, r.ID, checkout.ClearMismatchInput{
			ExpectedVersion: h.version(t, order, d), Decision: "failed", Note: "Looks failed"})
		return err
	}
	if err := clear("c1"); conflict(err) != "window_open" {
		t.Fatalf("inside the window: %v", err)
	}
	h.owner(t, `UPDATE business.refund_operation SET first_dispatch_at = first_dispatch_at - interval '24 hours',
		replay_until = replay_until - interval '24 hours', dispatch_started_at = dispatch_started_at - interval '24 hours'
		WHERE id = $1::uuid`, r.ID)
	h.owner(t, `UPDATE business.recovery_operation SET replay_until = replay_until - interval '24 hours' WHERE id = $1::uuid`, r.ID)
	h.coord.Now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	if err := clear("c2"); conflict(err) != "evidence_required" {
		t.Fatalf("after the window without a complete list: %v", err)
	}
	if _, err := h.co.DecideOperation(h.ctx, chef, "d1", order, r.ID, checkout.DecideOperationInput{
		ExpectedVersion: h.version(t, order, d), Decision: "failed", Note: "x"}); conflict(err) != "evidence_required" {
		t.Fatalf("decide failed on a contradicted refund: %v", err)
	}
	// The complete list shows the refund (metadata names the operation, the
	// real amount): it is decided succeeded by Stripe's record.
	h.stripe.mu.Lock()
	h.stripe.refunds.listFail = 0
	h.stripe.refunds.lie = 0
	h.stripe.mu.Unlock()
	h.due(t)
	h.drain(t)
	if row := h.refundRow(t, r.ID); row.State != "succeeded" || row.Mismatch == "" {
		t.Fatalf("after the list %+v", row)
	}
	if err := clear("c3"); conflict(err) != "evidence_required" {
		t.Fatalf("failed on a succeeded refund without a failed record: %v", err)
	}
	if _, err := h.co.ClearRefundMismatch(h.ctx, chef, "k", order, r.ID, checkout.ClearMismatchInput{
		ExpectedVersion: h.version(t, order, d), Decision: "keep", Note: "Stripe's record shows the full amount"}); err != nil {
		t.Fatal(err)
	}
	h.coord.Now = nil
	h.due(t)
	h.drain(t)
	if len(h.stripe.refundCreates()) != 1 {
		t.Fatalf("second POST: %v", h.stripe.refundCreates())
	}
	if b := h.balance(t, coll); b.Succeeded != 10000 || b.Exposure != 10000 {
		t.Fatalf("counted once %+v", b)
	}
	if view, raw := h.view(t, order, d); view.Refund.State != "returned" {
		t.Fatalf("view %s", raw)
	}
}
