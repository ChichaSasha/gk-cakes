//go:build integration

package checkout_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
)

// GK-026 B1 part 2: production start, late payment, K05 allocation and the
// AC2 accounting scenarios (migration 0022).

const (
	fulfilment = "00000000-0000-4000-8000-0000000006f1"
	support    = "00000000-0000-4000-8000-0000000006a1"
)

func (h *payHarness) productionJob(t *testing.T, order string) (string, time.Time) {
	t.Helper()
	var id string
	var at time.Time
	if err := h.admin.QueryRow(h.ctx, "SELECT job_id::text, scheduled_at FROM business.production_timer_job WHERE order_id = $1::uuid",
		order).Scan(&id, &at); err != nil {
		t.Fatalf("production job of %s: %v", order, err)
	}
	return id, at
}

// runProduction claims and runs the order's production timer now.
func (h *payHarness) runProduction(t *testing.T, order string) error {
	t.Helper()
	id, _ := h.productionJob(t, order)
	h.production = true
	defer func() { h.production = false }()
	job := h.claimOnly(t, id)
	err := h.productions.Handle(h.ctx, job)
	h.finish(t, job, err)
	return err
}

type transitionRow struct {
	Source                         string
	Scheduled, Effective, Observed time.Time
	Count, Audits                  int
}

