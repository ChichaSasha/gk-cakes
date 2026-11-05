//go:build integration

package checkout_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
	"github.com/jackc/pgx/v5"
)

// GK-025 A3c1: the 15-minute expire request, never-dispatched cleanup and
// the release of proven-unpaid holds (migration 0015).

// endHold ends the order's 15-minute hold (the passage of time).
func (h *payHarness) endHold(t *testing.T, order string) {
	t.Helper()
	h.owner(t, "UPDATE business.order_hold_review SET hold_until = now() - interval '1 second' WHERE order_id = $1::uuid", order)
}

// age moves the collection's initiation and first admitted dispatch 16
// minutes into the past.
func (h *payHarness) age(t *testing.T, coll string) {
	t.Helper()
	h.owner(t, `UPDATE business.payment_collection SET initiated_at = least(initiated_at, now()) - interval '16 minutes',
		created_at = created_at - interval '16 minutes', dispatch_started_at = dispatch_started_at - interval '16 minutes'
		WHERE id = $1::uuid`, coll)
}

// reviewJob is the order's waiting hold review job ("" when none).
func (h *payHarness) reviewJob(t *testing.T, order string) (string, time.Time) {
	t.Helper()
	var id string
	var at time.Time
	err := h.admin.QueryRow(h.ctx, `SELECT j.id::text, j.next_attempt_at FROM business.order_hold_review_job rj
		JOIN business.job j ON j.id = rj.job_id WHERE rj.order_id = $1::uuid AND j.state IN ('queued', 'retry')`, order).Scan(&id, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}
	}
	if err != nil {
		t.Fatal(err)
	}
	return id, at
}

// claimOnly leases exactly job id (every other waiting job is pushed back
// for a day; due makes them claimable again).
func (h *payHarness) claimOnly(t *testing.T, id string) jobs.Job {
	t.Helper()
	h.owner(t, `UPDATE business.job SET next_attempt_at = CASE WHEN id = $1::uuid THEN now() ELSE now() + interval '1 day' END
		WHERE state IN ('queued', 'retry')`, id)
	job := h.claim(t)
	if job.ID != id {
		t.Fatalf("claimed %s, want %s", job.ID, id)
	}
	return job
}

// finish finalizes a job the test ran itself, as the jobs worker would.
func (h *payHarness) finish(t *testing.T, job jobs.Job, herr error) {
	t.Helper()
	var (
		ae *jobs.AttentionError
		de *jobs.DeferError
		ok bool
	)
	args := []any{job.ID, job.Worker, job.Generation}
	var err error
	switch {
	case herr == nil:
		err = h.worker.QueryRow(h.ctx, "SELECT business.job_complete($1::uuid, $2::uuid, $3)", args...).Scan(&ok)
	case errors.As(herr, &ae):
		err = h.worker.QueryRow(h.ctx, "SELECT business.job_attention($1::uuid, $2::uuid, $3, $4)", append(args, ae.Reason)...).Scan(&ok)
	case errors.As(herr, &de):
		err = h.worker.QueryRow(h.ctx, "SELECT business.job_release($1::uuid, $2::uuid, $3, $4)", append(args, de.After)...).Scan(&ok)
	default:
		var state string
		err = h.worker.QueryRow(h.ctx, "SELECT job_state FROM business.job_fail($1::uuid, $2::uuid, $3, $4)",
			append(args, "test failure")...).Scan(&state)
	}
	if err != nil {
		t.Fatalf("finalize %v: %v", herr, err)
	}
}

type orderRow struct {
	Lifecycle, CancelReason string
	Version                 int
	CancelledAt             *time.Time
}

func (h *payHarness) orderRow(t *testing.T, order string) orderRow {
	t.Helper()
	var o orderRow
	var reason *string
	if err := h.admin.QueryRow(h.ctx, "SELECT lifecycle, cancel_reason, version, cancelled_at FROM business.orders WHERE id = $1::uuid",
		order).Scan(&o.Lifecycle, &reason, &o.Version, &o.CancelledAt); err != nil {
		t.Fatal(err)
	}
	if reason != nil {
		o.CancelReason = *reason
	}
	return o
}

// expireAttempts lists the recorded expire answers of a collection.
func (h *payHarness) expireAttempts(t *testing.T, coll string) []string {
	t.Helper()
	rows, err := h.admin.Query(h.ctx, `SELECT result FROM business.payment_attempt
		WHERE collection_id = $1::uuid AND kind = 'expire' ORDER BY attempt_no`, coll)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (h *payHarness) expireCalls() []string {
	h.stripe.mu.Lock()
	defer h.stripe.mu.Unlock()
	return append([]string(nil), h.stripe.expires...)
}

// codedOrder places a full-payment order with a 10% code: one
// made-to-order cake and one ready cake (holds: the day, the lot, the code).
func (h *payHarness) codedOrder(t *testing.T, f fixture, pol string) (string, []byte) {
	t.Helper()
	h.code(t, "c10", checkout.CodeInput{Code: "TEN", CodeTerms: checkout.CodeTerms{Kind: checkout.KindPercentage, BasisPoints: ptr(1000)}})
	quote, d := h.guestQuote(t, "q-coded", checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot, FulfillmentMode: schedule.Collection,
		DiscountCode: "TEN", Lines: []checkout.QuoteLine{{VariantID: f.v1001, Quantity: 1}, {LotID: f.readyLot, Quantity: 1}}})
	return must[catalog.Result](t)(h.checkout("", d, "k-coded", quote, pol)).ID, d
}

type releasedView struct {
	Version      int
	Lifecycle    string
	CancelReason *string    `json:"cancel_reason"`
	CancelledAt  *time.Time `json:"cancelled_at"`
	Payment      *struct {
		State       string
		CheckoutURL *string `json:"checkout_url"`
	}
	Refund *refundView
}

// refundView is order_get's "refund" (GK-025 A3c2).
type refundView struct {
	State          string
	TargetMinor    int64 `json:"target_minor"`
	SucceededMinor int64 `json:"succeeded_minor"`
	InProgress     int64 `json:"in_progress_minor"`
	Currency       string
}

func (h *payHarness) view(t *testing.T, order string, d []byte) (releasedView, string) {
	t.Helper()
	raw, err := h.co.GetOrder(h.ctx, order, "", d)
	if err != nil {
		t.Fatal(err)
	}
	var v releasedView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v, string(raw)
}

