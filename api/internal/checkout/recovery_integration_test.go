//go:build integration

package checkout_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
)

// GK-026 B2: chef refunds and recovery (migrations 0023+; contracts r6.5
// K05, K11, K00).

// auditCount counts the order's audit events of action whose detail
// contains fragment.
func (h *payHarness) auditCount(t *testing.T, order, action, fragment string) int {
	t.Helper()
	return h.count(t, `SELECT count(*) FROM business.audit_event WHERE subject_id = $1::uuid AND action = $2
		AND detail::text LIKE '%' || $3 || '%'`, order, action, fragment)
}

// B1 follow-up: a failed operation earlier in the order's history does not
// make a refund read needs_attention when all that is missing is the
// delivery share visibly waiting for the chef (delivery_pending_minor). A
// cake shortfall still does.
func TestDeliveryPendingIsNotAttention(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.tenThousandOrder(t, f, pol, "dp")
	cake := h.payNow(t, order, d, "dp")
	h.seedDelivery(t, order, 2000)
	var pi string
	if err := h.admin.QueryRow(h.ctx, "SELECT payment_intent_id FROM business.payment_collection WHERE id = $1::uuid", cake).Scan(&pi); err != nil {
		t.Fatal(err)
	}
	// An earlier goal whose only refund failed definitively.
	goal := payments.NewID()
	h.owner(t, `INSERT INTO business.refund_goal (id, order_id, target_minor, reason, source)
		VALUES ($1::uuid, $2::uuid, 1000, 'late_capture_terminal', 'system')`, goal, order)
	h.sentOperation(t, order, cake, goal, pi, "failed", 1000)
	if v, raw := h.cancelView(t, order, d); v.Refund == nil || v.Refund.State != "needs_attention" {
		t.Fatalf("a failed refund with money still owed: %s", raw)
	}
	// The customer cancels at the boundary: 50% of 12000p is 6000p, cake
	// 5000p sent, delivery 1000p waits for the chef.
	v, _ := h.cancelView(t, order, d)
	h.now = cancelDeadline
	p := h.preview(t, order, d)
	if p.AmountMinor != 6000 || p.DeliveryPending != 1000 {
		t.Fatalf("preview %+v", p)
	}
	h.withRefunds()
	if _, err := h.cancel(order, d, "c", v.Version, p); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("cancel:\n%s", reasons(errs))
	}
	if b := h.balance(t, cake); b.Succeeded != 5000 {
		t.Fatalf("cake %+v", b)
	}
	if v, raw := h.cancelView(t, order, d); v.Refund.State != "partly_returned" || v.Refund.DeliveryPending != 1000 ||
		v.Refund.TargetMinor != 6000 || v.Refund.SucceededMinor != 5000 {
		t.Fatalf("delivery pending after an old failure: %s", raw)
	}
}

// B1 follow-up: the commit-time honour_too_late refusal (defensive: the
// request refuses the same receipt instant first) is audited like the
// other refusals. The receipt is moved to the handover behind the request
// check to reach it.
func TestCommitTimeHonourTooLateIsAudited(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.lateOrder(t, f, pol, "htl")
	v, _ := h.cancelView(t, order, d)
	h.now = time.Date(2026, 11, 6, 13, 0, 0, 0, time.UTC)
	honour := checkout.LatePaymentInput{ExpectedVersion: v.Version, Decision: "honour",
		CustomerAgreement: &checkout.Agreement{Channel: "phone", AgreedAt: "2026-11-06T12:45:00Z", EvidenceRef: "Call note 7"}}
	res, err := h.co.LatePayment(h.ctx, chef, "lp", order, honour)
	if err != nil {
		t.Fatal(err)
	}
	h.owner(t, `ALTER TABLE business.order_command DISABLE TRIGGER order_command_immutable`)
	h.owner(t, `UPDATE business.order_command SET received_at = '2026-11-14T12:30:00Z' WHERE id = $1::uuid`, res.ID)
	h.owner(t, `ALTER TABLE business.order_command ENABLE TRIGGER order_command_immutable`)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("decision:\n%s", reasons(errs))
	}
	if v2, raw := h.cancelView(t, order, d); v2.Lifecycle != "paid_awaiting_agreement" || v2.Version != v.Version+1 {
		t.Fatalf("refused honour %s", raw)
	}
	if got := h.auditCount(t, order, "order.late_payment_refused", `"honour_too_late"`); got != 1 {
		t.Fatalf("honour_too_late audits %d, want 1", got)
	}
}

