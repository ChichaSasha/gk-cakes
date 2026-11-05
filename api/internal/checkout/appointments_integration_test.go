//go:build integration

package checkout_test

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
)

// GK-031 B1 part 1: same-day pickup moves and chef date/time amendments
// (migration 0031; contracts r6.5 K06, K05, K11, K08;
// BO-GK-001.move_time, move_date, refund_follows_move).

// The fixture order is Saturday 2026-11-14 12:30 London (GMT = UTC), so the
// six-hour cutoff is 06:30Z and full_refund_deadline_at 2026-11-09 12:30Z.
var moveCutoff = time.Date(2026, 11, 14, 6, 30, 0, 0, time.UTC)

type appointmentJSON struct {
	Date     string
	ID       string
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

type moveView struct {
	Version              int
	Lifecycle            string
	Date                 string
	Slot                 appointmentJSON
	FullRefundDeadlineAt time.Time  `json:"full_refund_deadline_at"`
	PickupMoveUntil      *time.Time `json:"pickup_move_until"`
	AppointmentChange    *struct {
		Kind        string
		State       string
		Reason      *string
		RequestedBy string          `json:"requested_by"`
		To          appointmentJSON `json:"to"`
	} `json:"appointment_change"`
}

func (h *payHarness) moveView(t *testing.T, order string, d []byte) (moveView, string) {
	t.Helper()
	raw, err := h.co.GetOrder(h.ctx, order, "", d)
	if err != nil {
		t.Fatal(err)
	}
	var v moveView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v, string(raw)
}

// slotsOn is the published slot IDs of a Saturday (12:30, 13:00, 13:30,
// 14:00), read as a new purchase would read them now.
func (h *payHarness) slotsOn(t *testing.T, date string) []string {
	t.Helper()
	c, err := h.sch.Choices(h.ctx, day(date), schedule.Collection, 1)
	if err != nil || len(c.Slots) != 4 {
		t.Fatalf("slots of %s: %+v %v", date, c, err)
	}
	var out []string
	for _, s := range c.Slots {
		out = append(out, s.ID)
	}
	return out
}

func (h *payHarness) move(order string, d []byte, key string, version int, slot string) (checkout.Moved, error) {
	return h.co.MovePickup(h.ctx, order, "", d, key, checkout.PickupMoveInput{TargetSlotID: slot, ExpectedVersion: version})
}

// moveJob runs the command's job once (claimOnly) and finalizes it.
func (h *payHarness) moveJob(t *testing.T, op string) error {
	t.Helper()
	job := h.claimOnly(t, h.commandJob(t, op))
	err := h.cmds.Handle(h.ctx, job)
	h.finish(t, job, err)
	return err
}

type orderSlotRow struct {
	Day, Slot string
	Starts    *time.Time
	Ends      time.Time
	Version   int
}

func (h *payHarness) orderSlot(t *testing.T, order string) orderSlotRow {
	t.Helper()
	var r orderSlotRow
	var d time.Time
	if err := h.admin.QueryRow(h.ctx, `SELECT handover_day, slot_id::text, handover_starts_at, handover_ends_at, version
		FROM business.orders WHERE id = $1::uuid`, order).Scan(&d, &r.Slot, &r.Starts, &r.Ends, &r.Version); err != nil {
		t.Fatal(err)
	}
	r.Day = d.Format(time.DateOnly)
	return r
}

func at(hhmm string) time.Time {
	t, _ := time.Parse(time.RFC3339, "2026-11-14T"+hhmm+":00Z")
	return t
}

// AC1: two orders move to the same start (no cap); stale, other-day and
// unknown targets keep the original; the move is protected under K11 (no
// change while the journal is down, the original shown while the outcome
// is not secured), then visible with one notice; competing versions on one
// order give one mutation and one conflict.
func TestPickupMovesAreProtectedAndUncapped(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	email := h.withNotices(t)
	slots := h.slotsOn(t, "2026-11-14")
	next := h.slotsOn(t, "2026-11-21")
	orderA, _, _ := h.paidCollection(t, f, pol, "ma")
	orderB, _, _ := h.paidCollection(t, f, pol, "mb")
	dA, dB := h.digestOf(t, orderA), h.digestOf(t, orderB)
	vA, raw := h.moveView(t, orderA, dA)
	if vA.Lifecycle != "confirmed" || vA.Slot.ID != slots[0] || !vA.Slot.StartsAt.Equal(at("12:30")) || vA.Date != "2026-11-14" ||
		vA.PickupMoveUntil == nil || !vA.PickupMoveUntil.Equal(moveCutoff) || vA.AppointmentChange != nil {
		t.Fatalf("paid order %s", raw)
	}
	keep := func(what string) {
		t.Helper()
		if v, raw := h.moveView(t, orderA, dA); v.Slot.ID != slots[0] || v.Version != vA.Version {
			t.Fatalf("%s changed the order: %s", what, raw)
		}
	}
	if _, err := h.move(orderA, dA, "m-stale", vA.Version+1, slots[2]); conflict(err) != "stale_version" {
		t.Fatalf("stale version: %v", err)
	}
	keep("a stale move")
	if _, err := h.move(orderA, dA, "m-day", vA.Version, next[2]); conflict(err) != "slot_unavailable" {
		t.Fatalf("another day's slot: %v", err)
	}
	if _, err := h.move(orderA, dA, "m-unknown", vA.Version, "8d3f0f8e-5d55-4c3e-9a49-34a0e2a9f001"); conflict(err) != "slot_unavailable" {
		t.Fatalf("unknown slot: %v", err)
	}
	keep("an invalid window")
	if m, err := h.move(orderA, dA, "m-same", vA.Version, slots[0]); err != nil || !m.Unchanged || m.ID != orderA {
		t.Fatalf("same slot: %+v %v", m, err)
	}
	if n := h.count(t, "SELECT count(*) FROM business.order_command WHERE order_id = $1::uuid AND command = 'pickup_move'", orderA); n != 0 {
		t.Fatalf("%d move commands after refusals and a no-op", n)
	}
	keep("a no-op")

	h.now = h.now.Add(time.Minute)
	receipt := h.now
	m, err := h.move(orderA, dA, "m1", vA.Version, slots[2])
	if err != nil || m.Replayed || m.Unchanged {
		t.Fatalf("move: %+v %v", m, err)
	}
	op := m.ID
	if again, err := h.move(orderA, dA, "m1", vA.Version, slots[2]); err != nil || !again.Replayed || again.ID != op {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, err := h.move(orderA, dA, "m1", vA.Version, slots[3]); conflict(err) != "key_reused" {
		t.Fatalf("same key, other body: %v", err)
	}
	// Another move at the same version while this one is pending: its
	// operation already holds that version (K11), a stale conflict.
	if _, err := h.move(orderA, dA, "m2", vA.Version, slots[3]); conflict(err) != "stale_version" {
		t.Fatalf("another move while one is pending: %v", err)
	}
	v, raw := h.moveView(t, orderA, dA)
	if v.Slot.ID != slots[0] || v.AppointmentChange == nil || v.AppointmentChange.State != "processing" ||
		v.AppointmentChange.Kind != "pickup_move" || v.AppointmentChange.To.ID != slots[2] || v.AppointmentChange.RequestedBy != "customer" {
		t.Fatalf("pending move shown as %s", raw)
	}

	// K11: the journal is down: nothing changes.
	h.journal.Fail = func(string, string) error { return errors.New("b2 unavailable") }
	if err := h.moveJob(t, op); err == nil {
		t.Fatal("the move committed without the journal")
	}
	if got, row := h.recoveryState(t, op), h.orderSlot(t, orderA); got != "reserved" || row.Slot != slots[0] || row.Starts != nil {
		t.Fatalf("journal down: %s %+v", got, row)
	}
	// The outcome envelope fails: committed, but customers still see the
	// original appointment and no notice is queued.
	h.journal.Fail = func(op, name string) error {
		if op == "put" && strings.HasSuffix(name, ".outcome") {
			return errors.New("b2 unavailable")
		}
		return nil
	}
	h.due(t)
	if err := h.moveJob(t, op); err == nil {
		t.Fatal("the move was secured without the outcome envelope")
	}
	row := h.orderSlot(t, orderA)
	if got := h.recoveryState(t, op); got != "committed" || row.Slot != slots[2] || row.Starts == nil || !row.Starts.Equal(at("13:30")) ||
		!row.Ends.Equal(at("14:30")) || row.Version != vA.Version+1 {
		t.Fatalf("committed: %s %+v", got, row)
	}
	if v, raw := h.moveView(t, orderA, dA); v.Slot.ID != slots[0] || !v.Slot.StartsAt.Equal(at("12:30")) ||
		v.AppointmentChange.State != "processing" || !v.PickupMoveUntil.Equal(moveCutoff) {
		t.Fatalf("committed but unsecured shown as %s", raw)
	}
	if strings.Contains(kinds(h.notices(t, orderA)), "pickup_moved") {
		t.Fatal("a move notice before the move was secured")
	}
	h.journal.Fail = nil
	h.due(t)
	if err := h.moveJob(t, op); err != nil {
		t.Fatalf("secure: %v", err)
	}
	v, raw = h.moveView(t, orderA, dA)
	if v.Slot.ID != slots[2] || !v.Slot.StartsAt.Equal(at("13:30")) || v.Date != "2026-11-14" || v.AppointmentChange.State != "moved" ||
		!v.FullRefundDeadlineAt.Equal(cancelDeadline) || !v.PickupMoveUntil.Equal(at("07:30")) || v.Version != vA.Version+1 {
		t.Fatalf("moved %s", raw)
	}
	key, _ := h.coord.EnvelopeKey(recovery.Operation{ID: op, AggregateID: orderA, PriorVersion: vA.Version})
	if h.journal.Puts(key+".prepared") != 1 || h.journal.Puts(key+".outcome") != 1 {
		t.Fatalf("envelopes %d/%d, want once each", h.journal.Puts(key+".prepared"), h.journal.Puts(key+".outcome"))
	}
	var received time.Time
	if err := h.admin.QueryRow(h.ctx, "SELECT received_at FROM business.pickup_change WHERE command_id = $1::uuid", op).Scan(&received); err != nil ||
		!received.Equal(receipt) {
		t.Fatalf("history receipt %s %v", received, err)
	}

	// The second order moves to the same start: no per-slot count.
	vB, _ := h.moveView(t, orderB, dB)
	mB, err := h.move(orderB, dB, "mb1", vB.Version, slots[2])
	if err != nil {
		t.Fatalf("second order to the same start: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	if v, raw := h.moveView(t, orderB, dB); v.Slot.ID != slots[2] || v.AppointmentChange.State != "moved" || h.recoveryState(t, mB.ID) != "secured" {
		t.Fatalf("second order %s", raw)
	}

	// Competing versions on one order: one mutation, one conflict; the
	// loser's version is stale afterwards.
	vA, _ = h.moveView(t, orderA, dA)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	targets := []string{slots[3], slots[1]}
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = h.move(orderA, dA, "race-"+targets[i], vA.Version, targets[i])
		}()
	}
	wg.Wait()
	winner := -1
	for i, err := range errs {
		switch {
		case err == nil && winner < 0:
			winner = i
		case conflict(err) == "stale_version":
		default:
			t.Fatalf("race results %v", errs)
		}
	}
	if winner < 0 {
		t.Fatalf("no move won: %v", errs)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	if _, err := h.move(orderA, dA, "race-retry", vA.Version, targets[1-winner]); conflict(err) != "stale_version" {
		t.Fatalf("loser retried at the old version: %v", err)
	}
	if v, raw := h.moveView(t, orderA, dA); v.Slot.ID != targets[winner] || v.Version != vA.Version+1 {
		t.Fatalf("after the race %s", raw)
	}

	// One notice per secured move, with the new and previous times and the
	// unchanged refund deadline.
	var moved []noticeRow
	for _, n := range h.notices(t, orderA) {
		if n.Kind == "pickup_moved" {
			moved = append(moved, n)
		}
	}
	if len(moved) != 2 || moved[0].Dedupe != op || moved[0].Template != "pickup-moved.v1" || moved[0].Outcome != "queued" {
		t.Fatalf("move notices %+v", moved)
	}
	req := h.request(t, moved[0])
	if req.Subject != "Your GK Cakes collection time has changed" {
		t.Fatalf("subject %q", req.Subject)
	}
	has(t, "move notice", req.Text, "New time: Saturday 14 November 2026, 13:30 to 14:30 (London time)",
		"Previous time: Saturday 14 November 2026, 12:30 to 13:30 (London time)", "Order reference: "+orderA,
		"cancel before 12:30 on Monday 9 November 2026 (London time) for a full refund",
		uiOrigin+"/orders/"+orderA)
	hasNot(t, "move notice", req.Text+req.HTML, secrets(dA, "")...)
	sent := 0
	for _, s := range email.all() {
		if s.req.Subject == req.Subject {
			sent++
		}
	}
	if sent != 3 {
		t.Fatalf("%d move emails sent, want 3 (two moves of A, one of B)", sent)
	}
	h.noPersonalDataInLogs(t)
}

// A refusal at the commit (the schedule changed after the request) and a
// freeze that lands at the commit both keep the original appointment and
// queue no notice.
func TestFailedMoveCommitKeepsTheOriginal(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.withNotices(t)
	slots := h.slotsOn(t, "2026-11-14")
	order, _, _ := h.paidCollection(t, f, pol, "fc")
	d := h.digestOf(t, order)
	v, _ := h.moveView(t, order, d)

	// Freeze at the commit: rolled back, nothing changes.
	m, err := h.move(order, d, "f1", v.Version, slots[1])
	if err != nil {
		t.Fatal(err)
	}
	h.journal.Fail = func(op, name string) error {
		if op == "put" && strings.HasSuffix(name, ".prepared") {
			h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
		}
		return nil
	}
	var de *jobs.DeferError
	if err := h.moveJob(t, m.ID); !errors.As(err, &de) {
		t.Fatalf("commit while frozen: %v", err)
	}
	if got, row := h.recoveryState(t, m.ID), h.orderSlot(t, order); got != "prepared" || row.Slot != slots[0] || row.Version != v.Version {
		t.Fatalf("frozen commit: %s %+v", got, row)
	}
	// While frozen a new move is refused; the same key replays.
	if _, err := h.move(order, d, "f-new", v.Version, slots[2]); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("new move while frozen: %v", err)
	}
	if again, err := h.move(order, d, "f1", v.Version, slots[1]); err != nil || !again.Replayed || again.ID != m.ID {
		t.Fatalf("replay while frozen: %+v %v", again, err)
	}
	h.journal.Fail = nil
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("after the freeze:\n%s", reasons(errs))
	}
	v, raw := h.moveView(t, order, d)
	if v.Slot.ID != slots[1] {
		t.Fatalf("after the freeze %s", raw)
	}

	// The chef publishes a new schedule version between request and
	// commit: the target's slot ID is no longer offered, the move is
	// refused and the order keeps its appointment.
	m, err = h.move(order, d, "f2", v.Version, slots[3])
	if err != nil {
		t.Fatal(err)
	}
	must[catalog.Result](t)(h.sch.PublishVersion(h.ctx, chef, "sched2", schedule.VersionInput{ExpectedRevision: ptr(1),
		StandardLimit: ptr(7), BookableUntil: "2027-01-31", Weekdays: []schedule.WeekdayInput{
			{Weekday: "saturday", Opens: "12:30", Closes: "15:00", Starts: []string{"12:30", "13:00", "13:30", "14:00"}}}}))
	notices := len(h.notices(t, order))
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("refused move:\n%s", reasons(errs))
	}
	after, raw := h.moveView(t, order, d)
	if after.Slot.ID != slots[1] || after.AppointmentChange.State != "refused" || after.AppointmentChange.Reason == nil ||
		*after.AppointmentChange.Reason != "slot_unavailable" || after.Version != v.Version+1 {
		t.Fatalf("refused move %s", raw)
	}
	if len(h.notices(t, order)) != notices {
		t.Fatal("a refused move queued a notice")
	}
}