func (h *payHarness) transition(t *testing.T, order string) transitionRow {
	t.Helper()
	var r transitionRow
	r.Count = h.count(t, "SELECT count(*) FROM business.production_transition WHERE order_id = $1::uuid", order)
	r.Audits = h.count(t, "SELECT count(*) FROM business.audit_event WHERE action = 'order.production_started' AND subject_id = $1::uuid", order)
	if r.Count == 1 {
		if err := h.admin.QueryRow(h.ctx, `SELECT source, scheduled_at, effective_at, observed_at
			FROM business.production_transition WHERE order_id = $1::uuid`, order).Scan(&r.Source, &r.Scheduled, &r.Effective, &r.Observed); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (h *payHarness) staffGrants(t *testing.T) {
	t.Helper()
	h.owner(t, "SELECT business.staff_grant($1::uuid, 'fulfillment', 'test')", fulfilment)
	h.owner(t, "SELECT business.staff_grant($1::uuid, 'technical_support', 'test')", support)
}

// Manual (fulfillment) then timer: one transition, effective at the
// receipt; the timer changes nothing. Start-first keeps the entitlement:
// 100% before the boundary, 50% at it.
func TestManualStartThenTimerIsOneTransition(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, _, _ := h.paidCollection(t, f, pol, "man")
	d := h.digestOf(t, order)
	v, _ := h.cancelView(t, order, d)
	if _, at := h.productionJob(t, order); !at.Equal(time.Date(2026, 11, 7, 10, 9, 0, 0, time.UTC)) {
		t.Fatalf("timer scheduled at %s, want paid_at + 48 h", at)
	}
	in := checkout.ProductionInput{ExpectedVersion: v.Version}
	if _, err := h.co.StartProduction(h.ctx, support, "s0", order, in); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("technical support: %v", err)
	}
	if _, err := h.co.StartProduction(h.ctx, fulfilment, "s0", order, checkout.ProductionInput{ExpectedVersion: v.Version + 1}); conflict(err) != "stale_version" {
		t.Fatalf("stale: %v", err)
	}
	h.now = h.now.Add(time.Hour)
	res, err := h.co.StartProduction(h.ctx, fulfilment, "s1", order, in)
	if err != nil || res.Replayed || res.ID != order {
		t.Fatalf("start: %+v %v", res, err)
	}
	if again, err := h.co.StartProduction(h.ctx, fulfilment, "s1", order, in); err != nil || !again.Replayed || again.Lifecycle != "in_production" {
		t.Fatalf("same key: %+v %v", again, err)
	}
	// During a restore freeze the same key still replays; a new one is refused.
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if again, err := h.co.StartProduction(h.ctx, fulfilment, "s1", order, in); err != nil || !again.Replayed {
		t.Fatalf("replay while frozen: %+v %v", again, err)
	}
	if _, err := h.co.StartProduction(h.ctx, fulfilment, "s9", order, in); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("new start while frozen: %v", err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	if again, err := h.co.StartProduction(h.ctx, chef, "s2", order, checkout.ProductionInput{ExpectedVersion: v.Version + 1}); err != nil || !again.Replayed {
		t.Fatalf("already started: %+v %v", again, err)
	}
	if err := h.runProduction(t, order); err != nil {
		t.Fatalf("timer: %v", err)
	}
	tr := h.transition(t, order)
	after, raw := h.cancelView(t, order, d)
	if tr.Count != 1 || tr.Audits != 1 || tr.Source != "manual" || !tr.Effective.Equal(h.now) || after.Lifecycle != "in_production" ||
		after.Version != v.Version+1 {
		t.Fatalf("transition %+v order %s", tr, raw)
	}
	if p := h.preview(t, order, d); p.Rate != 100 {
		t.Fatalf("in production before the boundary: %+v", p)
	}
	h.now = cancelDeadline
	p := h.preview(t, order, d)
	if p.Rate != 50 || p.AmountMinor != 5000 {
		t.Fatalf("in production at the boundary: %+v", p)
	}
	h.withRefunds()
	if _, err := h.cancel(order, d, "c1", after.Version, p); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("cancel:\n%s", reasons(errs))
	}
	if v, raw := h.cancelView(t, order, d); v.Lifecycle != "cancelled" || v.Refund.TargetMinor != 5000 || v.Refund.State != "returned" {
		t.Fatalf("cancelled %s", raw)
	}
}

// Timer then manual: the timer's transition is effective at the scheduled
// instant (observed separately); a later manual start is a no-op replay.
// Cancelling before the boundary still returns 100%.
func TestTimerThenManualStartIsANoOp(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, _, _ := h.paidCollection(t, f, pol, "tim")
	d := h.digestOf(t, order)
	v, _ := h.cancelView(t, order, d)
	// A restore freeze defers the timer before anything else.
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	var de *jobs.DeferError
	if err := h.runProduction(t, order); !errors.As(err, &de) || h.transition(t, order).Count != 0 {
		t.Fatalf("frozen timer: %v", err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	if err := h.runProduction(t, order); err != nil {
		t.Fatalf("timer: %v", err)
	}
	tr := h.transition(t, order)
	scheduled := time.Date(2026, 11, 7, 10, 9, 0, 0, time.UTC)
	if tr.Count != 1 || tr.Source != "timer" || !tr.Effective.Equal(scheduled) || !tr.Scheduled.Equal(scheduled) || tr.Observed.Equal(scheduled) {
		t.Fatalf("timer transition %+v", tr)
	}
	if res, err := h.co.StartProduction(h.ctx, chef, "late", order, checkout.ProductionInput{ExpectedVersion: v.Version}); err != nil || !res.Replayed {
		t.Fatalf("manual after the timer: %+v %v", res, err)
	}
	if jobID, _ := h.productionJob(t, order); h.count(t, "SELECT count(*) FROM business.job WHERE id = $1::uuid AND state = 'completed'", jobID) != 1 {
		t.Fatal("the timer job did not complete")
	}
	if tr := h.transition(t, order); tr.Count != 1 || tr.Audits != 1 {
		t.Fatalf("after replays %+v", tr)
	}
	after, _ := h.cancelView(t, order, d)
	if after.Lifecycle != "in_production" || after.Version != v.Version+1 {
		t.Fatalf("order %+v", after)
	}
	p := h.preview(t, order, d)
	if p.Rate != 100 || p.AmountMinor != 10000 {
		t.Fatalf("preview %+v", p)
	}
}

// Cancel-first at exactly the boundary: the timer (late) waits while the
// cancellation is unresolved, then finds the order cancelled; the
// entitlement stays 50% and nothing was started. A manual start meanwhile
// conflicts.
func TestCancelFirstAtTheBoundaryKeepsFiftyPercent(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, coll, _ := h.paidCollection(t, f, pol, "cf")
	d := h.digestOf(t, order)
	v, _ := h.cancelView(t, order, d)
	h.now = cancelDeadline
	p := h.preview(t, order, d)
	if _, err := h.cancel(order, d, "c1", v.Version, p); err != nil {
		t.Fatal(err)
	}
	var de *jobs.DeferError
	if err := h.runProduction(t, order); !errors.As(err, &de) {
		t.Fatalf("timer while the cancellation is pending: %v, want a deferral", err)
	}
	if _, err := h.co.StartProduction(h.ctx, chef, "s1", order, checkout.ProductionInput{ExpectedVersion: v.Version}); conflict(err) != "state_conflict" {
		t.Fatalf("manual start while the cancellation is pending: %v", err)
	}
	h.withRefunds()
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("cancel:\n%s", reasons(errs))
	}
	if err := h.runProduction(t, order); err != nil {
		t.Fatalf("timer after the cancellation: %v", err)
	}
	v2, raw := h.cancelView(t, order, d)
	if v2.Lifecycle != "cancelled" || *v2.Cancellation.Rate != 50 || v2.Refund.TargetMinor != 5000 || h.transition(t, order).Count != 0 {
		t.Fatalf("after %s", raw)
	}
	if ops := h.operations(t, coll); len(ops) != 1 || ops[0].State != "succeeded" || len(h.stripe.refundCreates()) != 1 {
		t.Fatalf("operations %+v", ops)
	}
}

// lateOrder places and pays a 10000p order after the lead time: it awaits
// the chef's agreement (K04 Q2).
func (h *payHarness) lateOrder(t *testing.T, f fixture, pol, key string) (string, []byte) {
	t.Helper()
	order, d := h.tenThousandOrder(t, f, pol, key)
	coll, session := h.started(t, order, d)
	h.stripe.pay(session, time.Date(2026, 11, 6, 12, 30, 1, 0, time.UTC))
	h.deliver(t, "evt_late_"+key, "checkout.session.completed", "checkout.session", session, coll, order)
	if got := attention(h.drain(t)); len(got) != 1 || !strings.Contains(got[0], "paid_awaiting_agreement") {
		t.Fatalf("late payment: %v", got)
	}
	return order, d
}

func TestLatePaymentHonoured(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d := h.lateOrder(t, f, pol, "hon")
	v, raw := h.cancelView(t, order, d)
	if v.Lifecycle != "paid_awaiting_agreement" {
		t.Fatalf("order %s", raw)
	}
	// Paid at 2026-11-06 12:30:01Z; the chef decides half an hour later.
	h.now = time.Date(2026, 11, 6, 13, 0, 0, 0, time.UTC)
	agreement := &checkout.Agreement{Channel: "phone", AgreedAt: "2026-11-06T12:45:00Z", EvidenceRef: "Call note 12"}
	honour := checkout.LatePaymentInput{ExpectedVersion: v.Version, Decision: "honour", CustomerAgreement: agreement}
	for name, in := range map[string]checkout.LatePaymentInput{
		"no agreement": {ExpectedVersion: v.Version, Decision: "honour"},
		"no channel":   {ExpectedVersion: v.Version, Decision: "honour", CustomerAgreement: &checkout.Agreement{AgreedAt: agreement.AgreedAt, EvidenceRef: "x"}},
		"no evidence":  {ExpectedVersion: v.Version, Decision: "honour", CustomerAgreement: &checkout.Agreement{Channel: "phone", AgreedAt: agreement.AgreedAt}},
		"half":         {ExpectedVersion: v.Version, Decision: "half_return"},
	} {
		if err := in.Validate(); err == nil {
			t.Errorf("%s: valid", name)
		}
	}
	// The database refuses what the API checks too, and a future agreement.
	future := checkout.LatePaymentInput{ExpectedVersion: v.Version, Decision: "honour",
		CustomerAgreement: &checkout.Agreement{Channel: "phone", AgreedAt: "2026-11-06T13:01:00Z", EvidenceRef: "x"}}
	if _, err := h.co.LatePayment(h.ctx, chef, "f", order, future); !strings.HasPrefix(invalidField(err), "customer_agreement:") {
		t.Fatalf("future agreement: %v", err)
	}
	before := checkout.LatePaymentInput{ExpectedVersion: v.Version, Decision: "honour",
		CustomerAgreement: &checkout.Agreement{Channel: "phone", AgreedAt: "2026-11-06T12:00:00Z", EvidenceRef: "x"}}
	if _, err := h.co.LatePayment(h.ctx, chef, "b", order, before); !strings.HasPrefix(invalidField(err), "customer_agreement:") {
		t.Fatalf("agreement before the payment: %v", err)
	}
	h.now = time.Date(2026, 11, 14, 12, 30, 0, 0, time.UTC) // the original handover
	if _, err := h.co.LatePayment(h.ctx, chef, "l", order, honour); conflict(err) != "honour_too_late" {
		t.Fatalf("honour at the handover: %v", err)
	}
	h.now = time.Date(2026, 11, 6, 13, 0, 0, 0, time.UTC)
	if _, err := h.co.LatePayment(h.ctx, chef, "n", order, checkout.LatePaymentInput{ExpectedVersion: v.Version, Decision: "honour"}); !strings.HasPrefix(invalidField(err), "customer_agreement:") {
		t.Fatalf("no agreement at the database: %v", err)
	}
	if _, err := h.co.LatePayment(h.ctx, fulfilment, "x", order, honour); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("fulfillment: %v", err)
	}
	res, err := h.co.LatePayment(h.ctx, chef, "lp1", order, honour)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := h.co.LatePayment(h.ctx, chef, "lp1", order, honour); err != nil || !again.Replayed || again.ID != res.ID {
		t.Fatalf("replay %+v %v", again, err)
	}
	if _, err := h.co.LatePayment(h.ctx, chef, "lp2", order, honour); conflict(err) != "state_conflict" {
		t.Fatalf("second decision while pending: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("decision:\n%s", reasons(errs))
	}
	v2, raw := h.cancelView(t, order, d)
	if v2.Lifecycle != "confirmed" || v2.Version != v.Version+1 || v2.Refund != nil {
		t.Fatalf("honoured %s", raw)
	}
	if got := h.allocation(t, order); got != "committed" {
		t.Fatalf("day %s", got)
	}
	if _, at := h.productionJob(t, order); !at.Equal(time.Date(2026, 11, 8, 12, 30, 1, 0, time.UTC)) {
		t.Fatalf("production scheduled at %s, want paid_at + 48 h", at)
	}
	var channel string
	if err := h.admin.QueryRow(h.ctx, "SELECT channel FROM business.late_payment_decision WHERE command_id = $1::uuid", res.ID).Scan(&channel); err != nil || channel != "phone" {
		t.Fatalf("decision %q %v", channel, err)
	}
	if strings.Contains(h.logs.String(), "Call note") {
		t.Fatal("evidence reference in the logs")
	}
	if _, err := h.co.LatePayment(h.ctx, chef, "lp3", order, checkout.LatePaymentInput{ExpectedVersion: v2.Version, Decision: "full_return"}); conflict(err) != "state_conflict" {
		t.Fatalf("decision on a confirmed order: %v", err)
	}
}

// Full return: 100% of the actual paid aggregate even after the boundary
// (never 50%), cancel_reason late_payment_returned, one refund.
func TestLatePaymentFullReturn(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.lateOrder(t, f, pol, "ret")
	v, _ := h.cancelView(t, order, d)
	h.now = cancelDeadline.Add(time.Hour)
	in := checkout.LatePaymentInput{ExpectedVersion: v.Version, Decision: "full_return"}
	if err := (checkout.LatePaymentInput{ExpectedVersion: 1, Decision: "full_return",
		CustomerAgreement: &checkout.Agreement{Channel: "phone"}}).Validate(); err == nil {
		t.Fatal("an agreement with full_return is valid")
	}
	h.withRefunds()
	if _, err := h.co.LatePayment(h.ctx, chef, "r1", order, in); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("return:\n%s", reasons(errs))
	}
	v2, raw := h.cancelView(t, order, d)
	if v2.Lifecycle != "cancelled" || *v2.CancelReason != "late_payment_returned" || v2.Refund == nil ||
		v2.Refund.TargetMinor != 10000 || v2.Refund.State != "returned" || len(h.stripe.refundCreates()) != 1 ||
		v2.Cancellation == nil || v2.Cancellation.State != "cancelled" || *v2.Cancellation.Reason != "late_payment_returned" ||
		*v2.Cancellation.Rate != 100 || v2.Cancellation.RequestedBy != "chef" {
		t.Fatalf("full return %s", raw)
	}
	if h.allocation(t, order) != "released" {
		t.Fatal("day not released")
	}
}

// AC2: a raised goal while an operation is unresolved sends nothing; the
// definitive outcome then reserves and sends the whole remainder once.
func TestRaisedGoalWaitsForTheDefinitiveOutcome(t *testing.T) {
	type prior struct {
		state  string
		amount int64
	}
	for name, c := range map[string]struct {
		goal     int64
		priors   []prior
		settle   string
		wantSent int64
	}{
		"unknown then success": {5000, []prior{{"succeeded", 2000}, {"unknown", 3000}}, "succeeded", 5000},
		"unknown then failure": {5000, []prior{{"succeeded", 2000}, {"unknown", 3000}}, "failed", 8000},
		"pending then success": {4000, []prior{{"pending", 4000}}, "succeeded", 6000},
		"pending then failure": {4000, []prior{{"pending", 4000}}, "failed", 10000},
	} {
		t.Run(name, func(t *testing.T) {
			h := newPayHarness(t)
			f := h.setup(t)
			pol := h.policy(t, "p1")
			order, coll, pi := h.paidCollection(t, f, pol, "ac2")
			d := h.digestOf(t, order)
			goal := payments.NewID()
			h.owner(t, `INSERT INTO business.refund_goal (id, order_id, target_minor, reason, source)
				VALUES ($1::uuid, $2::uuid, $3, 'late_capture_terminal', 'system')`, goal, order, c.goal)
			var open string
			for _, p := range c.priors {
				op := h.sentOperation(t, order, coll, goal, pi, p.state, p.amount)
				if p.state != "succeeded" {
					open = op
				}
			}
			v, _ := h.cancelView(t, order, d)
			h.withRefunds()
			if _, err := h.co.ChefCancel(h.ctx, chef, "cc", order, checkout.ChefCancelInput{ExpectedVersion: v.Version, Reason: "Kitchen closed"}); err != nil {
				t.Fatal(err)
			}
			if errs := h.drain(t); len(errs) != 0 {
				t.Fatalf("cancel:\n%s", reasons(errs))
			}
			if got := h.count(t, "SELECT target_minor FROM business.refund_goal WHERE id = $1::uuid", goal); got != 10000 ||
				len(h.stripe.refundCreates()) != 0 || len(h.operations(t, coll)) != len(c.priors) {
				t.Fatalf("raised goal %d sent %v ops %+v", got, h.stripe.refundCreates(), h.operations(t, coll))
			}
			refundID := "NULL"
			if c.settle == "succeeded" {
				refundID = "'re_local_settled'"
			}
			h.owner(t, `UPDATE business.refund_operation SET state = $2, provider_refund_id = `+refundID+`, settled_at = now()
				WHERE id = $1::uuid`, open, c.settle)
			h.due(t)
			if errs := h.drain(t); len(errs) != 0 {
				t.Fatalf("after the outcome:\n%s", reasons(errs))
			}
			creates := h.stripe.refundCreates()
			if len(creates) != 1 || h.stripe.refundForm(creates[0]).Get("amount") != strconv.FormatInt(c.wantSent, 10) {
				t.Fatalf("sent %v (%v), want one of %d", creates, h.stripe.refundForm(creates[0]), c.wantSent)
			}
			if h.count(t, "SELECT business.refund_reserve($1::uuid)", order) != 0 || len(h.stripe.refundCreates()) != 1 {
				t.Fatal("sent again")
			}
		})
	}
}

// seedDelivery records a succeeded 2000p GK delivery collection on the
// order (no delivery checkout exists yet) and its captured payment in the
// fake Stripe.
func (h *payHarness) seedDelivery(t *testing.T, order string, amount int64) string {
	t.Helper()
	id := payments.NewID()
	pi := "pi_fake_delivery_" + strings.ReplaceAll(id, "-", "")[:12]
	h.owner(t, `INSERT INTO business.payment_collection (id, order_id, kind, state, amount_minor, environment, provider_account_id,
		livemode, actor_kind, guest_capability_id, request_key, request_sha256, return_origin, initiated_at, payment_intent_id,
		succeeded_at, observed_at)
		SELECT $1::uuid, $2::uuid, 'delivery', 'succeeded', $3, 'test', $4, false, 'guest', g.id, 'delivery-seed', repeat('0', 64),
		       $5, now(), $6, now(), now()
		FROM business.order_guest_capability g WHERE g.order_id = $2::uuid ORDER BY g.created_at LIMIT 1`,
		id, order, amount, stripeAccount, uiOrigin, pi)
	h.stripe.mu.Lock()
	h.stripe.sessions["cs_"+id] = &fakeSession{id: "cs_" + id, amount: amount, pi: map[string]any{"id": pi, "status": "succeeded"}}
	h.stripe.mu.Unlock()
	return id
}

// K05 allocation across cake 9501p and delivery 2000p at 50%: 5751p
// (half-up), cake 4751p + delivery 1000p (floor + largest remainder). The
// cake share is sent; the delivery share is a visible obligation pending the
// chef (K05: delivery refunds are chef-only), never sent automatically.
func TestFiftyPercentIsAllocatedAcrossCakeAndDelivery(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	cake := h.payNow(t, order, d, "al")
	delivery := h.seedDelivery(t, order, 2000)
	v, _ := h.cancelView(t, order, d)
	h.now = cancelDeadline
	p := h.preview(t, order, d)
	if p.PaidMinor != 11501 || p.AmountMinor != 5751 || len(p.Allocations) != 1 || p.Allocations[0].CollectionID != cake ||
		p.Allocations[0].AmountMinor != 4751 || p.DeliveryPending != 1000 {
		t.Fatalf("preview %+v", p)
	}
	// The same preview again (same actor, version and amounts) is reused.
	if again := h.preview(t, order, d); again.PreviewID != p.PreviewID || again.PreviewHash != p.PreviewHash ||
		h.count(t, "SELECT count(*) FROM business.cancellation_preview WHERE order_id = $1::uuid", order) != 1 {
		t.Fatalf("second preview %+v", again)
	}
	h.withRefunds()
	if _, err := h.cancel(order, d, "c", v.Version, p); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("cancel:\n%s", reasons(errs))
	}
	creates := h.stripe.refundCreates()
	if len(creates) != 1 || h.stripe.refundForm(creates[0]).Get("amount") != "4751" || len(h.operations(t, delivery)) != 0 {
		t.Fatalf("creates %v", creates)
	}
	if v, raw := h.cancelView(t, order, d); v.Refund.State != "partly_returned" || v.Refund.SucceededMinor != 4751 ||
		v.Refund.TargetMinor != 5751 || v.Refund.DeliveryPending != 1000 {
		t.Fatalf("delivery pending %s", raw)
	}
}

