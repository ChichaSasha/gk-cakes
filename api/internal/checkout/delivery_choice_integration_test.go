//go:build integration

package checkout_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
)

// The Q5 choice when GK can't deliver (GK-017 r02b states 14 and 15; Q5,
// Q8, Q9, Q10; K05, K06, K11; migration 0036). Every delivery order here is
// due on Saturday 14 November 12:30 London (GMT); that day's published
// starts are 12:30, 13:00, 13:30 and 14:00.

type choiceTime struct {
	ID       string
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

type choiceBlock struct {
	Open    bool
	Reason  *string
	OpensAt *time.Time `json:"opens_at"`
	Pending bool
	Choices []string
	Times   []choiceTime
	Chosen  *struct {
		Choice        string
		Reason        string
		State         string
		RefusalReason *string   `json:"refusal_reason"`
		RequestedAt   time.Time `json:"requested_at"`
		To            *struct {
			FulfillmentMode string `json:"fulfillment_mode"`
			Date            string
			ID              string
			StartsAt        time.Time `json:"starts_at"`
		}
		DeliveryRefundMinor *int64 `json:"delivery_refund_minor"`
	}
}

type choiceOrderView struct {
	Version         int
	Lifecycle       string
	CancelReason    *string `json:"cancel_reason"`
	FulfillmentMode string  `json:"fulfillment_mode"`
	Date            string
	Slot            struct {
		ID       string
		StartsAt time.Time `json:"starts_at"`
	}
	PickupMoveUntil      *time.Time `json:"pickup_move_until"`
	FullRefundDeadlineAt time.Time  `json:"full_refund_deadline_at"`
	Delivery             *deliveryBlock
	DeliveryChoice       *choiceBlock `json:"delivery_choice"`
	Refund               *struct {
		refundView
		Reason string
	}
	Cancellation *struct {
		State       string
		Reason      *string
		Rate        *int
		AmountMinor *int64 `json:"amount_minor"`
		RequestedBy string `json:"requested_by"`
	}
}

func (h *payHarness) choiceView(t *testing.T, order string, d []byte) (choiceOrderView, string) {
	t.Helper()
	raw, err := h.co.GetOrder(h.ctx, order, "", d)
	if err != nil {
		t.Fatal(err)
	}
	var v choiceOrderView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return v, string(raw)
}

func (h *payHarness) notPossible(key, order string, version int, reason string) (catalog.Result, error) {
	in := checkout.DeliveryNotPossibleInput{ExpectedVersion: version}
	if reason != "" {
		in.Reason = &reason
	}
	return h.co.RecordDeliveryNotPossible(h.ctx, chef, key, order, in)
}

func (h *payHarness) choose(order string, d []byte, key string, version int, choice, slot string) (catalog.Result, error) {
	return h.co.ChooseDelivery(h.ctx, order, "", d, key, checkout.DeliveryChoiceInput{ExpectedVersion: version, Choice: choice,
		TargetSlotID: slot})
}

// quotedOrder is a paid 10000p GK delivery order with a 12000p delivery
// quote: the order, its digest, the cake collection and the quote.
func (h *payHarness) quotedOrder(t *testing.T, f fixture, pol, key string) (string, []byte, string, string) {
	t.Helper()
	order, d, cake := h.deliveryOrder(t, f, pol, key, "")
	v, _ := h.deliveryView(t, order, d)
	q, err := h.quoteDelivery("q-"+key, order, v.Version, 12000)
	if err != nil {
		t.Fatal(err)
	}
	return order, d, cake, q.ID
}

// paidDeliveryOrder is quotedOrder with the quote paid: also the delivery
// collection.
func (h *payHarness) paidDeliveryOrder(t *testing.T, f fixture, pol, key string) (string, []byte, string, string) {
	t.Helper()
	order, d, cake, _ := h.quotedOrder(t, f, pol, key)
	coll, s := h.deliveryStarted(t, order, d, "pay-"+key)
	h.deliveryPaid(t, order, coll, s, "evt_paid_delivery_"+key)
	return order, d, cake, coll
}

func timeIDs(ts []choiceTime) string {
	var out []string
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return strings.Join(out, ",")
}

func reasonOf(b *choiceBlock) string {
	if b == nil || b.Reason == nil {
		return ""
	}
	return *b.Reason
}

// The chef's "GK can't deliver": a GK delivery order with a visibly
// confirmed cake, never dispatched, chef only; versioned and idempotent
// (another key on a decided order is a conflict); refused during a restore freeze
// (replays answer) and while a delivery payment is in flight. It refunds
// nothing, opens the choice, closes the unpaid quote and stops the dispatch;
// its reason reaches only the chef.
func TestDeliveryNotPossibleRules(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	slots := h.slotsOn(t, "2026-11-14")

	// Not a GK delivery: refused.
	coll, _, _ := h.paidCollection(t, f, pol, "col")
	cd := h.digestOf(t, coll)
	cv, raw := h.choiceView(t, coll, cd)
	if cv.DeliveryChoice != nil || !strings.Contains(raw, `"delivery_choice" : null`) {
		t.Fatalf("collection order: %s", raw)
	}
	if _, err := h.notPossible("c0", coll, cv.Version, ""); conflict(err) != "state_conflict" {
		t.Fatalf("collection order: %v", err)
	}

	order, d, cake, quote := h.quotedOrder(t, f, pol, "np")
	v, raw := h.choiceView(t, order, d)
	if v.DeliveryChoice == nil || v.DeliveryChoice.Open || v.DeliveryChoice.OpensAt == nil ||
		!v.DeliveryChoice.OpensAt.Equal(deliveryPayBy) || len(v.DeliveryChoice.Choices) != 0 || len(v.DeliveryChoice.Times) != 0 ||
		v.DeliveryChoice.Chosen != nil || v.DeliveryChoice.Pending {
		t.Fatalf("before any decision: %s", raw)
	}
	for name, who := range map[string]string{"fulfilment": fulfilment, "support": support, "customer": customerA} {
		if _, err := h.co.RecordDeliveryNotPossible(h.ctx, who, "x", order, checkout.DeliveryNotPossibleInput{ExpectedVersion: v.Version}); !errors.Is(err, catalog.ErrForbidden) {
			t.Errorf("by %s: %v", name, err)
		}
	}
	bad := "two\nlines"
	if _, err := h.co.RecordDeliveryNotPossible(h.ctx, chef, "bad", order, checkout.DeliveryNotPossibleInput{ExpectedVersion: v.Version,
		Reason: &bad}); !strings.HasPrefix(invalidField(err), "reason") {
		t.Fatalf("two-line reason: %v", err)
	}
	if _, err := h.notPossible("stale", order, v.Version+1, ""); conflict(err) != "stale_version" {
		t.Fatalf("stale: %v", err)
	}
	// A delivery payment in flight is settled first.
	pay, err := h.payDelivery(order, d, "inflight", v.Version, quote)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.notPossible("flight", order, v.Version, ""); conflict(err) != "payment_in_progress" {
		t.Fatalf("payment in flight: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("dispatch: %v", errs)
	}
	sess := h.collection(t, pay.ID).SessionID
	h.stripe.session(sess).status = "expired"
	h.deliver(t, "evt_expired_np", "checkout.session.expired", "checkout.session", sess, pay.ID, order)
	if errs := h.drain(t); len(errs) != 0 || h.collection(t, pay.ID).State != "cancelled" {
		t.Fatalf("expiry: %v %+v", errs, h.collection(t, pay.ID))
	}
	// A restore freeze refuses the decision.
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	reason := "Van off the road on Saturday"
	if _, err := h.notPossible("np1", order, v.Version, reason); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("frozen: %v", err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	dec, err := h.notPossible("np1", order, v.Version, reason)
	if err != nil || dec.Replayed {
		t.Fatalf("decision: %+v %v", dec, err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if again, err := h.notPossible("np1", order, v.Version, reason); err != nil || !again.Replayed || again.ID != dec.ID {
		t.Fatalf("replay while frozen: %+v %v", again, err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	if _, err := h.notPossible("np1", order, v.Version, "Another reason"); conflict(err) != "key_reused" {
		t.Fatalf("key reused: %v", err)
	}
	// Another key on a decided order is a conflict (only the same key
	// replays), at any version.
	for _, version := range []int{v.Version + 1, v.Version} {
		if _, err := h.notPossible("np2", order, version, ""); conflict(err) != "state_conflict" {
			t.Fatalf("second decision at version %d: %v", version, err)
		}
	}
	if n := h.count(t, "SELECT count(*) FROM business.delivery_not_possible WHERE order_id = $1::uuid", order); n != 1 {
		t.Fatalf("%d decisions", n)
	}
	v, raw = h.choiceView(t, order, d)
	b := v.DeliveryChoice
	want := strings.Join(slots, ",")
	if b == nil || !b.Open || reasonOf(b) != "not_possible" || b.OpensAt != nil || len(b.Choices) != 3 || timeIDs(b.Times) != want ||
		v.Delivery.State != "quoted" || *v.Delivery.Payable || v.Refund != nil || v.Lifecycle != "confirmed" ||
		v.FulfillmentMode != "gk_delivery" {
		t.Fatalf("after the decision: %s", raw)
	}
	if strings.Contains(raw, "Van off") {
		t.Fatalf("the chef's reason reached the customer: %s", raw)
	}
	// Nothing is refunded by the decision.
	if n := h.count(t, "SELECT count(*) FROM business.refund_operation WHERE order_id = $1::uuid", order); n != 0 {
		t.Fatalf("%d refunds after the decision", n)
	}
	// The unpaid quote is closed: no payment, quote or waiver.
	if _, err := h.payDelivery(order, d, "after", v.Version, quote); conflict(err) != "state_conflict" {
		t.Fatalf("payment after the decision: %v", err)
	}
	if _, err := h.quoteDelivery("q-after", order, v.Version, 13000); conflict(err) != "state_conflict" {
		t.Fatalf("quote after the decision: %v", err)
	}
	if audits := h.auditDetails(t, "delivery.not_possible"); len(audits) != 1 || !strings.Contains(audits[0], dec.ID) ||
		strings.Contains(audits[0], "Van off") {
		t.Fatalf("audit %v", audits)
	}
	// Staff: the chef sees the reason and the flag; technical support not
	// the reason; fulfilment the state only.
	chefView := h.staffJSON(t, chef, order)
	if c := compactJSON(t, chefView["delivery_choice"]); !strings.Contains(c, `"reason":"Van off the road on Saturday"`) ||
		!strings.Contains(c, `"open":true`) || !strings.Contains(string(chefView["attention"]), "delivery_choice_open") {
		t.Fatalf("chef %s %s", c, chefView["attention"])
	}
	sup := h.staffJSON(t, support, order)
	if c := compactJSON(t, sup["delivery_choice"]); strings.Contains(c, "Van off") || !strings.Contains(c, `"not_possible":{`) {
		t.Fatalf("support %s", c)
	}
	var ful map[string]json.RawMessage
	if err := json.Unmarshal(h.staffJSON(t, fulfilment, order)["delivery_choice"], &ful); err != nil || len(ful) != 5 ||
		string(ful["open"]) != "true" || ful["times"] != nil {
		t.Fatalf("fulfilment %v %v", ful, err)
	}

	// Dispatch is refused once GK can't deliver.
	version := h.makeReady(t, order, d, "np")
	if _, err := h.fulfil(fulfilment, "dispatch", order, checkout.ActionDispatch, version); conflict(err) != "delivery_not_possible" {
		t.Fatalf("dispatch: %v", err)
	}
	// The expired session's money arrives after all: it never pays the
	// delivery GK can't make (a stray for the chef; nothing to choose
	// differently).
	h.latePaid(t, pay.ID, sess)
	if got := h.deliveryOutcome(t, pay.ID); got != "unmatched/delivery_not_possible" {
		t.Fatalf("late money outcome %s", got)
	}
	if v, raw := h.choiceView(t, order, d); v.Delivery.State == "paid" || v.Delivery.UnmatchedMinor == nil ||
		*v.Delivery.UnmatchedMinor != 12000 || !v.DeliveryChoice.Open {
		t.Fatalf("after late money: %s", raw)
	}
	_ = cake

	// A dispatched order: too late for the decision.
	sent, sd, _, _ := h.paidDeliveryOrder(t, f, pol, "sent")
	sv := h.makeReady(t, sent, sd, "sent")
	if _, err := h.fulfil(fulfilment, "dispatch-sent", sent, checkout.ActionDispatch, sv); err != nil {
		t.Fatal(err)
	}
	if _, err := h.notPossible("np-sent", sent, sv+1, ""); conflict(err) != "state_conflict" {
		t.Fatalf("after dispatch: %v", err)
	}
}

// Q9/Q8: an unpaid quote opens the choice at its pay-by without any chef
// action, with no deadline. Customer courier at any opening time of that
// day after the receipt, also within six hours of the delivery (Q10): the
// mode and time change through the protected command; the day place,
// refund deadline and production anchor stay; nothing is refunded (the
// delivery was not paid) and the quote is closed.
func TestDeliveryChoiceOpensWhenPayByPasses(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	slots := h.slotsOn(t, "2026-11-14")
	order, d, cake, quote := h.quotedOrder(t, f, pol, "pb")
	_, anchor := h.productionJob(t, order)
	v, _ := h.choiceView(t, order, d)
	if _, err := h.choose(order, d, "early", v.Version, checkout.ChoiceFullRefund, ""); conflict(err) != "choice_not_open" {
		t.Fatalf("before pay-by: %v", err)
	}
	h.now = deliveryPayBy.Add(-time.Second)
	if v, raw := h.choiceView(t, order, d); v.DeliveryChoice.Open || len(v.DeliveryChoice.Times) != 0 {
		t.Fatalf("a second before pay-by: %s", raw)
	}
	h.now = deliveryPayBy
	v, raw := h.choiceView(t, order, d)
	if b := v.DeliveryChoice; !b.Open || reasonOf(b) != "pay_by_passed" || timeIDs(b.Times) != strings.Join(slots, ",") ||
		v.Delivery.State != "pay_by_passed" {
		t.Fatalf("at pay-by: %s", raw)
	}

	// Access: another customer or guest link, a read-only link, and a link
	// without move_pickup for a time.
	_, other := proof(t)
	if _, err := h.choose(order, other, "x1", v.Version, checkout.ChoiceFullRefund, ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("another guest: %v", err)
	}
	if _, err := h.co.ChooseDelivery(h.ctx, order, customerB, nil, "x2", checkout.DeliveryChoiceInput{ExpectedVersion: v.Version,
		Choice: checkout.ChoiceFullRefund}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("another customer: %v", err)
	}
	_, readOnly := proof(t)
	h.owner(t, `INSERT INTO business.order_guest_capability (id, order_id, capability_digest, allowed_commands)
		VALUES (gen_random_uuid(), $1::uuid, $2, ARRAY['read'])`, order, readOnly)
	_, cancelOnly := proof(t)
	h.owner(t, `INSERT INTO business.order_guest_capability (id, order_id, capability_digest, allowed_commands)
		VALUES (gen_random_uuid(), $1::uuid, $2, ARRAY['read', 'cancel'])`, order, cancelOnly)
	if _, err := h.choose(order, readOnly, "x3", v.Version, checkout.ChoiceFullRefund, ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("read-only link refund: %v", err)
	}
	if _, err := h.choose(order, cancelOnly, "x4", v.Version, checkout.ChoiceCourier, slots[2]); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("link without move_pickup: %v", err)
	}

	// Saturday 13:15: the delivery start (12:30) has passed and no six-hour
	// cutoff applies; only later starts can be chosen.
	h.now = time.Date(2026, 11, 14, 13, 15, 0, 0, time.UTC)
	v, raw = h.choiceView(t, order, d)
	if b := v.DeliveryChoice; !b.Open || timeIDs(b.Times) != slots[2]+","+slots[3] {
		t.Fatalf("Saturday 13:15: %s", raw)
	}
	for name, c := range map[string]struct {
		slot string
		want string
	}{
		"a start already passed": {slots[1], "slot_unavailable"},
		"another day's start":    {h.slotsOn(t, "2026-11-21")[2], "slot_unavailable"},
		"an unknown slot":        {"8d3f0f8e-5d55-4c3e-9a49-34a0e2a9f001", "slot_unavailable"},
	} {
		if _, err := h.choose(order, d, "x-"+strings.ReplaceAll(name, " ", "-"), v.Version, checkout.ChoiceCourier, c.slot); conflict(err) != c.want {
			t.Errorf("%s: %v, want %s", name, err, c.want)
		}
	}
	if _, err := h.choose(order, d, "stale", v.Version-1, checkout.ChoiceCourier, slots[2]); conflict(err) != "stale_version" {
		t.Fatalf("stale: %v", err)
	}
	receipt := h.now
	op, err := h.choose(order, d, "c1", v.Version, checkout.ChoiceCourier, slots[2])
	if err != nil || op.Replayed {
		t.Fatalf("choice: %+v %v", op, err)
	}
	if again, err := h.choose(order, d, "c1", v.Version, checkout.ChoiceCourier, slots[2]); err != nil || !again.Replayed || again.ID != op.ID {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, err := h.choose(order, d, "c1", v.Version, checkout.ChoiceCourier, slots[3]); conflict(err) != "key_reused" {
		t.Fatalf("key reused: %v", err)
	}
	if _, err := h.choose(order, d, "c2", v.Version, checkout.ChoiceFullRefund, ""); conflict(err) != "state_conflict" {
		t.Fatalf("a second choice while one is pending: %v", err)
	}
	p, raw := h.choiceView(t, order, d)
	if b := p.DeliveryChoice; b.Open || !b.Pending || b.Chosen == nil || b.Chosen.State != "processing" ||
		b.Chosen.Choice != checkout.ChoiceCourier || len(b.Choices) != 0 || p.FulfillmentMode != "gk_delivery" {
		t.Fatalf("pending: %s", raw)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	v2, raw := h.choiceView(t, order, d)
	b := v2.DeliveryChoice
	if v2.FulfillmentMode != checkout.ChoiceCourier || v2.Slot.ID != slots[2] || !v2.Slot.StartsAt.Equal(at("13:30")) ||
		v2.Date != "2026-11-14" || !v2.FullRefundDeadlineAt.Equal(cancelDeadline) || v2.Lifecycle != "confirmed" ||
		v2.Version != v.Version+1 || v2.PickupMoveUntil == nil {
		t.Fatalf("chosen: %s", raw)
	}
	if b == nil || b.Open || b.Chosen == nil || b.Chosen.State != "chosen" || b.Chosen.Reason != "pay_by_passed" ||
		b.Chosen.To == nil || b.Chosen.To.FulfillmentMode != checkout.ChoiceCourier || b.Chosen.To.ID != slots[2] ||
		b.Chosen.DeliveryRefundMinor != nil || !b.Chosen.RequestedAt.Equal(receipt) || len(b.Times) != 0 {
		t.Fatalf("chosen block: %s", raw)
	}
	if v2.Delivery == nil || v2.Delivery.State != "cancelled_by_choice" || *v2.Delivery.Payable || v2.Refund != nil {
		t.Fatalf("delivery after the choice: %s", raw)
	}
	if _, at := h.productionJob(t, order); !at.Equal(anchor) {
		t.Fatalf("production anchor %s, was %s", at, anchor)
	}
	if got := h.allocation(t, order); got != "committed" {
		t.Fatalf("day place %s", got)
	}
	if n := h.count(t, "SELECT count(*) FROM business.refund_operation WHERE order_id = $1::uuid", order); n != 0 {
		t.Fatalf("%d refunds for an unpaid delivery", n)
	}
	// Closed: no choice again, no payment, no dispatch (not a GK delivery).
	if _, err := h.choose(order, d, "c3", v2.Version, checkout.ChoiceFullRefund, ""); conflict(err) != "choice_not_open" {
		t.Fatalf("choice after the choice: %v", err)
	}
	if _, err := h.payDelivery(order, d, "late", v2.Version, quote); conflict(err) != "state_conflict" {
		t.Fatalf("payment after the choice: %v", err)
	}
	version := h.makeReady(t, order, d, "pb")
	if _, err := h.fulfil(fulfilment, "dispatch", order, checkout.ActionDispatch, version); conflict(err) != "transition_not_allowed" {
		t.Fatalf("dispatch after the choice: %v", err)
	}
	// The history: the chef's and technical support's copies.
	if hist := compactJSON(t, h.staffJSON(t, chef, order)["delivery_choice"]); !strings.Contains(hist, `"history":[{`) ||
		!strings.Contains(hist, `"fulfillment_mode":"gk_delivery"`) || !strings.Contains(hist, `"access":"guest_link"`) {
		t.Fatalf("chef history %s", hist)
	}
	if audits := h.auditDetails(t, "order.delivery_choice_made"); len(audits) != 1 || !strings.Contains(audits[0], op.ID) {
		t.Fatalf("audit %v", audits)
	}
	hasNot(t, "audit", strings.Join(h.auditDetails(t, "order.delivery_choice_requested"), ""), "example.com", "Test Customer")
	_ = cake
	h.noPersonalDataInLogs(t)
}

// Collection when the delivery was paid: nothing is refunded until the
// customer chooses; the choice is a protected command (journal down ->
// nothing; outcome envelope down -> the order still reads as a GK delivery
// at its time; secured -> collection at the new time) and the delivery comes
// back in full through the 100% delivery floor, once; the cake is not
// refunded. Staff copies redact as usual.
func TestDeliveryChoiceCollectionReturnsThePaidDelivery(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	slots := h.slotsOn(t, "2026-11-14")
	order, d, cake, delivery := h.paidDeliveryOrder(t, f, pol, "pd")
	h.withRefunds()
	h.withNotices(t)
	v, _ := h.choiceView(t, order, d)
	if _, err := h.notPossible("np", order, v.Version, "Driver unavailable"); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	v, raw := h.choiceView(t, order, d)
	if v.Delivery.State != "paid" || *v.Delivery.Payable || v.Refund != nil || !v.DeliveryChoice.Open {
		t.Fatalf("decided, nothing refunded: %s", raw)
	}
	// The decision records the paid delivery's floor (an obligation): no
	// goal, nothing reserved.
	if n := h.count(t, "SELECT count(*) FROM business.refund_floor WHERE collection_id = $1::uuid", delivery); n != 1 ||
		h.count(t, "SELECT count(*) FROM business.refund_goal WHERE order_id = $1::uuid", order) != 0 {
		t.Fatalf("floor %d at the decision", n)
	}
	if n := h.count(t, "SELECT count(*) FROM business.refund_operation WHERE order_id = $1::uuid", order); n != 0 {
		t.Fatalf("%d refunds before the choice", n)
	}
	op, err := h.choose(order, d, "c1", v.Version, checkout.ChoiceCollection, slots[1])
	if err != nil {
		t.Fatal(err)
	}
	h.journal.Fail = func(string, string) error { return errors.New("b2 unavailable") }
	if err := h.moveJob(t, op.ID); err == nil {
		t.Fatal("committed without the journal")
	}
	if got, row := h.recoveryState(t, op.ID), h.orderSlot(t, order); got != "reserved" || row.Slot != f.slot || row.Version != v.Version {
		t.Fatalf("journal down: %s %+v", got, row)
	}
	h.journal.Fail = func(op, name string) error {
		if op == "put" && strings.HasSuffix(name, ".outcome") {
			return errors.New("b2 unavailable")
		}
		return nil
	}
	h.due(t)
	if err := h.moveJob(t, op.ID); err == nil {
		t.Fatal("secured without the outcome envelope")
	}
	var mode string
	if err := h.admin.QueryRow(h.ctx, "SELECT fulfillment_mode FROM business.orders WHERE id = $1::uuid", order).Scan(&mode); err != nil ||
		mode != "collection" || h.recoveryState(t, op.ID) != "committed" || h.orderSlot(t, order).Slot != slots[1] {
		t.Fatalf("committed: %s %v", mode, err)
	}
	// The reserved delivery refund even goes back while the outcome is
	// unprotected: nothing of it is visible or emailed yet (K11).
	var refundJob string
	if err := h.admin.QueryRow(h.ctx, `SELECT rj.job_id::text FROM business.refund_job rj
		JOIN business.refund_operation ro ON ro.id = rj.refund_operation_id WHERE ro.collection_id = $1::uuid`, delivery).Scan(&refundJob); err != nil {
		t.Fatal(err)
	}
	rjob := h.claimOnly(t, refundJob)
	h.finish(t, rjob, h.handler(h.ctx, rjob))
	if got := refundAmounts(t, h, delivery); len(got) != 1 || got[0] != 12000 {
		t.Fatalf("delivery refund before securing %v", got)
	}
	if p, raw := h.choiceView(t, order, d); p.FulfillmentMode != "gk_delivery" || p.Slot.ID != f.slot || !p.Slot.StartsAt.Equal(at("12:30")) ||
		p.DeliveryChoice.Chosen == nil || p.DeliveryChoice.Chosen.State != "processing" || p.Delivery.State != "paid" ||
		p.PickupMoveUntil != nil || p.Refund != nil || p.DeliveryChoice.Chosen.DeliveryRefundMinor != nil ||
		strings.Contains(raw, "delivery_refund_minor") {
		t.Fatalf("committed but unsecured shown as %s", raw)
	}
	if c := compactJSON(t, h.staffJSON(t, chef, order)["delivery_choice"]); strings.Contains(c, "delivery_refund_minor") {
		t.Fatalf("chef sees the unsecured refund %s", c)
	}
	if n := noticeCount(h.notices(t, order), "refund_returned"); n != 0 {
		t.Fatalf("a refund email before the choice is secured (%d)", n)
	}
	var block *string
	if err := h.admin.QueryRow(h.ctx, "SELECT business.account_order_block(o) FROM business.orders o WHERE o.id = $1::uuid",
		order).Scan(&block); err != nil || block == nil || *block != "order_change_in_progress" {
		t.Fatalf("account deletion block while unsecured: %v %v", block, err)
	}
	rows, _ := h.staffList(t, chef, november)
	for _, r := range rows {
		if r["id"] == order && r["refund"] != nil {
			t.Fatalf("the staff list shows the unsecured refund %v", r["refund"])
		}
	}
	h.journal.Fail = nil
	h.due(t)
	if err := h.moveJob(t, op.ID); err != nil {
		t.Fatalf("secure: %v", err)
	}
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	v2, raw := h.choiceView(t, order, d)
	b := v2.DeliveryChoice
	if v2.FulfillmentMode != "collection" || v2.Slot.ID != slots[1] || !v2.Slot.StartsAt.Equal(at("13:00")) ||
		!v2.FullRefundDeadlineAt.Equal(cancelDeadline) || b.Chosen.State != "chosen" || b.Chosen.Reason != "not_possible" ||
		b.Chosen.DeliveryRefundMinor == nil || *b.Chosen.DeliveryRefundMinor != 12000 {
		t.Fatalf("chosen: %s", raw)
	}
	if v2.Delivery.State != "cancelled_by_choice" || v2.Delivery.PaidMinor == nil || *v2.Delivery.PaidMinor != 12000 ||
		v2.Refund == nil || v2.Refund.TargetMinor != 12000 || v2.Refund.SucceededMinor != 12000 || v2.Refund.State != "returned" ||
		v2.Refund.Reason != "delivery_failure" || v2.Refund.DeliveryPending != 0 {
		t.Fatalf("delivery refunded: %s", raw)
	}
	if got := refundAmounts(t, h, delivery); len(got) != 1 || got[0] != 12000 {
		t.Fatalf("delivery refunds %v", got)
	}
	if n := h.count(t, "SELECT count(*) FROM business.refund_operation WHERE collection_id = $1::uuid", cake); n != 0 {
		t.Fatalf("the cake was refunded (%d)", n)
	}
	// No new email for the choice; the existing refund_returned notice
	// announces the returned delivery once.
	ns := h.notices(t, order)
	if noticeCount(ns, "refund_returned") != 1 || noticeCount(ns, "cancelled") != 0 || noticeCount(ns, "pickup_moved") != 0 {
		t.Fatalf("notices %+v", ns)
	}
	for _, n := range ns {
		if n.Kind == "refund_returned" {
			has(t, "refund notice", h.request(t, n).Text, "£120")
		}
	}
	key, _ := h.coord.EnvelopeKey(recovery.Operation{ID: op.ID, AggregateID: order, PriorVersion: v.Version})
	if h.journal.Puts(key+".prepared") != 1 || h.journal.Puts(key+".outcome") != 1 {
		t.Fatalf("envelopes %d/%d", h.journal.Puts(key+".prepared"), h.journal.Puts(key+".outcome"))
	}
	if again, err := h.choose(order, d, "c1", v.Version, checkout.ChoiceCollection, slots[1]); err != nil || !again.Replayed || again.ID != op.ID {
		t.Fatalf("replay after the outcome: %+v %v", again, err)
	}
	var floor int64
	var by string
	if err := h.admin.QueryRow(h.ctx, `SELECT f.floor_minor, a.provider_subject::text FROM business.refund_floor f
		JOIN business.account a ON a.id = f.recorded_by WHERE f.collection_id = $1::uuid`, delivery).Scan(&floor, &by); err != nil ||
		floor != 12000 || by != chef {
		t.Fatalf("floor %d by %s: %v", floor, by, err)
	}
	// Staff: money for the chef only; fulfilment sees the state.
	if c := compactJSON(t, h.staffJSON(t, chef, order)["delivery_choice"]); !strings.Contains(c, `"delivery_refund_minor":12000`) ||
		!strings.Contains(c, "Driver unavailable") {
		t.Fatalf("chef %s", c)
	}
	sup := compactJSON(t, h.staffJSON(t, support, order)["delivery_choice"])
	if strings.Contains(sup, "12000") || strings.Contains(sup, "Driver") || !strings.Contains(sup, `"state":"chosen"`) {
		t.Fatalf("support %s", sup)
	}
	if ful := compactJSON(t, h.staffJSON(t, fulfilment, order)["delivery_choice"]); strings.Contains(ful, "12000") ||
		!strings.Contains(ful, `"fulfillment_mode":"collection"`) {
		t.Fatalf("fulfilment %s", ful)
	}
	hasNot(t, "audit", strings.Join(h.auditDetails(t, "order.delivery_choice_made"), ""), "example.com", "Test Customer", "Driver")
}

// Full refund: the order is cancelled with 100% of everything actually
// paid (cake and paid delivery, both returned automatically), reason
// delivery_not_possible, requested by the customer, through the terminal
// return; one cancelled notice states the reason. With an unpaid quote
// (pay-by passed) only the cake is returned. The signed-in owner chooses too.
func TestDeliveryChoiceFullRefund(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d, cake, delivery := h.paidDeliveryOrder(t, f, pol, "fr")
	unpaid, ud, ucake, _ := h.quotedOrder(t, f, pol, "fu")
	h.withRefunds()
	h.withNotices(t)
	v, _ := h.choiceView(t, order, d)
	if _, err := h.notPossible("np", order, v.Version, ""); err != nil {
		t.Fatal(err)
	}
	v, _ = h.choiceView(t, order, d)
	op, err := h.choose(order, d, "r1", v.Version, checkout.ChoiceFullRefund, "")
	if err != nil {
		t.Fatal(err)
	}
	if p, raw := h.choiceView(t, order, d); p.Lifecycle != "cancellation_pending" || p.Cancellation == nil ||
		p.Cancellation.State != "processing" || p.CancelReason != nil {
		t.Fatalf("pending: %s", raw)
	}
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	v2, raw := h.choiceView(t, order, d)
	if v2.Lifecycle != "cancelled" || v2.CancelReason == nil || *v2.CancelReason != "delivery_not_possible" ||
		v2.Cancellation == nil || *v2.Cancellation.Reason != "delivery_not_possible" || *v2.Cancellation.Rate != 100 ||
		*v2.Cancellation.AmountMinor != 22000 || v2.Cancellation.RequestedBy != "customer" {
		t.Fatalf("cancelled: %s", raw)
	}
	if v2.Refund == nil || v2.Refund.TargetMinor != 22000 || v2.Refund.SucceededMinor != 22000 || v2.Refund.State != "returned" ||
		v2.Refund.DeliveryPending != 0 || v2.DeliveryChoice.Chosen.State != "cancelled" || v2.DeliveryChoice.Open {
		t.Fatalf("refunded: %s", raw)
	}
	if c, dl := refundAmounts(t, h, cake), refundAmounts(t, h, delivery); len(c) != 1 || c[0] != 10000 || len(dl) != 1 || dl[0] != 12000 {
		t.Fatalf("refunds cake %v delivery %v", c, dl)
	}
	if got := h.allocation(t, order); got != "released" {
		t.Fatalf("day place %s", got)
	}
	var cancelled []noticeRow
	for _, n := range h.notices(t, order) {
		if n.Kind == "cancelled" {
			cancelled = append(cancelled, n)
		}
	}
	if len(cancelled) != 1 {
		t.Fatalf("cancelled notices %+v", h.notices(t, order))
	}
	text := h.request(t, cancelled[0]).Text
	has(t, "cancelled notice", text, "We are sorry that we could not deliver your order.", "£220", "the full amount you paid")
	if again, err := h.choose(order, d, "r1", v.Version, checkout.ChoiceFullRefund, ""); err != nil || !again.Replayed || again.ID != op.ID {
		t.Fatalf("replay: %+v %v", again, err)
	}

	// Unpaid quote, pay-by passed, signed-in owner: the cake only.
	owner := h.resolve(t, customerA)
	h.owner(t, `SELECT business.order_owner_link($1::uuid, $2::uuid, 'chef', NULL, 'Linked in a test',
		(SELECT id FROM business.account WHERE provider_subject = $3::uuid), now())`, unpaid, owner.AccountID, chef)
	h.now = deliveryPayBy.Add(time.Hour)
	raw2, err := h.co.GetOrder(h.ctx, unpaid, customerA, nil)
	if err != nil {
		t.Fatal(err)
	}
	var uv choiceOrderView
	if err := json.Unmarshal(raw2, &uv); err != nil || !uv.DeliveryChoice.Open {
		t.Fatalf("owner view %s %v", raw2, err)
	}
	if _, err := h.choose(unpaid, ud, "g1", uv.Version, checkout.ChoiceFullRefund, ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("revoked guest link: %v", err)
	}
	uop, err := h.co.ChooseDelivery(h.ctx, unpaid, customerA, nil, "o1", checkout.DeliveryChoiceInput{ExpectedVersion: uv.Version,
		Choice: checkout.ChoiceFullRefund})
	if err != nil {
		t.Fatal(err)
	}
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	raw2, _ = h.co.GetOrder(h.ctx, unpaid, customerA, nil)
	if err := json.Unmarshal(raw2, &uv); err != nil || uv.Lifecycle != "cancelled" || uv.CancelReason == nil ||
		*uv.CancelReason != "delivery_not_possible" || uv.Refund == nil || uv.Refund.TargetMinor != 10000 || uv.Refund.State != "returned" || uv.DeliveryChoice.Chosen.Reason != "pay_by_passed" {
		t.Fatalf("owner refund %s %v", raw2, err)
	}
	if c := refundAmounts(t, h, ucake); len(c) != 1 || c[0] != 10000 {
		t.Fatalf("cake refunds %v", c)
	}
	var kind string
	var mine bool
	if err := h.admin.QueryRow(h.ctx, `SELECT actor_kind, actor_account_id = $2::uuid AND guest_capability_id IS NULL
		FROM business.delivery_choice WHERE command_id = $1::uuid`, uop.ID, owner.AccountID).Scan(&kind, &mine); err != nil ||
		kind != "customer" || !mine {
		t.Fatalf("owner choice %s %v %v", kind, mine, err)
	}
}

// A restore freeze refuses a new choice (503; the same key replays) and
// defers its commit; nothing changes until it lifts.
func TestDeliveryChoiceFreeze(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	slots := h.slotsOn(t, "2026-11-14")
	order, d, _, _ := h.quotedOrder(t, f, pol, "fz")
	v, _ := h.choiceView(t, order, d)
	if _, err := h.notPossible("np", order, v.Version, ""); err != nil {
		t.Fatal(err)
	}
	v, _ = h.choiceView(t, order, d)
	op, err := h.choose(order, d, "c1", v.Version, checkout.ChoiceCollection, slots[3])
	if err != nil {
		t.Fatal(err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if again, err := h.choose(order, d, "c1", v.Version, checkout.ChoiceCollection, slots[3]); err != nil || !again.Replayed || again.ID != op.ID {
		t.Fatalf("replay while frozen: %+v %v", again, err)
	}
	if _, err := h.choose(order, d, "c2", v.Version, checkout.ChoiceFullRefund, ""); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("new choice while frozen: %v", err)
	}
	if err := h.moveJob(t, op.ID); err == nil {
		t.Fatal("committed while frozen")
	}
	if got, row := h.recoveryState(t, op.ID), h.orderSlot(t, order); got != "reserved" || row.Slot != f.slot {
		t.Fatalf("while frozen: %s %+v", got, row)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	if v2, raw := h.choiceView(t, order, d); v2.FulfillmentMode != "collection" || v2.Slot.ID != slots[3] {
		t.Fatalf("after the freeze: %s", raw)
	}
}

// The chef publishes a new schedule version between the choice and its
// commit: the target's slot ID is no longer offered, the choice is refused
// (slot_unavailable, version + 1 only), the order stays a GK delivery at its
// time, nothing is refunded, and the choice is open again with the new
// version's times.
func TestDeliveryChoiceRefusedAtTheCommit(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	slots := h.slotsOn(t, "2026-11-14")
	order, d, _, delivery := h.paidDeliveryOrder(t, f, pol, "rc")
	h.withRefunds()
	v, _ := h.choiceView(t, order, d)
	if _, err := h.notPossible("np", order, v.Version, ""); err != nil {
		t.Fatal(err)
	}
	v, _ = h.choiceView(t, order, d)
	op, err := h.choose(order, d, "c1", v.Version, checkout.ChoiceCollection, slots[2])
	if err != nil {
		t.Fatal(err)
	}
	must[catalog.Result](t)(h.sch.PublishVersion(h.ctx, chef, "sched2", schedule.VersionInput{ExpectedRevision: ptr(1),
		StandardLimit: ptr(7), BookableUntil: "2027-01-31", Weekdays: []schedule.WeekdayInput{
			{Weekday: "saturday", Opens: "12:30", Closes: "15:00", Starts: []string{"12:30", "13:00", "13:30", "14:00"}}}}))
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	v2, raw := h.choiceView(t, order, d)
	b := v2.DeliveryChoice
	if v2.FulfillmentMode != "gk_delivery" || v2.Slot.ID != f.slot || v2.Version != v.Version+1 || b.Chosen == nil ||
		b.Chosen.State != "refused" || b.Chosen.RefusalReason == nil || *b.Chosen.RefusalReason != "slot_unavailable" ||
		!b.Open || len(b.Times) != 4 || timeIDs(b.Times) == strings.Join(slots, ",") || v2.Delivery.State != "paid" ||
		v2.Refund != nil {
		t.Fatalf("refused: %s", raw)
	}
	if n := h.count(t, "SELECT count(*) FROM business.refund_operation WHERE collection_id = $1::uuid", delivery); n != 0 ||
		h.count(t, "SELECT count(*) FROM business.refund_goal WHERE order_id = $1::uuid", order) != 0 {
		t.Fatalf("a refund for a refused choice (%d)", n)
	}
	if again, err := h.choose(order, d, "c1", v.Version, checkout.ChoiceCollection, slots[2]); err != nil || !again.Replayed || again.ID != op.ID {
		t.Fatalf("replay of the refused choice: %+v %v", again, err)
	}
	// The new version's time is taken.
	if _, err := h.choose(order, d, "c2", v2.Version, checkout.ChoiceCollection, b.Times[2].ID); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	if v3, raw := h.choiceView(t, order, d); v3.FulfillmentMode != "collection" || v3.Slot.ID != b.Times[2].ID ||
		v3.DeliveryChoice.Chosen.State != "chosen" || v3.Refund == nil || v3.Refund.SucceededMinor != 12000 {
		t.Fatalf("second choice: %s", raw)
	}
}

// K05 / state 14: once the chef recorded that GK can't deliver, an ordinary
// cancellation returns the paid delivery in full beside the cake's own
// rate: after the deadline cake 50% (5000p) + delivery 100% (12000p), both
// returned automatically; before it 100% + 100%. The preview shows the
// amount, its allocations and nothing pending for the chef, and stays
// current at the cancel; the notice explains the split.
func TestDeliveryNotPossibleOrdinaryCancellation(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	late, ld, lcake, ldelivery := h.paidDeliveryOrder(t, f, pol, "cl")
	early, ed, ecake, edelivery := h.paidDeliveryOrder(t, f, pol, "ce")
	h.withRefunds()
	h.withNotices(t)
	for _, o := range []struct {
		order string
		d     []byte
	}{{late, ld}, {early, ed}} {
		v, _ := h.choiceView(t, o.order, o.d)
		if _, err := h.notPossible("np-"+o.order[:8], o.order, v.Version, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		name             string
		order            string
		d                []byte
		cake, delivery   string
		now              time.Time
		rate             int
		amount, cakeBack int64
		notice           []string
	}{
		{"after the deadline", late, ld, lcake, ldelivery, cancelDeadline, 50, 17000, 5000,
			[]string{"Refund requested: £170: 50% of the £100 you paid for your cakes", "your delivery payment of £120 in full, because we could not deliver your order."}},
		{"before the deadline", early, ed, ecake, edelivery, cancelDeadline.Add(-time.Hour), 100, 22000, 10000,
			[]string{"Refund requested: £220, the full amount you paid."}},
	} {
		h.now = c.now
		p := h.preview(t, c.order, c.d)
		alloc := map[string]int64{}
		for _, a := range p.Allocations {
			alloc[a.CollectionID] = a.AmountMinor
		}
		if p.Rate != c.rate || p.PaidMinor != 22000 || p.AmountMinor != c.amount || alloc[c.cake] != c.cakeBack ||
			alloc[c.delivery] != 12000 || p.DeliveryPending != 0 {
			t.Fatalf("%s preview %+v", c.name, p)
		}
		v, _ := h.choiceView(t, c.order, c.d)
		if _, err := h.cancel(c.order, c.d, "cancel", v.Version, p); err != nil {
			t.Fatalf("%s cancel: %v", c.name, err)
		}
		h.due(t)
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("%s drain:\n%s", c.name, reasons(errs))
		}
		v2, raw := h.choiceView(t, c.order, c.d)
		if v2.Lifecycle != "cancelled" || *v2.CancelReason != "customer_cancelled" || v2.Refund == nil ||
			v2.Refund.TargetMinor != c.amount || v2.Refund.SucceededMinor != c.amount || v2.Refund.State != "returned" ||
			v2.Refund.DeliveryPending != 0 || *v2.Cancellation.AmountMinor != c.amount || *v2.Cancellation.Rate != c.rate {
			t.Fatalf("%s cancelled %s", c.name, raw)
		}
		if ck, dl := refundAmounts(t, h, c.cake), refundAmounts(t, h, c.delivery); len(ck) != 1 || ck[0] != c.cakeBack ||
			len(dl) != 1 || dl[0] != 12000 {
			t.Fatalf("%s refunds cake %v delivery %v", c.name, ck, dl)
		}
		var text string
		for _, n := range h.notices(t, c.order) {
			if n.Kind == "cancelled" {
				text = h.request(t, n).Text
			}
		}
		has(t, c.name+" notice", text, c.notice...)
	}
}

// The cancelled notice's Q5 sentence follows what was paid and whether it
// is returned already: nothing paid, being returned, returned.
func TestDeliveryChoiceCancelledNoticeWording(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	order, _ := h.paidOrder(t, f, h.policy(t, "p1"))
	render := func(patch, facts string) string {
		t.Helper()
		tx, err := h.db.MigratorConn(t).Begin(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(h.ctx)
		if _, err := tx.Exec(h.ctx, "SET LOCAL ROLE gk_owner"); err != nil {
			t.Fatal(err)
		}
		var text string
		if err := tx.QueryRow(h.ctx, `SELECT r.body_text FROM business.orders o,
			business.order_notice_render('cancelled', jsonb_populate_record(o, $2::jsonb), $3::json, 'https://gk.example') r
			WHERE o.id = $1::uuid`, order, patch, facts).Scan(&text); err != nil {
			t.Fatalf("render: %v", err)
		}
		return text
	}
	sorry := "We are sorry that we could not deliver your order. As you chose, it has been cancelled"
	text := render(`{"payment_requirement": "none", "fulfillment_mode": "gk_delivery"}`,
		`{"reason": "delivery_not_possible", "rate": 100, "paid_minor": 0, "refund_target_minor": 0}`)
	has(t, "nothing paid", text, sorry+".", "There was no payment for this order, so there is nothing to refund.")
	hasNot(t, "nothing paid", text, "everything you paid")
	text = render(`{"fulfillment_mode": "gk_delivery"}`,
		`{"reason": "delivery_not_possible", "rate": 100, "paid_minor": 22000, "refund_target_minor": 22000}`)
	has(t, "being returned", text, sorry+" and everything you paid is being returned.", "Refund requested: £220, the full amount you paid.")
	hasNot(t, "being returned", text, "everything you paid has been returned", "already been returned")
	text = render(`{"fulfillment_mode": "gk_delivery"}`,
		`{"reason": "delivery_not_possible", "rate": 100, "paid_minor": 22000, "refund_target_minor": 22000, "refund_returned": true}`)
	has(t, "returned", text, sorry+" and everything you paid has been returned.", "It has already been returned to you.")
	hasNot(t, "returned", text, "is being returned", "Refund requested")
}
