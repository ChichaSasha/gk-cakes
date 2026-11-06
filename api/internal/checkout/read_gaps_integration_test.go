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
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
)

// Read gaps of the customer and chef order pages (migration 0039; contracts
// K00 role redaction, K09 additive API v1, K11 nothing shown before it is
// secured). Every instant here comes from the test clock or the fake
// Stripe; updated_at (the database's commit clock) is only checked for
// presence.

var lifecycleTimeKeys = []string{"confirmed_at", "production_started_at", "ready_at", "dispatched_at", "handed_over_at",
	"delivery_paid_at"}

// timesOf reads the lifecycle times of a representation (nil: absent or
// null).
func timesOf(t *testing.T, raw []byte) map[string]*time.Time {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	out := map[string]*time.Time{}
	for _, k := range lifecycleTimeKeys {
		v, ok := m[k]
		if !ok || string(v) == "null" {
			out[k] = nil
			continue
		}
		var at time.Time
		if err := json.Unmarshal(v, &at); err != nil {
			t.Fatalf("%s: %s", k, v)
		}
		out[k] = &at
	}
	return out
}

func (h *payHarness) customerTimes(t *testing.T, order string, d []byte) (map[string]*time.Time, string) {
	t.Helper()
	raw, err := h.co.GetOrder(h.ctx, order, "", d)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range lifecycleTimeKeys {
		if !strings.Contains(string(raw), `"`+k+`"`) {
			t.Fatalf("the order view lacks %s: %s", k, raw)
		}
	}
	return timesOf(t, raw), string(raw)
}

// wantTimes compares the set times (want: key -> instant; every other key
// must be null).
func wantTimes(t *testing.T, what string, got map[string]*time.Time, want map[string]time.Time) {
	t.Helper()
	for _, k := range lifecycleTimeKeys {
		w, set := want[k]
		switch g := got[k]; {
		case set && (g == nil || !g.Equal(w)):
			t.Errorf("%s: %s = %v, want %s", what, k, g, w)
		case !set && g != nil:
			t.Errorf("%s: %s = %s, want null", what, k, g)
		}
	}
}