// At 15 minutes the reconcile asks Stripe to expire the open session
// (once), verifies the expired session (cancelled) and the hold review
// releases the day place, the ready cake and the code use, cancelling the
// order. A new payment is then refused.
func TestExpireAtFifteenMinutesReleasesTheHolds(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.codedOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	if got := h.holds(t, order, f.readyLot); got != "tentative/reserved/reserved 1r0s" {
		t.Fatalf("holds %s", got)
	}
	// Before 15 minutes a reconcile (an event brought it forward) does not
	// expire anything.
	h.deliver(t, "evt_early", "checkout.session.completed", "checkout.session", session, coll, order)
	errs := h.drain(t)
	var de *jobs.DeferError
	if len(errs) != 1 || !errors.As(errs[0], &de) || len(h.expireCalls()) != 0 {
		t.Fatalf("early reconcile: %v, expire calls %v", errs, h.expireCalls())
	}
	// 16 minutes later; the hold has ended.
	h.age(t, coll)
	h.endHold(t, order)
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("expiry: %v", errs)
	}
	if calls := h.expireCalls(); len(calls) != 1 || calls[0] != coll+"-expire" {
		t.Fatalf("expire calls %v", calls)
	}
	if got := h.expireAttempts(t, coll); len(got) != 1 || got[0] != "expired" {
		t.Fatalf("expire attempts %v", got)
	}
	if c := h.collection(t, coll); c.State != "cancelled" || h.stripe.session(session).status != "expired" {
		t.Fatalf("collection %+v", c)
	}
	o := h.orderRow(t, order)
	if o.Lifecycle != "cancelled" || o.CancelReason != "payment_not_completed" || o.CancelledAt == nil || o.Version != 2 {
		t.Fatalf("order %+v", o)
	}
	if got := h.holds(t, order, f.readyLot); got != "released/returned/released 0r0s" {
		t.Fatalf("holds after the release %s", got)
	}
	v, raw := h.view(t, order, d)
	if v.Lifecycle != "cancelled" || v.CancelReason == nil || *v.CancelReason != "payment_not_completed" || v.CancelledAt == nil ||
		v.Payment == nil || v.Payment.State != "cancelled" || v.Payment.CheckoutURL != nil || v.Refund != nil {
		t.Fatalf("customer view %s", raw)
	}
	if got := h.count(t, "SELECT count(*) FROM business.audit_event WHERE action = 'order.released_unpaid' AND subject_id = $1::uuid", order); got != 1 {
		t.Fatalf("%d release audit events", got)
	}
	// Another reconcile of the decided collection asks nothing more.
	h.owner(t, "SELECT business.payment_schedule_reconcile($1::uuid, now())", coll)
	if errs := h.drain(t); len(errs) != 0 || len(h.expireCalls()) != 1 {
		t.Fatalf("second reconcile: %v, expire calls %v", errs, h.expireCalls())
	}
	// The released order accepts no new payment.
	if _, err := h.start(order, d, "pay-2", 2); conflict(err) != "state_conflict" {
		t.Fatalf("payment after the release: %v", err)
	}
	// The released places are available again: the code's use too.
	if got := h.count(t, "SELECT count(*) FROM business.code_application WHERE state <> 'released'"); got != 0 {
		t.Fatalf("%d code uses still counted", got)
	}
	h.noPersonalDataInLogs(t)
}

// An expire request without an answer does not stop the reconcile: the
// session is still retrieved and verified, and while it stays pending the
// same key is asked again a minute later (Defer, not a counted failure).
// When Stripe expired it but the answer was lost, the retrieve alone
// decides (cancelled).
func TestUnansweredExpireRequestStillVerifiesAndRetries(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	h.age(t, coll)
	retry := func(status int, want []string) {
		t.Helper()
		h.stripe.mu.Lock()
		h.stripe.expireStatus = status
		h.stripe.mu.Unlock()
		h.owner(t, "UPDATE business.payment_collection SET expire_requested_at = expire_requested_at - interval '1 minute' WHERE id = $1::uuid", coll)
		h.due(t)
		errs := h.drain(t)
		var de *jobs.DeferError
		if len(errs) != 1 || !errors.As(errs[0], &de) || de.After != payments.ExpireRetry || h.collection(t, coll).State != "pending" {
			t.Fatalf("HTTP %d: %v %+v", status, errs, h.collection(t, coll))
		}
		if got := h.expireAttempts(t, coll); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("HTTP %d: attempts %v, want %v", status, got, want)
		}
		// The session was verified (still open) in the same run.
		if got := h.count(t, "SELECT count(*) FROM business.payment_collection WHERE id = $1::uuid AND last_observed_at IS NOT NULL", coll); got != 1 {
			t.Fatalf("HTTP %d: the session was not verified", status)
		}
	}
	retry(500, []string{"unknown"})
	retry(429, []string{"unknown", "deferred"})
	// Once an expire request was made the customer is no longer offered the
	// checkout link, though the payment still reads pending.
	if v, raw := h.view(t, order, d); v.Payment == nil || v.Payment.State != "pending" || v.Payment.CheckoutURL != nil {
		t.Fatalf("view after an expire request %s", raw)
	}
	// Stripe expires it but the answer (and net/http's own replay of a
	// request carrying an Idempotency-Key) is lost: the retrieve decides.
	h.stripe.mu.Lock()
	h.stripe.expireStatus, h.stripe.expireDrop = 0, 100
	h.stripe.mu.Unlock()
	h.owner(t, "UPDATE business.payment_collection SET expire_requested_at = expire_requested_at - interval '1 minute' WHERE id = $1::uuid", coll)
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("lost answer: %v", errs)
	}
	if got := h.expireAttempts(t, coll); strings.Join(got, ",") != "unknown,deferred,unknown" {
		t.Fatalf("attempts %v", got)
	}
	if c := h.collection(t, coll); c.State != "cancelled" || h.stripe.session(session).status != "expired" {
		t.Fatalf("collection %+v", c)
	}
	calls := h.expireCalls()
	if len(calls) < 3 {
		t.Fatalf("expire calls %v", calls)
	}
	for _, c := range calls {
		if c != coll+"-expire" {
			t.Fatalf("expire calls %v, want one key", calls)
		}
	}
	// The hold has not ended yet: nothing released before it.
	if o := h.orderRow(t, order); o.Lifecycle != "awaiting_payment" {
		t.Fatalf("released before the hold ended: %+v", o)
	}
	h.endHold(t, order)
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 || h.orderRow(t, order).Lifecycle != "cancelled" {
		t.Fatalf("after the hold: %v %+v", errs, h.orderRow(t, order))
	}
}

