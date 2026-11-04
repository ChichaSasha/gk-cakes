//go:build integration

package checkout_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
	"github.com/jackc/pgx/v5/pgxpool"
)

// commandHarness adds the worker side: a gk_worker pool, an in-memory
// independent journal and the order-command handler.
type commandHarness struct {
	*harness
	worker  *pgxpool.Pool
	journal *recovery.MemoryJournal
	coord   *recovery.Coordinator
	cmds    *checkout.Commands
	logs    *bytes.Buffer
	workers int
}

func newCommandHarness(t *testing.T) *commandHarness {
	t.Helper()
	h := newHarness(t)
	h.co.Env = "test"
	pool, err := pgxpool.New(h.ctx, h.db.LoginURL(t, "gk_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	sealer, err := recovery.NewSealer("k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	j := recovery.NewMemoryJournal()
	coord := &recovery.Coordinator{Journal: j, Sealer: sealer, Environment: "test"}
	logs := &bytes.Buffer{}
	return &commandHarness{harness: h, worker: pool, journal: j, coord: coord, logs: logs,
		cmds: &checkout.Commands{DB: pool, Coordinator: coord, Logger: slog.New(slog.NewJSONHandler(logs, nil))}}
}

// claim leases the next due job to a new worker identity.
func (h *commandHarness) claim(t *testing.T) jobs.Job {
	t.Helper()
	h.workers++
	job := jobs.Job{Worker: fmt.Sprintf("00000000-0000-4000-8000-%012d", h.workers)}
	if err := h.worker.QueryRow(h.ctx, `SELECT job_id::text, job_operation_id::text, job_generation
		FROM business.job_claim($1::uuid, interval '60 seconds')`, job.Worker).Scan(&job.ID, &job.OperationID, &job.Generation); err != nil {
		t.Fatalf("claim: %v", err)
	}
	return job
}

// expire ends a job's lease (the worker died) so it can be claimed again.
func (h *commandHarness) expire(t *testing.T, job jobs.Job) {
	t.Helper()
	h.owner(t, "UPDATE business.job SET state = 'retry', lease_owner = NULL, lease_until = NULL, next_attempt_at = now() WHERE id = $1::uuid", job.ID)
}

// freeOrder places a zero-amount order: a 100% code on one made-to-order
// cake plus one ready cake, reserved by checkout.
func (h *commandHarness) freeOrder(t *testing.T, f fixture, pol string) (order string, digest []byte) {
	t.Helper()
	return h.freeOrderOn(t, f, pol, "2026-11-14", f.slot)
}

func (h *commandHarness) freeOrderOn(t *testing.T, f fixture, pol, date, slot string) (order string, digest []byte) {
	t.Helper()
	h.code(t, "c100", checkout.CodeInput{Code: "GIFT100", CodeTerms: checkout.CodeTerms{Kind: checkout.KindPercentage, BasisPoints: ptr(10000)}})
	in := checkout.QuoteInput{Date: date, SlotID: slot, FulfillmentMode: schedule.Collection, DiscountCode: "GIFT100",
		Lines: []checkout.QuoteLine{{VariantID: f.v1001, Quantity: 1}, {LotID: f.readyLot, Quantity: 1}}}
	quote, d := h.guestQuote(t, "q-free", in)
	res, err := h.checkout("", d, "k-free", quote, pol)
	if err != nil {
		t.Fatal(err)
	}
	return res.ID, d
}

type freeOrderJSON struct {
	Version            int
	Lifecycle          string
	PaymentRequirement string     `json:"payment_requirement"`
	TotalMinor         int64      `json:"total_minor"`
	FreeConfirmedAt    *time.Time `json:"free_confirmed_at"`
	FreeConfirmation   *struct {
		State       string
		Reason      *string
		RequestedAt time.Time `json:"requested_at"`
	} `json:"free_confirmation"`
}

func (h *commandHarness) order(t *testing.T, id string, d []byte) freeOrderJSON {
	t.Helper()
	raw, err := h.co.GetOrder(h.ctx, id, "", d)
	if err != nil {
		t.Fatal(err)
	}
	var o freeOrderJSON
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	return o
}

func (h *commandHarness) holds(t *testing.T, order, lot string) string {
	t.Helper()
	var alloc, res, code string
	var reserved, sold int
	if err := h.admin.QueryRow(h.ctx, `SELECT
		(SELECT state FROM business.capacity_allocation WHERE order_ref = $1::uuid),
		(SELECT state FROM business.order_lot_reservation WHERE order_id = $1::uuid),
		(SELECT state FROM business.code_application WHERE order_id = $1::uuid),
		l.reserved_quantity, l.sold_quantity
		FROM business.finished_cake_lot l WHERE l.id = $2::uuid`, order, lot).Scan(&alloc, &res, &code, &reserved, &sold); err != nil {
		t.Fatal(err)
	}
	return strings.Join([]string{alloc, res, code}, "/") + " " + string(rune('0'+reserved)) + "r" + string(rune('0'+sold)) + "s"
}

func (h *commandHarness) recoveryState(t *testing.T, op string) string {
	t.Helper()
	var s string
	if err := h.admin.QueryRow(h.ctx, "SELECT state FROM business.recovery_operation WHERE id = $1::uuid", op).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFreeConfirmationIsProtectedBeforeItIsVisible(t *testing.T) {
	h := newCommandHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.freeOrder(t, f, pol)
	o := h.order(t, order, d)
	if o.TotalMinor != 0 || o.PaymentRequirement != "none" || o.Lifecycle != "awaiting_payment" || o.FreeConfirmation != nil {
		t.Fatalf("free order %+v", o)
	}
	if got := h.holds(t, order, f.readyLot); got != "tentative/reserved/reserved 1r0s" {
		t.Fatalf("holds before %s", got)
	}

	// The request records the command and answers; nothing is committed.
	h.now = h.now.Add(time.Minute)
	receipt := h.now
	res, err := h.co.FreeConfirm(h.ctx, order, "", d, "fc1", checkout.FreeConfirmInput{ExpectedVersion: 1})
	if err != nil || res.Replayed {
		t.Fatalf("FreeConfirm: %+v %v", res, err)
	}
	op := res.ID
	if again, err := h.co.FreeConfirm(h.ctx, order, "", d, "fc1", checkout.FreeConfirmInput{ExpectedVersion: 1}); err != nil ||
		again.ID != op || !again.Replayed {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, err := h.co.FreeConfirm(h.ctx, order, "", d, "fc2", checkout.FreeConfirmInput{ExpectedVersion: 1}); conflict(err) != "state_conflict" {
		t.Fatalf("a second confirmation with another key: %v", err)
	}
	if _, err := h.co.FreeConfirm(h.ctx, order, "", d, "fc1", checkout.FreeConfirmInput{ExpectedVersion: 2}); conflict(err) != "key_reused" {
		t.Fatalf("the same key with another body: %v", err)
	}
	o = h.order(t, order, d)
	if o.Lifecycle != "awaiting_payment" || o.FreeConfirmation == nil || o.FreeConfirmation.State != "processing" ||
		!o.FreeConfirmation.RequestedAt.Equal(receipt) {
		t.Fatalf("after the request: %+v", o)
	}

	// The journal is down: the worker commits nothing and retries.
	h.journal.Fail = func(string, string) error { return errors.New("b2 unavailable") }
	job := h.claim(t)
	if job.OperationID != op {
		t.Fatalf("claimed %s, want the command %s", job.OperationID, op)
	}
	if err := h.cmds.Handle(h.ctx, job); err == nil {
		t.Fatal("Handle succeeded without the journal")
	}
	if got := h.recoveryState(t, op); got != "reserved" || h.order(t, order, d).Lifecycle != "awaiting_payment" {
		t.Fatalf("journal down: recovery %s; want reserved and nothing committed", got)
	}

	// The prepared envelope is written and the commit happens, but the
	// outcome envelope fails: committed, shown as confirmation_pending.
	h.journal.Fail = func(op, name string) error {
		if op == "put" && strings.HasSuffix(name, ".outcome") {
			return errors.New("b2 unavailable")
		}
		return nil
	}
	h.expire(t, job)
	job = h.claim(t)
	if err := h.cmds.Handle(h.ctx, job); err == nil {
		t.Fatal("Handle succeeded without the outcome envelope")
	}
	if got := h.recoveryState(t, op); got != "committed" {
		t.Fatalf("outcome envelope down: recovery %s, want committed", got)
	}
	o = h.order(t, order, d)
	if o.Lifecycle != "confirmation_pending" || o.FreeConfirmedAt != nil || o.FreeConfirmation.State != "processing" || o.Version != 2 {
		t.Fatalf("committed but unsecured order shown as %+v", o)
	}
	if got := h.holds(t, order, f.readyLot); got != "committed/sold/consumed 0r1s" {
		t.Fatalf("holds after the commit %s", got)
	}

	// The next lease secures it without a second commit.
	h.journal.Fail = nil
	h.expire(t, job)
	job = h.claim(t)
	if err := h.cmds.Handle(h.ctx, job); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	o = h.order(t, order, d)
	if o.Lifecycle != "confirmed" || o.FreeConfirmedAt == nil || !o.FreeConfirmedAt.Equal(receipt) ||
		o.FreeConfirmation.State != "confirmed" || o.Version != 2 {
		t.Fatalf("secured order %+v; want confirmed at the receipt instant", o)
	}
	if got := h.holds(t, order, f.readyLot); got != "committed/sold/consumed 0r1s" {
		t.Fatalf("holds after securing %s (applied twice?)", got)
	}
	if n := h.count(t, "SELECT count(*) FROM business.audit_event WHERE action = 'order.free_confirmed' AND subject_id = $1::uuid", order); n != 1 {
		t.Fatalf("%d confirmation audits, want 1", n)
	}
	// The journal holds the exact accepted payload and the outcome, sealed.
	key, _ := h.coord.EnvelopeKey(recovery.Operation{ID: op, AggregateID: order, PriorVersion: 1})
	if h.journal.Puts(key+".prepared") != 1 || h.journal.Puts(key+".outcome") != 1 {
		t.Fatalf("envelopes written %d/%d times, want once each", h.journal.Puts(key+".prepared"), h.journal.Puts(key+".outcome"))
	}
	sealed, _ := h.journal.Get(h.ctx, key+".prepared")
	if bytes.Contains(sealed, []byte("test.customer@example.com")) {
		t.Fatal("prepared envelope stored unencrypted")
	}
	if pending, err := h.coord.Pending(h.ctx); err != nil || len(pending) != 0 {
		t.Fatalf("Pending = %v, %v; want none", pending, err)
	}
	var payload string
	if err := h.admin.QueryRow(h.ctx, "SELECT payload FROM business.order_command WHERE id = $1::uuid", op).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var p struct {
		Command    string
		ReceivedAt string `json:"received_at"`
		Order      struct {
			ID              string
			ContactEmail    string `json:"contact_email"`
			Items           []json.RawMessage
			LotReservations []json.RawMessage `json:"lot_reservations"`
			CodeApplication *json.RawMessage  `json:"code_application"`
		}
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil || p.Command != "order.free_confirm" || p.Order.ID != order ||
		p.Order.ContactEmail != "test.customer@example.com" || len(p.Order.Items) != 2 || len(p.Order.LotReservations) != 1 ||
		p.Order.CodeApplication == nil || p.ReceivedAt != receipt.Format("2006-01-02T15:04:05.000000Z") {
		t.Fatalf("payload %s (%v)", payload, err)
	}
	// Logs carry IDs only.
	if strings.Contains(h.logs.String(), "example.com") || strings.Contains(h.logs.String(), "Test Customer") {
		t.Fatalf("personal data in the worker logs: %s", h.logs.String())
	}
	// A confirmed order cannot be confirmed again.
	if _, err := h.co.FreeConfirm(h.ctx, order, "", d, "fc3", checkout.FreeConfirmInput{ExpectedVersion: 2}); conflict(err) != "state_conflict" {
		t.Fatalf("confirming a confirmed order: %v", err)
	}
}

func TestFreeConfirmationClocksAndAccess(t *testing.T) {
	h := newCommandHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	// Handover Sat 21 Nov 12:30 GMT, quoted Fri 13 Nov 10:00 (194.5 h before).
	h.now = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	c, err := h.sch.Choices(h.ctx, day("2026-11-21"), schedule.Collection, 1)
	if err != nil || len(c.Slots) == 0 {
		t.Fatalf("slots: %+v %v", c, err)
	}
	order, d := h.freeOrderOn(t, f, pol, "2026-11-21", c.Slots[0].ID)
	try := func(key string) error {
		_, err := h.co.FreeConfirm(h.ctx, order, "", d, key, checkout.FreeConfirmInput{ExpectedVersion: 1})
		return err
	}
	// Access: a wrong capability, a signed-in stranger and a stale version.
	_, other := proof(t)
	if _, err := h.co.FreeConfirm(h.ctx, order, "", other, "x", checkout.FreeConfirmInput{ExpectedVersion: 1}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("wrong capability: %v", err)
	}
	if _, err := h.co.FreeConfirm(h.ctx, order, "11111111-2222-4333-8444-555555555555", nil, "x", checkout.FreeConfirmInput{ExpectedVersion: 1}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("signed-in stranger: %v", err)
	}
	if _, err := h.co.FreeConfirm(h.ctx, order, "", d, "x", checkout.FreeConfirmInput{ExpectedVersion: 3}); conflict(err) != "stale_version" {
		t.Fatalf("stale version: %v", err)
	}
	// A capability without "pay" cannot confirm.
	h.owner(t, `UPDATE business.order_guest_capability SET allowed_commands = ARRAY['read'] WHERE order_id = $1::uuid`, order)
	if err := try("x"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("read-only capability: %v", err)
	}
	h.owner(t, `UPDATE business.order_guest_capability SET allowed_commands = ARRAY['read', 'pay', 'cancel', 'move_pickup'] WHERE order_id = $1::uuid`, order)

	// Lead time: 192 h before the 12:30 GMT handover on Sat 21 Nov is Fri
	// 13 Nov 12:30 UTC. One microsecond later is refused; equality is
	// allowed (the quote's 24 h window is still open).
	handover := time.Date(2026, 11, 21, 12, 30, 0, 0, time.UTC)
	h.now = handover.Add(-192 * time.Hour).Add(time.Microsecond)
	if err := try("late"); conflict(err) != "lead_time" {
		t.Fatalf("one microsecond inside the lead time: %v", err)
	}
	h.now = handover.Add(-192 * time.Hour)
	if err := try("exact"); err != nil {
		t.Fatalf("exactly 192 h before: %v", err)
	}
	if n := h.count(t, "SELECT count(*) FROM business.order_command WHERE order_id = $1::uuid", order); n != 1 {
		t.Fatalf("%d commands, want 1 (refusals record nothing)", n)
	}
}

func TestFreeConfirmationRefusalsAndQuoteWindow(t *testing.T) {
	h := newCommandHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.freeOrder(t, f, pol)
	// The 24 h window: equality expires (quote sent Thu 5 Nov 10:00).
	h.now = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	if _, err := h.co.FreeConfirm(h.ctx, order, "", d, "k", checkout.FreeConfirmInput{ExpectedVersion: 1}); conflict(err) != "quote_expired" {
		t.Fatalf("at the quote's expiry: %v", err)
	}
	h.now = h.now.Add(-time.Microsecond)
	res, err := h.co.FreeConfirm(h.ctx, order, "", d, "k", checkout.FreeConfirmInput{ExpectedVersion: 1})
	if err != nil {
		t.Fatalf("just before the expiry: %v", err)
	}
	// A hold released between the request and the commit (a later cleanup):
	// the commit records a refusal and changes nothing.
	h.owner(t, "UPDATE business.capacity_allocation SET state = 'released', expires_at = NULL WHERE order_ref = $1::uuid", order)
	job := h.claim(t)
	if err := h.cmds.Handle(h.ctx, job); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	o := h.order(t, order, d)
	if o.Lifecycle != "awaiting_payment" || o.FreeConfirmation == nil || o.FreeConfirmation.State != "refused" ||
		o.FreeConfirmation.Reason == nil || *o.FreeConfirmation.Reason != "holds_released" || o.Version != 1 {
		t.Fatalf("refused confirmation: %+v", o)
	}
	if got := h.holds(t, order, f.readyLot); got != "released/reserved/reserved 1r0s" {
		t.Fatalf("holds after a refusal %s", got)
	}
	if got := h.recoveryState(t, res.ID); got != "secured" {
		t.Fatalf("refusal recovery state %s, want secured", got)
	}

	// A paid order cannot be confirmed free.
	in := checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot, FulfillmentMode: schedule.Collection,
		Lines: []checkout.QuoteLine{{VariantID: f.v1000, Quantity: 1}}}
	h.now = time.Date(2026, 11, 5, 11, 0, 0, 0, time.UTC)
	quote, pd := h.guestQuote(t, "q-paid", in)
	paid, err := h.checkout("", pd, "k-paid", quote, pol)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.co.FreeConfirm(h.ctx, paid.ID, "", pd, "k", checkout.FreeConfirmInput{ExpectedVersion: 1}); conflict(err) != "payment_required" {
		t.Fatalf("paid order: %v", err)
	}
}

func TestOrderCommandsWaitWhileFrozenOrWithoutAJournal(t *testing.T) {
	h := newCommandHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.freeOrder(t, f, pol)
	res, err := h.co.FreeConfirm(h.ctx, order, "", d, "k", checkout.FreeConfirmInput{ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	var de *jobs.DeferError
	// No journal configured: deferred, nothing reserved or committed.
	noJournal := &checkout.Commands{DB: h.worker, Logger: slog.New(slog.NewJSONHandler(h.logs, nil))}
	job := h.claim(t)
	if err := noJournal.Handle(h.ctx, job); !errors.As(err, &de) {
		t.Fatalf("no journal: %v, want a deferral", err)
	}
	// A restore freeze: deferred.
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	h.expire(t, job)
	job = h.claim(t)
	if err := h.cmds.Handle(h.ctx, job); !errors.As(err, &de) {
		t.Fatalf("frozen: %v, want a deferral", err)
	}
	if got := h.recoveryState(t, res.ID); got != "reserved" || len(h.journal.Names()) != 0 {
		t.Fatalf("while waiting: recovery %s, journal %v; want nothing done", got, h.journal.Names())
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	h.expire(t, job)
	job = h.claim(t)
	if err := h.cmds.Handle(h.ctx, job); err != nil {
		t.Fatalf("after the freeze: %v", err)
	}
	if o := h.order(t, order, d); o.Lifecycle != "confirmed" {
		t.Fatalf("order %s, want confirmed", o.Lifecycle)
	}
	// Running it again (the job was not yet completed) changes nothing.
	if err := h.cmds.Handle(h.ctx, job); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if got := h.holds(t, order, f.readyLot); got != "committed/sold/consumed 0r1s" {
		t.Fatalf("holds after a repeat %s", got)
	}
	// A stale lease is fenced.
	h.expire(t, job)
	if err := h.cmds.Handle(h.ctx, job); !errors.Is(err, checkout.ErrLeaseLost) {
		t.Fatalf("stale lease: %v, want ErrLeaseLost", err)
	}
}
