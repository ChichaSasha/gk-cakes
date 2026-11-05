//go:build integration

package checkout_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
)

// GK-031 B1 part 2: fulfilment transitions (migration 0032; contracts r6.5
// K05, K06, K00, K11; BO-GK-003.no_show).

type fulfilView struct {
	Version     int
	Lifecycle   string
	Fulfillment struct {
		State   *string
		At      *time.Time
		Pending *string
	}
	Refund *refundView
}

func (h *payHarness) fulfilView(t *testing.T, order string, d []byte) (fulfilView, string) {
	t.Helper()
	raw, err := h.co.GetOrder(h.ctx, order, "", d)
	if err != nil {
		t.Fatal(err)
	}
	var v fulfilView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v, string(raw)
}

func (h *payHarness) fulfil(subject, key, order, action string, version int, reason ...string) (checkout.Transitioned, error) {
	in := checkout.TransitionInput{Action: action, ExpectedVersion: version}
	if len(reason) == 1 {
		in.Reason = &reason[0]
	}
	if err := in.Validate(); err != nil {
		return checkout.Transitioned{}, err
	}
	return h.co.Transition(h.ctx, subject, key, order, in)
}

func state(v fulfilView) string {
	if v.Fulfillment.State == nil {
		return "<nil>"
	}
	return *v.Fulfillment.State
}