// AC2: exactly six hours before the current start is allowed, a second
// later is MOVE_TOO_LATE; a target inside six hours is not offered; moves
// and production starts keep the original refund deadline (100% before it,
// 50% at it); the customer cannot change the date or a GK delivery.
func TestMoveCutoffAndUnchangedEntitlement(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	slots := h.slotsOn(t, "2026-11-14")
	order, _, _ := h.paidCollection(t, f, pol, "cut")
	d := h.digestOf(t, order)
	v, _ := h.moveView(t, order, d)

	// Production (timer) first: the move flow still works and the deadline
	// stays.
	if err := h.runProduction(t, order); err != nil {
		t.Fatalf("timer: %v", err)
	}
	v, raw := h.moveView(t, order, d)
	if v.Lifecycle != "in_production" {
		t.Fatalf("after the timer %s", raw)
	}
	h.now = moveCutoff.Add(time.Second)
	if _, err := h.move(order, d, "late", v.Version, slots[2]); conflict(err) != "move_too_late" {
		t.Fatalf("one second after the cutoff: %v", err)
	}
	h.now = moveCutoff
	// 12:30 + 6 h equality allowed; the 13:30 target is at least six hours
	// after the receipt.
	m, err := h.move(order, d, "eq", v.Version, slots[2])
	if err != nil {
		t.Fatalf("at exactly six hours: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	v, raw = h.moveView(t, order, d)
	if v.Slot.ID != slots[2] || !v.FullRefundDeadlineAt.Equal(cancelDeadline) || h.recoveryState(t, m.ID) != "secured" {
		t.Fatalf("moved at the cutoff %s", raw)
	}
	// The timer did not run twice and the production anchor did not move.
	if tr := h.transition(t, order); tr.Count != 1 || tr.Audits != 1 || tr.Source != "timer" ||
		!tr.Effective.Equal(time.Date(2026, 11, 7, 10, 9, 0, 0, time.UTC)) {
		t.Fatalf("production transition %+v", tr)
	}
	// Now at 13:30 (cutoff 07:30). At 06:31 the 12:30 start is less than
	// six hours away, so it is not a valid target.
	h.now = moveCutoff.Add(time.Minute)
	if _, err := h.move(order, d, "inside", v.Version, slots[0]); conflict(err) != "slot_unavailable" {
		t.Fatalf("target inside six hours: %v", err)
	}
	// The entitlement still follows the original deadline (BO-GK-001
	// refund_follows_move): 50% now (after 2026-11-09 12:30Z).
	if p := h.preview(t, order, d); p.Rate != 50 || !p.FullRefundDeadlineAt.Equal(cancelDeadline) {
		t.Fatalf("preview after the move %+v", p)
	}
	var original, deadline time.Time
	if err := h.admin.QueryRow(h.ctx, "SELECT original_handover_at, full_refund_deadline_at FROM business.orders WHERE id = $1::uuid",
		order).Scan(&original, &deadline); err != nil || !original.Equal(at("12:30")) || !deadline.Equal(cancelDeadline) {
		t.Fatalf("anchors %s %s %v", original, deadline, err)
	}

	// A move before the boundary keeps 100% (the move does not reset it).
	h.now = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	order2, _, _ := h.paidCollection(t, f, pol, "cut2")
	d2 := h.digestOf(t, order2)
	v2, _ := h.moveView(t, order2, d2)
	if _, err := h.move(order2, d2, "early", v2.Version, slots[3]); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	if p := h.preview(t, order2, d2); p.Rate != 100 || !p.FullRefundDeadlineAt.Equal(cancelDeadline) {
		t.Fatalf("preview before the boundary after a move %+v", p)
	}
	// A manual start after the move: one transition, the move history kept.
	v2, _ = h.moveView(t, order2, d2)
	if _, err := h.co.StartProduction(h.ctx, fulfilment, "s1", order2, checkout.ProductionInput{ExpectedVersion: v2.Version}); err != nil {
		t.Fatalf("manual start: %v", err)
	}
	if err := h.runProduction(t, order2); err != nil {
		t.Fatalf("timer after the manual start: %v", err)
	}
	if tr := h.transition(t, order2); tr.Count != 1 || tr.Source != "manual" {
		t.Fatalf("transition %+v", tr)
	}
}

// Access and eligibility: a guest capability without move_pickup, another
// customer's order, an unpaid order and a GK delivery order cannot be moved
// by the customer.
func TestMoveAccessAndEligibility(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	slots := h.slotsOn(t, "2026-11-14")
	order, _, _ := h.paidCollection(t, f, pol, "acc")
	d := h.digestOf(t, order)
	v, _ := h.moveView(t, order, d)
	_, readOnly := proof(t)
	h.owner(t, `INSERT INTO business.order_guest_capability (id, order_id, capability_digest, allowed_commands)
		VALUES (gen_random_uuid(), $1::uuid, $2, ARRAY['read', 'cancel'])`, order, readOnly)
	if _, err := h.move(order, readOnly, "ro", v.Version, slots[1]); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("capability without move_pickup: %v", err)
	}
	if _, err := h.co.MovePickup(h.ctx, order, customerA, nil, "own", checkout.PickupMoveInput{TargetSlotID: slots[1], ExpectedVersion: v.Version}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("someone else's order: %v", err)
	}
	if _, err := h.sch.MoveChoices(h.ctx, "", readOnly, order); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("choices without move_pickup: %v", err)
	}
	unpaid, du := h.tenThousandOrder(t, f, pol, "unpaid")
	if _, err := h.move(unpaid, du, "u", 1, slots[1]); conflict(err) != "move_not_eligible" {
		t.Fatalf("unpaid order: %v", err)
	}
	h.owner(t, "UPDATE business.orders SET fulfillment_mode = 'gk_delivery' WHERE id = $1::uuid", order)
	if _, err := h.move(order, d, "gk", v.Version, slots[1]); conflict(err) != "move_not_eligible" {
		t.Fatalf("GK delivery: %v", err)
	}
	c, err := h.sch.MoveChoices(h.ctx, "", d, order)
	if err != nil || c.DayStatus != "not_eligible" || c.MoveUntil != nil || len(c.Slots) != 4 || c.Slots[1].Availability != "unavailable" {
		t.Fatalf("GK delivery choices %+v %v", c, err)
	}
}