// A recorded delivery failure floor and the ordinary 50% policy combine the
// same in either order: cake 10000p + delivery 2000p gives goal 7000p (cake
// 5000p + delivery 2000p), each sent once.
func TestDeliveryFloorAndPolicyCombineInEitherOrder(t *testing.T) {
	for _, floorFirst := range []bool{true, false} {
		name := "cancel then floor"
		if floorFirst {
			name = "floor then cancel"
		}
		t.Run(name, func(t *testing.T) {
			h := newPayHarness(t)
			f := h.setup(t)
			pol := h.policy(t, "p1")
			order, d := h.tenThousandOrder(t, f, pol, "fo")
			cake := h.payNow(t, order, d, "fo")
			delivery := h.seedDelivery(t, order, 2000)
			h.withRefunds()
			floor := func() {
				if _, err := h.co.RecordDeliveryFailure(h.ctx, chef, "df", order, delivery); err != nil {
					t.Fatal(err)
				}
				if errs := h.drain(t); len(errs) != 0 {
					t.Fatalf("floor:\n%s", reasons(errs))
				}
			}
			if floorFirst {
				floor()
			}
			v, _ := h.cancelView(t, order, d)
			h.now = cancelDeadline
			p := h.preview(t, order, d)
			if p.Rate != 50 || p.AmountMinor != 6000 {
				t.Fatalf("preview %+v", p)
			}
			if _, err := h.cancel(order, d, "c", v.Version, p); err != nil {
				t.Fatal(err)
			}
			if errs := h.drain(t); len(errs) != 0 {
				t.Fatalf("cancel:\n%s", reasons(errs))
			}
			if !floorFirst {
				floor()
			}
			if got := h.count(t, "SELECT target_minor FROM business.refund_goal WHERE order_id = $1::uuid", order); got != 7000 {
				t.Fatalf("goal %d, want 7000", got)
			}
			if b := h.balance(t, cake); b.Succeeded != 5000 {
				t.Fatalf("cake %+v", b)
			}
			if b := h.balance(t, delivery); b.Succeeded != 2000 {
				t.Fatalf("delivery %+v", b)
			}
			if len(h.stripe.refundCreates()) != 2 || h.count(t, "SELECT business.refund_reserve($1::uuid)", order) != 0 {
				t.Fatalf("creates %v", h.stripe.refundCreates())
			}
			if v, raw := h.cancelView(t, order, d); v.Refund.State != "returned" || v.Refund.DeliveryPending != 0 {
				t.Fatalf("returned %s", raw)
			}
		})
	}
}