// readyOrder pays a 10000p collection order and moves it to ready
// (production started by the chef, ready by fulfilment).
func (h *payHarness) readyOrder(t *testing.T, f fixture, pol, key string) (string, []byte, string) {
	t.Helper()
	order, coll, _ := h.paidCollection(t, f, pol, key)
	d := h.digestOf(t, order)
	v, _ := h.fulfilView(t, order, d)
	if _, err := h.co.StartProduction(h.ctx, chef, "p-"+key, order, checkout.ProductionInput{ExpectedVersion: v.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.fulfil(fulfilment, "r-"+key, order, checkout.ActionReady, v.Version+1); err != nil {
		t.Fatalf("ready: %v", err)
	}
	return order, d, coll
}

// AC3: fulfilment can make an order ready, record an exception and hand it
// over; it cannot refund, record a no-show, change roles or amend; the
// handover is a K11 command shown only once secured; nothing is rewound.
func TestFulfilmentStepsAndHandover(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	h.withNotices(t)
	order, coll, _ := h.paidCollection(t, f, pol, "ful")
	d := h.digestOf(t, order)
	v, raw := h.fulfilView(t, order, d)
	if state(v) != "awaiting_handover" || v.Fulfillment.At != nil || v.Fulfillment.Pending != nil {
		t.Fatalf("confirmed order %s", raw)
	}
	if _, err := h.fulfil(fulfilment, "r0", order, checkout.ActionReady, v.Version); conflict(err) != "transition_not_allowed" {
		t.Fatalf("ready before production: %v", err)
	}
	if _, err := h.co.StartProduction(h.ctx, fulfilment, "p1", order, checkout.ProductionInput{ExpectedVersion: v.Version}); err != nil {
		t.Fatal(err)
	}
	v, _ = h.fulfilView(t, order, d)
	if _, err := h.fulfil(support, "r1", order, checkout.ActionReady, v.Version); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("technical support: %v", err)
	}
	if _, err := h.fulfil(fulfilment, "r1", order, checkout.ActionReady, v.Version+1); conflict(err) != "stale_version" {
		t.Fatalf("stale ready: %v", err)
	}
	h.now = h.now.Add(time.Hour)
	res, err := h.fulfil(fulfilment, "r1", order, checkout.ActionReady, v.Version)
	if err != nil || res.Replayed || res.Lifecycle != "ready" || res.FulfillmentState != "awaiting_handover" {
		t.Fatalf("ready: %+v %v", res, err)
	}
	if again, err := h.fulfil(fulfilment, "r1", order, checkout.ActionReady, v.Version); err != nil || !again.Replayed {
		t.Fatalf("same key: %+v %v", again, err)
	}
	if again, err := h.fulfil(chef, "r2", order, checkout.ActionReady, v.Version); err != nil || !again.Replayed || again.Lifecycle != "ready" {
		t.Fatalf("already ready: %+v %v", again, err)
	}
	if n := h.count(t, "SELECT count(*) FROM business.fulfillment_event WHERE order_id = $1::uuid AND action = 'ready'", order); n != 1 {
		t.Fatalf("%d ready events", n)
	}
	v, raw = h.fulfilView(t, order, d)
	if v.Lifecycle != "ready" || state(v) != "awaiting_handover" || v.Fulfillment.At == nil || !v.Fulfillment.At.Equal(h.now) {
		t.Fatalf("ready order %s", raw)
	}
	if _, err := h.fulfil(fulfilment, "x", order, checkout.ActionDispatch, v.Version); conflict(err) != "transition_not_allowed" {
		t.Fatalf("dispatching a collection: %v", err)
	}

	// Money and roles are not fulfilment's (K00).
	if _, err := h.fulfil(fulfilment, "ns", order, checkout.ActionNoShow, v.Version); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("fulfilment no-show: %v", err)
	}
	if _, err := h.co.RecordRefund(h.ctx, fulfilment, "rf", order, checkout.RefundInput{ExpectedVersion: v.Version, AmountMinor: 100,
		Currency: "GBP", Allocations: []checkout.RefundAllocation{{CollectionID: coll, AmountMinor: 100}}, Reason: "goodwill"}); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("fulfilment refund: %v", err)
	}
	if _, err := h.co.ChefCancel(h.ctx, fulfilment, "cc", order, checkout.ChefCancelInput{ExpectedVersion: v.Version, Reason: "x"}); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("fulfilment cancel: %v", err)
	}
	if _, err := h.api.Exec(h.ctx, "SELECT business.staff_grant($1::uuid, 'chef', 'escalation')", fulfilment); err == nil {
		t.Fatal("the API role can grant capabilities")
	}

	// A missed collection is recorded as an exception (actionable, not
	// completed, no refund).
	exc, err := h.fulfil(fulfilment, "e1", order, checkout.ActionDeliveryException, v.Version, "Customer did not arrive at 12:30")
	if err != nil || exc.FulfillmentState != "exception" || exc.Lifecycle != "ready" {
		t.Fatalf("exception: %+v %v", exc, err)
	}
	if h.count(t, "SELECT count(*) FROM business.refund_goal WHERE order_id = $1::uuid", order) != 0 {
		t.Fatal("an exception created a refund goal")
	}

	// Handover (K11).
	v, _ = h.fulfilView(t, order, d)
	h.now = time.Date(2026, 11, 14, 12, 40, 0, 0, time.UTC)
	hand, err := h.fulfil(fulfilment, "h1", order, checkout.ActionHandover, v.Version)
	if err != nil || hand.Replayed {
		t.Fatalf("handover: %+v %v", hand, err)
	}
	op := hand.ID
	if again, err := h.fulfil(fulfilment, "h1", order, checkout.ActionHandover, v.Version); err != nil || !again.Replayed || again.ID != op {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, err := h.fulfil(chef, "h2", order, checkout.ActionHandover, v.Version); conflict(err) != "stale_version" {
		t.Fatalf("a second handover while one is pending: %v", err)
	}
	if _, err := h.fulfil(fulfilment, "e2", order, checkout.ActionDeliveryException, v.Version, "late"); conflict(err) != "state_conflict" {
		t.Fatalf("a plain step while the handover is pending: %v", err)
	}
	h.journal.Fail = func(op, name string) error {
		if op == "put" && strings.HasSuffix(name, ".outcome") {
			return errors.New("b2 unavailable")
		}
		return nil
	}
	if err := h.moveJob(t, op); err == nil {
		t.Fatal("secured without the outcome envelope")
	}
	if row := h.orderRow(t, order); h.recoveryState(t, op) != "committed" || row.Lifecycle != "completed" {
		t.Fatalf("committed: %s %+v", h.recoveryState(t, op), row)
	}
	if v, raw := h.fulfilView(t, order, d); v.Lifecycle != "ready" || state(v) != "exception" || v.Fulfillment.Pending == nil ||
		*v.Fulfillment.Pending != "handover" {
		t.Fatalf("committed but unsecured shown as %s", raw)
	}
	h.journal.Fail = nil
	h.due(t)
	if err := h.moveJob(t, op); err != nil {
		t.Fatalf("secure: %v", err)
	}
	v, raw = h.fulfilView(t, order, d)
	if v.Lifecycle != "completed" || state(v) != "completed" || v.Fulfillment.Pending != nil || !v.Fulfillment.At.Equal(h.now) {
		t.Fatalf("handed over %s", raw)
	}
	key, _ := h.coord.EnvelopeKey(recovery.Operation{ID: op, AggregateID: order, PriorVersion: v.Version - 1})
	if h.journal.Puts(key+".prepared") != 1 || h.journal.Puts(key+".outcome") != 1 {
		t.Fatalf("envelopes %d/%d", h.journal.Puts(key+".prepared"), h.journal.Puts(key+".outcome"))
	}
	var actor string
	if err := h.admin.QueryRow(h.ctx, `SELECT a.provider_subject::text FROM business.fulfillment_event e
		JOIN business.account a ON a.id = e.actor_account_id WHERE e.command_id = $1::uuid`, op).Scan(&actor); err != nil || actor != fulfilment {
		t.Fatalf("handover actor %s %v", actor, err)
	}
	// Never rewound: no step, cancellation or move after the handover; no
	// handover email.
	if _, err := h.fulfil(chef, "after-r", order, checkout.ActionReady, v.Version); conflict(err) != "transition_not_allowed" {
		t.Fatalf("ready after the handover: %v", err)
	}
	if _, err := h.fulfil(chef, "after-e", order, checkout.ActionDeliveryException, v.Version, "x"); conflict(err) != "transition_not_allowed" {
		t.Fatalf("exception after the handover: %v", err)
	}
	if _, err := h.fulfil(chef, "after-h", order, checkout.ActionHandover, v.Version); conflict(err) != "transition_not_allowed" {
		t.Fatalf("second handover: %v", err)
	}
	if _, err := h.co.PreviewCancellation(h.ctx, order, "", d); conflict(err) != "cancellation_not_eligible" {
		t.Fatalf("cancelling a completed order: %v", err)
	}
	if strings.Contains(kinds(h.notices(t, order)), "handover") || len(h.notices(t, order)) != 1 {
		t.Fatalf("notices %s", kinds(h.notices(t, order)))
	}
	// Staff history: reasons for chef and fulfilment only.
	for who, wantReason := range map[string]bool{chef: true, fulfilment: true, support: false} {
		raw, err := h.co.StaffOrder(h.ctx, who, order)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		_ = json.Unmarshal(raw, &m)
		hist := string(m["fulfillment_history"])
		has(t, who+" history", hist, `"action": "ready"`, `"action": "delivery_exception"`, `"action": "handover"`, `"to": "completed"`)
		if strings.Contains(hist, "Customer did not arrive") != wantReason {
			t.Errorf("%s history reasons: %s", who, hist)
		}
		if !strings.Contains(string(m["fulfillment"]), `"state": "completed"`) {
			t.Errorf("%s fulfilment block %s", who, m["fulfillment"])
		}
	}
	h.noPersonalDataInLogs(t)
}

