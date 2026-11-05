//go:build integration

package checkout_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/notifications"
	"github.com/jackc/pgx/v5"
)

// Order notices (migration 0030; contracts r6.5 K08, K11, K05).

// fakeEmail is the email provider: it accepts every request, records it
// and never contacts anyone.
type fakeEmail struct {
	mu   sync.Mutex
	sent []sentNotice
}

type sentNotice struct {
	key string
	req notifications.Request
}

func (f *fakeEmail) Send(_ context.Context, key string, body []byte) notifications.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	var req notifications.Request
	if err := json.Unmarshal(body, &req); err != nil {
		return notifications.Result{Kind: notifications.Rejected, Status: 422, Code: "validation_error"}
	}
	for _, s := range f.sent {
		if s.key == key {
			return notifications.Result{Kind: notifications.Accepted, Status: 200, MessageID: "re-replayed"}
		}
	}
	f.sent = append(f.sent, sentNotice{key: key, req: req})
	return notifications.Result{Kind: notifications.Accepted, Status: 200, MessageID: fmt.Sprintf("re-msg-%d", len(f.sent))}
}

func (f *fakeEmail) all() []sentNotice {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentNotice(nil), f.sent...)
}

// withNotices records the notice settings (as the API does before it is
// ready) and chains the email dispatcher after the current handlers (call
// withRefunds first when needed).
func (h *payHarness) withNotices(t *testing.T) *fakeEmail {
	t.Helper()
	if err := notifications.RecordSettings(h.ctx, h.api, "test", uiOrigin, nil); err != nil {
		t.Fatal(err)
	}
	email := &fakeEmail{}
	h.dispatchTo(t, email)
	return email
}