// sentAmounts are the amounts of every refund create call so far, in order.
func (h *payHarness) sentAmounts() []string {
	var out []string
	for _, c := range h.stripe.refundCreates() {
		out = append(out, h.stripe.refundForm(c).Get("amount"))
	}
	return out
}

func refund(version int, amount int64, reason string, allocs ...checkout.RefundAllocation) checkout.RefundInput {
	return checkout.RefundInput{ExpectedVersion: version, AmountMinor: amount, Currency: "GBP", Allocations: allocs, Reason: reason}
}

func alloc(coll string, amount int64) checkout.RefundAllocation {
	return checkout.RefundAllocation{CollectionID: coll, AmountMinor: amount}
}

// K05 agreed exception: chef only, bounded by actual collected funds net of
// succeeded and reserved returns (REFUND_LIMIT) and each collection's room,
// exactly the named allocations reserved once under a raised goal, nothing
// new while a refund of the order is unresolved, idempotent by key.
func TestChefPartialRefundGoal(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d := h.tenThousandOrder(t, f, pol, "ex")
	cake := h.payNow(t, order, d, "ex")
	delivery := h.seedDelivery(t, order, 2000)
	other, od := h.tenThousandOrder(t, f, pol, "ot")
	otherColl := h.payNow(t, other, od, "ot")
	v, _ := h.cancelView(t, order, d)
	h.withRefunds()

	in := refund(v.Version, 2500, "Agreed adjustment: late decoration", alloc(cake, 2000), alloc(delivery, 500))
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		subject string
		in      checkout.RefundInput
		check   func(error) bool
	}{
		"fulfillment": {fulfilment, in, func(err error) bool { return errors.Is(err, catalog.ErrForbidden) }},
		"support":     {support, in, func(err error) bool { return errors.Is(err, catalog.ErrForbidden) }},
		"stale":       {chef, refund(v.Version-1, 2500, "x", alloc(cake, 2000), alloc(delivery, 500)), func(err error) bool { return conflict(err) == "stale_version" }},
		"over paid":   {chef, refund(v.Version, 12001, "x", alloc(cake, 10001), alloc(delivery, 2000)), func(err error) bool { return conflict(err) == "refund_limit" }},
		"over room":   {chef, refund(v.Version, 2001, "x", alloc(delivery, 2001)), func(err error) bool { return conflict(err) == "refund_limit" }},
		"other order": {chef, refund(v.Version, 100, "x", alloc(otherColl, 100)), func(err error) bool { return invalidField(err) != "" }},
		"sum":         {chef, refund(v.Version, 2400, "x", alloc(cake, 2000), alloc(delivery, 500)), func(err error) bool { return invalidField(err) != "" }},
		"twice":       {chef, refund(v.Version, 200, "x", alloc(cake, 100), alloc(cake, 100)), func(err error) bool { return invalidField(err) != "" }},
		"euro": {chef, checkout.RefundInput{ExpectedVersion: v.Version, AmountMinor: 100, Currency: "EUR",
			Allocations: []checkout.RefundAllocation{alloc(cake, 100)}, Reason: "x"}, func(err error) bool { return invalidField(err) != "" }},
	} {
		if _, err := h.co.RecordRefund(h.ctx, c.subject, "bad-"+strings.ReplaceAll(name, " ", "-"), order, c.in); !c.check(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if h.count(t, "SELECT count(*) FROM business.refund_goal WHERE order_id = $1::uuid", order) != 0 {
		t.Fatal("a refused exception recorded a goal")
	}

	res, err := h.co.RecordRefund(h.ctx, chef, "ex1", order, in)
	if err != nil || res.Replayed {
		t.Fatalf("exception: %+v %v", res, err)
	}
	if again, err := h.co.RecordRefund(h.ctx, chef, "ex1", order, in); err != nil || !again.Replayed || again.ID != res.ID {
		t.Fatalf("replay %+v %v", again, err)
	}
	if _, err := h.co.RecordRefund(h.ctx, chef, "ex1", order, refund(v.Version, 100, "x", alloc(cake, 100))); conflict(err) != "key_reused" {
		t.Fatalf("key reused: %v", err)
	}
	v2, _ := h.cancelView(t, order, d)
	if v2.Version != v.Version+1 {
		t.Fatalf("order version %d, want %d", v2.Version, v.Version+1)
	}
	// While its refunds are unresolved nothing more is committed.
	if _, err := h.co.RecordRefund(h.ctx, chef, "ex-busy", order, refund(v2.Version, 100, "x", alloc(cake, 100))); conflict(err) != "refund_in_progress" {
		t.Fatalf("while in progress: %v", err)
	}
	var target, version int
	var reason string
	if err := h.admin.QueryRow(h.ctx, "SELECT target_minor, version, reason FROM business.refund_goal WHERE order_id = $1::uuid", order).Scan(
		&target, &version, &reason); err != nil || target != 2500 || version != 1 || reason != "chef_exception" {
		t.Fatalf("goal %d v%d %s %v", target, version, reason, err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("exception refunds:\n%s", reasons(errs))
	}
	got := h.sentAmounts()
	slices.Sort(got)
	if !slices.Equal(got, []string{"2000", "500"}) {
		t.Fatalf("sent %v, want 2000 and 500 once each", got)
	}
	if v, raw := h.cancelView(t, order, d); v.Refund == nil || v.Refund.State != "returned" || v.Refund.SucceededMinor != 2500 {
		t.Fatalf("returned %s", raw)
	}
	if h.auditCount(t, order, "refund.exception_recorded", res.ID) != 1 || h.auditCount(t, order, "refund.exception_recorded", "late decoration") != 0 {
		t.Fatal("exception audit (with its reason kept out)")
	}
	var stored string
	if err := h.admin.QueryRow(h.ctx, "SELECT reason FROM business.refund_exception WHERE id = $1::uuid", res.ID).Scan(&stored); err != nil ||
		stored != in.Reason {
		t.Fatalf("stored reason %q %v", stored, err)
	}

	// A second exception raises the goal from what was returned: 2500 + 7000.
	// Paid 12000 - returned 2500 bounds it (9501 is over the limit).
	v3, _ := h.cancelView(t, order, d)
	if _, err := h.co.RecordRefund(h.ctx, chef, "ex-over", order, refund(v3.Version, 9501, "x", alloc(cake, 8000), alloc(delivery, 1501))); conflict(err) != "refund_limit" {
		t.Fatalf("over the net limit: %v", err)
	}
	if _, err := h.co.RecordRefund(h.ctx, chef, "ex2", order, refund(v3.Version, 7000, "Goodwill", alloc(cake, 7000))); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("second exception:\n%s", reasons(errs))
	}
	if got := h.count(t, "SELECT target_minor FROM business.refund_goal WHERE order_id = $1::uuid", order); got != 9500 {
		t.Fatalf("goal %d, want 9500", got)
	}
	// The customer's later 50% (6000p) is already covered: only the policy
	// target rises (audited), no new refund.
	v4, _ := h.cancelView(t, order, d)
	h.now = cancelDeadline
	p := h.preview(t, order, d)
	if p.AmountMinor != 6000 || len(p.Allocations) != 0 || p.DeliveryPending != 0 {
		t.Fatalf("preview after exceptions %+v", p)
	}
	if _, err := h.cancel(order, d, "c", v4.Version, p); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("cancel:\n%s", reasons(errs))
	}
	if got := h.sentAmounts(); len(got) != 3 {
		t.Fatalf("sent %v after a covered cancellation", got)
	}
	if h.auditCount(t, order, "refund.policy_target_raised", `"policy_target_minor" : 6000`) != 1 {
		t.Fatal("policy target raised alone is not audited")
	}
	if v, raw := h.cancelView(t, order, d); v.Refund.State != "returned" || v.Refund.TargetMinor != 9500 || v.Refund.SucceededMinor != 9500 {
		t.Fatalf("after the cancellation %s", raw)
	}
}