// No-show (K05 50%, chef only): not before the appointment starts; while it
// is not secured the order reads ready with no refund block and no refund
// email, even if the refund already came back; once secured, one no_show
// notice and then the refund_returned one.
func TestNoShowIsTheChefsAndReturnsHalf(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	h.withRefunds()
	h.withNotices(t)
	order, d, coll := h.readyOrder(t, f, pol, "ns")
	v, _ := h.fulfilView(t, order, d)
	if _, err := h.fulfil(chef, "n0", order, checkout.ActionNoShow, v.Version); conflict(err) != "handover_not_due" {
		t.Fatalf("no-show before the appointment: %v", err)
	}
	h.now = at("12:30") // the appointment start: equality allowed
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if _, err := h.fulfil(chef, "n1", order, checkout.ActionNoShow, v.Version); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("no-show while frozen: %v", err)
	}
	if _, err := h.fulfil(fulfilment, "f1", order, checkout.ActionDeliveryException, v.Version, "absent"); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("plain step while frozen: %v", err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	res, err := h.fulfil(chef, "n1", order, checkout.ActionNoShow, v.Version)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := h.fulfil(chef, "n1", order, checkout.ActionNoShow, v.Version); err != nil || !again.Replayed || again.ID != res.ID {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, err := h.fulfil(chef, "n1", order, checkout.ActionHandover, v.Version); conflict(err) != "stale_version" {
		t.Fatalf("handover while the no-show is pending: %v", err)
	}
	// Committed, outcome envelope down.
	h.journal.Fail = func(op, name string) error {
		if op == "put" && strings.HasSuffix(name, ".outcome") && strings.Contains(name, res.ID) {
			return errors.New("b2 unavailable")
		}
		return nil
	}
	if err := h.moveJob(t, res.ID); err == nil {
		t.Fatal("secured without the outcome envelope")
	}
	h.journal.Fail = nil
	if v, raw := h.fulfilView(t, order, d); v.Lifecycle != "ready" || v.Refund != nil || *v.Fulfillment.Pending != "no_show" {
		t.Fatalf("committed no-show shown as %s", raw)
	}
	// The refund is dispatched and returned while the no-show is still
	// unsecured: no refund email yet.
	h.owner(t, "UPDATE business.job SET next_attempt_at = now() + interval '1 day' WHERE id = $1::uuid", h.commandJob(t, res.ID))
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("refund:\n%s", reasons(errs))
	}
	if ops := h.operations(t, coll); len(ops) != 1 || ops[0].State != "succeeded" {
		t.Fatalf("refund operations %+v", ops)
	}
	if k := kinds(h.notices(t, order)); strings.Contains(k, "refund_returned") || strings.Contains(k, "no_show") {
		t.Fatalf("notices before the no-show was secured: %s", k)
	}
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("secure:\n%s", reasons(errs))
	}
	v, raw := h.fulfilView(t, order, d)
	if v.Lifecycle != "completed" || state(v) != "no_show" || v.Refund == nil || v.Refund.TargetMinor != 5000 || v.Refund.State != "returned" {
		t.Fatalf("no-show %s", raw)
	}
	var reason string
	var rate int
	if err := h.admin.QueryRow(h.ctx, "SELECT reason, policy_rate FROM business.refund_goal WHERE order_id = $1::uuid", order).Scan(&reason, &rate); err != nil ||
		reason != "no_show" || rate != 50 {
		t.Fatalf("goal %s %d %v", reason, rate, err)
	}
	ns := h.notices(t, order)
	if k := kinds(ns); !strings.Contains(k, "no_show:queued") || !strings.Contains(k, "refund_returned:queued") {
		t.Fatalf("notices %s", k)
	}
	for _, n := range ns {
		if n.Kind != "no_show" {
			continue
		}
		if n.Template != "order-no-show.v1" || n.Dedupe != res.ID {
			t.Fatalf("no_show notice %+v", n)
		}
		req := h.request(t, n)
		if req.Subject != "Your GK Cakes order was not collected" {
			t.Fatalf("subject %q", req.Subject)
		}
		has(t, "no-show notice", req.Text, "Our terms return 50% of what you paid when an order is not collected: £50 of the £100 you paid.",
			"Refund: £50.", "It has already been returned to you.",
			"Saturday 14 November 2026, 12:30 to 13:30 (London time)", "Order reference: "+order)
		hasNot(t, "no-show notice", req.Text+req.HTML, secrets(d, "")...)
	}
	if _, err := h.co.PreviewCancellation(h.ctx, order, "", d); conflict(err) != "cancellation_not_eligible" {
		t.Fatalf("cancelling after a no-show: %v", err)
	}
	if _, err := h.fulfil(chef, "n2", order, checkout.ActionNoShow, v.Version); conflict(err) != "transition_not_allowed" {
		t.Fatalf("second no-show: %v", err)
	}
}