// dispatchTo chains an email dispatcher sending to email after the current
// handlers, with the worker's EMAIL_RECIPIENT_ALLOWLIST allow.
func (h *payHarness) dispatchTo(t *testing.T, email *fakeEmail, allow ...string) {
	t.Helper()
	d, err := notifications.NewDispatcher(notifications.DispatcherConfig{DB: h.worker, Coordinator: h.coord, Sender: email,
		Environment: "test", Allowlist: allow, Logger: slog.New(slog.NewJSONHandler(h.logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	h.handler = jobs.Chain(h.handler, d.Handle)
}

type noticeRow struct {
	Kind, Dedupe, Outcome, Template string
	Detail, Intent, State           *string
	Key, Payload                    *string
}

// notices is the order's order_notice rows with their intents, oldest
// first.
func (h *payHarness) notices(t *testing.T, order string) []noticeRow {
	t.Helper()
	rows, err := h.admin.Query(h.ctx, `SELECT n.kind, n.dedupe, n.outcome, n.template_revision, n.detail, n.intent_id::text,
		i.state, i.idempotency_key::text, i.payload::text
		FROM business.order_notice n LEFT JOIN business.notification_intent i ON i.id = n.intent_id
		WHERE n.order_id = $1::uuid ORDER BY n.created_at, n.kind`, order)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (noticeRow, error) {
		var n noticeRow
		err := r.Scan(&n.Kind, &n.Dedupe, &n.Outcome, &n.Template, &n.Detail, &n.Intent, &n.State, &n.Key, &n.Payload)
		return n, err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func kinds(ns []noticeRow) string {
	var out []string
	for _, n := range ns {
		s := n.Kind + ":" + n.Outcome
		if n.State != nil {
			s += ":" + *n.State
		}
		out = append(out, s)
	}
	return strings.Join(out, ",")
}

// request is the stored provider request of a queued notice, checked as
// the dispatcher checks it (environment, key, intent tag, sender, one
// recipient, canonical bytes).
func (h *payHarness) request(t *testing.T, n noticeRow) notifications.Request {
	t.Helper()
	if n.Intent == nil || n.Payload == nil {
		t.Fatalf("notice %+v has no intent", n)
	}
	d, err := notifications.ParseDispatch([]byte(*n.Payload), "test", *n.Intent, *n.Key)
	if err != nil {
		t.Fatalf("stored %s notice refused by the dispatcher: %v", n.Kind, err)
	}
	var req notifications.Request
	if err := json.Unmarshal(d.Request, &req); err != nil {
		t.Fatal(err)
	}
	return req
}

func has(t *testing.T, what, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("%s lacks %q:\n%s", what, w, body)
		}
	}
}

func hasNot(t *testing.T, what, body string, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(strings.ToLower(body), strings.ToLower(w)) {
			t.Errorf("%s contains %q:\n%s", what, w, body)
		}
	}
}

// secrets is what no notice may carry: the guest capability, payment
// links and references, keys and the customer's name (not needed).
func secrets(d []byte, session string) []string {
	out := []string{"refunded", "Test Customer", "sk_test", stripeSecret, "checkout.stripe", "cs_", "pi_", "re_fake",
		fmt.Sprintf("%x", d), "capability"}
	if session != "" {
		out = append(out, session)
	}
	return out
}

// A paid confirmation is emailed exactly once, in the transaction that
// secures it: nothing while it is confirmation_pending (the outcome envelope
// failed once, K11), one notice after the resumed job secures it, none more
// for duplicate and late events. Its content is the contract's: London
// time, money, the refund deadline, the order page without a capability.
func TestPaidConfirmationNoticeIsQueuedOnceWhenSecured(t *testing.T) {
	h := newPayHarness(t)
	email := h.withNotices(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	h.stripe.pay(session, time.Date(2026, 11, 5, 10, 7, 3, 0, time.UTC))
	h.deliver(t, "evt_completed", "checkout.session.completed", "checkout.session", session, coll, order)
	h.journal.Fail = func(op, name string) error {
		if op == "put" && strings.HasSuffix(name, ".outcome") && !strings.Contains(name, coll) {
			return fmt.Errorf("b2 unavailable")
		}
		return nil
	}
	if errs := h.drain(t); len(errs) != 1 {
		t.Fatalf("with the outcome envelope failing: %v", errs)
	}
	if o := h.paidOrderView(t, order, d); o.Lifecycle != "confirmation_pending" {
		t.Fatalf("order %+v", o)
	}
	if ns := h.notices(t, order); len(ns) != 0 || len(email.all()) != 0 {
		t.Fatalf("notices before the confirmation is secured: %s", kinds(ns))
	}
	h.journal.Fail = nil
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("securing: %s", reasons(errs))
	}
	ns := h.notices(t, order)
	if kinds(ns) != "confirmed:queued:accepted" || ns[0].Template != "order-confirmed.v1" {
		t.Fatalf("notices %s %+v", kinds(ns), ns)
	}
	// Duplicate and late events, and another reconcile: still one.
	h.deliver(t, "evt_completed", "checkout.session.completed", "checkout.session", session, coll, order)
	h.deliver(t, "evt_pi", "payment_intent.succeeded", "payment_intent", "pi_fake_"+session, coll, order)
	h.owner(t, "SELECT business.payment_schedule_reconcile($1::uuid, now())", coll)
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("late events: %s", reasons(errs))
	}
	if ns := h.notices(t, order); len(ns) != 1 {
		t.Fatalf("notices after duplicates: %s", kinds(ns))
	}
	sent := email.all()
	if len(sent) != 1 || sent[0].key != *ns[0].Key {
		t.Fatalf("sent %+v", sent)
	}
	req := h.request(t, ns[0])
	if fmt.Sprint(req) != fmt.Sprint(sent[0].req) {
		t.Fatalf("sent request differs from the stored one")
	}
	if req.From != notifications.From || len(req.ReplyTo) != 1 || req.ReplyTo[0] != notifications.ReplyTo ||
		len(req.To) != 1 || req.To[0] != "test.customer@example.com" || req.Subject != "Your GK Cakes order is confirmed" {
		t.Fatalf("headers %+v", req)
	}
	has(t, "text", req.Text, "Hello,\n\nThank you. Your order is confirmed and we have received your payment of £95.01.",
		"Order reference: "+order+"\nCollection: Saturday 14 November 2026, 12:30 to 13:30 (London time)",
		"Your cakes:\n- Pink piping (Vanilla Raspberry, S · 6 inches): £10.01\n- Pink piping (Vanilla Raspberry, L · 8 inches, ready-made): £85\nTotal paid: £95.01",
		"Collection is from St Katharine Docks, London. The chef will send you the exact address and instructions about two days before.",
		"If you need to cancel: cancel before 12:30 on Monday 9 November 2026 (London time) for a full refund of what you paid. From then on, the refund is 50%.",
		"Your order page: "+uiOrigin+"/orders/"+order+"\nIf you ordered without signing in, open this link on the same device and browser you ordered with.",
		"GK Cakes, London")
	has(t, "html", req.HTML, `<html lang="en-GB">`, `<a href="`+uiOrigin+"/orders/"+order+`"`, "£95.01", "Pink piping (Vanilla Raspberry, S · 6 inches): £10.01<br>")
	for what, body := range map[string]string{"text": req.Text, "html": req.HTML} {
		hasNot(t, what, body, secrets(d, session)...)
	}
	// The audit trail and the logs carry no personal data.
	var detail string
	if err := h.admin.QueryRow(h.ctx, `SELECT detail::text FROM business.audit_event WHERE action = 'notification.queued'
		AND subject_id = $1::uuid`, *ns[0].Intent).Scan(&detail); err != nil || strings.Contains(detail, "example.com") {
		t.Fatalf("audit %s %v", detail, err)
	}
	h.noPersonalDataInLogs(t)
}

// A free confirmation: no payment, no refund sentence; an idempotent
// replay of the request makes no second notice.
func TestFreeConfirmationNotice(t *testing.T) {
	h := newPayHarness(t)
	email := h.withNotices(t)
	f := h.setup(t)
	order, d := h.freeOrder(t, f, h.policy(t, "p1"))
	for range 2 {
		if _, err := h.co.FreeConfirm(h.ctx, order, "", d, "fc1", checkout.FreeConfirmInput{ExpectedVersion: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("confirm: %s", reasons(errs))
	}
	ns := h.notices(t, order)
	if kinds(ns) != "confirmed:queued:accepted" || len(email.all()) != 1 {
		t.Fatalf("notices %s, sent %d", kinds(ns), len(email.all()))
	}
	req := h.request(t, ns[0])
	has(t, "text", req.Text, "Thank you. Your order is confirmed. It is free, so there is nothing to pay.",
		"Cakes: £95.01\nDiscount: −£95.01\nTotal: £0")
	hasNot(t, "text", req.Text, append(secrets(d, ""), "If you need to cancel", "Total paid")...)
}

// A payment after the preparation time promises nothing: the chef will
// contact the customer, and the money is returned if no handover is agreed.
func TestPaymentAfterTheLeadTimeNotice(t *testing.T) {
	h := newPayHarness(t)
	email := h.withNotices(t)
	f := h.setup(t)
	order, d := h.paidOrder(t, f, h.policy(t, "p1"))
	coll, session := h.started(t, order, d)
	h.stripe.pay(session, time.Date(2026, 11, 6, 12, 30, 1, 0, time.UTC))
	h.deliver(t, "evt_completed", "checkout.session.completed", "checkout.session", session, coll, order)
	if errs := h.drain(t); len(attention(errs)) != 1 {
		t.Fatalf("late payment: %s", reasons(errs))
	}
	ns := h.notices(t, order)
	if kinds(ns) != "paid_awaiting_agreement:queued:accepted" || ns[0].Template != "order-paid-late.v1" || len(email.all()) != 1 {
		t.Fatalf("notices %s", kinds(ns))
	}
	req := h.request(t, ns[0])
	if req.Subject != "Payment received for your GK Cakes order" {
		t.Fatalf("subject %q", req.Subject)
	}
	has(t, "text", req.Text, "We have received your payment of £95.01. It arrived after the preparation time needed for your chosen date, so your order is not confirmed yet.",
		"The chef will contact you about whether the date can still work. If you do not agree a handover, the full amount you paid will be returned.",
		"Requested collection: Saturday 14 November 2026, 12:30 to 13:30 (London time)", "Paid: £95.01")
	hasNot(t, "text", req.Text, append(secrets(d, session), "is confirmed", "If you need to cancel")...)
}

// A cancellation is emailed with the refund requested (never "returned"
// or "refunded"); the return is emailed once, only when the refund has
// succeeded (a pending refund first, its webhook later, then duplicates).
func TestCancellationAndReturnNotices(t *testing.T) {
	h := newPayHarness(t)
	h.withRefunds()
	email := h.withNotices(t)
	f := h.setup(t)
	order, d := h.paidOrder(t, f, h.policy(t, "p1"))
	coll := h.payNow(t, order, d, "b1")
	v, _ := h.cancelView(t, order, d)
	h.now = cancelDeadline.Add(time.Minute)
	h.stripe.mu.Lock()
	h.stripe.refunds.newStatus = "pending"
	h.stripe.mu.Unlock()
	if _, err := h.cancel(order, d, "c-ok", v.Version, h.preview(t, order, d)); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("cancel: %s", reasons(errs))
	}
	ns := h.notices(t, order)
	if kinds(ns) != "confirmed:queued:accepted,cancelled:queued:accepted" || ns[1].Template != "order-cancelled.v1" {
		t.Fatalf("notices after the cancellation %s", kinds(ns))
	}
	req := h.request(t, ns[1])
	if req.Subject != "Your GK Cakes order is cancelled" {
		t.Fatalf("subject %q", req.Subject)
	}
	has(t, "text", req.Text, "Your order has been cancelled as you asked.",
		"Refund requested: £47.51, which is 50% of the £95.01 you paid, because the full-refund deadline had been reached.",
		"We have asked for this refund and will email you once it has been returned.",
		"It was planned for: Saturday 14 November 2026, 12:30 to 13:30 (London time)")
	hasNot(t, "text", req.Text, append(secrets(d, ""), "We have returned", "already been returned", "not been returned")...)

	// The refund succeeds; its events arrive twice.
	h.stripe.setRefund("re_fake1", "succeeded", "")
	for range 2 {
		h.refundEvent(t, "evt_re_updated", "refund.updated", "re_fake1")
	}
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("return: %s", reasons(errs))
	}
	if ops := h.operations(t, coll); len(ops) != 1 || ops[0].State != "succeeded" {
		t.Fatalf("operations %+v", ops)
	}
	ns = h.notices(t, order)
	if kinds(ns) != "confirmed:queued:accepted,cancelled:queued:accepted,refund_returned:queued:accepted" ||
		ns[2].Dedupe != "1" || len(email.all()) != 3 {
		t.Fatalf("notices after the return %s, sent %d", kinds(ns), len(email.all()))
	}
	req = h.request(t, ns[2])
	if req.Subject != "Your GK Cakes refund has been returned" {
		t.Fatalf("subject %q", req.Subject)
	}
	has(t, "text", req.Text, "We have returned £47.51 to you for this order. Depending on your bank, it can take a few working days to show on your statement.",
		"Order reference: "+order)
	hasNot(t, "text", req.Text, secrets(d, "")...)
	// Another reconcile of the settled refund: nothing more.
	h.refundEvent(t, "evt_re_again", "refund.updated", "re_fake1")
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 || len(h.notices(t, order)) != 3 || len(email.all()) != 3 {
		t.Fatalf("after another event: %s %s", reasons(errs), kinds(h.notices(t, order)))
	}
}

// A release is emailed only when the customer reached payment: a started
// payment that expired is; a checkout abandoned before paying is not, nor a
// free order nobody asked to confirm; a refused free confirmation is.
func TestReleaseNotices(t *testing.T) {
	t.Run("payment started", func(t *testing.T) {
		h := newPayHarness(t)
		email := h.withNotices(t)
		f := h.setup(t)
		order, d := h.codedOrder(t, f, h.policy(t, "p1"))
		coll, session := h.started(t, order, d)
		h.age(t, coll)
		h.endHold(t, order)
		h.due(t)
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("expiry: %s", reasons(errs))
		}
		if o := h.orderRow(t, order); o.CancelReason != "payment_not_completed" {
			t.Fatalf("order %+v", o)
		}
		ns := h.notices(t, order)
		if kinds(ns) != "released:queued:accepted" || ns[0].Template != "order-released.v1" || len(email.all()) != 1 {
			t.Fatalf("notices %s", kinds(ns))
		}
		req := h.request(t, ns[0])
		if req.Subject != "Your GK Cakes order has been released" {
			t.Fatalf("subject %q", req.Subject)
		}
		has(t, "text", req.Text, "Your order has been released because its payment was not completed. The date and cakes are no longer held for you.",
			"We have no record of a successful payment for this order. If a payment does arrive later, the full amount will be returned.",
			"Requested collection: Saturday 14 November 2026, 12:30 to 13:30 (London time)")
		hasNot(t, "text", req.Text, append(secrets(d, session), "/orders/", "new order", "place")...)
	})
	t.Run("never paid", func(t *testing.T) {
		h := newPayHarness(t)
		h.withNotices(t)
		f := h.setup(t)
		order, _ := h.paidOrder(t, f, h.policy(t, "p1"))
		free, _ := h.freeOrder(t, f, h.policy(t, "p2"))
		h.endHold(t, order)
		h.endHold(t, free)
		h.due(t)
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatal(reasons(errs))
		}
		for _, o := range []string{order, free} {
			if r := h.orderRow(t, o); r.Lifecycle != "cancelled" {
				t.Fatalf("order %+v", r)
			}
			if ns := h.notices(t, o); len(ns) != 0 {
				t.Fatalf("abandoned checkout emailed: %s", kinds(ns))
			}
		}
	})
	t.Run("payment never dispatched to Stripe", func(t *testing.T) {
		h := newPayHarness(t)
		h.withNotices(t)
		f := h.setup(t)
		order, d := h.paidOrder(t, f, h.policy(t, "p1"))
		res, err := h.start(order, d, "pay-1", 1)
		if err != nil {
			t.Fatal(err)
		}
		h.journal.Fail = func(op, name string) error {
			if op == "put" && strings.HasSuffix(name, ".prepared") {
				return fmt.Errorf("b2 unavailable")
			}
			return nil
		}
		if errs := h.drain(t); len(errs) != 1 {
			t.Fatalf("journal down: %s", reasons(errs))
		}
		h.age(t, res.ID)
		h.endHold(t, order)
		id, _ := h.reviewJob(t, order)
		job := h.claimOnly(t, id)
		if err := h.reviews.Handle(h.ctx, job); err != nil {
			t.Fatal(err)
		}
		h.finish(t, job, nil)
		if r := h.orderRow(t, order); r.Lifecycle != "cancelled" || h.collection(t, res.ID).State != "cancelled" {
			t.Fatalf("order %+v", r)
		}
		if ns := h.notices(t, order); len(ns) != 0 {
			t.Fatalf("never-dispatched payment emailed: %s", kinds(ns))
		}
	})
	t.Run("refused free confirmation", func(t *testing.T) {
		h := newPayHarness(t)
		h.withNotices(t)
		f := h.setup(t)
		order, d := h.freeOrder(t, f, h.policy(t, "p1"))
		if _, err := h.co.FreeConfirm(h.ctx, order, "", d, "fc1", checkout.FreeConfirmInput{ExpectedVersion: 1}); err != nil {
			t.Fatal(err)
		}
		h.owner(t, "UPDATE business.orders SET version = version + 1 WHERE id = $1::uuid", order)
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatal(reasons(errs))
		}
		h.endHold(t, order)
		h.due(t)
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatal(reasons(errs))
		}
		ns := h.notices(t, order)
		if kinds(ns) != "released:queued:accepted" {
			t.Fatalf("notices %s", kinds(ns))
		}
		req := h.request(t, ns[0])
		has(t, "text", req.Text, "Your free order has been released because it was not confirmed in time.")
		hasNot(t, "text", req.Text, "We have no record of a successful payment")
	})
}

