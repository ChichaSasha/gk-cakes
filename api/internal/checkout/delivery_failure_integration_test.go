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
)

// A failed GK delivery (GK-017 r02b state 19; Q17, Q18, Q10; K05, K06,
// K11; migration 0037). Every delivery order here is due on Saturday 14
// November 12:30 London (GMT) with a 10000p cake and a 12000p delivery; that
// day's published starts are 12:30, 13:00, 13:30 and 14:00. The delivery is
// dispatched at 11:30 and fails at 12:45 unless a test says otherwise. Every
// instant is the API's (test) clock: nothing compares the database clock
// with these fixture dates.

var (
	// The harness's starting clock: orders are placed, paid and quoted then.
	failOrderedAt  = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	failDispatchAt = time.Date(2026, 11, 14, 11, 30, 0, 0, time.UTC)
	failAt         = time.Date(2026, 11, 14, 12, 45, 0, 0, time.UTC)
)

type failureBlock struct {
	Cause             string
	At                time.Time
	State             string
	Open              bool
	Pending           bool
	Choices           []string
	Times             []choiceTime
	DeliveryPaidMinor *int64 `json:"delivery_paid_minor"`
	Chosen            *struct {
		Choice        string
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
	RedeliveryTimeSet *bool  `json:"redelivery_time_set"`
	CakeRefundMinor   *int64 `json:"cake_refund_minor"`
	DeliveryKeptMinor *int64 `json:"delivery_kept_minor"`
}

type failureOrderView struct {
	Version              int
	Lifecycle            string
	FulfillmentMode      string `json:"fulfillment_mode"`
	Date                 string
	FullRefundDeadlineAt time.Time `json:"full_refund_deadline_at"`
	Slot                 struct {
		ID       string
		StartsAt time.Time `json:"starts_at"`
	}
	Fulfillment struct {
		State   *string
		Pending *string
	}
	Delivery        *deliveryBlock
	DeliveryChoice  *choiceBlock  `json:"delivery_choice"`
	DeliveryFailure *failureBlock `json:"delivery_failure"`
	Refund          *struct {
		refundView
		Reason string
	}
}

func (h *payHarness) failureView(t *testing.T, order string, d []byte) (failureOrderView, string) {
	t.Helper()
	raw, err := h.co.GetOrder(h.ctx, order, "", d)
	if err != nil {
		t.Fatal(err)
	}
	var v failureOrderView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return v, string(raw)
}

func (h *payHarness) deliveryFailed(subject, key, order string, version int, cause, reason string) (checkout.DeliveryFailed, error) {
	in := checkout.DeliveryFailedInput{ExpectedVersion: version, Cause: cause}
	if reason != "" {
		in.Reason = &reason
	}
	return h.co.RecordFailedDelivery(h.ctx, subject, key, order, in)
}

func (h *payHarness) chooseAfterFailure(order string, d []byte, key string, version int, choice, slot string) (catalog.Result, error) {
	return h.co.ChooseAfterDeliveryFailure(h.ctx, order, "", d, key, checkout.DeliveryFailureChoiceInput{ExpectedVersion: version,
		Choice: choice, TargetSlotID: slot})
}

// dispatchedOrder is paidDeliveryOrder made ready and dispatched at
// failDispatchAt: the order, its digest, the cake and delivery collections
// and the order's version.
func (h *payHarness) dispatchedOrder(t *testing.T, f fixture, pol, key string) (string, []byte, string, string, int) {
	t.Helper()
	h.now = failOrderedAt
	order, d, cake, delivery := h.paidDeliveryOrder(t, f, pol, key)
	version := h.makeReady(t, order, d, key)
	h.now = failDispatchAt
	if _, err := h.fulfil(fulfilment, "dispatch-"+key, order, checkout.ActionDispatch, version, "Courier ref "+key); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	return order, d, cake, delivery, version + 1
}

// chefAgreesTime is the chef's appointment change (0031) to target on
// date, agreed with the customer by phone, drained to its secured outcome.
func (h *payHarness) chefAgreesTime(t *testing.T, order, key string, version int, date, slot string) {
	t.Helper()
	in := checkout.AppointmentInput{ExpectedVersion: version, TargetDate: date, TargetSlotID: slot,
		CustomerAgreement: &checkout.Agreement{Channel: "phone", AgreedAt: h.now.Add(-time.Minute).Format(time.RFC3339),
			EvidenceRef: "call after the failed delivery"},
		FeasibilityNote: "Redelivery agreed"}
	if _, err := h.co.ChangeAppointment(h.ctx, chef, key, order, in); err != nil {
		t.Fatalf("appointment change: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
}

// The chef records a failed delivery only once it was dispatched, chef only,
// versioned and idempotent, refused during a restore freeze (the same key
// replays), once per dispatch. Our side opens the customer's choice without
// refunding anything; the reason reaches only the chef; dispatch, a no-show
// for that dispatch, the chef's appointment change, a cancellation and a Q5
// decision all wait for the choice.
func TestDeliveryFailedRules(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	slots := h.slotsOn(t, "2026-11-14")

	// Not a GK delivery, and a GK delivery not dispatched yet: refused.
	coll, cd, _ := h.readyOrder(t, f, pol, "col")
	cv, _ := h.failureView(t, coll, cd)
	for _, cause := range []string{checkout.CauseOurSide, checkout.CauseNobodyAvailable} {
		h.now = failAt
		if _, err := h.deliveryFailed(chef, "c-"+cause, coll, cv.Version, cause, ""); conflict(err) != "transition_not_allowed" {
			t.Fatalf("collection order %s: %v", cause, err)
		}
	}
	h.now = failOrderedAt
	order, d, cake, delivery := h.paidDeliveryOrder(t, f, pol, "df")
	version := h.makeReady(t, order, d, "df")
	h.now = failAt
	if _, err := h.deliveryFailed(chef, "early", order, version, checkout.CauseOurSide, ""); conflict(err) != "transition_not_allowed" {
		t.Fatalf("before the dispatch: %v", err)
	}
	if _, err := h.deliveryFailed(chef, "early-n", order, version, checkout.CauseNobodyAvailable, ""); conflict(err) != "transition_not_allowed" {
		t.Fatalf("nobody available before the dispatch: %v", err)
	}
	h.now = failDispatchAt
	if _, err := h.fulfil(fulfilment, "dispatch", order, checkout.ActionDispatch, version); err != nil {
		t.Fatal(err)
	}
	version++
	h.now = failAt
	if v, raw := h.failureView(t, order, d); v.DeliveryFailure != nil || !strings.Contains(strings.ReplaceAll(raw, " ", ""), `"delivery_failure":null`) {
		t.Fatalf("no failure yet: %s", raw)
	}
	// Chef only: fulfilment records a delivery exception instead.
	for name, who := range map[string]string{"fulfilment": fulfilment, "support": support, "customer": customerA} {
		for _, cause := range []string{checkout.CauseOurSide, checkout.CauseNobodyAvailable} {
			if _, err := h.deliveryFailed(who, "x", order, version, cause, ""); !errors.Is(err, catalog.ErrForbidden) {
				t.Errorf("%s by %s: %v", cause, name, err)
			}
		}
	}
	if _, err := h.deliveryFailed(chef, "bad", order, version, "weather", ""); !strings.HasPrefix(invalidField(err), "cause:") {
		t.Fatalf("unknown cause: %v", err)
	}
	if _, err := h.deliveryFailed(chef, "bad2", order, version, checkout.CauseOurSide, "two\nlines"); !strings.HasPrefix(invalidField(err), "reason") {
		t.Fatalf("two-line reason: %v", err)
	}
	if _, err := h.deliveryFailed(chef, "stale", order, version-1, checkout.CauseOurSide, ""); conflict(err) != "stale_version" {
		t.Fatalf("stale: %v", err)
	}
	// A restore freeze refuses it before any outcome; the same key replays.
	reason := "Van broke down on the A13"
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if _, err := h.deliveryFailed(chef, "f1", order, version, checkout.CauseOurSide, reason); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("frozen: %v", err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	rec, err := h.deliveryFailed(chef, "f1", order, version, checkout.CauseOurSide, reason)
	if err != nil || rec.Replayed || rec.Cause != checkout.CauseOurSide {
		t.Fatalf("failure: %+v %v", rec, err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if again, err := h.deliveryFailed(chef, "f1", order, version, checkout.CauseOurSide, reason); err != nil || !again.Replayed ||
		again.ID != rec.ID || again.Cause != checkout.CauseOurSide {
		t.Fatalf("replay while frozen: %+v %v", again, err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	if _, err := h.deliveryFailed(chef, "f1", order, version, checkout.CauseOurSide, "Another reason"); conflict(err) != "key_reused" {
		t.Fatalf("key reused: %v", err)
	}
	// One failure per dispatch; no no-show for a dispatch that failed on our side.
	if _, err := h.deliveryFailed(chef, "f2", order, version+1, checkout.CauseOurSide, ""); conflict(err) != "transition_not_allowed" {
		t.Fatalf("a second failure of the same dispatch: %v", err)
	}
	if _, err := h.deliveryFailed(chef, "n1", order, version+1, checkout.CauseNobodyAvailable, ""); conflict(err) != "transition_not_allowed" {
		t.Fatalf("nobody available after our side: %v", err)
	}
	if _, err := h.fulfil(chef, "ns", order, checkout.ActionNoShow, version+1); conflict(err) != "transition_not_allowed" {
		t.Fatalf("no-show after our side: %v", err)
	}

	// The customer's view: the choice is open with the times still ahead
	// today (API clock), the delivery at stake, nothing refunded; no reason.
	v, raw := h.failureView(t, order, d)
	b := v.DeliveryFailure
	if b == nil || b.Cause != checkout.CauseOurSide || b.State != "recorded" || !b.At.Equal(failAt) || !b.Open || b.Pending ||
		strings.Join(b.Choices, ",") != "redelivery,collection" || timeIDs(b.Times) != strings.Join(slots[1:], ",") ||
		b.DeliveryPaidMinor == nil || *b.DeliveryPaidMinor != 12000 || b.Chosen != nil || v.Version != version+1 ||
		v.Lifecycle != "ready" || state2(v) != "exception" || v.FulfillmentMode != "gk_delivery" || v.Refund != nil ||
		v.Delivery.State != "paid" || *v.Delivery.Payable {
		t.Fatalf("after the failure: %s", raw)
	}
	if strings.Contains(raw, "A13") {
		t.Fatalf("the chef's reason reached the customer: %s", raw)
	}
	if n := h.count(t, "SELECT count(*) FROM business.refund_operation WHERE order_id = $1::uuid", order); n != 0 ||
		h.count(t, "SELECT count(*) FROM business.refund_floor WHERE order_id = $1::uuid", order) != 0 ||
		h.count(t, "SELECT count(*) FROM business.refund_goal WHERE order_id = $1::uuid", order) != 0 {
		t.Fatalf("money moved at the failure (%d refunds)", n)
	}
	if audits := h.auditDetails(t, "delivery.failed"); len(audits) != 1 || !strings.Contains(audits[0], rec.ID) ||
		strings.Contains(audits[0], "A13") {
		t.Fatalf("audit %v", audits)
	}
	// Staff: the chef sees the reason and the flag; technical support
	// neither the reason nor money; fulfilment the state.
	chefView := h.staffJSON(t, chef, order)
	c := compactJSON(t, chefView["delivery_failure"])
	if !strings.Contains(c, `"reason":"Van broke down on the A13"`) || !strings.Contains(c, `"open":true`) ||
		!strings.Contains(c, `"delivery_minor":12000`) || !strings.Contains(string(chefView["attention"]), "delivery_failure_open") {
		t.Fatalf("chef %s %s", c, chefView["attention"])
	}
	if hist := compactJSON(t, chefView["fulfillment_history"]); !strings.Contains(hist, `"action":"delivery_failed"`) {
		t.Fatalf("fulfilment history %s", hist)
	}
	sup := compactJSON(t, h.staffJSON(t, support, order)["delivery_failure"])
	if strings.Contains(sup, "A13") || strings.Contains(sup, "12000") || !strings.Contains(sup, `"history":[{`) {
		t.Fatalf("support %s", sup)
	}
	var ful map[string]json.RawMessage
	if err := json.Unmarshal(h.staffJSON(t, fulfilment, order)["delivery_failure"], &ful); err != nil || len(ful) != 6 ||
		string(ful["open"]) != "true" || ful["times"] != nil || ful["delivery_paid_minor"] != nil {
		t.Fatalf("fulfilment %v %v", ful, err)
	}
	// Everything else waits for the customer's choice.
	if _, err := h.fulfil(fulfilment, "re-dispatch", order, checkout.ActionDispatch, version+1); conflict(err) != "delivery_failure_open" {
		t.Fatalf("dispatch before the choice: %v", err)
	}
	if _, err := h.co.ChangeAppointment(h.ctx, chef, "a0", order, checkout.AppointmentInput{ExpectedVersion: version + 1,
		TargetDate: "2026-11-21", TargetSlotID: h.slotsOn(t, "2026-11-21")[0],
		CustomerAgreement: &checkout.Agreement{Channel: "phone", AgreedAt: "2026-11-14T12:40:00Z", EvidenceRef: "call"},
		FeasibilityNote:   "x"}); conflict(err) != "move_not_eligible" {
		t.Fatalf("appointment change before the choice: %v", err)
	}
	if _, err := h.co.PreviewCancellation(h.ctx, order, "", d); conflict(err) != "cancellation_not_eligible" {
		t.Fatalf("cancellation preview while the choice is open: %v", err)
	}
	// Q5 is before dispatch: no "GK can't deliver" on this order (an order
	// can't have both an open Q5 choice and a failure choice).
	if _, err := h.notPossible("np", order, version+1, ""); conflict(err) != "state_conflict" {
		t.Fatalf("GK can't deliver after a dispatch: %v", err)
	}
	if v.DeliveryChoice == nil || v.DeliveryChoice.Open {
		t.Fatalf("Q5 block %s", raw)
	}
	_, _ = cake, delivery

	// An exception after the dispatch is still a delivery on its way.
	exc, ed, _, _, ev := h.dispatchedOrder(t, f, pol, "ex")
	if _, err := h.fulfil(fulfilment, "e1", exc, checkout.ActionDeliveryException, ev, "Traffic"); err != nil {
		t.Fatal(err)
	}
	h.now = failAt
	if _, err := h.deliveryFailed(chef, "f-exc", exc, ev+1, checkout.CauseOurSide, ""); err != nil {
		t.Fatalf("failure after an exception: %v", err)
	}
	if v, raw := h.failureView(t, exc, ed); v.DeliveryFailure == nil || !v.DeliveryFailure.Open {
		t.Fatalf("exception then failure: %s", raw)
	}
	h.noPersonalDataInLogs(t)
}

func state2(v failureOrderView) string {
	if v.Fulfillment.State == nil {
		return "<nil>"
	}
	return *v.Fulfillment.State
}

// Redelivery: the order stays a GK delivery and nothing is refunded (no
// floor is left behind: a later cancellation follows the ordinary rates);
// the chef sets the new time through the appointment change, after which the
// delivery can be dispatched again and handed over. Access: the owner or a
// guest link with move_pickup.
func TestDeliveryFailureRedelivery(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	h.withRefunds()
	order, d, _, delivery, version := h.dispatchedOrder(t, f, pol, "rd")
	h.now = failAt
	if _, err := h.deliveryFailed(chef, "f1", order, version, checkout.CauseOurSide, ""); err != nil {
		t.Fatal(err)
	}
	v, _ := h.failureView(t, order, d)
	// Access: another guest or customer, and a link without move_pickup.
	_, other := proof(t)
	if _, err := h.chooseAfterFailure(order, other, "x1", v.Version, checkout.FailureRedelivery, ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("another guest: %v", err)
	}
	if _, err := h.co.ChooseAfterDeliveryFailure(h.ctx, order, customerB, nil, "x2", checkout.DeliveryFailureChoiceInput{
		ExpectedVersion: v.Version, Choice: checkout.FailureRedelivery}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("another customer: %v", err)
	}
	_, cancelOnly := proof(t)
	h.owner(t, `INSERT INTO business.order_guest_capability (id, order_id, capability_digest, allowed_commands)
		VALUES (gen_random_uuid(), $1::uuid, $2, ARRAY['read', 'cancel'])`, order, cancelOnly)
	if _, err := h.chooseAfterFailure(order, cancelOnly, "x3", v.Version, checkout.FailureRedelivery, ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("link without move_pickup: %v", err)
	}
	if _, err := h.chooseAfterFailure(order, d, "x4", v.Version, checkout.FailureRedelivery, h.slotsOn(t, "2026-11-14")[2]); !strings.HasPrefix(invalidField(err), "target_slot_id:") {
		t.Fatalf("redelivery with a time: %v", err)
	}
	if _, err := h.chooseAfterFailure(order, d, "x5", v.Version, "full_refund", ""); !strings.HasPrefix(invalidField(err), "choice:") {
		t.Fatalf("unknown choice: %v", err)
	}
	if _, err := h.chooseAfterFailure(order, d, "stale", v.Version-1, checkout.FailureRedelivery, ""); conflict(err) != "stale_version" {
		t.Fatalf("stale: %v", err)
	}
	// A restore freeze refuses a new choice before any outcome.
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if _, err := h.chooseAfterFailure(order, d, "c1", v.Version, checkout.FailureRedelivery, ""); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("frozen: %v", err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	receipt := h.now
	op, err := h.chooseAfterFailure(order, d, "c1", v.Version, checkout.FailureRedelivery, "")
	if err != nil || op.Replayed {
		t.Fatalf("choice: %+v %v", op, err)
	}
	// The same key replays, also while frozen; the commit waits for the thaw.
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if again, err := h.chooseAfterFailure(order, d, "c1", v.Version, checkout.FailureRedelivery, ""); err != nil || !again.Replayed || again.ID != op.ID {
		t.Fatalf("replay while frozen: %+v %v", again, err)
	}
	if err := h.moveJob(t, op.ID); err == nil {
		t.Fatal("committed while frozen")
	}
	if got := h.recoveryState(t, op.ID); got != "reserved" {
		t.Fatalf("while frozen: %s", got)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	h.due(t)
	if _, err := h.chooseAfterFailure(order, d, "c1", v.Version, checkout.FailureCollection, h.slotsOn(t, "2026-11-14")[2]); conflict(err) != "key_reused" {
		t.Fatalf("key reused: %v", err)
	}
	if _, err := h.chooseAfterFailure(order, d, "c2", v.Version, checkout.FailureCollection, h.slotsOn(t, "2026-11-14")[2]); conflict(err) != "state_conflict" {
		t.Fatalf("a second choice while one is pending: %v", err)
	}
	if p, raw := h.failureView(t, order, d); p.DeliveryFailure.Open || !p.DeliveryFailure.Pending || len(p.DeliveryFailure.Choices) != 0 ||
		len(p.DeliveryFailure.Times) != 0 || p.DeliveryFailure.Chosen == nil || p.DeliveryFailure.Chosen.State != "processing" {
		t.Fatalf("pending: %s", raw)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	v2, raw := h.failureView(t, order, d)
	b := v2.DeliveryFailure
	if b.Open || b.Chosen == nil || b.Chosen.State != "chosen" || b.Chosen.Choice != checkout.FailureRedelivery || b.Chosen.To != nil ||
		b.Chosen.DeliveryRefundMinor != nil || !b.Chosen.RequestedAt.Equal(receipt) || b.RedeliveryTimeSet == nil || *b.RedeliveryTimeSet ||
		v2.FulfillmentMode != "gk_delivery" || v2.Version != v.Version+1 || v2.Refund != nil || v2.Delivery.State != "paid" ||
		v2.Slot.ID != f.slot {
		t.Fatalf("redelivery chosen: %s", raw)
	}
	if n := h.count(t, "SELECT count(*) FROM business.refund_operation WHERE order_id = $1::uuid", order); n != 0 ||
		h.count(t, "SELECT count(*) FROM business.refund_floor WHERE collection_id = $1::uuid", delivery) != 0 {
		t.Fatalf("money moved for a redelivery (%d refunds)", n)
	}
	if flags := string(h.staffJSON(t, chef, order)["attention"]); !strings.Contains(flags, "redelivery_time_needed") ||
		strings.Contains(flags, "delivery_failure_open") {
		t.Fatalf("flags %s", flags)
	}
	if _, err := h.chooseAfterFailure(order, d, "c3", v2.Version, checkout.FailureCollection, h.slotsOn(t, "2026-11-14")[3]); conflict(err) != "choice_not_open" {
		t.Fatalf("choice after the choice: %v", err)
	}
	// Back to waiting for a dispatch: an ordinary cancellation preview again
	// (50% of everything after the deadline, the delivery share pending the
	// chef as K05 says): no 100% delivery floor survives the redelivery.
	p := h.preview(t, order, d)
	if p.Rate != 50 || p.PaidMinor != 22000 || p.AmountMinor != 11000 || p.DeliveryPending != 6000 {
		t.Fatalf("preview after the redelivery choice %+v", p)
	}
	// The chef sets the new time (another day, agreed with the customer);
	// only then can it be dispatched again.
	if _, err := h.fulfil(fulfilment, "d-early", order, checkout.ActionDispatch, v2.Version); conflict(err) != "redelivery_time_not_set" {
		t.Fatalf("dispatch before the new time: %v", err)
	}
	next := h.slotsOn(t, "2026-11-21")
	h.chefAgreesTime(t, order, "a1", v2.Version, "2026-11-21", next[1])
	v3, raw := h.failureView(t, order, d)
	if v3.Date != "2026-11-21" || v3.Slot.ID != next[1] || v3.DeliveryFailure.RedeliveryTimeSet == nil || !*v3.DeliveryFailure.RedeliveryTimeSet ||
		!v3.FullRefundDeadlineAt.Equal(cancelDeadline) {
		t.Fatalf("new time set: %s", raw)
	}
	if flags := string(h.staffJSON(t, chef, order)["attention"]); strings.Contains(flags, "redelivery_time_needed") {
		t.Fatalf("flags after the new time %s", flags)
	}
	h.now = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	if _, err := h.fulfil(fulfilment, "d2", order, checkout.ActionDispatch, v3.Version); err != nil {
		t.Fatalf("re-dispatch: %v", err)
	}
	if _, err := h.co.PreviewCancellation(h.ctx, order, "", d); conflict(err) != "cancellation_not_eligible" {
		t.Fatalf("cancellation after the re-dispatch: %v", err)
	}
	if _, err := h.fulfil(fulfilment, "h1", order, checkout.ActionHandover, v3.Version+1); err != nil {
		t.Fatalf("handover: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	if v4, raw := h.failureView(t, order, d); v4.Lifecycle != "completed" || state2(v4) != "completed" || v4.Refund != nil {
		t.Fatalf("delivered %s", raw)
	}
	if audits := h.auditDetails(t, "order.delivery_failure_choice_made"); len(audits) != 1 || !strings.Contains(audits[0], op.ID) {
		t.Fatalf("audit %v", audits)
	}
}

// Collection today: the times still ahead today (API clock), the protected
// command through its crash points (journal down -> nothing; outcome
// envelope down -> committed, still shown as the GK delivery with no refund
// even after the reserved delivery refund went back; secured -> collection
// at the new time), the paid delivery returned once in full through its
// floor, the cake untouched, one refund email, staff redaction.
func TestDeliveryFailureCollection(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	slots := h.slotsOn(t, "2026-11-14")
	order, d, cake, delivery, version := h.dispatchedOrder(t, f, pol, "cl")
	h.withRefunds()
	h.withNotices(t)
	_, anchor := h.productionJob(t, order)
	h.now = failAt
	if _, err := h.deliveryFailed(chef, "f1", order, version, checkout.CauseOurSide, "Driver ill"); err != nil {
		t.Fatal(err)
	}
	// Saturday 13:15: only the later starts are still ahead.
	h.now = time.Date(2026, 11, 14, 13, 15, 0, 0, time.UTC)
	v, raw := h.failureView(t, order, d)
	if timeIDs(v.DeliveryFailure.Times) != slots[2]+","+slots[3] {
		t.Fatalf("Saturday 13:15: %s", raw)
	}
	for name, slot := range map[string]string{"a start already passed": slots[1], "another day's start": h.slotsOn(t, "2026-11-21")[2],
		"an unknown slot": "8d3f0f8e-5d55-4c3e-9a49-34a0e2a9f001"} {
		if _, err := h.chooseAfterFailure(order, d, "x-"+strings.ReplaceAll(name, " ", "-"), v.Version, checkout.FailureCollection, slot); conflict(err) != "slot_unavailable" {
			t.Errorf("%s: %v", name, err)
		}
	}
	op, err := h.chooseAfterFailure(order, d, "c1", v.Version, checkout.FailureCollection, slots[2])
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
		mode != "collection" || h.recoveryState(t, op.ID) != "committed" || h.orderSlot(t, order).Slot != slots[2] {
		t.Fatalf("committed: %s %v", mode, err)
	}
	// The reserved delivery refund goes back while the outcome is
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
	if p, raw := h.failureView(t, order, d); p.FulfillmentMode != "gk_delivery" || p.Slot.ID != f.slot || p.Delivery.State != "paid" ||
		p.Refund != nil || p.DeliveryFailure.Chosen == nil || p.DeliveryFailure.Chosen.State != "processing" ||
		strings.Contains(raw, "delivery_refund_minor") || p.DeliveryChoice == nil {
		t.Fatalf("committed but unsecured shown as %s", raw)
	}
	if n := noticeCount(h.notices(t, order), "refund_returned"); n != 0 {
		t.Fatalf("a refund email before the choice is secured (%d)", n)
	}
	var block *string
	if err := h.admin.QueryRow(h.ctx, "SELECT business.account_order_block(o) FROM business.orders o WHERE o.id = $1::uuid",
		order).Scan(&block); err != nil || block == nil || *block != "order_change_in_progress" {
		t.Fatalf("account deletion block while unsecured: %v %v", block, err)
	}
	if c := compactJSON(t, h.staffJSON(t, chef, order)["delivery_failure"]); strings.Contains(c, "delivery_refund_minor") {
		t.Fatalf("chef sees the unsecured refund %s", c)
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
	v2, raw := h.failureView(t, order, d)
	b := v2.DeliveryFailure
	if v2.FulfillmentMode != "collection" || v2.Slot.ID != slots[2] || !v2.Slot.StartsAt.Equal(at("13:30")) || v2.Date != "2026-11-14" ||
		!v2.FullRefundDeadlineAt.Equal(cancelDeadline) || b.Chosen.State != "chosen" || b.Chosen.To == nil ||
		b.Chosen.To.FulfillmentMode != "collection" || b.Chosen.DeliveryRefundMinor == nil || *b.Chosen.DeliveryRefundMinor != 12000 ||
		b.Open || v2.DeliveryChoice != nil {
		t.Fatalf("collection chosen: %s", raw)
	}
	if v2.Delivery == nil || v2.Delivery.State != "cancelled_by_choice" || v2.Delivery.PaidMinor == nil || *v2.Delivery.PaidMinor != 12000 ||
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
	if _, at := h.productionJob(t, order); !at.Equal(anchor) {
		t.Fatalf("production anchor %s, was %s", at, anchor)
	}
	if got := h.allocation(t, order); got != "committed" {
		t.Fatalf("day place %s", got)
	}
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
	if again, err := h.chooseAfterFailure(order, d, "c1", v.Version, checkout.FailureCollection, slots[2]); err != nil || !again.Replayed || again.ID != op.ID {
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
	if c := compactJSON(t, h.staffJSON(t, chef, order)["delivery_failure"]); !strings.Contains(c, `"delivery_refund_minor":12000`) ||
		!strings.Contains(c, "Driver ill") || !strings.Contains(c, `"access":"guest_link"`) {
		t.Fatalf("chef %s", c)
	}
	sup := compactJSON(t, h.staffJSON(t, support, order)["delivery_failure"])
	if strings.Contains(sup, "12000") || strings.Contains(sup, "Driver") || !strings.Contains(sup, `"state":"chosen"`) {
		t.Fatalf("support %s", sup)
	}
	if ful := compactJSON(t, h.staffJSON(t, fulfilment, order)["delivery_failure"]); strings.Contains(ful, "12000") ||
		!strings.Contains(ful, `"fulfillment_mode":"collection"`) {
		t.Fatalf("fulfilment %s", ful)
	}
	hasNot(t, "audit", strings.Join(h.auditDetails(t, "order.delivery_failure_choice_made"), ""), "example.com", "Test Customer", "Driver")
	// A collection order again: cancellable under the ordinary rate, the
	// delivery's floor keeping it at 100% (cake 50% = 5000p still to come).
	p := h.preview(t, order, d)
	if p.Rate != 50 || p.PaidMinor != 22000 || p.AmountMinor != 17000 || p.DeliveryPending != 0 || len(p.Allocations) != 1 ||
		p.Allocations[0].CollectionID != cake || p.Allocations[0].AmountMinor != 5000 {
		t.Fatalf("preview after the collection choice %+v", p)
	}
	// The cake is collected later that day.
	h.now = at("13:40")
	if _, err := h.fulfil(fulfilment, "h1", order, checkout.ActionHandover, v2.Version); err != nil {
		t.Fatalf("handover: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	if v3, raw := h.failureView(t, order, d); v3.Lifecycle != "completed" || v3.Refund == nil || v3.Refund.TargetMinor != 12000 {
		t.Fatalf("collected %s", raw)
	}
}

// Nobody available (Q18): the chef's no-show for a delivery on its way, at
// or after the appointment start, recorded through the delivery-failed
// route as the existing no-show command: 50% of the cake back, requested
// automatically; the delivery payment kept (nothing of it refunded or left
// pending for the chef); the no_show notice says so.
func TestDeliveryFailureNobodyAvailable(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d, cake, delivery, version := h.dispatchedOrder(t, f, pol, "na")
	h.withRefunds()
	h.withNotices(t)
	h.now = at("12:29")
	if _, err := h.deliveryFailed(chef, "early", order, version, checkout.CauseNobodyAvailable, ""); conflict(err) != "handover_not_due" {
		t.Fatalf("before the appointment: %v", err)
	}
	h.now = failAt
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if _, err := h.deliveryFailed(chef, "n1", order, version, checkout.CauseNobodyAvailable, "Rang twice"); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("frozen: %v", err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	rec, err := h.deliveryFailed(chef, "n1", order, version, checkout.CauseNobodyAvailable, "Rang twice")
	if err != nil || rec.Replayed || rec.Cause != checkout.CauseNobodyAvailable {
		t.Fatalf("nobody available: %+v %v", rec, err)
	}
	if again, err := h.deliveryFailed(chef, "n1", order, version, checkout.CauseNobodyAvailable, "Rang twice"); err != nil ||
		!again.Replayed || again.ID != rec.ID || again.Cause != checkout.CauseNobodyAvailable {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, err := h.deliveryFailed(chef, "n1", order, version, checkout.CauseOurSide, ""); conflict(err) != "key_reused" {
		t.Fatalf("key reused: %v", err)
	}
	// The result is the no-show command (K11): pending, nothing visible.
	var command string
	if err := h.admin.QueryRow(h.ctx, "SELECT command FROM business.order_command WHERE id = $1::uuid", rec.ID).Scan(&command); err != nil ||
		command != "no_show" {
		t.Fatalf("command %s %v", command, err)
	}
	if p, raw := h.failureView(t, order, d); p.Lifecycle != "ready" || p.Fulfillment.Pending == nil || *p.Fulfillment.Pending != "no_show" ||
		p.DeliveryFailure == nil || p.DeliveryFailure.State != "processing" || p.DeliveryFailure.CakeRefundMinor != nil || p.Refund != nil ||
		strings.Contains(raw, "Rang") {
		t.Fatalf("pending: %s", raw)
	}
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	v, raw := h.failureView(t, order, d)
	b := v.DeliveryFailure
	if v.Lifecycle != "completed" || state2(v) != "no_show" || b.Cause != checkout.CauseNobodyAvailable || b.State != "no_show" ||
		b.Open || b.CakeRefundMinor == nil || *b.CakeRefundMinor != 5000 || b.DeliveryKeptMinor == nil || *b.DeliveryKeptMinor != 12000 {
		t.Fatalf("no-show: %s", raw)
	}
	if v.Refund == nil || v.Refund.TargetMinor != 5000 || v.Refund.SucceededMinor != 5000 || v.Refund.DeliveryPending != 0 ||
		v.Refund.State != "returned" {
		t.Fatalf("refund: %s", raw)
	}
	if c, dl := refundAmounts(t, h, cake), refundAmounts(t, h, delivery); len(c) != 1 || c[0] != 5000 || len(dl) != 0 {
		t.Fatalf("refunds cake %v delivery %v", c, dl)
	}
	if n := h.count(t, "SELECT count(*) FROM business.delivery_kept WHERE collection_id = $1::uuid AND amount_minor = 12000", delivery); n != 1 {
		t.Fatalf("delivery kept rows %d", n)
	}
	var text string
	for _, n := range h.notices(t, order) {
		if n.Kind == "no_show" {
			text = h.request(t, n).Text
		}
	}
	has(t, "no-show notice", text, "Our terms return 50% of the cake price when nobody is available to receive a delivery: £50 of the £100 you paid for your cakes.",
		"Your delivery payment of £120 is not refunded, because the delivery was attempted.", "£50")
	hasNot(t, "no-show notice", text, "£110", "Rang twice", "The chef will contact you")
	// Staff: the chef sees the reason and the money; technical support neither.
	if c := compactJSON(t, h.staffJSON(t, chef, order)["delivery_failure"]); !strings.Contains(c, "Rang twice") ||
		!strings.Contains(c, `"delivery_kept_minor":12000`) || !strings.Contains(c, rec.ID) {
		t.Fatalf("chef %s", c)
	}
	if sup := compactJSON(t, h.staffJSON(t, support, order)["delivery_failure"]); strings.Contains(sup, "Rang") ||
		strings.Contains(sup, "12000") || strings.Contains(sup, "5000") {
		t.Fatalf("support %s", sup)
	}
	if audits := h.auditDetails(t, "order.no_show_requested"); len(audits) != 1 || !strings.Contains(audits[0], "nobody_available") ||
		strings.Contains(audits[0], "Rang") {
		t.Fatalf("audit %v", audits)
	}
	// Completed: nothing more on this order.
	if _, err := h.deliveryFailed(chef, "n2", order, v.Version, checkout.CauseNobodyAvailable, ""); conflict(err) != "transition_not_allowed" {
		t.Fatalf("after the no-show: %v", err)
	}
}

// #55 strays and the failure: a stray delivery payment (an earlier quote
// paid after it was replaced) is returned in full on top whichever way the
// failure goes: with a collection after our side (paid delivery + stray),
// with nobody available (50% of the cake + stray; the matched delivery is
// kept, the stray is not).
func TestDeliveryFailureWithAStray(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	slots := h.slotsOn(t, "2026-11-14")
	strayed := func(key string) (string, []byte, string, string, string, int) {
		h.now = failOrderedAt
		order, d, cake, stray, _ := h.strayOrder(t, f, pol, key)
		coll, s := h.deliveryStarted(t, order, d, "c2-"+key)
		h.deliveryPaid(t, order, coll, s, "evt_paid_q2_"+key)
		version := h.makeReady(t, order, d, key)
		h.now = failDispatchAt
		if _, err := h.fulfil(fulfilment, "dispatch-"+key, order, checkout.ActionDispatch, version); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		return order, d, cake, stray, coll, version + 1
	}
	order, d, cake, stray, delivery, version := strayed("sc")
	other, od, ocake, ostray, odelivery, oversion := strayed("sn")
	h.withRefunds()
	h.now = failAt
	if _, err := h.deliveryFailed(chef, "f1", order, version, checkout.CauseOurSide, ""); err != nil {
		t.Fatal(err)
	}
	v, raw := h.failureView(t, order, d)
	if v.DeliveryFailure.DeliveryPaidMinor == nil || *v.DeliveryFailure.DeliveryPaidMinor != 14000 {
		t.Fatalf("failure with a stray: %s", raw)
	}
	if _, err := h.chooseAfterFailure(order, d, "c1", v.Version, checkout.FailureCollection, slots[3]); err != nil {
		t.Fatal(err)
	}
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	if v, raw := h.failureView(t, order, d); v.Refund == nil || v.Refund.TargetMinor != 26000 || v.Refund.SucceededMinor != 26000 ||
		*v.DeliveryFailure.Chosen.DeliveryRefundMinor != 14000 {
		t.Fatalf("collection with a stray: %s", raw)
	}
	if c, dl, s := refundAmounts(t, h, cake), refundAmounts(t, h, delivery), refundAmounts(t, h, stray); len(c) != 0 ||
		len(dl) != 1 || dl[0] != 14000 || len(s) != 1 || s[0] != 12000 {
		t.Fatalf("refunds cake %v delivery %v stray %v", c, dl, s)
	}

	if _, err := h.deliveryFailed(chef, "n1", other, oversion, checkout.CauseNobodyAvailable, ""); err != nil {
		t.Fatal(err)
	}
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	ov, raw := h.failureView(t, other, od)
	if ov.Refund == nil || ov.Refund.TargetMinor != 17000 || ov.Refund.SucceededMinor != 17000 || ov.Refund.DeliveryPending != 0 ||
		*ov.DeliveryFailure.CakeRefundMinor != 5000 || *ov.DeliveryFailure.DeliveryKeptMinor != 14000 {
		t.Fatalf("nobody available with a stray: %s", raw)
	}
	if c, dl, s := refundAmounts(t, h, ocake), refundAmounts(t, h, odelivery), refundAmounts(t, h, ostray); len(c) != 1 || c[0] != 5000 ||
		len(dl) != 0 || len(s) != 1 || s[0] != 12000 {
		t.Fatalf("refunds cake %v delivery %v stray %v", c, dl, s)
	}
}

// #60 Q5 is before the dispatch: an order where the chef recorded "GK can't
// deliver" is never dispatched, so no failure can be recorded on it (and the
// reverse is in TestDeliveryFailedRules).
func TestDeliveryFailureNotAfterQ5(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d, _, _ := h.paidDeliveryOrder(t, f, pol, "q5")
	v, _ := h.failureView(t, order, d)
	if _, err := h.notPossible("np", order, v.Version, ""); err != nil {
		t.Fatal(err)
	}
	version := h.makeReady(t, order, d, "q5")
	if _, err := h.fulfil(fulfilment, "d1", order, checkout.ActionDispatch, version); conflict(err) != "delivery_not_possible" {
		t.Fatalf("dispatch: %v", err)
	}
	h.now = failAt
	for _, cause := range []string{checkout.CauseOurSide, checkout.CauseNobodyAvailable} {
		if _, err := h.deliveryFailed(chef, "f-"+cause, order, version, cause, ""); conflict(err) != "transition_not_allowed" {
			t.Fatalf("%s after GK can't deliver: %v", cause, err)
		}
	}
	if v, raw := h.failureView(t, order, d); v.DeliveryFailure != nil || !v.DeliveryChoice.Open {
		t.Fatalf("Q5 order %s", raw)
	}
}

// The customer cannot cancel once the GK delivery is on its way (out for
// delivery or an exception after the dispatch; K05/K06, Q18): preview and
// cancel are 409 cancellation_not_eligible; the chef's cancellation stays
// possible. Before the dispatch the preview works.
func TestCancellationAfterDispatch(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d, _, _ := h.paidDeliveryOrder(t, f, pol, "cx")
	version := h.makeReady(t, order, d, "cx")
	h.now = failDispatchAt
	p := h.preview(t, order, d)
	if p.Rate != 50 || p.AmountMinor != 11000 {
		t.Fatalf("preview before the dispatch %+v", p)
	}
	if _, err := h.fulfil(fulfilment, "d1", order, checkout.ActionDispatch, version); err != nil {
		t.Fatal(err)
	}
	if _, err := h.co.PreviewCancellation(h.ctx, order, "", d); conflict(err) != "cancellation_not_eligible" {
		t.Fatalf("preview out for delivery: %v", err)
	}
	if _, err := h.cancel(order, d, "cancel", version+1, p); conflict(err) != "cancellation_not_eligible" {
		t.Fatalf("cancel out for delivery: %v", err)
	}
	if _, err := h.fulfil(fulfilment, "e1", order, checkout.ActionDeliveryException, version+1, "Traffic"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.co.PreviewCancellation(h.ctx, order, "", d); conflict(err) != "cancellation_not_eligible" {
		t.Fatalf("preview after an exception: %v", err)
	}
	if _, err := h.co.ChefCancel(h.ctx, chef, "chef-cancel", order, checkout.ChefCancelInput{ExpectedVersion: version + 2,
		Reason: "Delivery could not be completed"}); err != nil {
		t.Fatalf("chef cancellation: %v", err)
	}
}