// Stripe refuses the expire request because the session was paid
// meanwhile: the reconcile still verifies it and confirms the order;
// nothing is released.
func TestExpireOfAPaidSessionConfirmsAndReleasesNothing(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	h.stripe.pay(session, time.Date(2026, 11, 5, 10, 14, 0, 0, time.UTC))
	h.age(t, coll)
	h.endHold(t, order)
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("paid at expiry: %v", errs)
	}
	if got := h.expireAttempts(t, coll); len(got) != 1 || got[0] != "not_open" {
		t.Fatalf("expire attempts %v", got)
	}
	if c := h.collection(t, coll); c.State != "succeeded" {
		t.Fatalf("collection %+v", c)
	}
	if o := h.paidOrderView(t, order, d); o.Lifecycle != "confirmed" || o.PaidAt == nil {
		t.Fatalf("order %+v", o)
	}
	if got := h.paidHolds(t, order, f.readyLot); got != "committed/sold 0r1s" {
		t.Fatalf("holds %s", got)
	}
	// The review ends (whenever it runs) without releasing anything.
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatal(errs)
	}
	if got := h.count(t, "SELECT count(*) FROM business.order_hold_review WHERE order_id = $1::uuid AND outcome IN ('closed', 'paid')", order); got != 1 {
		t.Fatal("the hold review did not end")
	}
	if o := h.orderRow(t, order); o.Lifecycle != "confirmed" || o.CancelReason != "" {
		t.Fatalf("order %+v", o)
	}
}

// A payment still processing at 15 minutes: the expire request is made and
// refused (the session is complete, no longer open); the collection stays
// pending and the customer keeps seeing "pending" (still checking; do not
// pay again); no second expire request is made. If the delayed payment then
// fails (the payment intent back to requires_payment_method) the session
// stays complete (Stripe never expires a complete session), so Evaluate
// keeps it pending and the holds stay: no reviewed rule exists yet for
// that case (README "Not yet").
func TestProcessingPaymentAtFifteenMinutesKeepsTheHolds(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	h.stripe.processing(session)
	h.age(t, coll)
	h.endHold(t, order)
	h.due(t)
	errs := h.drain(t)
	var de *jobs.DeferError
	if len(errs) != 1 || !errors.As(errs[0], &de) {
		t.Fatalf("processing: %v", errs)
	}
	if calls := h.expireCalls(); len(calls) != 1 {
		t.Fatalf("expire calls %v", calls)
	}
	if got := h.expireAttempts(t, coll); len(got) != 1 || got[0] != "not_open" {
		t.Fatalf("expire attempts %v", got)
	}
	if c := h.collection(t, coll); c.State != "pending" {
		t.Fatalf("collection %+v", c)
	}
	if got := h.paidHolds(t, order, f.readyLot); got != "tentative/reserved 1r0s" {
		t.Fatalf("holds %s", got)
	}
	v, _ := h.view(t, order, d)
	if v.Lifecycle != "awaiting_payment" || v.Payment == nil || v.Payment.State != "pending" || v.CancelReason != nil {
		t.Fatalf("customer view %+v", v)
	}
	// The review keeps the holds and looks again later.
	if id, at := h.reviewJob(t, order); id == "" || time.Until(at) < 14*time.Minute {
		t.Fatalf("review job %s at %s, want one about 15 minutes away", id, at)
	}
	// Later reconciles: still processing, then the delayed payment fails.
	h.due(t)
	if errs := h.drain(t); len(errs) != 1 || h.orderRow(t, order).Lifecycle != "awaiting_payment" {
		t.Fatalf("still processing: %v %+v", errs, h.orderRow(t, order))
	}
	func() {
		h.stripe.mu.Lock()
		defer h.stripe.mu.Unlock()
		s := h.stripe.sessions[session]
		s.pi["status"] = "requires_payment_method"
		s.pi["latest_charge"].(map[string]any)["status"] = "failed"
	}()
	for range 2 {
		h.due(t)
		errs := h.drain(t)
		if len(errs) != 1 || !errors.As(errs[0], &de) || h.collection(t, coll).State != "pending" {
			t.Fatalf("failed delayed payment: %v %+v", errs, h.collection(t, coll))
		}
	}
	if h.stripe.session(session).status != "complete" {
		t.Fatal("the fake changed the session's status")
	}
	if calls := h.expireCalls(); len(calls) != 1 {
		t.Fatalf("expire calls %v, want exactly one", calls)
	}
	if o := h.orderRow(t, order); o.Lifecycle != "awaiting_payment" || o.CancelReason != "" {
		t.Fatalf("order %+v", o)
	}
	if got := h.paidHolds(t, order, f.readyLot); got != "tentative/reserved 1r0s" {
		t.Fatalf("holds %s", got)
	}
	if id, _ := h.reviewJob(t, order); id == "" {
		t.Fatal("the review stopped looking at a pending collection")
	}
}