func (h *payHarness) accountOf(t *testing.T, subject string) string {
	t.Helper()
	var id string
	if err := h.admin.QueryRow(h.ctx, "SELECT id::text FROM business.account WHERE provider_subject = $1::uuid", subject).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func failOutcomes(op, name string) error {
	if op == "put" && strings.HasSuffix(name, ".outcome") {
		return errors.New("b2 unavailable")
	}
	return nil
}

// Each time appears only once its outcome is visible: a paid confirmation
// and a handover only once secured (K11), the production start, ready and
// dispatch steps once committed (operational steps). The chef's detail
// holds the customer's values; technical support the same without money;
// fulfilment the first five without the payment time. Staff accounts of
// production and fulfilment steps for the chef and technical support only.
func TestLifecycleTimesFollowVisibleOutcomes(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d := h.tenThousandOrder(t, f, pol, "lt")
	coll, session := h.started(t, order, d)
	got, raw := h.customerTimes(t, order, d)
	wantTimes(t, "awaiting payment", got, nil)

	// Paid, but the outcome envelope cannot be written: confirmation_pending
	// and no confirmed_at.
	paidAt := time.Date(2026, 11, 5, 10, 9, 0, 0, time.UTC)
	h.stripe.pay(session, paidAt)
	h.journal.Fail = failOutcomes
	h.deliver(t, "evt_paid_lt", "checkout.session.completed", "checkout.session", session, coll, order)
	_ = h.drain(t)
	if v, raw := h.fulfilView(t, order, d); v.Lifecycle != "confirmation_pending" {
		t.Fatalf("not pending: %s", raw)
	}
	got, raw = h.customerTimes(t, order, d)
	wantTimes(t, "confirmation not secured", got, nil)
	h.journal.Fail = nil
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("secure:\n%s", reasons(errs))
	}
	got, raw = h.customerTimes(t, order, d)
	wantTimes(t, "confirmed", got, map[string]time.Time{"confirmed_at": paidAt})
	if !strings.Contains(raw, `"paid_at" : "2026-11-05T10:09:00+00:00"`) {
		t.Fatalf("confirmed_at is not the paid confirmation: %s", raw)
	}

	// Production (chef), ready (fulfilment): visible once committed.
	h.now = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	started := h.now
	v, _ := h.fulfilView(t, order, d)
	if _, err := h.co.StartProduction(h.ctx, chef, "p-lt", order, checkout.ProductionInput{ExpectedVersion: v.Version}); err != nil {
		t.Fatal(err)
	}
	h.now = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	ready := h.now
	if _, err := h.fulfil(fulfilment, "r-lt", order, checkout.ActionReady, v.Version+1); err != nil {
		t.Fatalf("ready: %v", err)
	}
	got, _ = h.customerTimes(t, order, d)
	wantTimes(t, "ready", got, map[string]time.Time{"confirmed_at": paidAt, "production_started_at": started, "ready_at": ready})

	// Handover (K11): absent while its outcome is not secured.
	h.now = time.Date(2026, 11, 14, 12, 40, 0, 0, time.UTC)
	handed := h.now
	v, _ = h.fulfilView(t, order, d)
	hand, err := h.fulfil(fulfilment, "h-lt", order, checkout.ActionHandover, v.Version)
	if err != nil {
		t.Fatalf("handover: %v", err)
	}
	h.journal.Fail = failOutcomes
	if err := h.moveJob(t, hand.ID); err == nil {
		t.Fatal("secured without the outcome envelope")
	}
	if h.recoveryState(t, hand.ID) != "committed" {
		t.Fatalf("handover %s", h.recoveryState(t, hand.ID))
	}
	got, raw = h.customerTimes(t, order, d)
	wantTimes(t, "handover committed", got, map[string]time.Time{"confirmed_at": paidAt, "production_started_at": started,
		"ready_at": ready})
	for _, who := range []string{chef, support, fulfilment} {
		sraw, err := h.co.StaffOrder(h.ctx, who, order)
		if err != nil {
			t.Fatal(err)
		}
		if g := timesOf(t, sraw); g["handed_over_at"] != nil {
			t.Fatalf("%s sees an unsecured handover: %s", who, sraw)
		}
	}
	h.journal.Fail = nil
	h.due(t)
	if err := h.moveJob(t, hand.ID); err != nil {
		t.Fatalf("secure: %v", err)
	}
	all := map[string]time.Time{"confirmed_at": paidAt, "production_started_at": started, "ready_at": ready, "handed_over_at": handed}
	got, raw = h.customerTimes(t, order, d)
	wantTimes(t, "handed over", got, all)

	// Staff views.
	chefAccount, fulAccount := h.accountOf(t, chef), h.accountOf(t, fulfilment)
	cv := h.staffJSON(t, chef, order)
	var customer map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &customer); err != nil {
		t.Fatal(err)
	}
	for _, k := range lifecycleTimeKeys {
		if compactJSON(t, cv[k]) != compactJSON(t, customer[k]) {
			t.Errorf("chef %s %s, customer %s", k, cv[k], customer[k])
		}
	}
	steps := func(raw json.RawMessage) []map[string]json.RawMessage {
		var out []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	for who, view := range map[string]map[string]json.RawMessage{chef: cv, support: h.staffJSON(t, support, order)} {
		wantTimes(t, who, timesOf(t, mustJSON(t, view)), all)
		if compactJSON(t, view["original_handover_at"]) != `"2026-11-14T12:30:00+00:00"` || compactJSON(t, view["free_delivery_code"]) != "false" ||
			len(view["updated_at"]) < 10 {
			t.Errorf("%s anchor, code, updated_at: %s %s %s", who, view["original_handover_at"], view["free_delivery_code"], view["updated_at"])
		}
		var prod struct {
			State string
			Actor *string `json:"actor_account_id"`
		}
		_ = json.Unmarshal(view["production"], &prod)
		if prod.State != "started" || prod.Actor == nil || *prod.Actor != chefAccount {
			t.Errorf("%s production %s, want the chef's account", who, view["production"])
		}
		hist := steps(view["fulfillment_history"])
		if len(hist) != 2 || compactJSON(t, hist[0]["actor_account_id"]) != `"`+fulAccount+`"` ||
			compactJSON(t, hist[1]["actor_account_id"]) != `"`+fulAccount+`"` {
			t.Errorf("%s fulfilment history %s", who, view["fulfillment_history"])
		}
	}
	sv := h.staffJSON(t, support, order)
	var supportAny map[string]any
	_ = json.Unmarshal(mustJSON(t, sv), &supportAny)
	noneOf(t, "support detail", supportAny, true, notForSupport...)
	fv := h.staffJSON(t, fulfilment, order)
	five := map[string]time.Time{}
	for k, v := range all {
		five[k] = v
	}
	wantTimes(t, "fulfilment", timesOf(t, mustJSON(t, fv)), five)
	for _, k := range []string{"delivery_paid_at", "original_handover_at", "free_delivery_code"} {
		if _, ok := fv[k]; ok {
			t.Errorf("fulfilment sees %s", k)
		}
	}
	if len(fv["updated_at"]) < 10 {
		t.Errorf("fulfilment updated_at %s", fv["updated_at"])
	}
	var fulAny map[string]any
	_ = json.Unmarshal(mustJSON(t, fv), &fulAny)
	noneOf(t, "fulfilment detail", fulAny, true, append(notForSupport, "actor_account_id")...)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A late payment is confirmed by the chef's honour, not by the payment:
// confirmed_at is the honour's receipt once secured, while paid_at keeps the
// payment.
func TestConfirmedAtOfAnHonouredLatePayment(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d := h.lateOrder(t, f, pol, "lh")
	got, raw := h.customerTimes(t, order, d)
	wantTimes(t, "paid late", got, nil)
	if !strings.Contains(raw, `"paid_at" : "2026-11-06T12:30:01+00:00"`) {
		t.Fatalf("paid late %s", raw)
	}
	h.now = time.Date(2026, 11, 6, 13, 0, 0, 0, time.UTC)
	v, _ := h.cancelView(t, order, d)
	honour := checkout.LatePaymentInput{ExpectedVersion: v.Version, Decision: "honour",
		CustomerAgreement: &checkout.Agreement{Channel: "phone", AgreedAt: "2026-11-06T12:45:00Z", EvidenceRef: "Call note"}}
	if _, err := h.co.LatePayment(h.ctx, chef, "lp", order, honour); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.customerTimes(t, order, d); got["confirmed_at"] != nil {
		t.Fatal("confirmed before the honour is secured")
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("decision:\n%s", reasons(errs))
	}
	got, raw = h.customerTimes(t, order, d)
	wantTimes(t, "honoured", got, map[string]time.Time{"confirmed_at": h.now})
	if !strings.Contains(raw, `"lifecycle" : "confirmed"`) {
		t.Fatalf("honoured %s", raw)
	}
}

// GK delivery: delivery_paid_at once the current quote is paid (the
// collection's provider success instant), dispatched_at at the latest
// dispatch; free_delivery_code says whether the order used a free-delivery
// code (never the code). Fulfilment learns whether the redelivery time is
// set, so it is not offered a dispatch the API refuses.
func TestDeliveryTimesCodeAndRedeliveryTime(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	h.withRefunds()
	h.code(t, "fd", checkout.CodeInput{Code: "FREEDEL", CodeTerms: checkout.CodeTerms{Kind: checkout.KindFreeDelivery}})
	coded, _, _ := h.deliveryOrder(t, f, pol, "rc", "FREEDEL")
	for _, who := range []string{chef, support} {
		if v := h.staffJSON(t, who, coded); compactJSON(t, v["free_delivery_code"]) != "true" || strings.Contains(string(mustJSON(t, v)), "FREEDEL") {
			t.Errorf("%s free_delivery_code %s", who, v["free_delivery_code"])
		}
	}

	order, d, _, delivery, version := h.dispatchedOrder(t, f, pol, "rt")
	deliveryPaid := time.Date(2026, 11, 5, 11, 0, 0, 0, time.UTC) // deliveryPaid's charge
	got, _ := h.customerTimes(t, order, d)
	if got["delivery_paid_at"] == nil || !got["delivery_paid_at"].Equal(deliveryPaid) || got["dispatched_at"] == nil ||
		!got["dispatched_at"].Equal(failDispatchAt) || got["ready_at"] == nil || got["handed_over_at"] != nil {
		t.Fatalf("dispatched: %v", got)
	}
	var succeeded time.Time
	if err := h.admin.QueryRow(h.ctx, "SELECT succeeded_at FROM business.payment_collection WHERE id = $1::uuid", delivery).Scan(&succeeded); err != nil ||
		!succeeded.Equal(deliveryPaid) {
		t.Fatalf("delivery collection succeeded at %s %v", succeeded, err)
	}
	if v := h.staffJSON(t, support, order); compactJSON(t, v["delivery_paid_at"]) != `"2026-11-05T11:00:00+00:00"` ||
		compactJSON(t, v["free_delivery_code"]) != "false" {
		t.Fatalf("support %s %s", v["delivery_paid_at"], v["free_delivery_code"])
	}

	// Our-side failure, redelivery chosen: fulfilment sees the time is not
	// set (the dispatch is refused), then set (the dispatch is accepted).
	h.now = failAt
	if _, err := h.deliveryFailed(chef, "f1", order, version, checkout.CauseOurSide, ""); err != nil {
		t.Fatal(err)
	}
	fulFailure := func() map[string]json.RawMessage {
		var b map[string]json.RawMessage
		if err := json.Unmarshal(h.staffJSON(t, fulfilment, order)["delivery_failure"], &b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	if _, ok := fulFailure()["redelivery_time_set"]; ok {
		t.Fatal("redelivery_time_set before a redelivery is chosen")
	}
	v, _ := h.failureView(t, order, d)
	if _, err := h.chooseAfterFailure(order, d, "c1", v.Version, checkout.FailureRedelivery, ""); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	b := fulFailure()
	if compactJSON(t, b["redelivery_time_set"]) != "false" {
		t.Fatalf("fulfilment failure block %v", b)
	}
	v2, _ := h.failureView(t, order, d)
	if _, err := h.fulfil(fulfilment, "d-early", order, checkout.ActionDispatch, v2.Version); conflict(err) != "redelivery_time_not_set" {
		t.Fatalf("dispatch before the new time: %v", err)
	}
	h.chefAgreesTime(t, order, "a1", v2.Version, "2026-11-21", h.slotsOn(t, "2026-11-21")[1])
	b = fulFailure()
	var fulAny map[string]any
	_ = json.Unmarshal(mustJSON(t, b), &fulAny)
	if compactJSON(t, b["redelivery_time_set"]) != "true" {
		t.Fatalf("fulfilment failure block after the new time %v", b)
	}
	noneOf(t, "fulfilment failure block", fulAny, true)
	h.now = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	v3, _ := h.failureView(t, order, d)
	if _, err := h.fulfil(fulfilment, "d2", order, checkout.ActionDispatch, v3.Version); err != nil {
		t.Fatalf("re-dispatch: %v", err)
	}
	if got, _ := h.customerTimes(t, order, d); got["dispatched_at"] == nil || !got["dispatched_at"].Equal(h.now) {
		t.Fatalf("dispatched_at after the re-dispatch %v", got["dispatched_at"])
	}
}

// ----------------------------------------------------- staff schedule choices

func (h *payHarness) staffChoices(t *testing.T, order, date string) schedule.StaffChoices {
	t.Helper()
	c, err := h.sch.StaffChoices(h.ctx, chef, order, day(date))
	if err != nil {
		t.Fatalf("choices of %s on %s: %v", order, date, err)
	}
	return c
}

// choiceSummary is "HH:MM availability[/reason][/allocation]" per start.
func choiceSummary(c schedule.StaffChoices) string {
	var out []string
	for _, s := range c.Slots {
		x := s.StartsAt.UTC().Format("15:04") + " " + s.Availability
		if s.ReasonCode != nil {
			x += "/" + *s.ReasonCode
		}
		if s.Allocation != nil {
			x += "/" + *s.Allocation
		}
		out = append(out, x)
	}
	return strings.Join(out, ", ")
}

func (h *payHarness) changeAppointment(order, key string, version int, date, slot string) (catalog.Result, error) {
	in := checkout.AppointmentInput{ExpectedVersion: version, TargetDate: date, TargetSlotID: slot,
		CustomerAgreement: &checkout.Agreement{Channel: "phone", AgreedAt: h.now.Add(-time.Minute).Format(time.RFC3339),
			EvidenceRef: "call"},
		FeasibilityNote: "Agreed"}
	res, err := h.co.ChangeAppointment(h.ctx, chef, key, order, in)
	return res.Result, err
}

// The chef's choices agree with the appointment change: the current slot,
// past starts and the ready cakes' shelf life are unavailable; another day
// is standard within its limit and chef_extra beyond it (the change still
// commits there); a closed day has none; a pending command or an ineligible
// order makes every start unavailable. Chef only; reads create no day row.
func TestStaffChoicesForAnAppointmentChange(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, _, _ := h.paidCollection(t, f, pol, "sc")
	filler, _ := h.tenThousandOrder(t, f, pol, "fill") // placed within the lead time
	open, _ := h.tenThousandOrder(t, f, pol, "open")
	h.now = time.Date(2026, 11, 14, 12, 50, 0, 0, time.UTC)
	c := h.staffChoices(t, order, "2026-11-14")
	if c.Purpose != schedule.PurposeAppointmentChange || c.DayStatus != "open" || c.FulfillmentMode != schedule.Collection ||
		c.CurrentSlot.ID != f.slot || c.CurrentSlot.Date != "2026-11-14" || c.Capacity != nil || !c.AsOf.Equal(h.now) ||
		c.ScheduleRevision == nil || *c.ScheduleRevision != 1 || c.Timezone != schedule.Timezone {
		t.Fatalf("same day %+v", c)
	}
	if got := choiceSummary(c); got != "12:30 unavailable/current_slot, 13:00 available, 13:30 available, 14:00 available" {
		t.Fatalf("same day: %s", got)
	}
	h.now = time.Date(2026, 11, 14, 13, 0, 0, 0, time.UTC) // a start equal to the receipt has passed
	c = h.staffChoices(t, order, "2026-11-14")
	if got := choiceSummary(c); got != "12:30 unavailable/current_slot, 13:00 unavailable/past, 13:30 available, 14:00 available" {
		t.Fatalf("at 13:00: %s", got)
	}
	if _, err := h.changeAppointment(order, "past", h.orderSlot(t, order).Version, "2026-11-14", c.Slots[1].ID); conflict(err) != "slot_unavailable" {
		t.Fatalf("a past start: %v", err)
	}

	// Another day: standard within its limit (7), chef_extra beyond it.
	h.now = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	c = h.staffChoices(t, order, "2026-11-21")
	if got := choiceSummary(c); got != "12:30 available/standard, 13:00 available/standard, 13:30 available/standard, 14:00 available/standard" ||
		c.Capacity == nil || *c.Capacity != (schedule.ChoiceCapacity{StandardLimit: 7, CommittedStandard: 0, OrderCakes: 1}) {
		t.Fatalf("next Saturday: %s %+v", got, c.Capacity)
	}
	if n := h.count(t, "SELECT count(*) FROM business.handover_day WHERE day = '2026-11-21'"); n != 0 {
		t.Fatalf("the read created a day row (%d)", n)
	}
	h.otherOrdersOn(t, filler, "2026-11-21", 7)
	c = h.staffChoices(t, order, "2026-11-21")
	if got := choiceSummary(c); !strings.HasPrefix(got, "12:30 available/chef_extra, 13:00 available/chef_extra") ||
		*c.Capacity != (schedule.ChoiceCapacity{StandardLimit: 7, CommittedStandard: 7, OrderCakes: 1}) {
		t.Fatalf("a full day: %s %+v", got, c.Capacity)
	}
	if c := h.staffChoices(t, order, "2026-11-20"); c.DayStatus != "closed" || len(c.Slots) != 0 || c.Capacity != nil {
		t.Fatalf("a Friday %+v", c)
	}
	for who, want := range map[string]error{support: catalog.ErrForbidden, fulfilment: catalog.ErrForbidden, customerA: catalog.ErrForbidden} {
		if _, err := h.sch.StaffChoices(h.ctx, who, order, day("2026-11-21")); !errors.Is(err, want) {
			t.Errorf("choices by %s: %v", who, err)
		}
	}
	for _, missing := range []string{"00000000-0000-4000-8000-000000000404", "not-a-uuid"} {
		if _, err := h.sch.StaffChoices(h.ctx, chef, missing, day("2026-11-21")); !errors.Is(err, catalog.ErrNotFound) {
			t.Errorf("unknown order %s: %v", missing, err)
		}
	}

	// The change to an offered chef_extra start commits as chef_extra; while
	// it is unresolved every start reads pending.
	slot := c.Slots[1].ID
	res, err := h.changeAppointment(order, "ch1", h.orderSlot(t, order).Version, "2026-11-21", slot)
	if err != nil {
		t.Fatal(err)
	}
	p := h.staffChoices(t, order, "2026-11-21")
	if p.DayStatus != "pending" || p.Capacity != nil || p.CurrentSlot.Date != "2026-11-14" ||
		choiceSummary(p) != "12:30 unavailable/pending, 13:00 unavailable/pending, 13:30 unavailable/pending, 14:00 unavailable/pending" {
		t.Fatalf("pending: %s %+v", choiceSummary(p), p)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	if h.recoveryState(t, res.ID) != "secured" || h.allocationOn(t, order, "2026-11-21").Class != "chef_extra" {
		t.Fatalf("change %s %+v", h.recoveryState(t, res.ID), h.allocationOn(t, order, "2026-11-21"))
	}
	if got := choiceSummary(h.staffChoices(t, order, "2026-11-21")); got != "12:30 available, 13:00 unavailable/current_slot, 13:30 available, 14:00 available" {
		t.Fatalf("after the change: %s", got)
	}

	// Not eligible: an order awaiting payment.
	if c := h.staffChoices(t, open, "2026-11-21"); c.DayStatus != "not_eligible" || c.Capacity != nil ||
		choiceSummary(c) != "12:30 unavailable/not_eligible, 13:00 unavailable/not_eligible, 13:30 unavailable/not_eligible, 14:00 unavailable/not_eligible" {
		t.Fatalf("awaiting payment: %s", choiceSummary(c))
	}
}

// Ready cakes' shelf life: a start whose appointment would end after a
// ready cake expires is unavailable (shelf_life), as the change refuses it.
func TestStaffChoicesShelfLife(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	h.stripe.pay(session, time.Date(2026, 11, 5, 10, 9, 0, 0, time.UTC))
	h.deliver(t, "evt_paid_shelf", "checkout.session.completed", "checkout.session", session, coll, order)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("paid: %v", errs)
	}
	h.owner(t, "UPDATE business.finished_cake_lot SET expires_at = '2026-11-14T14:00:00Z' WHERE id = $1::uuid", f.readyLot)
	h.now = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	c := h.staffChoices(t, order, "2026-11-14")
	if got := choiceSummary(c); got != "12:30 unavailable/current_slot, 13:00 available, 13:30 unavailable/shelf_life, 14:00 unavailable/shelf_life" {
		t.Fatalf("shelf life: %s", got)
	}
	if _, err := h.changeAppointment(order, "late", h.orderSlot(t, order).Version, "2026-11-14", c.Slots[2].ID); conflict(err) != "slot_unavailable" {
		t.Fatalf("a start past the shelf life: %v", err)
	}
}

// An order paid late: the choices are those of an offer of another date
// (0034): within the day's standard places only (day_full otherwise), the
// requested appointment itself is current_slot (kind requested), and the
// offer route agrees.
func TestStaffChoicesForALatePaymentOffer(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	filler, _ := h.tenThousandOrder(t, f, pol, "fill") // placed within the lead time
	order, _, v := h.lateAgreementOrder(t, f, pol, "so")
	c := h.staffChoices(t, order, "2026-11-14")
	if c.Purpose != schedule.PurposeLatePaymentOffer || c.DayStatus != "open" ||
		choiceSummary(c) != "12:30 unavailable/current_slot, 13:00 available, 13:30 available, 14:00 available" {
		t.Fatalf("same day: %s %+v", choiceSummary(c), c)
	}
	c = h.staffChoices(t, order, "2026-11-21")
	if got := choiceSummary(c); got != "12:30 available/standard, 13:00 available/standard, 13:30 available/standard, 14:00 available/standard" {
		t.Fatalf("next Saturday: %s", got)
	}
	h.otherOrdersOn(t, filler, "2026-11-21", 7)
	c = h.staffChoices(t, order, "2026-11-21")
	if got := choiceSummary(c); got != "12:30 unavailable/day_full, 13:00 unavailable/day_full, 13:30 unavailable/day_full, 14:00 unavailable/day_full" ||
		c.Capacity == nil || c.Capacity.CommittedStandard != 7 {
		t.Fatalf("a full day: %s %+v", got, c.Capacity)
	}
	if _, err := h.offer(order, "o1", v.Version, "other", "2026-11-21", c.Slots[0].ID); conflict(err) != "slot_unavailable" {
		t.Fatalf("offer on a full day: %v", err)
	}
	h.otherOrdersOn(t, filler, "2026-11-21", 6)
	c = h.staffChoices(t, order, "2026-11-21")
	if c.Slots[0].Availability != "available" {
		t.Fatalf("a day with one place: %s", choiceSummary(c))
	}
	if _, err := h.offer(order, "o2", v.Version, "other", "2026-11-21", c.Slots[0].ID); err != nil {
		t.Fatalf("offer: %v", err)
	}
}