// RECONCILIATION_REQUIRED: an unresolved quarantined observation or
// contradicting evidence anywhere on the order refuses both chef refund
// commands, records nothing and sends nothing. A restore freeze answers
// frozen for a new request but still replays a recorded one.
func TestChefRefundsWaitForReconciliationAndFreeze(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, coll, pi := h.paidCollection(t, f, pol, "rq")
	d := h.digestOf(t, order)
	delivery := h.seedDelivery(t, order, 2000)
	h.withRefunds()
	v, _ := h.cancelView(t, order, d)
	firstIn := refund(v.Version, 1000, "Agreed", alloc(coll, 1000))
	res, err := h.co.RecordRefund(h.ctx, chef, "first", order, firstIn)
	if err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("first:\n%s", reasons(errs))
	}
	v, _ = h.cancelView(t, order, d)

	h.owner(t, `INSERT INTO business.refund_observation (id, collection_id, provider_account_id, livemode, provider_refund_id,
		amount_minor, currency, status, quarantined, quarantine_reason, source)
		VALUES (gen_random_uuid(), $1::uuid, $2, false, 're_external_dashboard', 5000, 'gbp', 'succeeded', true, 'unmatched', 'reconcile')`,
		coll, stripeAccount)
	creates := len(h.stripe.refundCreates())
	ops := h.count(t, "SELECT count(*) FROM business.refund_operation WHERE order_id = $1::uuid", order)
	if _, err := h.co.RecordRefund(h.ctx, chef, "q", order, refund(v.Version, 500, "x", alloc(delivery, 500))); conflict(err) != "reconciliation_required" {
		t.Fatalf("exception under quarantine: %v", err)
	}
	if _, err := h.co.PayDeliveryRefund(h.ctx, chef, "q", order, checkout.DeliveryRefundInput{ExpectedVersion: v.Version, CollectionID: delivery}); conflict(err) != "reconciliation_required" {
		t.Fatalf("delivery under quarantine: %v", err)
	}
	h.owner(t, `UPDATE business.refund_observation SET resolved_at = now(), resolution_note = 'dashboard refund confirmed'
		WHERE collection_id = $1::uuid`, coll)
	// Contradicting evidence on a decided operation.
	var goal string
	if err := h.admin.QueryRow(h.ctx, "SELECT id::text FROM business.refund_goal WHERE order_id = $1::uuid", order).Scan(&goal); err != nil {
		t.Fatal(err)
	}
	h.owner(t, `UPDATE business.refund_operation SET mismatch_reason = 'stripe reported failed after success' WHERE order_id = $1::uuid`, order)
	if _, err := h.co.RecordRefund(h.ctx, chef, "m", order, refund(v.Version, 500, "x", alloc(delivery, 500))); conflict(err) != "reconciliation_required" {
		t.Fatalf("exception with a contradiction: %v", err)
	}
	h.owner(t, `UPDATE business.refund_operation SET mismatch_reason = NULL WHERE order_id = $1::uuid`, order)
	// An unknown refund past its window, waiting in reconcile.
	op := h.sentOperation(t, order, delivery, goal, pi, "unknown", 300)
	h.owner(t, `INSERT INTO business.recovery_operation (id, kind, aggregate_id, prior_version, payload_hash, envelope_key,
		replay_until, state, reconcile_reason, prepared_ack_at)
		VALUES ($1::uuid, 'dispatch', $1::uuid, 0, repeat('0', 64), 'recovery/test/op/' || $1, now(), 'reconcile',
		        'unknown past its window', now())`, op)
	if _, err := h.co.RecordRefund(h.ctx, chef, "u", order, refund(v.Version, 500, "x", alloc(coll, 500))); conflict(err) != "reconciliation_required" {
		t.Fatalf("exception with an unknown refund in reconcile: %v", err)
	}
	if len(h.stripe.refundCreates()) != creates ||
		h.count(t, "SELECT count(*) FROM business.refund_operation WHERE order_id = $1::uuid", order) != ops+1 {
		t.Fatal("a refused refund command reserved or sent money")
	}

	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if _, err := h.co.RecordRefund(h.ctx, chef, "fz", order, refund(v.Version, 500, "x", alloc(coll, 500))); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("frozen exception: %v", err)
	}
	if _, err := h.co.PayDeliveryRefund(h.ctx, chef, "fz", order, checkout.DeliveryRefundInput{ExpectedVersion: v.Version, CollectionID: delivery}); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("frozen delivery refund: %v", err)
	}
	if again, err := h.co.RecordRefund(h.ctx, chef, "first", order, firstIn); err != nil || !again.Replayed || again.ID != res.ID {
		t.Fatalf("replay during the freeze: %+v %v", again, err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
}

// K05: the delivery share of an ordinary 50% cancellation waits for the
// chef; the chef's delivery refund reserves exactly that share once (cake
// 9501p + delivery 2000p: 5751p = cake 4751p + delivery 1000p).
func TestChefPaysThePendingDeliveryShare(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d := h.paidOrder(t, f, pol)
	cake := h.payNow(t, order, d, "dv")
	delivery := h.seedDelivery(t, order, 2000)
	v, _ := h.cancelView(t, order, d)
	pay := func(subject, key string, version int, coll string) (catalog.Result, error) {
		return h.co.PayDeliveryRefund(h.ctx, subject, key, order, checkout.DeliveryRefundInput{ExpectedVersion: version, CollectionID: coll})
	}
	// Nothing is pending before a cancellation (no goal).
	if _, err := pay(chef, "early", v.Version, delivery); conflict(err) != "no_refund_pending" {
		t.Fatalf("before a cancellation: %v", err)
	}
	h.now = cancelDeadline
	p := h.preview(t, order, d)
	h.withRefunds()
	if _, err := h.cancel(order, d, "c", v.Version, p); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("cancel:\n%s", reasons(errs))
	}
	v2, raw := h.cancelView(t, order, d)
	if v2.Refund.DeliveryPending != 1000 || v2.Refund.State != "partly_returned" {
		t.Fatalf("pending %s", raw)
	}
	if _, err := pay(fulfilment, "f", v2.Version, delivery); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("fulfillment: %v", err)
	}
	if _, err := pay(chef, "s", v2.Version-1, delivery); conflict(err) != "stale_version" {
		t.Fatalf("stale: %v", err)
	}
	if _, err := pay(chef, "k", v2.Version, cake); conflict(err) != "state_conflict" {
		t.Fatalf("cake collection: %v", err)
	}
	if _, err := pay(chef, "o", v2.Version, payments.NewID()); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("unknown collection: %v", err)
	}
	res, err := pay(chef, "dp", v2.Version, delivery)
	if err != nil || res.Replayed {
		t.Fatalf("delivery refund %+v %v", res, err)
	}
	if again, err := pay(chef, "dp", v2.Version, delivery); err != nil || !again.Replayed || again.ID != res.ID {
		t.Fatalf("replay %+v %v", again, err)
	}
	v3, _ := h.cancelView(t, order, d)
	if v3.Version != v2.Version+1 {
		t.Fatalf("version %d", v3.Version)
	}
	if _, err := pay(chef, "dp2", v3.Version, delivery); conflict(err) != "refund_in_progress" {
		t.Fatalf("while in progress: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("delivery refund:\n%s", reasons(errs))
	}
	got := h.sentAmounts()
	if len(got) != 2 || got[1] != "1000" || h.stripe.refundForm(h.stripe.refundCreates()[1]).Get("metadata[refund_operation_id]") != res.ID {
		t.Fatalf("sent %v", got)
	}
	if v, raw := h.cancelView(t, order, d); v.Refund.State != "returned" || v.Refund.DeliveryPending != 0 || v.Refund.SucceededMinor != 5751 {
		t.Fatalf("returned %s", raw)
	}
	if _, err := pay(chef, "dp3", v3.Version, delivery); conflict(err) != "no_refund_pending" {
		t.Fatalf("nothing left: %v", err)
	}
	if h.count(t, "SELECT business.refund_reserve($1::uuid)", order) != 0 || len(h.stripe.refundCreates()) != 2 {
		t.Fatal("sent again")
	}
	if h.auditCount(t, order, "refund.delivery_paid", res.ID) != 1 {
		t.Fatal("delivery refund not audited")
	}
}
