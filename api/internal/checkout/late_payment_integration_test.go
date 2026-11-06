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
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
)

// GK-017 r02b Q6: the late-payment agreement on the order page (migration
// 0034; contracts r6.5 K04 Q2, K05, K08, K09, K11). The late order is paid
// at 2026-11-06 12:30:01Z for Saturday 2026-11-14 12:30 London (GMT), so it
// awaits agreement; its production anchor is 2026-11-08 12:30:01Z.

var (
	latePaidAnchor = time.Date(2026, 11, 8, 12, 30, 1, 0, time.UTC)
	lateHandover   = time.Date(2026, 11, 14, 12, 30, 0, 0, time.UTC)
)

type offerJSON struct {
	ID        string
	Kind      string
	Date      string
	SlotID    string `json:"slot_id"`
	Start     time.Time
	End       time.Time
	OfferedAt time.Time `json:"offered_at"`
}

type lateView struct {
	Version      int
	Lifecycle    string
	Date         string
	Slot         appointmentJSON
	CancelReason *string `json:"cancel_reason"`
	Cancellation *struct {
		State       string
		Reason      *string
		Rate        *int
		RequestedBy string `json:"requested_by"`
	}
	Refund      *refundView
	LatePayment *struct {
		State  string
		Offer  *offerJSON
		Answer *struct {
			OfferID    string `json:"offer_id"`
			Answer     string
			State      string
			Reason     *string
			ReceivedAt time.Time `json:"received_at"`
		}
	} `json:"late_payment"`
	Agreed *struct {
		At      time.Time
		Kind    string
		Date    string
		Start   time.Time
		End     time.Time
		OfferID string `json:"offer_id"`
	}
}

func (h *payHarness) lateView(t *testing.T, order string, d []byte) (lateView, string) {
	t.Helper()
	raw, err := h.co.GetOrder(h.ctx, order, "", d)
	if err != nil {
		t.Fatal(err)
	}
	var v lateView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v, string(raw)
}

func (h *payHarness) offer(order, key string, version int, kind, date, slot string) (catalog.Result, error) {
	return h.co.OfferLatePayment(h.ctx, chef, key, order, checkout.LatePaymentOfferInput{ExpectedVersion: version, Kind: kind,
		TargetDate: date, TargetSlotID: slot})
}

func (h *payHarness) answer(order string, d []byte, key string, version int, offer, answer string) (catalog.Result, error) {
	return h.co.AnswerLatePayment(h.ctx, order, "", d, key, checkout.LatePaymentAnswerInput{ExpectedVersion: version,
		OfferID: offer, Answer: answer})
}

// lateAgreementOrder is a late-paid 10000p order (paid_awaiting_agreement)
// with a fresh guest capability, the clock half an hour after the payment.
func (h *payHarness) lateAgreementOrder(t *testing.T, f fixture, pol, key string) (string, []byte, lateView) {
	t.Helper()
	order, _ := h.lateOrder(t, f, pol, key)
	d := h.digestOf(t, order)
	h.now = time.Date(2026, 11, 6, 13, 0, 0, 0, time.UTC)
	v, raw := h.lateView(t, order, d)
	if v.Lifecycle != "paid_awaiting_agreement" || v.LatePayment == nil || v.LatePayment.State != "waiting" ||
		v.LatePayment.Offer != nil || v.LatePayment.Answer != nil || v.Agreed != nil {
		t.Fatalf("late order %s", raw)
	}
	return order, d, v
}