// Never-dispatched cleanup wins: the collection is closed and the order
// released; the dispatch that comes later never calls Stripe, whether its
// request was fixed already or not.
func TestNeverDispatchedCleanupWinsAndNothingIsSent(t *testing.T) {
	for _, fixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("fixed=%v", fixed), func(t *testing.T) {
			h := newPayHarness(t)
			f := h.setup(t)
			pol := h.policy(t, "p1")
			order, d := h.paidOrder(t, f, pol)
			res, err := h.start(order, d, "pay-1", 1)
			if err != nil {
				t.Fatal(err)
			}
			if fixed {
				// The request is fixed and reserved, but its prepared
				// envelope keeps failing: nothing was admitted.
				h.journal.Fail = func(op, name string) error {
					if op == "put" && strings.HasSuffix(name, ".prepared") {
						return errors.New("b2 unavailable")
					}
					return nil
				}
				if errs := h.drain(t); len(errs) != 1 || h.recoveryState(t, res.ID) != "reserved" {
					t.Fatalf("journal down: %v", errs)
				}
			} else {
				keyless := *h.pay
				keyless.Provider = nil
				h.handler = jobs.Chain(h.cmds.Handle, keyless.Handle, h.reviews.Handle)
				if errs := h.drain(t); len(errs) != 1 {
					t.Fatalf("without a key: %v", errs)
				}
				h.handler = jobs.Chain(h.cmds.Handle, h.pay.Handle, h.reviews.Handle)
			}
			h.age(t, res.ID)
			h.endHold(t, order)
			id, _ := h.reviewJob(t, order)
			job := h.claimOnly(t, id)
			if err := h.reviews.Handle(h.ctx, job); err != nil {
				t.Fatal(err)
			}
			h.finish(t, job, nil)
			c := h.collection(t, res.ID)
			if c.State != "cancelled" || h.orderRow(t, order).Lifecycle != "cancelled" {
				t.Fatalf("after the cleanup %+v %+v", c, h.orderRow(t, order))
			}
			if got := h.count(t, `SELECT count(*) FROM business.payment_collection WHERE id = $1::uuid
				AND cancel_reason = 'never_dispatched' AND dispatch_started_at IS NULL`, res.ID); got != 1 {
				t.Fatal("not closed as never dispatched")
			}
			// The dispatch resumes: nothing is sent, ever.
			h.journal.Fail = nil
			h.due(t)
			if errs := h.drain(t); len(errs) != 0 {
				t.Fatalf("dispatch after the cleanup: %v", errs)
			}
			if len(h.stripe.creates) != 0 || h.count(t, "SELECT count(*) FROM business.payment_attempt WHERE collection_id = $1::uuid", res.ID) != 0 {
				t.Fatalf("creates %v", h.stripe.creates)
			}
			if c := h.collection(t, res.ID); c.State != "cancelled" {
				t.Fatalf("collection %+v", c)
			}
			if fixed && h.recoveryState(t, res.ID) != "secured" {
				t.Fatalf("dispatch %s, want secured with its never-dispatched outcome", h.recoveryState(t, res.ID))
			}
			if got := h.count(t, `SELECT count(*) FROM business.audit_event WHERE action = 'payment.failed'
				AND (subject_id = $1::uuid OR detail ->> 'collection_id' = $1::text)`, res.ID); got != 0 {
				t.Fatalf("%d payment.failed audits", got)
			}
			// Admission after the cleanup is refused by the database itself.
			h.owner(t, "UPDATE business.job SET state = 'retry', next_attempt_at = now(), lease_owner = NULL, lease_until = NULL WHERE id IN (SELECT job_id FROM business.payment_job WHERE collection_id = $1::uuid AND purpose = 'dispatch')", res.ID)
			job = h.claim(t)
			var status string
			if err := h.worker.QueryRow(h.ctx, "SELECT business.payment_dispatch_admit($1::uuid, $2::uuid, $3, $4::uuid)",
				job.ID, job.Worker, job.Generation, res.ID).Scan(&status); err != nil || status != "closed" {
				t.Fatalf("late admission: %s %v", status, err)
			}
			h.finish(t, job, nil)
		})
	}
}