// Cake 10000p + delivery 2000p, prior cake return 6000p: the ordinary 50%
// goal (6000p) sends nothing more (no delivery 1000p); a GK-caused failed
// delivery floor 2000p raises the combined goal to 8000p and sends the
// delivery 2000p once.
func TestDeliveryFailureFloorRaisesTheCombinedGoal(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.tenThousandOrder(t, f, pol, "dl")
	cake := h.payNow(t, order, d, "dl")
	delivery := h.seedDelivery(t, order, 2000)
	var pi string
	if err := h.admin.QueryRow(h.ctx, "SELECT payment_intent_id FROM business.payment_collection WHERE id = $1::uuid", cake).Scan(&pi); err != nil {
		t.Fatal(err)
	}
	goal := payments.NewID()
	h.owner(t, `INSERT INTO business.refund_goal (id, order_id, target_minor, reason, source)
		VALUES ($1::uuid, $2::uuid, 6000, 'late_capture_terminal', 'system')`, goal, order)
	h.sentOperation(t, order, cake, goal, pi, "succeeded", 6000)
	v, _ := h.cancelView(t, order, d)
	h.now = cancelDeadline
	p := h.preview(t, order, d)
	if p.Rate != 50 || p.PaidMinor != 12000 || p.AmountMinor != 6000 || len(p.Allocations) != 0 {
		t.Fatalf("preview %+v", p)
	}
	h.withRefunds()
	if _, err := h.cancel(order, d, "c", v.Version, p); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("cancel:\n%s", reasons(errs))
	}
	if len(h.stripe.refundCreates()) != 0 || len(h.operations(t, delivery)) != 0 {
		t.Fatalf("ordinary 50%% sent %v", h.stripe.refundCreates())
	}
	// The floor: chef only, a delivery collection only.
	if _, err := h.co.RecordDeliveryFailure(h.ctx, fulfilment, "df", order, delivery); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("not chef: %v", err)
	}
	if _, err := h.co.RecordDeliveryFailure(h.ctx, chef, "dc", order, cake); conflict(err) != "state_conflict" {
		t.Fatalf("cake collection: %v", err)
	}
	res, err := h.co.RecordDeliveryFailure(h.ctx, chef, "df", order, delivery)
	if err != nil || res.Replayed {
		t.Fatalf("floor: %+v %v", res, err)
	}
	if again, err := h.co.RecordDeliveryFailure(h.ctx, chef, "df", order, delivery); err != nil || !again.Replayed || again.ID != res.ID {
		t.Fatalf("replay %+v %v", again, err)
	}
	if got := h.count(t, "SELECT target_minor FROM business.refund_goal WHERE order_id = $1::uuid", order); got != 8000 {
		t.Fatalf("combined goal %d, want 8000", got)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("delivery return:\n%s", reasons(errs))
	}
	creates := h.stripe.refundCreates()
	if len(creates) != 1 || h.stripe.refundForm(creates[0]).Get("amount") != "2000" ||
		!strings.HasPrefix(h.stripe.refundForm(creates[0]).Get("payment_intent"), "pi_fake_delivery_") {
		t.Fatalf("creates %v", creates)
	}
	if h.count(t, "SELECT business.refund_reserve($1::uuid)", order) != 0 || len(h.stripe.refundCreates()) != 1 {
		t.Fatal("sent again")
	}
	if v, raw := h.cancelView(t, order, d); v.Refund.TargetMinor != 8000 || v.Refund.SucceededMinor != 8000 || v.Refund.State != "returned" {
		t.Fatalf("returned %s", raw)
	}
}