// GK delivery: dispatch needs a paid delivery collection (none is created
// by checkout yet), then exception, re-dispatch and handover.
func TestDeliveryDispatchNeedsPayment(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d, _ := h.readyOrder(t, f, pol, "dl")
	h.owner(t, "UPDATE business.orders SET fulfillment_mode = 'gk_delivery' WHERE id = $1::uuid", order)
	v, _ := h.fulfilView(t, order, d)
	if _, err := h.fulfil(fulfilment, "h0", order, checkout.ActionHandover, v.Version); conflict(err) != "transition_not_allowed" {
		t.Fatalf("handover before dispatch: %v", err)
	}
	// Not sent out yet: a missed delivery is not the customer's no-show.
	h.now = at("12:30")
	if _, err := h.fulfil(chef, "n0", order, checkout.ActionNoShow, v.Version); conflict(err) != "transition_not_allowed" {
		t.Fatalf("GK delivery no-show before dispatch: %v", err)
	}
	if _, err := h.fulfil(fulfilment, "e0", order, checkout.ActionDeliveryException, v.Version, "Van broke down"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.fulfil(chef, "n1", order, checkout.ActionNoShow, v.Version+1); conflict(err) != "transition_not_allowed" {
		t.Fatalf("GK delivery no-show after an exception without a dispatch: %v", err)
	}
	v, _ = h.fulfilView(t, order, d)
	if _, err := h.fulfil(fulfilment, "d0", order, checkout.ActionDispatch, v.Version); conflict(err) != "delivery_not_paid" {
		t.Fatalf("unpaid delivery: %v", err)
	}
	h.seedDelivery(t, order, 2000)
	res, err := h.fulfil(fulfilment, "d1", order, checkout.ActionDispatch, v.Version, "Courier ref GK-123")
	if err != nil || res.FulfillmentState != "out_for_delivery" || res.Lifecycle != "ready" {
		t.Fatalf("dispatch: %+v %v", res, err)
	}
	if again, err := h.fulfil(chef, "d2", order, checkout.ActionDispatch, v.Version); err != nil || !again.Replayed {
		t.Fatalf("already out for delivery: %+v %v", again, err)
	}
	if _, err := h.fulfil(fulfilment, "e1", order, checkout.ActionDeliveryException, v.Version+1, "Nobody at the address"); err != nil {
		t.Fatalf("exception: %v", err)
	}
	if _, err := h.fulfil(fulfilment, "h1", order, checkout.ActionHandover, v.Version+2); conflict(err) != "transition_not_allowed" {
		t.Fatalf("handover from the exception without a new dispatch: %v", err)
	}
	if _, err := h.fulfil(fulfilment, "d3", order, checkout.ActionDispatch, v.Version+2); err != nil {
		t.Fatalf("re-dispatch: %v", err)
	}
	if _, err := h.fulfil(fulfilment, "h2", order, checkout.ActionHandover, v.Version+3); err != nil {
		t.Fatalf("handover: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	if v, raw := h.fulfilView(t, order, d); v.Lifecycle != "completed" || state(v) != "completed" {
		t.Fatalf("delivered %s", raw)
	}
	if n := h.count(t, "SELECT count(*) FROM business.fulfillment_event WHERE order_id = $1::uuid", order); n != 6 {
		t.Fatalf("%d fulfilment events, want ready, exception, dispatch, exception, dispatch, handover", n)
	}
}

// K05 cumulative goal: a no-show after an earlier chef refund reserves only
// what the 50% still needs, never a second 50% (no over-return), and the
// email states that new amount (or that nothing more is due).
func TestNoShowAfterEarlierRefunds(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	h.withRefunds()
	h.withNotices(t)
	type run struct {
		key            string
		earlier, fresh int64
		says           string
	}
	for _, c := range []run{
		{"big", 6000, 0, "The £60 already refunded covers this, so nothing more is due."},
		{"small", 2000, 3000, "Refund requested: £30 (the £20 already refunded counts towards it)."},
	} {
		h.now = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
		order, d, coll := h.readyOrder(t, f, pol, c.key)
		v, _ := h.fulfilView(t, order, d)
		if _, err := h.co.RecordRefund(h.ctx, chef, "rf-"+c.key, order, checkout.RefundInput{ExpectedVersion: v.Version,
			AmountMinor: c.earlier, Currency: "GBP", Allocations: []checkout.RefundAllocation{{CollectionID: coll, AmountMinor: c.earlier}},
			Reason: "Agreed adjustment"}); err != nil {
			t.Fatalf("%s: earlier refund: %v", c.key, err)
		}
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("%s: earlier refund:\n%s", c.key, reasons(errs))
		}
		v, _ = h.fulfilView(t, order, d)
		h.now = at("12:30")
		res, err := h.fulfil(chef, "ns-"+c.key, order, checkout.ActionNoShow, v.Version)
		if err != nil {
			t.Fatalf("%s: no-show: %v", c.key, err)
		}
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("%s: no-show:\n%s", c.key, reasons(errs))
		}
		ops := h.operations(t, coll)
		var total int64
		for _, op := range ops {
			if op.State != "succeeded" {
				t.Fatalf("%s: operations %+v", c.key, ops)
			}
		}
		if err := h.admin.QueryRow(h.ctx, `SELECT coalesce(sum(amount_minor), 0) FROM business.refund_operation
			WHERE collection_id = $1::uuid AND state = 'succeeded'`, coll).Scan(&total); err != nil {
			t.Fatal(err)
		}
		wantOps := 1
		if c.fresh > 0 {
			wantOps = 2
		}
		if len(ops) != wantOps || total != c.earlier+c.fresh {
			t.Fatalf("%s: %d operations, %dp returned; want %d and %dp", c.key, len(ops), total, wantOps, c.earlier+c.fresh)
		}
		var evidence string
		if err := h.admin.QueryRow(h.ctx, "SELECT outcome -> 'evidence' ->> 'new_refund_minor' FROM business.recovery_operation WHERE id = $1::uuid",
			res.ID).Scan(&evidence); err != nil || evidence != fmt.Sprint(c.fresh) {
			t.Fatalf("%s: new_refund_minor %s %v", c.key, evidence, err)
		}
		for _, n := range h.notices(t, order) {
			if n.Kind == "no_show" {
				req := h.request(t, n)
				has(t, c.key+" no-show notice", req.Text, "£50 of the £100 you paid.", c.says)
				if c.fresh == 0 {
					hasNot(t, c.key+" no-show notice", req.Text, "Refund requested")
				}
			}
		}
	}
}