// Admission wins: the call is underway when the review runs; the cleanup
// leaves the collection alone and the session is created.
func TestNeverDispatchedCleanupLosesToAnAdmittedCall(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	res, err := h.start(order, d, "pay-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	h.stripe.mu.Lock()
	h.stripe.block, h.stripe.arrived = make(chan struct{}), make(chan struct{}, 1)
	block, arrived := h.stripe.block, h.stripe.arrived
	h.stripe.mu.Unlock()
	dispatchJob := h.claim(t)
	done := make(chan error, 1)
	go func() { done <- h.handler(h.ctx, dispatchJob) }()
	select {
	case <-arrived:
	case err := <-done:
		t.Fatalf("dispatch ended before calling Stripe: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("the create request never arrived")
	}
	// The call is in flight (admitted); the collection is 15 minutes old.
	h.owner(t, `UPDATE business.payment_collection SET initiated_at = least(initiated_at, now()) - interval '16 minutes',
		created_at = created_at - interval '16 minutes' WHERE id = $1::uuid`, res.ID)
	h.endHold(t, order)
	id, _ := h.reviewJob(t, order)
	job := h.claimOnly(t, id)
	if err := h.reviews.Handle(h.ctx, job); err != nil {
		t.Fatal(err)
	}
	h.finish(t, job, nil)
	if c := h.collection(t, res.ID); c.State != "created" || h.orderRow(t, order).Lifecycle != "awaiting_payment" {
		t.Fatalf("cleanup touched an admitted collection: %+v %+v", c, h.orderRow(t, order))
	}
	h.stripe.mu.Lock()
	h.stripe.block = nil
	h.stripe.mu.Unlock()
	close(block)
	if err := <-done; err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	h.finish(t, dispatchJob, nil)
	if c := h.collection(t, res.ID); c.State != "pending" || len(h.stripe.creates) != 1 {
		t.Fatalf("collection %+v, creates %v", c, h.stripe.creates)
	}
	if o := h.orderRow(t, order); o.Lifecycle != "awaiting_payment" {
		t.Fatalf("order %+v", o)
	}
}

// An order nobody started paying is abandoned when its hold ends: at
// hold_until exactly (equality ends the hold), not a microsecond before.
func TestAbandonedOrderIsReleasedWhenItsHoldEnds(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	var placed, holdUntil, inserted time.Time
	if err := h.admin.QueryRow(h.ctx, `SELECT o.placed_at, r.hold_until, r.created_at FROM business.orders o
		JOIN business.order_hold_review r ON r.order_id = o.id WHERE o.id = $1::uuid`, order).Scan(&placed, &holdUntil, &inserted); err != nil {
		t.Fatal(err)
	}
	later := placed
	if inserted.After(later) {
		later = inserted
	}
	if !holdUntil.Equal(later.Add(15 * time.Minute)) {
		t.Fatalf("hold until %s; placed %s, inserted %s", holdUntil, placed, inserted)
	}
	if id, at := h.reviewJob(t, order); id == "" || !at.Equal(holdUntil) {
		t.Fatalf("review job %s at %s, want at %s", id, at, holdUntil)
	}
	if ran, _ := h.step(t); ran {
		t.Fatal("a job was due before the hold ended")
	}
	run := func(holdOffset string) string {
		t.Helper()
		id, _ := h.reviewJob(t, order)
		job := h.claimOnly(t, id)
		// One transaction: hold_until relative to the same now() the
		// review compares with.
		tx, err := h.admin.Begin(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(h.ctx)
		if _, err := tx.Exec(h.ctx, "UPDATE business.order_hold_review SET hold_until = now() + $2::interval WHERE order_id = $1::uuid",
			order, holdOffset); err != nil {
			t.Fatal(err)
		}
		var status string
		if err := tx.QueryRow(h.ctx, "SELECT status FROM business.order_hold_review_run($1::uuid, $2::uuid, $3)",
			job.ID, job.Worker, job.Generation).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(h.ctx); err != nil {
			t.Fatal(err)
		}
		h.finish(t, job, nil)
		return status
	}
	if got := run("1 microsecond"); got != "waiting" || h.orderRow(t, order).Lifecycle != "awaiting_payment" {
		t.Fatalf("a microsecond before the hold ends: %s %+v", got, h.orderRow(t, order))
	}
	if id, at := h.reviewJob(t, order); id == "" || time.Until(at) > time.Minute {
		t.Fatalf("next review %s at %s, want at the hold's end", id, at)
	}
	if got := run("0"); got != "released" {
		t.Fatalf("at the hold's end: %s", got)
	}
	o := h.orderRow(t, order)
	if o.Lifecycle != "cancelled" || o.CancelReason != "payment_not_completed" || o.Version != 2 {
		t.Fatalf("order %+v", o)
	}
	if got := h.paidHolds(t, order, f.readyLot); got != "released/returned 0r0s" {
		t.Fatalf("holds %s", got)
	}
	if id, _ := h.reviewJob(t, order); id != "" {
		t.Fatal("a review is still waiting after the release")
	}
	if _, err := h.start(order, d, "pay-1", 2); conflict(err) != "state_conflict" {
		t.Fatalf("payment after the release: %v", err)
	}
	// The returned ready cake can be bought again.
	quote, d2 := h.guestQuote(t, "q-again", checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot, FulfillmentMode: schedule.Collection,
		Lines: []checkout.QuoteLine{{LotID: f.readyLot, Quantity: 2}}})
	if _, err := h.checkout("", d2, "k-again", quote, pol); err != nil {
		t.Fatalf("both ready cakes after the release: %v", err)
	}
}

// A free order nobody confirmed is released (code use included); one whose
// free confirmation is pending is not; a refused one is.
func TestAbandonedFreeOrders(t *testing.T) {
	t.Run("unconfirmed", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		order, _ := h.freeOrder(t, f, h.policy(t, "p1"))
		h.endHold(t, order)
		h.due(t)
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatal(errs)
		}
		if o := h.orderRow(t, order); o.Lifecycle != "cancelled" || o.CancelReason != "free_not_confirmed" {
			t.Fatalf("order %+v", o)
		}
		if got := h.holds(t, order, f.readyLot); got != "released/returned/released 0r0s" {
			t.Fatalf("holds %s", got)
		}
	})
	t.Run("pending confirmation, then refused", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		order, d := h.freeOrder(t, f, h.policy(t, "p1"))
		res, err := h.co.FreeConfirm(h.ctx, order, "", d, "fc1", checkout.FreeConfirmInput{ExpectedVersion: 1})
		if err != nil {
			t.Fatal(err)
		}
		h.endHold(t, order)
		id, _ := h.reviewJob(t, order)
		job := h.claimOnly(t, id)
		if err := h.reviews.Handle(h.ctx, job); err != nil {
			t.Fatal(err)
		}
		h.finish(t, job, nil)
		if o := h.orderRow(t, order); o.Lifecycle != "awaiting_payment" {
			t.Fatalf("released with a pending free confirmation: %+v", o)
		}
		if got := h.holds(t, order, f.readyLot); got != "tentative/reserved/reserved 1r0s" {
			t.Fatalf("holds %s", got)
		}
		// The confirmation is refused (the order changed meanwhile).
		h.owner(t, "UPDATE business.orders SET version = version + 1 WHERE id = $1::uuid", order)
		h.due(t)
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatal(errs)
		}
		if got := h.recoveryState(t, res.ID); got != "secured" {
			t.Fatalf("command %s", got)
		}
		// The next review (15 minutes later) releases it.
		h.due(t)
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatal(errs)
		}
		if o := h.orderRow(t, order); o.Lifecycle != "cancelled" || o.CancelReason != "free_not_confirmed" {
			t.Fatalf("order %+v", o)
		}
		if fc := h.order(t, order, d).FreeConfirmation; fc == nil || fc.State != "refused" {
			t.Fatalf("free confirmation %+v", fc)
		}
	})
	t.Run("confirmed", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		order, d := h.freeOrder(t, f, h.policy(t, "p1"))
		if _, err := h.co.FreeConfirm(h.ctx, order, "", d, "fc1", checkout.FreeConfirmInput{ExpectedVersion: 1}); err != nil {
			t.Fatal(err)
		}
		h.endHold(t, order)
		h.due(t)
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatal(errs)
		}
		h.due(t)
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatal(errs)
		}
		if o := h.order(t, order, d); o.Lifecycle != "confirmed" {
			t.Fatalf("order %+v", o)
		}
		if got := h.holds(t, order, f.readyLot); got != "committed/sold/consumed 0r1s" {
			t.Fatalf("holds %s", got)
		}
	})
}