// The schedule purpose=pickup_move projection: the order's day and mode,
// its current slot, the cutoff; a full day and the order's own last ready
// cake never hide valid moves; past the cutoff nothing is available.
func TestMoveChoices(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	slots := h.slotsOn(t, "2026-11-14")
	order, d := h.paidOrder(t, f, pol) // a made-to-order cake and a ready cake
	h.payNow(t, order, d, "mc")
	// The day is full and the lot has no stock left for new purchases.
	must[catalog.Result](t)(h.sch.SetDayLimit(h.ctx, chef, "full", day("2026-11-14"), schedule.DayLimitInput{ExpectedVersion: ptr(1), StandardLimit: ptr(0)}))
	h.owner(t, "UPDATE business.finished_cake_lot SET reserved_quantity = produced_quantity - sold_quantity WHERE id = $1::uuid", f.readyLot)
	c, err := h.sch.MoveChoices(h.ctx, "", d, order)
	if err != nil {
		t.Fatal(err)
	}
	if c.Purpose != "pickup_move" || c.OrderID != order || c.Date != "2026-11-14" || c.FulfillmentMode != "collection" ||
		c.DayStatus != "open" || c.CurrentSlot == nil || c.CurrentSlot.ID != slots[0] || c.MoveUntil == nil ||
		!c.MoveUntil.Equal(moveCutoff) || len(c.Slots) != 4 || c.Timezone != "Europe/London" {
		t.Fatalf("choices %+v", c)
	}
	if c.Slots[0].Availability != "unavailable" || *c.Slots[0].ReasonCode != "current_slot" {
		t.Fatalf("current slot %+v", c.Slots[0])
	}
	for _, s := range c.Slots[1:] {
		if s.Availability != "available" || s.ReasonCode != nil {
			t.Fatalf("a full day hid a move: %+v", s)
		}
	}
	// A new purchase sees the day as full.
	if pub, err := h.sch.Choices(h.ctx, day("2026-11-14"), schedule.Collection, 1); err != nil || pub.Slots[1].Availability != "unavailable" {
		t.Fatalf("public choices %+v %v", pub, err)
	}
	// Past the cutoff.
	h.now = moveCutoff.Add(time.Second)
	c, err = h.sch.MoveChoices(h.ctx, "", d, order)
	if err != nil || c.DayStatus != "too_late" || c.Slots[1].Availability != "unavailable" || *c.Slots[1].ReasonCode != "too_late" {
		t.Fatalf("after the cutoff %+v %v", c, err)
	}
	// At 06:00 (cutoff 06:30) the day is open and 13:00 is offered.
	h.now = moveCutoff.Add(-30 * time.Minute)
	c, err = h.sch.MoveChoices(h.ctx, "", d, order)
	if err != nil || c.DayStatus != "open" || c.Slots[1].Availability != "available" {
		t.Fatalf("before the cutoff %+v %v", c, err)
	}
}