// Before the API has recorded the notice settings, a transition still
// commits and its notice is recorded as not_configured (no intent); a
// later replay does not create one either.
func TestNoticeWithoutSettings(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	order, d := h.freeOrder(t, f, h.policy(t, "p1"))
	if _, err := h.co.FreeConfirm(h.ctx, order, "", d, "fc1", checkout.FreeConfirmInput{ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatal(reasons(errs))
	}
	if o := h.order(t, order, d); o.Lifecycle != "confirmed" {
		t.Fatalf("order %+v", o)
	}
	if ns := h.notices(t, order); kinds(ns) != "confirmed:not_configured" || ns[0].Intent != nil {
		t.Fatalf("notices %s", kinds(ns))
	}
	if n := h.count(t, "SELECT count(*) FROM business.notification_intent"); n != 0 {
		t.Fatalf("%d intents", n)
	}
	// Audited on the order without personal data; flagged for staff only
	// once settings exist (a deploy window), not while none were ever
	// recorded (tooling and tests).
	if got := h.audits(t, order, "order.notice_not_queued"); len(got) != 1 ||
		got[0] != `{"kind" : "confirmed", "outcome" : "not_configured", "detail" : null}` {
		t.Fatalf("audit %q", got)
	}
	if got := h.flags(t, order); got != "{}" {
		t.Fatalf("flags before any settings %s", got)
	}
	if err := notifications.RecordSettings(h.ctx, h.api, "test", uiOrigin, nil); err != nil {
		t.Fatal(err)
	}
	if got := h.flags(t, order); got != "{notice_not_sent}" {
		t.Fatalf("flags with settings %s", got)
	}
}

// audits is the order's audit details for action, oldest first.
func (h *payHarness) audits(t *testing.T, order, action string) []string {
	t.Helper()
	rows, err := h.admin.Query(h.ctx, `SELECT detail::text FROM business.audit_event WHERE subject_kind = 'order'
		AND subject_id = $1::uuid AND action = $2 ORDER BY occurred_at, id`, order, action)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// flags is the order's staff attention flags.
func (h *payHarness) flags(t *testing.T, order string) string {
	t.Helper()
	var s string
	if err := h.admin.QueryRow(h.ctx, "SELECT business.order_attention_flags(o)::text FROM business.orders o WHERE o.id = $1::uuid",
		order).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// Outside production the recorded allowlist keeps a notice to any other
// recipient from being queued at all (whichever worker runs); an
// allowlisted recipient is queued and sent. Audited, never flagged.
func TestAllowlistKeepsOrderNoticesFromBeingQueued(t *testing.T) {
	h := newPayHarness(t)
	email := h.withNotices(t)
	if err := notifications.RecordSettings(h.ctx, h.api, "test", uiOrigin, []string{"@gkcake.com"}); err != nil {
		t.Fatal(err)
	}
	f := h.setup(t)
	order, d := h.freeOrder(t, f, h.policy(t, "p1"))
	if _, err := h.co.FreeConfirm(h.ctx, order, "", d, "fc1", checkout.FreeConfirmInput{ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatal(reasons(errs))
	}
	ns := h.notices(t, order)
	if kinds(ns) != "confirmed:not_queued" || ns[0].Detail == nil || *ns[0].Detail != "allowlist" || ns[0].Intent != nil ||
		len(email.all()) != 0 || h.count(t, "SELECT count(*) FROM business.notification_intent") != 0 {
		t.Fatalf("notices %s, sent %d", kinds(ns), len(email.all()))
	}
	if got := h.audits(t, order, "order.notice_not_queued"); len(got) != 1 || strings.Contains(got[0], "example.com") ||
		!strings.Contains(got[0], `"allowlist"`) {
		t.Fatalf("audit %q", got)
	}
	if got := h.flags(t, order); got != "{}" {
		t.Fatalf("flags %s", got)
	}
	if err := notifications.RecordSettings(h.ctx, h.api, "test", uiOrigin, []string{"@gkcake.com", "Test.Customer@example.com"}); err != nil {
		t.Fatal(err)
	}
	paid, pd := h.paidOrder(t, f, h.policy(t, "p2"))
	h.payNow(t, paid, pd, "b1")
	if ns := h.notices(t, paid); kinds(ns) != "confirmed:queued:accepted" || len(email.all()) != 1 {
		t.Fatalf("allowlisted: %s", kinds(ns))
	}
	// The settings refuse what the configuration refuses.
	for env, list := range map[string][]string{"production": {"@gkcake.com"}, "staging": nil, "test": {"*@gkcake.com"}} {
		if err := notifications.RecordSettings(h.ctx, h.api, env, "https://shop.example", list); err == nil {
			t.Errorf("%s with %v recorded", env, list)
		}
	}
}

// The worker's own allowlist is the second layer: a notice queued under
// settings without an allowlist is suppressed by the dispatcher, never sent.
func TestDispatcherAllowlistSuppressesAnOrderNotice(t *testing.T) {
	h := newPayHarness(t)
	if err := notifications.RecordSettings(h.ctx, h.api, "test", uiOrigin, nil); err != nil {
		t.Fatal(err)
	}
	email := &fakeEmail{}
	h.dispatchTo(t, email, "@gkcake.com")
	f := h.setup(t)
	order, d := h.freeOrder(t, f, h.policy(t, "p1"))
	if _, err := h.co.FreeConfirm(h.ctx, order, "", d, "fc1", checkout.FreeConfirmInput{ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatal(reasons(errs))
	}
	ns := h.notices(t, order)
	var reason *string
	if err := h.admin.QueryRow(h.ctx, "SELECT suspended_reason FROM business.notification_intent WHERE id = $1::uuid", *ns[0].Intent).
		Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if kinds(ns) != "confirmed:queued:queued" || reason == nil || !strings.Contains(*reason, "EMAIL_RECIPIENT_ALLOWLIST") || len(email.all()) != 0 {
		t.Fatalf("notices %s, reason %v, sent %d", kinds(ns), reason, len(email.all()))
	}
}

// A refund that succeeds before its cancellation is secured (the outcome
// envelope failed once): the cancellation email says it was already
// returned, never "requested", and the return is emailed once.
func TestRefundReturnedBeforeTheCancellationIsSecured(t *testing.T) {
	h := newPayHarness(t)
	h.withRefunds()
	email := h.withNotices(t)
	f := h.setup(t)
	order, d := h.paidOrder(t, f, h.policy(t, "p1"))
	coll := h.payNow(t, order, d, "b1")
	v, _ := h.cancelView(t, order, d)
	res, err := h.cancel(order, d, "c-ok", v.Version, h.preview(t, order, d))
	if err != nil {
		t.Fatal(err)
	}
	h.journal.Fail = func(op, name string) error {
		if op == "put" && strings.HasSuffix(name, ".outcome") && strings.Contains(name, res.ID) {
			return fmt.Errorf("b2 unavailable")
		}
		return nil
	}
	h.drain(t)
	if ops := h.operations(t, coll); len(ops) != 1 || ops[0].State != "succeeded" {
		t.Fatalf("refund before the cancellation is secured: %+v", ops)
	}
	if v, raw := h.cancelView(t, order, d); v.Lifecycle != "cancellation_pending" {
		t.Fatalf("order %s", raw)
	}
	if ns := h.notices(t, order); kinds(ns) != "confirmed:queued:accepted" {
		t.Fatalf("notices before the cancellation is secured %s", kinds(ns))
	}
	h.journal.Fail = nil
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatal(reasons(errs))
	}
	ns := h.notices(t, order)
	if kinds(ns) != "confirmed:queued:accepted,cancelled:queued:accepted,refund_returned:queued:accepted" || len(email.all()) != 3 {
		t.Fatalf("notices %s", kinds(ns))
	}
	req := h.request(t, ns[1])
	has(t, "text", req.Text, "Refund: £95.01, the full amount you paid.", "It has already been returned to you.")
	hasNot(t, "text", req.Text, "Refund requested", "will email you once", "refunded")
}

// A restore replay: the notice rows of a secured confirmation are lost and
// the transition is applied again; the notice is rebuilt with the same
// intent, key and byte-identical payload, and nothing is sent twice.
func TestRestoreReplayRebuildsTheSameNotice(t *testing.T) {
	h := newPayHarness(t)
	email := h.withNotices(t)
	f := h.setup(t)
	order, d := h.freeOrder(t, f, h.policy(t, "p1"))
	res, err := h.co.FreeConfirm(h.ctx, order, "", d, "fc1", checkout.FreeConfirmInput{ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatal(reasons(errs))
	}
	before := h.notices(t, order)
	if len(before) != 1 || before[0].Dedupe != res.ID {
		t.Fatalf("notices %+v", before)
	}
	tx, err := h.admin.Begin(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(h.ctx)
	intent, key := *before[0].Intent, *before[0].Key
	for _, st := range []struct {
		sql string
		arg []any
	}{
		{"SET LOCAL session_replication_role = replica", nil}, // past the immutability triggers
		{"DELETE FROM business.order_notice WHERE order_id = $1::uuid", []any{order}},
		{"DELETE FROM business.notification_attempt WHERE intent_id = $1::uuid", []any{intent}},
		{"DELETE FROM business.audit_event WHERE subject_id = $1::uuid", []any{intent}},
		{"DELETE FROM business.job WHERE id = (SELECT job_id FROM business.notification_intent WHERE id = $1::uuid)", []any{intent}},
		{"DELETE FROM business.notification_intent WHERE id = $1::uuid", []any{intent}},
		{"DELETE FROM business.recovery_operation WHERE id = $1::uuid", []any{key}},
	} {
		if _, err := tx.Exec(h.ctx, st.sql, st.arg...); err != nil {
			t.Fatalf("%s: %v", st.sql, err)
		}
	}
	if err := tx.Commit(h.ctx); err != nil {
		t.Fatal(err)
	}
	// The secured transition is replayed (as order_command_secured enqueues).
	h.owner(t, `SELECT business.order_notice_enqueue(c.order_id, 'confirmed', c.id::text, c.id,
		json_build_object('received_at', c.received_at)) FROM business.order_command c WHERE c.id = $1::uuid`, res.ID)
	after := h.notices(t, order)
	if len(after) != 1 || *after[0].Intent != *before[0].Intent || *after[0].Key != *before[0].Key ||
		*after[0].Payload != *before[0].Payload {
		t.Fatalf("replayed notice differs:\nbefore %+v\nafter  %+v", before[0], after[0])
	}
	// Dispatching it again never sends a second email: the independent
	// journal still holds the original dispatch (restore tooling replays
	// it, GK-034/035), so the worker stops rather than send blindly, and any
	// request would carry the original key the provider deduplicates.
	h.due(t)
	errs := h.drain(t)
	if sent := email.all(); len(sent) != 1 || sent[0].key != key {
		t.Fatalf("sent %d after the replay (%s)", len(sent), reasons(errs))
	}
	for _, err := range errs {
		if !strings.Contains(err.Error(), "existing prepared envelope") {
			t.Fatalf("unexpected dispatch error %v", err)
		}
	}
}

// The rendering of every notice for inputs the flows above do not reach:
// a BST handover, a courier and a GK delivery, chef and late-payment
// cancellations, a free cancellation, a second goal version, a payment
// confirmed after the full-refund deadline. Each payload is accepted by the
// dispatcher's checks.
func TestOrderNoticeRendering(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	order, _ := h.paidOrder(t, f, h.policy(t, "p1"))
	render := func(kind, patch, facts string) (string, string, string) {
		t.Helper()
		var subject, text, html string
		tx, err := h.db.MigratorConn(t).Begin(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(h.ctx)
		if _, err := tx.Exec(h.ctx, "SET LOCAL ROLE gk_owner"); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(h.ctx, `SELECT r.subject, r.body_text, r.body_html FROM business.orders o,
			business.order_notice_render($2, jsonb_populate_record(o, $3::jsonb), $4::json, 'https://gk.example') r
			WHERE o.id = $1::uuid`, order, kind, patch, facts).Scan(&subject, &text, &html); err != nil {
			t.Fatalf("render %s: %v", kind, err)
		}
		if len(text) > notifications.MaxTextBytes || len(html) > notifications.MaxHTMLBytes || strings.ContainsAny(subject, "\r\n") {
			t.Fatalf("%s: sizes %d/%d", kind, len(text), len(html))
		}
		return subject, text, html
	}
	bst := `{"original_handover_at": "2027-06-12T11:30:00Z", "handover_ends_at": "2027-06-12T12:30:00Z",
		"full_refund_deadline_at": "2027-06-07T11:30:00Z", "fulfillment_mode": "customer_courier"}`
	_, text, _ := render("confirmed", bst, `{"received_at": "2027-05-01T10:00:00Z"}`)
	has(t, "BST courier confirmation", text, "Courier collection: Saturday 12 June 2027, 12:30 to 13:30 (London time)",
		"cancel before 12:30 on Monday 7 June 2027 (London time) for a full refund",
		"Please arrange and pay for your own courier to collect at the time above.")
	_, text, _ = render("confirmed", `{"fulfillment_mode": "gk_delivery"}`, `{"received_at": "2026-11-10T09:00:00Z"}`)
	has(t, "late GK delivery confirmation", text, "Handover: Saturday 14 November 2026, 12:30 to 13:30 (London time)",
		"Delivery by GK Cakes is agreed and paid separately. The chef will contact you about it.",
		"If you need to cancel: the refund is now 50% of what you paid.")
	_, text, _ = render("cancelled", `{}`, `{"reason": "chef_cancelled", "rate": 100, "paid_minor": 9501, "refund_target_minor": 9501}`)
	has(t, "chef cancellation", text, "We are sorry: the chef has had to cancel your order.", "Refund requested: £95.01, the full amount you paid.")
	_, text, _ = render("cancelled", `{}`, `{"reason": "late_payment_returned", "rate": 100, "paid_minor": 9501, "refund_target_minor": 9501}`)
	has(t, "late-payment return", text, "a handover could not be agreed after your payment arrived", "Refund requested: £95.01")
	_, text, _ = render("cancelled", `{"payment_requirement": "none"}`, `{"reason": "customer_cancelled", "rate": 100, "paid_minor": 0, "refund_target_minor": 0}`)
	has(t, "free cancellation", text, "There was no payment for this order, so there is nothing to refund.")
	hasNot(t, "free cancellation", text, "We will email you again", "Refund requested")
	_, text, _ = render("cancelled", `{}`, `{"reason": "customer_cancelled", "rate": 50, "paid_minor": 9501, "refund_target_minor": 4751, "refund_returned": true}`)
	has(t, "returned before securing", text, "Refund: £47.51, which is 50% of the £95.01 you paid", "It has already been returned to you.")
	hasNot(t, "returned before securing", text, "requested", "will email")
	_, text, _ = render("refund_returned", `{}`, `{"target_minor": 7000, "goal_version": 2}`)
	has(t, "second goal", text, "We have returned £70 in total to you for this order.")
	for _, kind := range []string{"confirmed", "paid_awaiting_agreement", "released", "cancelled", "refund_returned"} {
		subject, text, html := render(kind, `{}`, `{"received_at": "2026-11-05T10:00:00Z", "reason": "customer_cancelled", "rate": 100,
			"paid_minor": 9501, "refund_target_minor": 9501, "target_minor": 9501, "goal_version": 1}`)
		hasNot(t, kind, subject+text+html, "refunded", "Test Customer", "test.customer@example.com", "<script")
		if !strings.HasPrefix(text, "Hello,\n\n") || !strings.HasSuffix(html, "</html>") {
			t.Errorf("%s: frame %q", kind, text[:20])
		}
	}
}

// A return completed by a Q5 manual payout is emailed once, when its
// outcome is secured (published), not while it is only submitted; a late
// capture after a release is returned in full the same way.
func TestManualPayoutReturnNotice(t *testing.T) {
	h := newPayHarness(t)
	email := h.withNotices(t)
	f := h.setup(t)
	h.staffGrants(t)
	order, d, coll := h.lateCapture(t, f, h.policy(t, "p1"), "q5")
	if ns := h.notices(t, order); kinds(ns) != "released:queued:accepted" {
		t.Fatalf("notices after the release %s", kinds(ns))
	}
	h.failedProviderRefund(t, coll) // chains the refund worker without the dispatcher
	h.dispatchTo(t, email)
	op := h.reserveManual(t, order, d, coll, "m1")
	if err := h.manualOutcome(t, order, d, op, "sub", checkout.ManualPayoutOutcomeInput{Outcome: "submitted"}); err != nil {
		t.Fatal(err)
	}
	if ns := h.notices(t, order); len(ns) != 1 {
		t.Fatalf("notices while submitted %s", kinds(ns))
	}
	success := checkout.ManualPayoutOutcomeInput{Outcome: "succeeded", TransferIdentity: "bank-statement/nov/line-3",
		EvidenceRef: "evidence/case-7", AmountMinor: 10000, Currency: "GBP"}
	// The success commits, but its outcome envelope fails: not published,
	// not emailed.
	h.journal.Fail = func(op, name string) error {
		if op == "put" && strings.HasSuffix(name, ".outcome") {
			return fmt.Errorf("b2 unavailable")
		}
		return nil
	}
	success.ExpectedVersion = h.version(t, order, d)
	if _, err := h.co.RecordManualPayoutOutcome(h.ctx, chef, "ok", order, op, success); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) == 0 {
		t.Fatal("the outcome envelope did not fail")
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State == "returned" {
		t.Fatalf("published before protection %s", raw)
	}
	if ns := h.notices(t, order); len(ns) != 1 {
		t.Fatalf("notices before the outcome is secured %s", kinds(ns))
	}
	h.journal.Fail = nil
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatal(reasons(errs))
	}
	ns := h.notices(t, order)
	if kinds(ns) != "released:queued:accepted,refund_returned:queued:accepted" || len(email.all()) != 2 {
		t.Fatalf("notices %s, sent %d", kinds(ns), len(email.all()))
	}
	req := h.request(t, ns[1])
	has(t, "text", req.Text, "We have returned £100 to you for this order.")
	hasNot(t, "text", req.Text, "bank-statement", "evidence/case-7", "recipient-verified", "refunded")
}