// An unknown collection (money may exist) is never released; neither is a
// succeeded one whose paid confirmation was refused.
func TestUnknownAndSucceededCollectionsAreNeverReleased(t *testing.T) {
	t.Run("unknown", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		order, d := h.paidOrder(t, f, pol)
		coll, session := h.started(t, order, d)
		h.stripe.pay(session, time.Date(2026, 11, 5, 10, 9, 0, 0, time.UTC))
		h.stripe.session(session).amount = 100
		h.endHold(t, order)
		h.deliver(t, "evt_completed", "checkout.session.completed", "checkout.session", session, coll, order)
		errs := h.drain(t)
		if got := attention(errs); len(got) != 1 || h.collection(t, coll).State != "unknown" {
			t.Fatalf("mismatch: %v", errs)
		}
		for range 3 {
			h.due(t)
			if errs := h.drain(t); len(errs) != 0 {
				t.Fatal(errs)
			}
		}
		if o := h.orderRow(t, order); o.Lifecycle != "awaiting_payment" {
			t.Fatalf("order %+v", o)
		}
		if got := h.paidHolds(t, order, f.readyLot); got != "tentative/reserved 1r0s" {
			t.Fatalf("holds %s", got)
		}
		if id, _ := h.reviewJob(t, order); id == "" {
			t.Fatal("the review stopped looking at an unknown collection")
		}
	})
	t.Run("succeeded", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		order, d := h.paidOrder(t, f, pol)
		coll, session := h.started(t, order, d)
		h.stripe.pay(session, time.Date(2026, 11, 5, 10, 9, 0, 0, time.UTC))
		h.deliver(t, "evt_completed", "checkout.session.completed", "checkout.session", session, coll, order)
		if ran, err := h.step(t); !ran || err != nil {
			t.Fatalf("reconcile: %v %v", ran, err)
		}
		h.owner(t, "UPDATE business.capacity_allocation SET state = 'released', expires_at = NULL WHERE order_ref = $1::uuid", order)
		if got := attention(h.drain(t)); len(got) != 1 {
			t.Fatalf("refused paid confirmation: %v", got)
		}
		h.endHold(t, order)
		h.due(t)
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatal(errs)
		}
		if o := h.orderRow(t, order); o.Lifecycle != "awaiting_payment" || o.CancelReason != "" {
			t.Fatalf("order %+v", o)
		}
		// Not terminal: the refusal goes to a manager; nothing is refunded
		// automatically (no goal, no operation).
		if h.count(t, "SELECT count(*) FROM business.refund_goal WHERE order_id = $1::uuid", order) != 0 ||
			h.count(t, "SELECT count(*) FROM business.refund_operation WHERE order_id = $1::uuid", order) != 0 {
			t.Fatal("a full return was recorded for an order that is not cancelled")
		}
		if v, raw := h.view(t, order, d); v.Refund != nil {
			t.Fatalf("view %s", raw)
		}
		if got := h.count(t, "SELECT count(*) FROM business.order_hold_review WHERE order_id = $1::uuid AND outcome = 'paid'", order); got != 1 {
			t.Fatal("the review did not end as paid")
		}
	})
}

// A success observed after the release never reopens the order: the
// collection is succeeded with its evidence, the paid confirmation is
// refused (attention: full return), the order stays cancelled.
func TestLateSuccessAfterTheReleaseKeepsTheOrderCancelled(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	h.age(t, coll)
	h.endHold(t, order)
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 || h.orderRow(t, order).Lifecycle != "cancelled" {
		t.Fatalf("release: %v %+v", errs, h.orderRow(t, order))
	}
	at := time.Date(2026, 11, 5, 10, 20, 0, 0, time.UTC)
	ev := `{"session_id":"` + session + `","payment_status":"paid","charge_status":"succeeded"}`
	if got := h.recordDirect(t, coll, session, "paid", ev, &at, nil); got != "succeeded" {
		t.Fatalf("late success recorded as %s", got)
	}
	if v, raw := h.view(t, order, d); v.Lifecycle != "cancelled" || v.Payment == nil || v.Payment.State != "succeeded" {
		t.Fatalf("view while the confirmation is pending %s", raw)
	}
	errs := h.drain(t)
	if got := attention(errs); len(got) != 1 || !strings.Contains(got[0], "full return") {
		t.Fatalf("late success: %v", errs)
	}
	o := h.orderRow(t, order)
	if o.Lifecycle != "cancelled" || o.CancelReason != "payment_not_completed" {
		t.Fatalf("order %+v", o)
	}
	var evidence string
	if err := h.admin.QueryRow(h.ctx, "SELECT evidence::text FROM business.payment_collection WHERE id = $1::uuid AND state = 'succeeded'", coll).Scan(&evidence); err != nil ||
		!strings.Contains(evidence, `"charge_status":"succeeded"`) {
		t.Fatalf("evidence %s %v", evidence, err)
	}
	// The full return is requested (GK-025 A3c2): nothing sent yet.
	if v, raw := h.view(t, order, d); v.Lifecycle != "cancelled" || v.Refund == nil || v.Refund.State != "requested" ||
		v.Refund.TargetMinor <= 0 || v.Refund.SucceededMinor != 0 || v.Refund.InProgress != v.Refund.TargetMinor {
		t.Fatalf("view %s", raw)
	}
	if got := h.count(t, "SELECT count(*) FROM business.audit_event WHERE action = 'order.paid_confirmation_refused' AND subject_id = $1::uuid", order); got != 1 {
		t.Fatalf("%d refusals", got)
	}
}

