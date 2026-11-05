//go:build integration

package checkout_test

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
)

// GK-026 B1 part 1: customer and chef cancellation (migration 0021;
// contracts r6.5 K05, K03, K11; BO-GK-001.ready_cancellation).

// The fixture handover is Saturday 2026-11-14 12:30 Europe/London (GMT), so
// full_refund_deadline_at is 2026-11-09 12:30Z.
var cancelDeadline = time.Date(2026, 11, 9, 12, 30, 0, 0, time.UTC)

type previewJSON struct {
	PreviewID    string `json:"preview_id"`
	PreviewHash  string `json:"preview_hash"`
	OrderVersion int    `json:"order_version"`
	Rate         int
	PaidMinor    int64 `json:"paid_minor"`
	AmountMinor  int64 `json:"amount_minor"`
	Currency     string
	Allocations  []struct {
		CollectionID string `json:"collection_id"`
		AmountMinor  int64  `json:"amount_minor"`
	}
	FullRefundDeadlineAt time.Time `json:"full_refund_deadline_at"`
	ComputedAt           time.Time `json:"computed_at"`
	ExpiresAt            time.Time `json:"expires_at"`
}

func (h *payHarness) preview(t *testing.T, order string, d []byte) previewJSON {
	t.Helper()
	raw, err := h.co.PreviewCancellation(h.ctx, order, "", d)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	var p previewJSON
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func (h *payHarness) cancel(order string, d []byte, key string, version int, p previewJSON) (catalog.Result, error) {
	return h.co.Cancel(h.ctx, order, "", d, key, checkout.CancelInput{ExpectedVersion: version, PreviewID: p.PreviewID,
		PreviewHash: p.PreviewHash})
}

type cancelView struct {
	Version      int
	Lifecycle    string
	CancelReason *string    `json:"cancel_reason"`
	CancelledAt  *time.Time `json:"cancelled_at"`
	Cancellation *struct {
		State       string
		Reason      *string
		Rate        *int
		AmountMinor *int64    `json:"amount_minor"`
		RequestedBy string    `json:"requested_by"`
		RequestedAt time.Time `json:"requested_at"`
	}
	Refund *refundView
}

func (h *payHarness) cancelView(t *testing.T, order string, d []byte) (cancelView, string) {
	t.Helper()
	raw, err := h.co.GetOrder(h.ctx, order, "", d)
	if err != nil {
		t.Fatal(err)
	}
	var v cancelView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v, string(raw)
}

// payNow pays a placed order through the fake Stripe and returns its
// succeeded collection.
func (h *payHarness) payNow(t *testing.T, order string, d []byte, key string) string {
	t.Helper()
	coll, session := h.started(t, order, d)
	h.stripe.pay(session, time.Date(2026, 11, 5, 10, 9, 0, 0, time.UTC))
	h.deliver(t, "evt_paid_"+key, "checkout.session.completed", "checkout.session", session, coll, order)
	if errs := h.drain(t); len(errs) != 0 || h.collection(t, coll).State != "succeeded" {
		t.Fatalf("paid: %v %+v", errs, h.collection(t, coll))
	}
	return coll
}

func (h *payHarness) commandJob(t *testing.T, op string) string {
	t.Helper()
	var id string
	if err := h.admin.QueryRow(h.ctx, "SELECT job_id::text FROM business.order_command WHERE id = $1::uuid", op).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (h *payHarness) allocation(t *testing.T, order string) string {
	t.Helper()
	var s string
	if err := h.admin.QueryRow(h.ctx, "SELECT state FROM business.capacity_allocation WHERE order_ref = $1::uuid", order).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// Preview: 100% strictly before the deadline (production does not reduce
// it), 50% at equality with a half penny for the customer; a preview that
// said 100% is refused once the receipt is at the boundary; the 50% cancel
// commits, reserves 4751p and the A3c2 dispatch sends exactly one refund.
func TestCancellationAtTheBoundary(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol) // 1001p cake + 8500p ready cake = 9501p
	coll := h.payNow(t, order, d, "b1")
	v, raw := h.cancelView(t, order, d)
	if v.Lifecycle != "confirmed" {
		t.Fatalf("paid order %s", raw)
	}
	version := v.Version

	early := h.preview(t, order, d)
	if early.Rate != 100 || early.PaidMinor != 9501 || early.AmountMinor != 9501 || early.Currency != "GBP" ||
		len(early.Allocations) != 1 || early.Allocations[0].CollectionID != coll || early.Allocations[0].AmountMinor != 9501 ||
		!early.FullRefundDeadlineAt.Equal(cancelDeadline) || !early.ExpiresAt.Equal(h.now.Add(10*time.Minute)) ||
		early.OrderVersion != version || len(early.PreviewHash) != 64 {
		t.Fatalf("early preview %+v", early)
	}
	// Production never removes the entitlement (K05): still 100% before
	// the boundary.
	h.owner(t, "UPDATE business.orders SET lifecycle = 'in_production' WHERE id = $1::uuid", order)
	h.now = cancelDeadline.Add(-time.Second)
	last := h.preview(t, order, d)
	if last.Rate != 100 || last.AmountMinor != 9501 {
		t.Fatalf("preview one second before the boundary %+v", last)
	}
	// At the boundary: the preview that said 100% is stale; refresh.
	h.now = cancelDeadline
	if _, err := h.cancel(order, d, "c-stale", version, last); conflict(err) != "preview_stale" {
		t.Fatalf("100%% preview at the boundary: %v", err)
	}
	at := h.preview(t, order, d)
	if at.Rate != 50 || at.AmountMinor != 4751 || at.Allocations[0].AmountMinor != 4751 {
		t.Fatalf("preview at the boundary %+v (half-up 9501 x 50%% = 4751)", at)
	}
	// Wrong hash, another preview's hash, expired.
	bad := at
	bad.PreviewHash = strings.Repeat("0", 64)
	if _, err := h.cancel(order, d, "c-bad", version, bad); !strings.HasPrefix(invalidField(err), "preview_id:") {
		t.Fatalf("wrong hash: %v", err)
	}
	h.now = cancelDeadline.Add(10 * time.Minute)
	if _, err := h.cancel(order, d, "c-old", version, at); conflict(err) != "preview_stale" {
		t.Fatalf("expired preview: %v", err)
	}
	h.now = cancelDeadline.Add(time.Minute)
	fresh := h.preview(t, order, d)
	h.withRefunds()
	res, err := h.cancel(order, d, "c-ok", version, fresh)
	if err != nil || res.Replayed {
		t.Fatalf("cancel: %+v %v", res, err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("cancel and refund:\n%s", reasons(errs))
	}
	v, raw = h.cancelView(t, order, d)
	if v.Lifecycle != "cancelled" || v.CancelReason == nil || *v.CancelReason != "customer_cancelled" || v.CancelledAt == nil ||
		!v.CancelledAt.Equal(cancelDeadline.Add(time.Minute)) || v.Cancellation == nil || v.Cancellation.State != "cancelled" ||
		*v.Cancellation.Rate != 50 || *v.Cancellation.AmountMinor != 4751 || v.Refund == nil || v.Refund.TargetMinor != 4751 ||
		v.Refund.State != "returned" || v.Refund.SucceededMinor != 4751 {
		t.Fatalf("cancelled %s", raw)
	}
	creates := h.stripe.refundCreates()
	ops := h.operations(t, coll)
	if len(creates) != 1 || len(ops) != 1 || ops[0].State != "succeeded" || h.stripe.refundForm(creates[0]).Get("amount") != "4751" {
		t.Fatalf("refunds %v %+v", creates, ops)
	}
	if got := h.allocation(t, order); got != "released" {
		t.Fatalf("day place %s, want released", got)
	}
	var rate int
	var basis int64
	if err := h.admin.QueryRow(h.ctx, "SELECT policy_rate, paid_basis_minor FROM business.refund_goal WHERE order_id = $1::uuid",
		order).Scan(&rate, &basis); err != nil || rate != 50 || basis != 9501 {
		t.Fatalf("goal %d %d %v", rate, basis, err)
	}
	h.noPersonalDataInLogs(t)
}

// K11: journal down -> nothing committed; outcome envelope down -> the
// commit is shown as cancellation_pending (no reason, no refund) with its
// refund operation reserved; the next lease secures it once. Then the
// dispatch sends exactly one 10000p refund with the exact request.
func TestCancellationIsProtectedBeforeItIsVisible(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, coll, pi := h.paidCollection(t, f, pol, "k11")
	digest := h.digestOf(t, order)
	v, _ := h.cancelView(t, order, digest)
	version := v.Version
	p := h.preview(t, order, digest)
	if p.Rate != 100 || p.AmountMinor != 10000 {
		t.Fatalf("preview %+v", p)
	}
	h.now = h.now.Add(time.Minute)
	receipt := h.now
	res, err := h.cancel(order, digest, "ck1", version, p)
	if err != nil {
		t.Fatal(err)
	}
	op := res.ID
	if again, err := h.cancel(order, digest, "ck1", version, p); err != nil || !again.Replayed || again.ID != op {
		t.Fatalf("replay %+v %v", again, err)
	}
	if _, err := h.cancel(order, digest, "ck2", version, p); conflict(err) != "state_conflict" {
		t.Fatalf("another key while pending: %v", err)
	}
	if _, err := h.cancel(order, digest, "ck1", version+1, p); conflict(err) != "key_reused" {
		t.Fatalf("same key, other body: %v", err)
	}
	if v, raw := h.cancelView(t, order, digest); v.Lifecycle != "cancellation_pending" || v.Cancellation == nil ||
		v.Cancellation.State != "processing" || !v.Cancellation.RequestedAt.Equal(receipt) || v.Refund != nil {
		t.Fatalf("after the request %s", raw)
	}

	h.journal.Fail = func(string, string) error { return errors.New("b2 unavailable") }
	job := h.claimOnly(t, h.commandJob(t, op))
	if err := h.cmds.Handle(h.ctx, job); err == nil {
		t.Fatal("Handle succeeded without the journal")
	}
	if got := h.recoveryState(t, op); got != "reserved" || h.orderRow(t, order).Lifecycle != "confirmed" ||
		h.count(t, "SELECT count(*) FROM business.refund_goal WHERE order_id = $1::uuid", order) != 0 {
		t.Fatalf("journal down: %s %+v", got, h.orderRow(t, order))
	}

	h.journal.Fail = func(op, name string) error {
		if op == "put" && strings.HasSuffix(name, ".outcome") {
			return errors.New("b2 unavailable")
		}
		return nil
	}
	h.expire(t, job)
	job = h.claimOnly(t, h.commandJob(t, op))
	if err := h.cmds.Handle(h.ctx, job); err == nil {
		t.Fatal("Handle succeeded without the outcome envelope")
	}
	if got := h.recoveryState(t, op); got != "committed" {
		t.Fatalf("outcome envelope down: %s", got)
	}
	if row := h.orderRow(t, order); row.Lifecycle != "cancelled" || row.CancelReason != "customer_cancelled" || row.Version != version+1 {
		t.Fatalf("committed row %+v", row)
	}
	if v, raw := h.cancelView(t, order, digest); v.Lifecycle != "cancellation_pending" || v.CancelReason != nil ||
		v.CancelledAt != nil || v.Refund != nil || v.Cancellation.State != "processing" {
		t.Fatalf("committed but unsecured shown as %s", raw)
	}
	r := h.reservedRefund(t, coll)
	if r.Amount != 10000 || h.allocation(t, order) != "released" {
		t.Fatalf("reserved %+v, day %s", r, h.allocation(t, order))
	}

	h.journal.Fail = nil
	h.expire(t, job)
	job = h.claimOnly(t, h.commandJob(t, op))
	if err := h.cmds.Handle(h.ctx, job); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	h.finish(t, job, nil)
	v, raw := h.cancelView(t, order, digest)
	if v.Lifecycle != "cancelled" || *v.CancelReason != "customer_cancelled" || !v.CancelledAt.Equal(receipt) ||
		v.Refund == nil || v.Refund.State != "requested" || v.Refund.TargetMinor != 10000 || v.Cancellation.State != "cancelled" ||
		*v.Cancellation.Rate != 100 || v.Cancellation.RequestedBy != "customer" {
		t.Fatalf("secured %s", raw)
	}
	key, _ := h.coord.EnvelopeKey(recovery.Operation{ID: op, AggregateID: order, PriorVersion: version})
	if h.journal.Puts(key+".prepared") != 1 || h.journal.Puts(key+".outcome") != 1 {
		t.Fatalf("envelopes %d/%d, want once each", h.journal.Puts(key+".prepared"), h.journal.Puts(key+".outcome"))
	}
	if n := h.count(t, "SELECT count(*) FROM business.audit_event WHERE action = 'order.cancelled' AND subject_id = $1::uuid", order); n != 1 {
		t.Fatalf("%d cancellation audits", n)
	}
	// Replays and later requests on a cancelled order.
	if again, err := h.cancel(order, digest, "ck1", version, p); err != nil || !again.Replayed || again.ID != op {
		t.Fatalf("replay after commit %+v %v", again, err)
	}
	if _, err := h.cancel(order, digest, "ck3", version+1, p); conflict(err) != "cancellation_not_eligible" {
		t.Fatalf("cancelling a cancelled order: %v", err)
	}
	if _, err := h.co.PreviewCancellation(h.ctx, order, "", digest); conflict(err) != "cancellation_not_eligible" {
		t.Fatalf("previewing a cancelled order: %v", err)
	}

	h.withRefunds()
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("dispatch:\n%s", reasons(errs))
	}
	creates := h.stripe.refundCreates()
	if len(creates) != 1 || creates[0] != r.ID {
		t.Fatalf("creates %v", creates)
	}
	want := url.Values{"payment_intent": {pi}, "amount": {"10000"}, "reason": {"requested_by_customer"},
		"metadata[order_id]": {order}, "metadata[collection_id]": {coll}, "metadata[refund_operation_id]": {r.ID},
		"metadata[environment]": {"test"}}
	if form := h.stripe.refundForm(r.ID); form.Encode() != want.Encode() {
		t.Fatalf("request %s", form.Encode())
	}
	if v, raw := h.cancelView(t, order, digest); v.Refund.State != "returned" || v.Refund.SucceededMinor != 10000 {
		t.Fatalf("returned %s", raw)
	}
	if h.count(t, "SELECT business.refund_reserve($1::uuid)", order) != 0 || len(h.stripe.refundCreates()) != 1 {
		t.Fatal("a second refund was reserved or sent")
	}
	h.noPersonalDataInLogs(t)
}

// digestOf gives the order a fresh guest capability with every command and
// returns its digest (paidCollection keeps its own proof private).
func (h *payHarness) digestOf(t *testing.T, order string) []byte {
	t.Helper()
	_, d := proof(t)
	h.owner(t, `INSERT INTO business.order_guest_capability (id, order_id, capability_digest, allowed_commands)
		VALUES (gen_random_uuid(), $1::uuid, $2, ARRAY['read', 'pay', 'cancel', 'move_pickup'])`, order, d)
	return d
}

// The chef cancels after the boundary: 100% of the paid aggregate, the
// chef only, same K11 path and idempotency.
func TestChefCancellationReturnsEverything(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, coll, _ := h.paidCollection(t, f, pol, "chef")
	digest := h.digestOf(t, order)
	v, _ := h.cancelView(t, order, digest)
	h.now = cancelDeadline.Add(time.Hour)
	in := checkout.ChefCancelInput{ExpectedVersion: v.Version, Reason: "Oven failure"}

	supportID := "00000000-0000-4000-8000-000000000501"
	h.owner(t, "SELECT business.staff_grant($1::uuid, 'technical_support', 'test')", supportID)
	if _, err := h.co.ChefCancel(h.ctx, supportID, "x", order, in); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("technical support: %v", err)
	}
	if _, err := h.co.ChefCancel(h.ctx, "00000000-0000-4000-8000-000000000502", "x", order, in); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("not staff: %v", err)
	}
	if _, err := h.co.ChefCancel(h.ctx, chef, "x", order, checkout.ChefCancelInput{ExpectedVersion: v.Version + 1, Reason: "x"}); conflict(err) != "stale_version" {
		t.Fatalf("stale: %v", err)
	}
	h.withRefunds()
	res, err := h.co.ChefCancel(h.ctx, chef, "chef-1", order, in)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := h.co.ChefCancel(h.ctx, chef, "chef-1", order, in); err != nil || !again.Replayed || again.ID != res.ID {
		t.Fatalf("replay %+v %v", again, err)
	}
	if _, err := h.co.ChefCancel(h.ctx, chef, "chef-1", order, checkout.ChefCancelInput{ExpectedVersion: v.Version, Reason: "Other"}); conflict(err) != "key_reused" {
		t.Fatalf("key reused: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("drain:\n%s", reasons(errs))
	}
	v, raw := h.cancelView(t, order, digest)
	if v.Lifecycle != "cancelled" || *v.CancelReason != "chef_cancelled" || *v.Cancellation.Rate != 100 ||
		*v.Cancellation.AmountMinor != 10000 || v.Cancellation.RequestedBy != "chef" || v.Refund.State != "returned" ||
		v.Refund.TargetMinor != 10000 {
		t.Fatalf("chef cancelled %s", raw)
	}
	if ops := h.operations(t, coll); len(ops) != 1 || ops[0].State != "succeeded" || len(h.stripe.refundCreates()) != 1 {
		t.Fatalf("operations %+v", ops)
	}
	var note string
	if err := h.admin.QueryRow(h.ctx, "SELECT note FROM business.order_command WHERE id = $1::uuid", res.ID).Scan(&note); err != nil || note != "Oven failure" {
		t.Fatalf("note %q %v", note, err)
	}
	if strings.Contains(h.logs.String(), "Oven failure") {
		t.Fatal("the chef's note is in the logs")
	}
}

// Access and eligibility: a guest capability without "cancel" sees 404; an
// order awaiting payment is not eligible; a free confirmed order cancels
// with nothing to return, releases its day place and keeps its code use
// consumed (K03).
func TestCancellationAccessEligibilityAndFreeOrders(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	unpaid, ud := h.tenThousandOrder(t, f, pol, "unpaid")
	if _, err := h.co.PreviewCancellation(h.ctx, unpaid, "", ud); conflict(err) != "cancellation_not_eligible" {
		t.Fatalf("awaiting payment preview: %v", err)
	}
	fake := previewJSON{PreviewID: "00000000-0000-4000-8000-000000000001", PreviewHash: strings.Repeat("a", 64)}
	if _, err := h.cancel(unpaid, ud, "u1", 1, fake); conflict(err) != "cancellation_not_eligible" {
		t.Fatalf("awaiting payment cancel: %v", err)
	}

	order, d := h.freeOrder(t, f, pol)
	if _, err := h.co.FreeConfirm(h.ctx, order, "", d, "fc", checkout.FreeConfirmInput{ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if errs := withoutRefundDispatch(h.drain(t)); len(errs) != 0 {
		t.Fatalf("free confirm:\n%s", reasons(errs))
	}
	// No "cancel" on this capability: 404 for preview and cancel.
	_, readOnly := proof(t)
	h.owner(t, `INSERT INTO business.order_guest_capability (id, order_id, capability_digest, allowed_commands)
		VALUES (gen_random_uuid(), $1::uuid, $2, ARRAY['read', 'pay'])`, order, readOnly)
	if _, err := h.co.PreviewCancellation(h.ctx, order, "", readOnly); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("read-only capability preview: %v", err)
	}
	p := h.preview(t, order, d)
	if p.Rate != 100 || p.PaidMinor != 0 || p.AmountMinor != 0 || len(p.Allocations) != 0 {
		t.Fatalf("free preview %+v", p)
	}
	if _, err := h.cancel(order, readOnly, "fx", 2, p); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("read-only capability cancel: %v", err)
	}
	// A preview belongs to the capability that asked for it.
	other := h.digestOf(t, order)
	if _, err := h.cancel(order, other, "fo", 2, p); !strings.HasPrefix(invalidField(err), "preview_id:") {
		t.Fatalf("someone else's preview: %v", err)
	}
	if _, err := h.cancel(order, d, "fc-1", 2, p); err != nil {
		t.Fatal(err)
	}
	if errs := withoutRefundDispatch(h.drain(t)); len(errs) != 0 {
		t.Fatalf("cancel:\n%s", reasons(errs))
	}
	v, raw := h.cancelView(t, order, d)
	if v.Lifecycle != "cancelled" || *v.CancelReason != "customer_cancelled" || v.Refund != nil || *v.Cancellation.AmountMinor != 0 {
		t.Fatalf("free cancelled %s", raw)
	}
	if got := h.holds(t, order, f.readyLot); !strings.HasPrefix(got, "released/sold/consumed") {
		t.Fatalf("holds %s: the day is released, the code use stays consumed", got)
	}
	if h.count(t, "SELECT count(*) FROM business.refund_goal WHERE order_id = $1::uuid", order) != 0 {
		t.Fatal("a zero refund goal was recorded")
	}
}

// A restore freeze defers the cancel command: nothing is committed until it
// lifts.
func TestRestoreFreezeDefersCancellation(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, _, _ := h.paidCollection(t, f, pol, "frz")
	digest := h.digestOf(t, order)
	v, _ := h.cancelView(t, order, digest)
	res, err := h.cancel(order, digest, "f1", v.Version, h.preview(t, order, digest))
	if err != nil {
		t.Fatal(err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	job := h.claimOnly(t, h.commandJob(t, res.ID))
	var de *jobs.DeferError
	if err := h.cmds.Handle(h.ctx, job); !errors.As(err, &de) {
		t.Fatalf("frozen: %v, want a deferral", err)
	}
	if got := h.recoveryState(t, res.ID); got != "reserved" || h.orderRow(t, order).Lifecycle != "confirmed" {
		t.Fatalf("while frozen: %s %+v", got, h.orderRow(t, order))
	}
	h.finish(t, job, de)
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	job = h.claimOnly(t, h.commandJob(t, res.ID))
	if err := h.cmds.Handle(h.ctx, job); err != nil {
		t.Fatalf("after the freeze: %v", err)
	}
	if v, raw := h.cancelView(t, order, digest); v.Lifecycle != "cancelled" {
		t.Fatalf("after the freeze %s", raw)
	}
}