// The chef's date amendment needs the customer's recorded agreement; it
// moves the order and its day place (standard within the limit, a recorded
// chef_extra beyond it), keeps the refund deadline and production anchor,
// shows the history (agreement to the chef only) and emails the customer.
func TestChefAppointmentChange(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	h.withNotices(t)
	next := h.slotsOn(t, "2026-11-21")
	order, _, _ := h.paidCollection(t, f, pol, "chef")
	d := h.digestOf(t, order)
	v, _ := h.moveView(t, order, d)
	h.now = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC) // placed at 10:00
	in := checkout.AppointmentInput{ExpectedVersion: v.Version, TargetDate: "2026-11-21", TargetSlotID: next[1],
		CustomerAgreement: &checkout.Agreement{Channel: "instagram", AgreedAt: "2026-11-05T11:30:00Z", EvidenceRef: "IG DM 5 Nov"},
		FeasibilityNote:   "Baked fresh for the new date; within shelf life"}
	if err := (checkout.AppointmentInput{ExpectedVersion: 1, TargetDate: "2026-11-21", TargetSlotID: next[1],
		FeasibilityNote: "x"}).Validate(); !strings.HasPrefix(invalidField(err), "customer_agreement:") {
		t.Fatalf("no agreement: %v", err)
	}
	if _, err := h.co.ChangeAppointment(h.ctx, fulfilment, "a0", order, in); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("fulfillment: %v", err)
	}
	future := in
	future.CustomerAgreement = &checkout.Agreement{Channel: "phone", AgreedAt: "2026-11-05T12:30:00Z", EvidenceRef: "call"}
	if _, err := h.co.ChangeAppointment(h.ctx, chef, "a-future", order, future); !strings.HasPrefix(invalidField(err), "customer_agreement:") {
		t.Fatalf("agreement after the receipt: %v", err)
	}
	early := in
	early.CustomerAgreement = &checkout.Agreement{Channel: "phone", AgreedAt: "2026-11-05T09:00:00Z", EvidenceRef: "call"}
	if _, err := h.co.ChangeAppointment(h.ctx, chef, "a-early", order, early); !strings.HasPrefix(invalidField(err), "customer_agreement:") {
		t.Fatalf("agreement before the order: %v", err)
	}
	// The new day is full of committed standard cakes: chef_extra.
	must[catalog.Result](t)(h.sch.SetDayLimit(h.ctx, chef, "lim", day("2026-11-21"), schedule.DayLimitInput{ExpectedVersion: ptr(0), StandardLimit: ptr(0)}))
	res, err := h.co.ChangeAppointment(h.ctx, chef, "a1", order, in)
	if err != nil || res.Unchanged {
		t.Fatalf("change: %+v %v", res, err)
	}
	if v, raw := h.moveView(t, order, d); v.Date != "2026-11-14" || v.AppointmentChange.State != "processing" ||
		v.AppointmentChange.RequestedBy != "chef" || v.AppointmentChange.Kind != "appointment_change" {
		t.Fatalf("pending change %s", raw)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	v, raw := h.moveView(t, order, d)
	if v.Date != "2026-11-21" || v.Slot.ID != next[1] || !v.Slot.StartsAt.Equal(time.Date(2026, 11, 21, 13, 0, 0, 0, time.UTC)) ||
		!v.FullRefundDeadlineAt.Equal(cancelDeadline) || v.AppointmentChange.State != "moved" {
		t.Fatalf("changed %s", raw)
	}
	var old, newState, class string
	var evidence *string
	if err := h.admin.QueryRow(h.ctx, `SELECT
		(SELECT state FROM business.capacity_allocation WHERE order_ref = $1::uuid AND day = '2026-11-14'),
		n.state, n.classification, n.chef_evidence
		FROM business.capacity_allocation n WHERE n.order_ref = $1::uuid AND n.day = '2026-11-21'`, order).Scan(&old, &newState, &class, &evidence); err != nil {
		t.Fatal(err)
	}
	if old != "released" || newState != "committed" || class != "chef_extra" || evidence == nil || *evidence != in.FeasibilityNote {
		t.Fatalf("allocations %s %s %s %v", old, newState, class, evidence)
	}
	if _, at := h.productionJob(t, order); !at.Equal(time.Date(2026, 11, 7, 10, 9, 0, 0, time.UTC)) {
		t.Fatalf("production anchor moved to %s", at)
	}
	// Staff history: the agreement for the chef only.
	staffView := func(subject string) map[string]json.RawMessage {
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
	ch := string(staffView(chef)["appointment_history"])
	has(t, "chef history", ch, `"kind": "appointment_change"`, `"evidence_ref": "IG DM 5 Nov"`, `"allocation": "chef_extra"`,
		`"state": "moved"`, in.FeasibilityNote)
	for _, who := range []string{fulfilment, support} {
		hist := string(staffView(who)["appointment_history"])
		has(t, who+" history", hist, `"kind": "appointment_change"`, `"state": "moved"`)
		hasNot(t, who+" history", hist, "IG DM", "evidence_ref", "feasibility_note")
	}
	// Notice.
	var changed []noticeRow
	for _, n := range h.notices(t, order) {
		if n.Kind == "appointment_changed" {
			changed = append(changed, n)
		}
	}
	if len(changed) != 1 || changed[0].Template != "appointment-changed.v1" {
		t.Fatalf("notices %+v", changed)
	}
	req := h.request(t, changed[0])
	has(t, "change notice", req.Text, "As agreed with you, the chef has changed your collection date or time.",
		"New time: Saturday 21 November 2026, 13:00 to 14:00 (London time)",
		"Previous time: Saturday 14 November 2026, 12:30 to 13:30 (London time)")
	hasNot(t, "change notice", req.Text, "IG DM", "shelf life")
	// The same key replays; the customer's own move flow stays same-day:
	// the new day's slots are its choices now.
	if again, err := h.co.ChangeAppointment(h.ctx, chef, "a1", order, in); err != nil || !again.Replayed || again.ID != res.ID {
		t.Fatalf("replay %+v %v", again, err)
	}
	c, err := h.sch.MoveChoices(h.ctx, "", d, order)
	if err != nil || c.Date != "2026-11-21" || c.CurrentSlot.ID != next[1] {
		t.Fatalf("choices after the change %+v %v", c, err)
	}
	// Back to the original day (an existing released row is reused): a
	// standard place again (limit 7 on the 14th).
	v, _ = h.moveView(t, order, d)
	back := in
	back.ExpectedVersion, back.TargetDate, back.TargetSlotID = v.Version, "2026-11-14", h.slotsOn(t, "2026-11-14")[0]
	if _, err := h.co.ChangeAppointment(h.ctx, chef, "a2", order, back); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	if err := h.admin.QueryRow(h.ctx, `SELECT state, classification FROM business.capacity_allocation
		WHERE order_ref = $1::uuid AND day = '2026-11-14'`, order).Scan(&newState, &class); err != nil || newState != "committed" || class != "standard" {
		t.Fatalf("back on the 14th: %s %s %v", newState, class, err)
	}
	if n := h.count(t, "SELECT count(*) FROM business.capacity_allocation WHERE order_ref = $1::uuid AND state <> 'released'", order); n != 1 {
		t.Fatalf("%d live allocations", n)
	}
}