// The hold review and a paid record race on one order: exactly one outcome
// (confirmed, or released and then the paid confirmation refused), never a
// deadlock.
func TestHoldReviewRacesAPaidRecord(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	outcomes := map[string]int{}
	for round := range 4 {
		quote, d := h.guestQuote(t, fmt.Sprintf("q-race-%d", round), checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot,
			FulfillmentMode: schedule.Collection, Lines: []checkout.QuoteLine{{VariantID: f.v1000, Quantity: 1}}})
		order := must[catalog.Result](t)(h.checkout("", d, fmt.Sprintf("k-race-%d", round), quote, pol)).ID
		res, err := h.start(order, d, "pay", 1)
		if err != nil {
			t.Fatal(err)
		}
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("dispatch: %v", errs)
		}
		coll := res.ID
		session := h.collection(t, coll).SessionID
		// The session expires unpaid (cancelled); a payment for it is then
		// observed while the review releases the order.
		h.stripe.session(session).status = "expired"
		h.deliver(t, fmt.Sprintf("evt_exp_%d", round), "checkout.session.expired", "checkout.session", session, coll, order)
		if errs := h.drain(t); len(errs) != 0 || h.collection(t, coll).State != "cancelled" {
			t.Fatalf("expired: %v", errs)
		}
		h.endHold(t, order)
		reviewID, _ := h.reviewJob(t, order)
		review := h.claimOnly(t, reviewID)
		h.owner(t, "SELECT business.payment_schedule_reconcile($1::uuid, now() + interval '1 day')", coll)
		var recID string
		if err := h.admin.QueryRow(h.ctx, `SELECT j.id::text FROM business.payment_job pj JOIN business.job j ON j.id = pj.job_id
			WHERE pj.collection_id = $1::uuid AND pj.purpose = 'reconcile' AND j.state IN ('queued', 'retry')`, coll).Scan(&recID); err != nil {
			t.Fatal(err)
		}
		rec := h.claimOnly(t, recID)
		at := time.Date(2026, 11, 5, 10, 20, 0, 0, time.UTC)
		var wg sync.WaitGroup
		var reviewErr, recordErr error
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			reviewErr = h.reviews.Handle(h.ctx, review)
		}()
		go func() {
			defer wg.Done()
			<-start
			var state string
			var cmd *string
			recordErr = h.worker.QueryRow(context.Background(), `SELECT collection_state, command_id::text FROM business.payment_reconcile_record(
				$1::uuid, $2::uuid, $3, $4::uuid, 'paid', $5, 'complete', 'paid', NULL, $6::json, $7, NULL)`,
				rec.ID, rec.Worker, rec.Generation, coll, session, `{"session_id":"`+session+`"}`, at).Scan(&state, &cmd)
		}()
		close(start)
		wg.Wait()
		if reviewErr != nil || recordErr != nil {
			t.Fatalf("round %d: review %v, record %v (deadlock: %v)", round, reviewErr, recordErr,
				dbtest.SQLState(reviewErr) == "40P01" || dbtest.SQLState(recordErr) == "40P01")
		}
		h.finish(t, review, nil)
		h.finish(t, rec, nil)
		h.due(t)
		errs := withoutRefundDispatch(h.drain(t))
		o := h.orderRow(t, order)
		switch o.Lifecycle {
		case "confirmed":
			if len(errs) != 0 || h.collection(t, coll).State != "succeeded" {
				t.Fatalf("round %d confirmed: %v", round, errs)
			}
		case "cancelled":
			if got := attention(errs); len(got) != 1 || !strings.Contains(got[0], "full return") ||
				h.collection(t, coll).State != "succeeded" {
				t.Fatalf("round %d released: %v", round, errs)
			}
		default:
			t.Fatalf("round %d: order %+v, errors %v", round, o, errs)
		}
		outcomes[o.Lifecycle]++
		if got := h.count(t, `SELECT count(*) FROM business.audit_event WHERE subject_id = $1::uuid
			AND action IN ('order.paid_confirmed', 'order.released_unpaid')`, order); got != 1 {
			t.Fatalf("round %d: %d outcomes", round, got)
		}
	}
	t.Logf("outcomes %v", outcomes)
}

// A restore freeze defers the hold review and the expire request: nothing
// is released, expired or recorded until it lifts.
func TestRestoreFreezeDefersExpiryAndRelease(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, _ := h.started(t, order, d)
	h.age(t, coll)
	h.endHold(t, order)
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	h.due(t)
	errs := h.drain(t)
	var de *jobs.DeferError
	for _, err := range errs {
		if !errors.As(err, &de) {
			t.Fatalf("frozen: %v", errs)
		}
	}
	if len(errs) != 2 || len(h.expireCalls()) != 0 || h.collection(t, coll).State != "pending" ||
		h.orderRow(t, order).Lifecycle != "awaiting_payment" {
		t.Fatalf("frozen: %v, expire calls %v", errs, h.expireCalls())
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("after the freeze: %v", errs)
	}
	if o := h.orderRow(t, order); o.Lifecycle != "cancelled" || len(h.expireCalls()) != 1 {
		t.Fatalf("order %+v, expire calls %v", o, h.expireCalls())
	}
}