func (h *payHarness) staffJSON(t *testing.T, subject, order string) map[string]json.RawMessage {
	t.Helper()
	raw, err := h.co.StaffOrder(h.ctx, subject, order)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func noticeCount(ns []noticeRow, kind string) int {
	n := 0
	for _, r := range ns {
		if r.Kind == kind {
			n++
		}
	}
	return n
}

// Waiting has no answer; the chef offers the requested time (chef only,
// versioned, idempotent; a new offer withdraws the open one); only the
// current offer can be answered, by the order's own access at its version;
// accepting it is the honour path with the button press as agreement
// evidence: confirmed once secured, production from paid_at + 48 h, the
// agreed block, one confirmation notice, no personal data in the audit.
func TestLatePaymentRequestedOfferAccepted(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	h.withNotices(t)
	order, d, v := h.lateAgreementOrder(t, f, pol, "agr")

	// Waiting: nothing to answer.
	if _, err := h.answer(order, d, "w1", v.Version, "00000000-0000-4000-8000-000000000001", "accept"); conflict(err) != "offer_not_current" {
		t.Fatalf("answer while waiting: %v", err)
	}
	req := checkout.LatePaymentOfferInput{ExpectedVersion: v.Version, Kind: checkout.OfferRequested}
	for _, who := range []string{fulfilment, support, "00000000-0000-4000-8000-000000000502"} {
		if _, err := h.co.OfferLatePayment(h.ctx, who, "o0", order, req); !errors.Is(err, catalog.ErrForbidden) {
			t.Fatalf("offer by %s: %v", who, err)
		}
	}
	if _, err := h.offer(order, "o-stale", v.Version+1, checkout.OfferRequested, "", ""); conflict(err) != "stale_version" {
		t.Fatalf("stale offer: %v", err)
	}
	first, err := h.offer(order, "o1", v.Version, checkout.OfferRequested, "", "")
	if err != nil || first.Replayed {
		t.Fatalf("offer: %+v %v", first, err)
	}
	if again, err := h.offer(order, "o1", v.Version, checkout.OfferRequested, "", ""); err != nil || !again.Replayed || again.ID != first.ID {
		t.Fatalf("offer replay: %+v %v", again, err)
	}
	if _, err := h.offer(order, "o1", v.Version+1, checkout.OfferRequested, "", ""); conflict(err) != "key_reused" {
		t.Fatalf("offer key reused: %v", err)
	}
	v1, raw := h.lateView(t, order, d)
	if v1.Version != v.Version+1 || v1.Lifecycle != "paid_awaiting_agreement" || v1.LatePayment.State != "offered" ||
		v1.LatePayment.Offer == nil || v1.LatePayment.Offer.ID != first.ID || v1.LatePayment.Offer.Kind != "requested" ||
		v1.LatePayment.Offer.Date != "2026-11-14" || !v1.LatePayment.Offer.Start.Equal(lateHandover) ||
		!v1.LatePayment.Offer.End.Equal(lateHandover.Add(time.Hour)) || !v1.LatePayment.Offer.OfferedAt.Equal(h.now) {
		t.Fatalf("offered %s", raw)
	}
	// A new offer withdraws the open one (rows are kept).
	second, err := h.offer(order, "o2", v1.Version, checkout.OfferRequested, "", "")
	if err != nil {
		t.Fatal(err)
	}
	v2, _ := h.lateView(t, order, d)
	if v2.LatePayment.Offer == nil || v2.LatePayment.Offer.ID != second.ID || v2.Version != v1.Version+1 {
		t.Fatalf("second offer %+v", v2.LatePayment)
	}
	if _, err := h.answer(order, d, "a-old", v2.Version, first.ID, "accept"); conflict(err) != "offer_not_current" {
		t.Fatalf("superseded offer: %v", err)
	}
	// Access: a capability without "cancel", another customer, the wrong
	// version.
	_, readOnly := proof(t)
	h.owner(t, `INSERT INTO business.order_guest_capability (id, order_id, capability_digest, allowed_commands)
		VALUES (gen_random_uuid(), $1::uuid, $2, ARRAY['read', 'pay'])`, order, readOnly)
	if _, err := h.answer(order, readOnly, "a-ro", v2.Version, second.ID, "accept"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("read-only capability: %v", err)
	}
	h.resolve(t, customerB)
	if _, err := h.co.AnswerLatePayment(h.ctx, order, customerB, nil, "a-b", checkout.LatePaymentAnswerInput{
		ExpectedVersion: v2.Version, OfferID: second.ID, Answer: "accept"}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("another customer: %v", err)
	}
	if _, err := h.answer(order, d, "a-stale", v2.Version-1, second.ID, "accept"); conflict(err) != "stale_version" {
		t.Fatalf("stale answer: %v", err)
	}
	if n := h.count(t, "SELECT count(*) FROM business.late_payment_answer WHERE order_id = $1::uuid", order); n != 0 {
		t.Fatalf("%d answers recorded by refusals", n)
	}

	h.now = time.Date(2026, 11, 6, 14, 0, 0, 0, time.UTC)
	receipt := h.now
	res, err := h.answer(order, d, "a1", v2.Version, second.ID, "accept")
	if err != nil || res.Replayed {
		t.Fatalf("accept: %+v %v", res, err)
	}
	if again, err := h.answer(order, d, "a1", v2.Version, second.ID, "accept"); err != nil || !again.Replayed || again.ID != res.ID {
		t.Fatalf("answer replay: %+v %v", again, err)
	}
	if _, err := h.answer(order, d, "a1", v2.Version, second.ID, "decline"); conflict(err) != "key_reused" {
		t.Fatalf("answer key reused: %v", err)
	}
	if _, err := h.answer(order, d, "a2", v2.Version, second.ID, "decline"); conflict(err) != "state_conflict" {
		t.Fatalf("second answer while pending: %v", err)
	}
	if _, err := h.offer(order, "o3", v2.Version, checkout.OfferRequested, "", ""); conflict(err) != "state_conflict" {
		t.Fatalf("offer while an answer is pending: %v", err)
	}
	if _, err := h.co.LatePayment(h.ctx, chef, "lp", order, checkout.LatePaymentInput{ExpectedVersion: v2.Version,
		Decision: "full_return"}); conflict(err) != "state_conflict" {
		t.Fatalf("chef decision while an answer is pending: %v", err)
	}
	if p, raw := h.lateView(t, order, d); p.LatePayment == nil || p.LatePayment.State != "processing" ||
		p.LatePayment.Offer == nil || p.LatePayment.Offer.ID != second.ID || p.LatePayment.Answer == nil ||
		p.LatePayment.Answer.State != "processing" || p.LatePayment.Answer.Answer != "accept" {
		t.Fatalf("processing %s", raw)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("answer:\n%s", reasons(errs))
	}
	v3, raw := h.lateView(t, order, d)
	if v3.Lifecycle != "confirmed" || v3.LatePayment != nil || v3.Agreed == nil || !v3.Agreed.At.Equal(receipt) ||
		v3.Agreed.Kind != "requested" || !v3.Agreed.Start.Equal(lateHandover) || v3.Agreed.Date != "2026-11-14" ||
		v3.Agreed.OfferID != second.ID || v3.Date != "2026-11-14" || v3.Refund != nil || v3.Version != v2.Version+1 {
		t.Fatalf("agreed %s", raw)
	}
	if _, at := h.productionJob(t, order); !at.Equal(latePaidAnchor) {
		t.Fatalf("production scheduled at %s, want paid_at + 48 h", at)
	}
	if got := h.allocation(t, order); got != "committed" {
		t.Fatalf("day %s", got)
	}
	// The agreement evidence: the button press, when, through which access.
	var answerID, channel, evidence, actorKind string
	var agreedAt time.Time
	var recordedByChef, guestAccess bool
	if err := h.admin.QueryRow(h.ctx, `SELECT a.id::text, d.channel, d.agreed_at, d.evidence_ref,
			d.recorded_by = (SELECT id FROM business.account WHERE provider_subject = $2::uuid),
			a.actor_kind, a.guest_capability_id IS NOT NULL AND a.actor_account_id IS NULL
		FROM business.late_payment_answer a JOIN business.late_payment_decision d ON d.command_id = a.command_id
		WHERE a.command_id = $1::uuid`, res.ID, chef).Scan(&answerID, &channel, &agreedAt, &evidence, &recordedByChef,
		&actorKind, &guestAccess); err != nil {
		t.Fatal(err)
	}
	if channel != "order_page" || !agreedAt.Equal(receipt) || evidence != answerID || !recordedByChef || actorKind != "guest" || !guestAccess {
		t.Fatalf("evidence %s %s %s %s %v %s %v", answerID, channel, agreedAt, evidence, recordedByChef, actorKind, guestAccess)
	}
	if n := noticeCount(h.notices(t, order), "confirmed"); n != 1 {
		t.Fatalf("%d confirmation notices", n)
	}
	// Replays after the commit; nothing more to answer.
	if again, err := h.answer(order, d, "a1", v2.Version, second.ID, "accept"); err != nil || !again.Replayed || again.ID != res.ID {
		t.Fatalf("replay after the commit: %+v %v", again, err)
	}
	if _, err := h.answer(order, d, "a3", v3.Version, second.ID, "decline"); conflict(err) != "offer_not_current" {
		t.Fatalf("answer on a confirmed order: %v", err)
	}
	if _, err := h.offer(order, "o4", v3.Version, checkout.OfferRequested, "", ""); conflict(err) != "state_conflict" {
		t.Fatalf("offer on a confirmed order: %v", err)
	}
	// Staff views: the chef and technical support see the offers, fulfilment
	// none of it.
	chefView := h.staffJSON(t, chef, order)
	has(t, "chef offers", string(chefView["late_payment_offers"]), `"state": "withdrawn"`, `"state": "accepted"`,
		`"access": "guest_link"`, `"answer": "accept"`)
	has(t, "chef agreed", string(chefView["agreed"]), `"kind": "requested"`)
	supportView := h.staffJSON(t, support, order)
	has(t, "support offers", string(supportView["late_payment_offers"]), `"state": "accepted"`)
	if _, ok := supportView["agreed"]; !ok {
		t.Fatal("technical support does not see the agreement")
	}
	if _, ok := supportView["contact_email"]; ok {
		t.Fatal("technical support sees the contact email")
	}
	for k := range h.staffJSON(t, fulfilment, order) {
		if strings.HasPrefix(k, "late_payment") || k == "agreed" {
			t.Fatalf("fulfilment sees %s", k)
		}
	}
	// Audit without personal data.
	var details string
	if err := h.admin.QueryRow(h.ctx, `SELECT string_agg(action || ' ' || coalesce(detail::text, ''), E'\n' ORDER BY occurred_at)
		FROM business.audit_event WHERE subject_id = $1::uuid`, order).Scan(&details); err != nil {
		t.Fatal(err)
	}
	has(t, "audit", details, "order.late_payment_offered", "order.late_payment_offer_withdrawn", "order.late_payment_answered",
		"order.late_payment_honoured")
	hasNot(t, "audit", details, "example.com", "Test Customer")
	h.noPersonalDataInLogs(t)
}

// Another date: the offer is only an offer (the order keeps its date and
// holds); accepting it moves the appointment and day place inside the
// protected command: journal down -> nothing; outcome envelope down -> the
// order reads confirmation_pending on its requested date; secured ->
// confirmed on the offered date, envelopes once, one notice with the new
// time. The full-refund deadline and production anchor stay (K05).
func TestLatePaymentOtherOfferMovesTheOrder(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.withNotices(t)
	next := h.slotsOn(t, "2026-11-21")
	order, d, v := h.lateAgreementOrder(t, f, pol, "oth")

	for name, c := range map[string]struct {
		date, slot, want string
	}{
		"unknown slot":        {"2026-11-21", "8d3f0f8e-5d55-4c3e-9a49-34a0e2a9f001", "slot_unavailable"},
		"slot of another day": {"2026-11-28", next[1], "slot_unavailable"},
		"a closed weekday":    {"2026-11-20", next[1], "slot_unavailable"},
	} {
		if _, err := h.offer(order, "x-"+strings.ReplaceAll(name, " ", "-"), v.Version, checkout.OfferOther, c.date, c.slot); conflict(err) != c.want {
			t.Errorf("%s: %v, want %s", name, err, c.want)
		}
	}
	if _, err := h.offer(order, "x-same", v.Version, checkout.OfferOther, "2026-11-14", f.slot); !strings.HasPrefix(invalidField(err), "target_slot_id:") {
		t.Fatalf("the requested time as another: %v", err)
	}
	res, err := h.offer(order, "o1", v.Version, checkout.OfferOther, "2026-11-21", next[1])
	if err != nil {
		t.Fatal(err)
	}
	v1, raw := h.lateView(t, order, d)
	want := time.Date(2026, 11, 21, 13, 0, 0, 0, time.UTC)
	if v1.LatePayment.State != "offered" || v1.LatePayment.Offer.ID != res.ID || v1.LatePayment.Offer.Kind != "other" ||
		v1.LatePayment.Offer.Date != "2026-11-21" || v1.LatePayment.Offer.SlotID != next[1] || !v1.LatePayment.Offer.Start.Equal(want) ||
		v1.Date != "2026-11-14" || v1.Slot.ID != f.slot {
		t.Fatalf("other offer %s", raw)
	}
	// Only an offer: no hold anywhere new.
	if n := h.count(t, "SELECT count(*) FROM business.capacity_allocation WHERE order_ref = $1::uuid AND day = '2026-11-21'", order); n != 0 {
		t.Fatalf("the offer holds a place (%d)", n)
	}

	h.now = h.now.Add(time.Hour)
	receipt := h.now
	ans, err := h.answer(order, d, "a1", v1.Version, res.ID, "accept")
	if err != nil {
		t.Fatal(err)
	}
	h.journal.Fail = func(string, string) error { return errors.New("b2 unavailable") }
	job := h.claimOnly(t, h.commandJob(t, ans.ID))
	if err := h.cmds.Handle(h.ctx, job); err == nil {
		t.Fatal("Handle succeeded without the journal")
	}
	if got := h.recoveryState(t, ans.ID); got != "reserved" || h.orderSlot(t, order).Day != "2026-11-14" || h.orderRow(t, order).Lifecycle != "paid_awaiting_agreement" {
		t.Fatalf("journal down: %s %+v", got, h.orderSlot(t, order))
	}
	h.journal.Fail = func(op, name string) error {
		if op == "put" && strings.HasSuffix(name, ".outcome") {
			return errors.New("b2 unavailable")
		}
		return nil
	}
	h.expire(t, job)
	job = h.claimOnly(t, h.commandJob(t, ans.ID))
	if err := h.cmds.Handle(h.ctx, job); err == nil {
		t.Fatal("Handle succeeded without the outcome envelope")
	}
	if got := h.recoveryState(t, ans.ID); got != "committed" || h.orderSlot(t, order).Day != "2026-11-21" {
		t.Fatalf("outcome envelope down: %s %+v", got, h.orderSlot(t, order))
	}
	if p, raw := h.lateView(t, order, d); p.Lifecycle != "confirmation_pending" || p.Date != "2026-11-14" || p.Slot.ID != f.slot ||
		p.Agreed != nil {
		t.Fatalf("committed but unsecured shown as %s", raw)
	}
	if n := noticeCount(h.notices(t, order), "confirmed"); n != 0 {
		t.Fatalf("a confirmation notice before the outcome is secured (%d)", n)
	}
	h.journal.Fail = nil
	h.expire(t, job)
	job = h.claimOnly(t, h.commandJob(t, ans.ID))
	if err := h.cmds.Handle(h.ctx, job); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	h.finish(t, job, nil)
	v2, raw := h.lateView(t, order, d)
	if v2.Lifecycle != "confirmed" || v2.Date != "2026-11-21" || v2.Slot.ID != next[1] || !v2.Slot.StartsAt.Equal(want) ||
		v2.Agreed == nil || v2.Agreed.Kind != "other" || !v2.Agreed.At.Equal(receipt) || !v2.Agreed.Start.Equal(want) ||
		v2.Agreed.Date != "2026-11-21" {
		t.Fatalf("moved %s", raw)
	}
	var full time.Time
	if err := h.admin.QueryRow(h.ctx, "SELECT full_refund_deadline_at FROM business.orders WHERE id = $1::uuid", order).Scan(&full); err != nil ||
		!full.Equal(cancelDeadline) {
		t.Fatalf("deadline %s %v", full, err)
	}
	var old, newState, class string
	if err := h.admin.QueryRow(h.ctx, `SELECT
		(SELECT state FROM business.capacity_allocation WHERE order_ref = $1::uuid AND day = '2026-11-14'),
		n.state, n.classification FROM business.capacity_allocation n WHERE n.order_ref = $1::uuid AND n.day = '2026-11-21'`,
		order).Scan(&old, &newState, &class); err != nil || old != "released" || newState != "committed" || class != "standard" {
		t.Fatalf("allocations %s %s %s %v", old, newState, class, err)
	}
	if _, at := h.productionJob(t, order); !at.Equal(latePaidAnchor) {
		t.Fatalf("production anchor %s", at)
	}
	key, _ := h.coord.EnvelopeKey(recovery.Operation{ID: ans.ID, AggregateID: order, PriorVersion: v1.Version})
	if h.journal.Puts(key+".prepared") != 1 || h.journal.Puts(key+".outcome") != 1 {
		t.Fatalf("envelopes %d/%d", h.journal.Puts(key+".prepared"), h.journal.Puts(key+".outcome"))
	}
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	var confirmed []noticeRow
	for _, n := range h.notices(t, order) {
		if n.Kind == "confirmed" {
			confirmed = append(confirmed, n)
		}
	}
	if len(confirmed) != 1 {
		t.Fatalf("notices %s", kinds(h.notices(t, order)))
	}
	has(t, "confirmation", h.request(t, confirmed[0]).Text, "21 November 2026")
	// The staff list shows the order on its new day.
	rows, _ := h.staffList(t, chef, checkout.StaffOrderFilter{From: day("2026-11-21"), To: day("2026-11-21")})
	if len(rows) != 1 || rows[0]["id"] != order {
		t.Fatalf("staff list on the 21st %v", rows)
	}
}

// The offered slot can no longer take the order: at the answer (a full day)
// the offer lapses with 409 slot_unavailable, nothing else changes, the
// replay says the same and the chef must offer again; at the commit (the day
// filled between the answer and the worker) the command is refused
// slot_unavailable and the order keeps its date and place. An offer on a
// full day is refused outright.
func TestLatePaymentOtherOfferLapsesWhenTheSlotIsFull(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	next := h.slotsOn(t, "2026-11-21")
	order, d, v := h.lateAgreementOrder(t, f, pol, "full")

	res, err := h.offer(order, "o1", v.Version, checkout.OfferOther, "2026-11-21", next[0])
	if err != nil {
		t.Fatal(err)
	}
	// The offer read the day under its lock (the day row exists now).
	must[catalog.Result](t)(h.sch.SetDayLimit(h.ctx, chef, "lim", day("2026-11-21"), schedule.DayLimitInput{ExpectedVersion: ptr(1), StandardLimit: ptr(0)}))
	v1, _ := h.lateView(t, order, d)
	if _, err := h.answer(order, d, "a1", v1.Version, res.ID, "accept"); !errors.Is(err, checkout.ErrOfferLapsed) || conflict(err) != "slot_unavailable" {
		t.Fatalf("accept on a full day: %v", err)
	}
	if _, err := h.answer(order, d, "a1", v1.Version, res.ID, "accept"); conflict(err) != "slot_unavailable" {
		t.Fatalf("replay of the lapsed answer: %v", err)
	}
	v2, raw := h.lateView(t, order, d)
	if v2.Lifecycle != "paid_awaiting_agreement" || v2.LatePayment.State != "waiting" || v2.LatePayment.Offer != nil ||
		v2.LatePayment.Answer == nil || v2.LatePayment.Answer.State != "lapsed" || *v2.LatePayment.Answer.Reason != "slot_unavailable" ||
		v2.Date != "2026-11-14" || v2.Version != v1.Version+1 {
		t.Fatalf("lapsed %s", raw)
	}
	if h.count(t, "SELECT count(*) FROM business.order_command WHERE order_id = $1::uuid AND command = 'late_payment'", order) != 0 ||
		h.allocation(t, order) != "committed" {
		t.Fatal("a lapsed offer changed the order")
	}
	if _, err := h.answer(order, d, "a2", v2.Version, res.ID, "decline"); conflict(err) != "offer_not_current" {
		t.Fatalf("answering a lapsed offer: %v", err)
	}
	if _, err := h.offer(order, "o2", v2.Version, checkout.OfferOther, "2026-11-21", next[1]); conflict(err) != "slot_unavailable" {
		t.Fatalf("offer on a full day: %v", err)
	}
	// The same day stays possible (the order's own place is reused).
	same := h.slotsOn(t, "2026-11-14")
	res2, err := h.offer(order, "o3", v2.Version, checkout.OfferOther, "2026-11-14", same[2])
	if err != nil {
		t.Fatalf("another time the same day: %v", err)
	}
	v3, _ := h.lateView(t, order, d)
	// Commit-time lapse: the slot stops being published (a new schedule
	// version without 13:30) after the answer, before the worker runs.
	ans, err := h.answer(order, d, "a3", v3.Version, res2.ID, "accept")
	if err != nil {
		t.Fatal(err)
	}
	must[catalog.Result](t)(h.sch.PublishVersion(h.ctx, chef, "sched2", schedule.VersionInput{ExpectedRevision: ptr(1),
		StandardLimit: ptr(7), BookableUntil: "2027-01-31", Weekdays: []schedule.WeekdayInput{
			{Weekday: "saturday", Opens: "12:30", Closes: "15:00", Starts: []string{"12:30", "13:00", "14:00"}}}}))
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	v4, raw := h.lateView(t, order, d)
	if v4.Lifecycle != "paid_awaiting_agreement" || v4.LatePayment.State != "waiting" || v4.LatePayment.Answer == nil ||
		v4.LatePayment.Answer.State != "lapsed" || v4.Date != "2026-11-14" || v4.Slot.ID != f.slot || v4.Agreed != nil {
		t.Fatalf("refused at the commit %s", raw)
	}
	var outcome string
	if err := h.admin.QueryRow(h.ctx, "SELECT outcome::text FROM business.recovery_operation WHERE id = $1::uuid", ans.ID).Scan(&outcome); err != nil ||
		!strings.Contains(outcome, "slot_unavailable") {
		t.Fatalf("outcome %s %v", outcome, err)
	}
	if h.allocation(t, order) != "committed" || h.orderSlot(t, order).Starts != nil {
		t.Fatal("a refused acceptance moved the order")
	}
	// The chef offers again; the customer can still answer.
	if _, err := h.offer(order, "o4", v4.Version, checkout.OfferRequested, "", ""); err != nil {
		t.Fatalf("offer after a lapse: %v", err)
	}
}

// Decline: the full return (100% of the actual paid aggregate even after the
// boundary), read like the late-payment full return, requested by the
// customer; one cancellation notice.
func TestLatePaymentDeclineReturnsEverything(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.withRefunds()
	h.withNotices(t)
	order, d, v := h.lateAgreementOrder(t, f, pol, "dec")
	res, err := h.offer(order, "o1", v.Version, checkout.OfferRequested, "", "")
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := h.lateView(t, order, d)
	h.now = cancelDeadline.Add(time.Hour)
	ans, err := h.answer(order, d, "d1", v1.Version, res.ID, "decline")
	if err != nil {
		t.Fatal(err)
	}
	if p, raw := h.lateView(t, order, d); p.Lifecycle != "cancellation_pending" || p.LatePayment == nil || p.LatePayment.State != "processing" {
		t.Fatalf("decline pending %s", raw)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("decline:\n%s", reasons(errs))
	}
	v2, raw := h.lateView(t, order, d)
	if v2.Lifecycle != "cancelled" || *v2.CancelReason != "late_payment_returned" || v2.Refund == nil || v2.Refund.TargetMinor != 10000 ||
		v2.Refund.State != "returned" || v2.Cancellation == nil || v2.Cancellation.State != "cancelled" ||
		*v2.Cancellation.Reason != "late_payment_returned" || *v2.Cancellation.Rate != 100 || v2.Cancellation.RequestedBy != "customer" ||
		v2.LatePayment != nil || v2.Agreed != nil {
		t.Fatalf("declined %s", raw)
	}
	if len(h.stripe.refundCreates()) != 1 || h.allocation(t, order) != "released" {
		t.Fatalf("refunds %v, day %s", h.stripe.refundCreates(), h.allocation(t, order))
	}
	var decision string
	var channel *string
	if err := h.admin.QueryRow(h.ctx, "SELECT decision, channel FROM business.late_payment_decision WHERE command_id = $1::uuid",
		ans.ID).Scan(&decision, &channel); err != nil || decision != "full_return" || channel != nil {
		t.Fatalf("decision %s %v %v", decision, channel, err)
	}
	var source string
	if err := h.admin.QueryRow(h.ctx, "SELECT source FROM business.refund_goal WHERE order_id = $1::uuid", order).Scan(&source); err != nil || source != "customer" {
		t.Fatalf("goal source %s %v", source, err)
	}
	if n := noticeCount(h.notices(t, order), "cancelled"); n != 1 {
		t.Fatalf("notices %s", kinds(h.notices(t, order)))
	}
}

// HONOUR_TOO_LATE: the requested time cannot be offered at or after the
// original handover, nor accepted then (declining still can); a restore
// freeze refuses new offers and answers (503) but replays them, and defers
// the commit; the signed-in owner answers with their account.
func TestLatePaymentTooLateFreezeAndOwner(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, v := h.lateAgreementOrder(t, f, pol, "late")
	saved := h.now
	h.now = lateHandover
	if _, err := h.offer(order, "o-late", v.Version, checkout.OfferRequested, "", ""); conflict(err) != "honour_too_late" {
		t.Fatalf("requested offer at the handover: %v", err)
	}
	h.now = saved
	res, err := h.offer(order, "o1", v.Version, checkout.OfferRequested, "", "")
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := h.lateView(t, order, d)
	h.now = lateHandover.Add(time.Minute)
	if _, err := h.answer(order, d, "a-late", v1.Version, res.ID, "accept"); conflict(err) != "honour_too_late" {
		t.Fatalf("accept after the handover: %v", err)
	}
	if p, _ := h.lateView(t, order, d); p.LatePayment.State != "offered" {
		t.Fatalf("a too-late acceptance closed the offer: %+v", p.LatePayment)
	}

	// Freeze: new offers and answers refused, replays answered.
	h.now = saved
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if again, err := h.offer(order, "o1", v.Version, checkout.OfferRequested, "", ""); err != nil || !again.Replayed || again.ID != res.ID {
		t.Fatalf("offer replay while frozen: %+v %v", again, err)
	}
	if _, err := h.offer(order, "o2", v1.Version, checkout.OfferRequested, "", ""); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("new offer while frozen: %v", err)
	}
	if _, err := h.answer(order, d, "a1", v1.Version, res.ID, "accept"); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("answer while frozen: %v", err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")

	// The signed-in owner (the guest links are revoked when the order is
	// added to the account).
	owner := h.resolve(t, customerA)
	h.owner(t, `SELECT business.order_owner_link($1::uuid, $2::uuid, 'chef', NULL, 'Linked in a test',
		(SELECT id FROM business.account WHERE provider_subject = $3::uuid), now())`, order, owner.AccountID, chef)
	vo, _ := h.lateView2(t, order, customerA)
	if _, err := h.answer(order, d, "a-guest", vo.Version, res.ID, "accept"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("revoked guest link: %v", err)
	}
	in := checkout.LatePaymentAnswerInput{ExpectedVersion: vo.Version, OfferID: res.ID, Answer: "accept"}
	ans, err := h.co.AnswerLatePayment(h.ctx, order, customerA, nil, "a1", in)
	if err != nil {
		t.Fatal(err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if again, err := h.co.AnswerLatePayment(h.ctx, order, customerA, nil, "a1", in); err != nil || !again.Replayed || again.ID != ans.ID {
		t.Fatalf("answer replay while frozen: %+v %v", again, err)
	}
	job := h.claimOnly(t, h.commandJob(t, ans.ID))
	var de *jobs.DeferError
	if err := h.cmds.Handle(h.ctx, job); !errors.As(err, &de) {
		t.Fatalf("commit while frozen: %v, want a deferral", err)
	}
	if got := h.recoveryState(t, ans.ID); got != "reserved" || h.orderRow(t, order).Lifecycle != "paid_awaiting_agreement" {
		t.Fatalf("while frozen: %s %+v", got, h.orderRow(t, order))
	}
	h.finish(t, job, de)
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	vo, raw := h.lateView2(t, order, customerA)
	if vo.Lifecycle != "confirmed" || vo.Agreed == nil {
		t.Fatalf("owner agreed %s", raw)
	}
	var kind string
	var mine bool
	if err := h.admin.QueryRow(h.ctx, `SELECT actor_kind, actor_account_id = $2::uuid AND guest_capability_id IS NULL
		FROM business.late_payment_answer WHERE command_id = $1::uuid`, ans.ID, owner.AccountID).Scan(&kind, &mine); err != nil ||
		kind != "customer" || !mine {
		t.Fatalf("owner answer %s %v %v", kind, mine, err)
	}
}

func (h *payHarness) lateView2(t *testing.T, order, subject string) (lateView, string) {
	t.Helper()
	raw, err := h.co.GetOrder(h.ctx, order, subject, nil)
	if err != nil {
		t.Fatal(err)
	}
	var v lateView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v, string(raw)
}

// The offered day fills between the acceptance and the worker: the commit
// checks the place again and refuses slot_unavailable (the offer lapses);
// the order keeps its requested date and day place, and the new day gets
// nothing.
func TestLatePaymentAcceptanceRefusedWhenTheDayFillsBeforeTheCommit(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	next := h.slotsOn(t, "2026-11-21")
	order, d, v := h.lateAgreementOrder(t, f, pol, "fill")
	res, err := h.offer(order, "o1", v.Version, checkout.OfferOther, "2026-11-21", next[3])
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := h.lateView(t, order, d)
	ans, err := h.answer(order, d, "a1", v1.Version, res.ID, "accept")
	if err != nil {
		t.Fatal(err)
	}
	must[catalog.Result](t)(h.sch.SetDayLimit(h.ctx, chef, "lim", day("2026-11-21"), schedule.DayLimitInput{ExpectedVersion: ptr(1), StandardLimit: ptr(0)}))
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	v2, raw := h.lateView(t, order, d)
	if v2.Lifecycle != "paid_awaiting_agreement" || v2.LatePayment == nil || v2.LatePayment.State != "waiting" ||
		v2.LatePayment.Answer == nil || v2.LatePayment.Answer.State != "lapsed" || *v2.LatePayment.Answer.Reason != "slot_unavailable" ||
		v2.Date != "2026-11-14" || v2.Agreed != nil {
		t.Fatalf("refused %s", raw)
	}
	if got := h.recoveryState(t, ans.ID); got != "secured" {
		t.Fatalf("refusal %s", got)
	}
	if h.allocation(t, order) != "committed" ||
		h.count(t, "SELECT count(*) FROM business.capacity_allocation WHERE order_ref = $1::uuid AND day = '2026-11-21'", order) != 0 {
		t.Fatal("a refused acceptance moved the day place")
	}
	if _, err := h.answer(order, d, "a2", v2.Version, res.ID, "accept"); conflict(err) != "offer_not_current" {
		t.Fatalf("answering the lapsed offer again: %v", err)
	}
}