// The worker's expire and review gateways are fenced by the job lease.
func TestExpiryGatewaysAreLeaseFenced(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, _ := h.started(t, order, d)
	id, _ := h.reviewJob(t, order)
	stranger := "00000000-0000-4000-8000-999999999999"
	for _, sql := range []string{
		"SELECT status FROM business.order_hold_review_run($1::uuid, $2::uuid, 1)",
	} {
		if _, err := h.worker.Exec(h.ctx, sql, id, stranger); dbtest.SQLState(err) != "GK001" {
			t.Errorf("%s: %v, want GK001", sql, err)
		}
	}
	var recID string
	h.owner(t, "SELECT business.payment_schedule_reconcile($1::uuid, now())", coll)
	if err := h.admin.QueryRow(h.ctx, `SELECT j.id::text FROM business.payment_job pj JOIN business.job j ON j.id = pj.job_id
		WHERE pj.collection_id = $1::uuid AND pj.purpose = 'reconcile' AND j.state IN ('queued', 'retry')`, coll).Scan(&recID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.worker.Exec(h.ctx, "SELECT business.payment_expire_admit($1::uuid, $2::uuid, 1, $3::uuid)", recID, stranger, coll); dbtest.SQLState(err) != "GK001" {
		t.Errorf("expire admit without the lease: %v", err)
	}
	// A review job is not a reconcile: the expire gateway refuses it.
	review := h.claimOnly(t, id)
	if _, err := h.worker.Exec(h.ctx, "SELECT business.payment_expire_admit($1::uuid, $2::uuid, $3, $4::uuid)",
		review.ID, review.Worker, review.Generation, coll); err == nil {
		t.Error("expire admit accepted a review job")
	}
	// The API login cannot run them.
	conn := h.db.LoginConn(t, "gk_api")
	for _, sql := range []string{"SELECT * FROM business.order_hold_review_run(gen_random_uuid(), gen_random_uuid(), 1)",
		"SELECT count(*) FROM business.order_hold_review"} {
		if _, err := conn.Exec(h.ctx, sql); dbtest.SQLState(err) != "42501" {
			t.Errorf("gk_api: %s: %v, want 42501", sql, err)
		}
	}
}

// A collection Stripe rejected (failed, nothing created) brings the review
// forward; the order is released once its hold has ended, not before.
func TestRejectedCollectionIsReleasedAfterTheHold(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	res, err := h.start(order, d, "pay-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	h.stripe.status, h.stripe.errType = 400, "invalid_request_error"
	if got := attention(h.drain(t)); len(got) != 1 || !strings.Contains(got[0], "rejected") {
		t.Fatalf("rejected dispatch: %v", got)
	}
	if c := h.collection(t, res.ID); c.State != "failed" {
		t.Fatalf("collection %+v", c)
	}
	// One payment.failed audit, on the order (the collection in its data)
	// like every other payment audit.
	if got := h.count(t, `SELECT count(*) FROM business.audit_event WHERE action = 'payment.failed'
		AND subject_kind = 'order' AND subject_id = $1::uuid AND detail ->> 'collection_id' = $2::text
		AND detail ->> 'reason' = 'rejected_by_stripe'`, order, res.ID); got != 1 {
		t.Fatalf("%d payment.failed audits on the order", got)
	}
	if got := h.count(t, "SELECT count(*) FROM business.audit_event WHERE action = 'payment.failed'"); got != 1 {
		t.Fatalf("%d payment.failed audits", got)
	}
	// Before the hold ends the review keeps the holds.
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 || h.orderRow(t, order).Lifecycle != "awaiting_payment" {
		t.Fatalf("before the hold ended: %v %+v", errs, h.orderRow(t, order))
	}
	h.endHold(t, order)
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatal(errs)
	}
	if o := h.orderRow(t, order); o.Lifecycle != "cancelled" || o.CancelReason != "payment_not_completed" {
		t.Fatalf("order %+v", o)
	}
	if got := h.paidHolds(t, order, f.readyLot); got != "released/returned 0r0s" {
		t.Fatalf("holds %s", got)
	}
	if len(h.stripe.sessions) != 0 {
		t.Fatalf("sessions %d", len(h.stripe.sessions))
	}
}

// Concurrent schedulers (collections failing at once, the review itself)
// leave at most one waiting review job, also while the current one runs.
func TestConcurrentReviewSchedulersLeaveOneWaitingJob(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, _ := h.paidOrder(t, f, pol)
	waiting := func() int {
		return h.count(t, `SELECT count(*) FROM business.order_hold_review_job rj JOIN business.job j ON j.id = rj.job_id
			WHERE rj.order_id = $1::uuid AND j.state IN ('queued', 'retry')`, order)
	}
	for round := range 3 {
		// The waiting review is leased (running): every scheduler must
		// enqueue anew, and only one may.
		id, _ := h.reviewJob(t, order)
		job := h.claimOnly(t, id)
		conns := make([]*pgx.Conn, 6)
		for i := range conns {
			conns[i] = h.db.MigratorConn(t)
		}
		errs := make([]error, len(conns))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i, conn := range conns {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs[i] = func() error {
					tx, err := conn.Begin(context.Background())
					if err != nil {
						return err
					}
					defer tx.Rollback(context.Background())
					if _, err := tx.Exec(context.Background(), "SET LOCAL ROLE gk_owner"); err != nil {
						return err
					}
					if _, err := tx.Exec(context.Background(), "SELECT business.order_hold_review_schedule($1::uuid, now())", order); err != nil {
						return err
					}
					return tx.Commit(context.Background())
				}()
			}()
		}
		close(start)
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
		if got := waiting(); got != 1 {
			t.Fatalf("round %d: %d waiting review jobs, want 1", round, got)
		}
		h.finish(t, job, nil)
	}
	if got := h.count(t, "SELECT count(*) FROM business.order_hold_review_job WHERE order_id = $1::uuid", order); got != 4 {
		t.Fatalf("%d review jobs in all, want 4 (one per round plus the first)", got)
	}
}

// Orders already awaiting payment when migration 0015 is applied get one
// hold review each, due no earlier than 15 minutes after the migration.
func TestMigrationBackfillsHoldReviews(t *testing.T) {
	h := &payHarness{commandHarness: &commandHarness{harness: newHarnessAt(t, "0014_payments")}}
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, _ := h.paidOrder(t, f, pol)
	before := time.Now()
	if _, err := migrate.Run(h.ctx, h.db.MigratorConn(t), migrations.FS); err != nil {
		t.Fatal(err)
	}
	after := time.Now()
	var placed, holdUntil, due time.Time
	var jobsN int
	if err := h.admin.QueryRow(h.ctx, `SELECT o.placed_at, r.hold_until,
		(SELECT count(*) FROM business.order_hold_review_job rj WHERE rj.order_id = o.id),
		(SELECT j.next_attempt_at FROM business.order_hold_review_job rj JOIN business.job j ON j.id = rj.job_id
		 WHERE rj.order_id = o.id AND j.state = 'queued')
		FROM business.orders o JOIN business.order_hold_review r ON r.order_id = o.id WHERE o.id = $1::uuid`, order).Scan(
		&placed, &holdUntil, &jobsN, &due); err != nil {
		t.Fatal(err)
	}
	if got := h.count(t, "SELECT count(*) FROM business.order_hold_review"); got != 1 || jobsN != 1 || !due.Equal(holdUntil) {
		t.Fatalf("%d reviews, %d jobs, due %s, hold until %s", got, jobsN, due, holdUntil)
	}
	// The later of placed_at (a test clock) and the migration instant.
	lo, hi := before.Add(15*time.Minute-2*time.Second), after.Add(15*time.Minute+2*time.Second)
	if placed.After(after) {
		lo, hi = placed.Add(15*time.Minute), placed.Add(15*time.Minute)
	}
	if holdUntil.Before(lo) || holdUntil.After(hi) {
		t.Fatalf("hold until %s, want between %s and %s (placed %s)", holdUntil, lo, hi, placed)
	}
}

// Once Stripe has expired the session by itself (its expires_at passed),
// no expire request is asked: the retrieve alone decides (cancelled).
func TestNoExpireRequestAfterTheSessionExpiredByItself(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	h.age(t, coll)
	h.owner(t, "UPDATE business.payment_collection SET provider_expires_at = now() - interval '1 second' WHERE id = $1::uuid", coll)
	h.stripe.session(session).status = "expired"
	h.owner(t, "SELECT business.payment_schedule_reconcile($1::uuid, now())", coll)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("reconcile: %v", errs)
	}
	if calls := h.expireCalls(); len(calls) != 0 {
		t.Fatalf("expire calls %v", calls)
	}
	if got := h.expireAttempts(t, coll); len(got) != 0 {
		t.Fatalf("expire attempts %v", got)
	}
	if got := h.count(t, "SELECT count(*) FROM business.payment_collection WHERE id = $1::uuid AND expire_requested_at IS NOT NULL", coll); got != 0 {
		t.Fatal("the expire request was admitted")
	}
	if c := h.collection(t, coll); c.State != "cancelled" {
		t.Fatalf("collection %+v", c)
	}
}
