//go:build integration

package checkout_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
)

// Order view refinements (migration 0038): the caller's actions, labelled
// allocations, refund parts, account summaries that ask for an answer, and
// the no-show notice after a stray refund reserved earlier. The API clock
// is the harness's (h.now); no test reads the real date.

type refundPart struct {
	Kind        string
	TargetMinor int64 `json:"target_minor"`
	Succeeded   int64 `json:"succeeded_minor"`
	InProgress  int64 `json:"in_progress_minor"`
	PendingChef int64 `json:"pending_chef_minor"`
}

type refinedView struct {
	Version        int
	Lifecycle      string
	Actions        []string
	DeliveryChoice *struct {
		Choices        []string
		AllowedChoices []string `json:"allowed_choices"`
	} `json:"delivery_choice"`
	Refund *struct {
		refundView
		Parts []refundPart
	}
}

func (h *payHarness) refined(t *testing.T, order, subject string, d []byte) (refinedView, string) {
	t.Helper()
	raw, err := h.co.GetOrder(h.ctx, order, subject, d)
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	var v refinedView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.Actions == nil {
		t.Fatalf("no actions: %s", raw)
	}
	if v.Refund != nil {
		partsAddUp(t, v, string(raw))
	}
	return v, string(raw)
}

// partsAddUp is the documented invariant: the parts add up to the block's
// target, succeeded, in progress and delivery pending amounts exactly, in
// the order cake, delivery, stray, without an all-zero part.
func partsAddUp(t *testing.T, v refinedView, raw string) {
	t.Helper()
	r := v.Refund
	if r.Parts == nil {
		t.Fatalf("refund without parts: %s", raw)
	}
	var target, succeeded, progress, pending int64
	last := -1
	for _, p := range r.Parts {
		i := slices.Index([]string{"cake", "delivery", "stray"}, p.Kind)
		if i <= last {
			t.Fatalf("part order: %s", raw)
		}
		last = i
		if p.TargetMinor == 0 && p.Succeeded == 0 && p.InProgress == 0 && p.PendingChef == 0 {
			t.Fatalf("an all-zero part: %s", raw)
		}
		if p.Kind != "delivery" && p.PendingChef != 0 {
			t.Fatalf("only a delivery waits for the chef: %s", raw)
		}
		target += p.TargetMinor
		succeeded += p.Succeeded
		progress += p.InProgress
		pending += p.PendingChef
	}
	if target != r.TargetMinor || succeeded != r.SucceededMinor || progress != r.InProgress || pending != r.DeliveryPending {
		t.Fatalf("parts %+v do not add up to the block: %s", r.Parts, raw)
	}
}

func partsOf(v refinedView) string {
	var out []string
	for _, p := range v.Refund.Parts {
		out = append(out, p.Kind+" "+itoa(p.TargetMinor)+"/"+itoa(p.Succeeded)+"/"+itoa(p.InProgress)+"/"+itoa(p.PendingChef))
	}
	return strings.Join(out, ", ")
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// capability gives the order a guest capability with exactly these grants.
func (h *payHarness) capability(t *testing.T, order string, grants ...string) []byte {
	t.Helper()
	_, d := proof(t)
	h.owner(t, `INSERT INTO business.order_guest_capability (id, order_id, capability_digest, allowed_commands)
		VALUES (gen_random_uuid(), $1::uuid, $2, $3::text[])`, order, d, grants)
	return d
}

func (h *payHarness) wantActions(t *testing.T, what, order, subject string, d []byte, want string) refinedView {
	t.Helper()
	v, raw := h.refined(t, order, subject, d)
	if got := strings.Join(v.Actions, ","); got != want {
		t.Fatalf("%s: actions %q, want %q: %s", what, got, want, raw)
	}
	return v
}

// Awaiting payment: pay while the quote window is open (equality has
// passed), as the payment route decides; a read-only link gets nothing; a
// free order offers its confirmation; the owner reads its own actions.
func TestActionsAwaitingPayment(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.tenThousandOrder(t, f, pol, "aw")
	h.wantActions(t, "placed", order, "", d, "pay")
	h.wantActions(t, "read-only link", order, "", h.capability(t, order, "read"), "")
	h.wantActions(t, "cancel-only link", order, "", h.capability(t, order, "read", "cancel"), "")
	expires := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC) // quoted at 10:00 the day before
	h.now = expires.Add(-time.Second)
	h.wantActions(t, "just before the quote expires", order, "", d, "pay")
	h.now = expires
	h.wantActions(t, "at the quote expiry", order, "", d, "")
	if _, err := h.start(order, d, "late", 1); conflict(err) != "quote_expired" {
		t.Fatalf("payment at the expiry: %v", err)
	}
	h.now = expires.Add(-time.Second)
	if _, err := h.start(order, d, "pay", 1); err != nil {
		t.Fatalf("payment offered and refused: %v", err)
	}
	// In flight: "do not pay again".
	h.wantActions(t, "payment in flight", order, "", d, "")

	free, fd := h.freeOrder(t, f, pol)
	v := h.wantActions(t, "free order", free, "", fd, "free_confirmation")
	if _, err := h.co.FreeConfirm(h.ctx, free, "", fd, "fc", checkout.FreeConfirmInput{ExpectedVersion: v.Version}); err != nil {
		t.Fatal(err)
	}
	h.wantActions(t, "free confirmation pending", free, "", fd, "")

	h.resolve(t, customerA)
	h.resolve(t, customerB)
	mine := h.signedInOrder(t, f, pol, customerA, "own")
	h.wantActions(t, "owner", mine, customerA, nil, "pay")
	if _, err := h.co.GetOrder(h.ctx, mine, customerB, nil); err == nil {
		t.Fatal("another customer read the order")
	}
}

// A confirmed collection order: cancel and the same-day move until six
// hours before the start (equality allowed), each needing its grant;
// nothing while a cancellation is pending or after it.
func TestActionsOnAConfirmedOrder(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.withRefunds()
	order, _, _ := h.paidCollection(t, f, pol, "co")
	d := h.digestOf(t, order)
	v := h.wantActions(t, "confirmed", order, "", d, "cancel,move_pickup")
	h.wantActions(t, "cancel link", order, "", h.capability(t, order, "read", "cancel"), "cancel")
	h.wantActions(t, "move link", order, "", h.capability(t, order, "read", "move_pickup"), "move_pickup")
	h.wantActions(t, "pay link", order, "", h.capability(t, order, "read", "pay"), "")
	slots := h.slotsOn(t, "2026-11-14")
	h.now = time.Date(2026, 11, 14, 6, 30, 0, 0, time.UTC) // the 12:30 start - 6 h
	h.wantActions(t, "at the move cutoff", order, "", d, "cancel,move_pickup")
	h.now = h.now.Add(time.Second)
	h.wantActions(t, "after the move cutoff", order, "", d, "cancel")
	if _, err := h.move(order, d, "m", v.Version, slots[2]); conflict(err) != "move_too_late" {
		t.Fatalf("move after the cutoff: %v", err)
	}
	h.now = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	p := h.preview(t, order, d)
	if _, err := h.cancel(order, d, "c", v.Version, p); err != nil {
		t.Fatal(err)
	}
	h.wantActions(t, "cancellation pending", order, "", d, "")
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("cancel:\n%s", reasons(errs))
	}
	h.wantActions(t, "cancelled", order, "", d, "")
}

// Paid late: nothing until the chef offers, then answering (the
// cancellation's access); nothing while the answer is pending.
func TestActionsOnALatePayment(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, v := h.lateAgreementOrder(t, f, pol, "lp")
	h.wantActions(t, "waiting for an offer", order, "", d, "")
	res, err := h.offer(order, "o1", v.Version, "requested", "", "")
	if err != nil {
		t.Fatal(err)
	}
	lv := h.wantActions(t, "offered", order, "", d, "answer_late_payment")
	h.wantActions(t, "pay and move link", order, "", h.capability(t, order, "read", "pay", "move_pickup"), "")
	if _, err := h.answer(order, d, "a1", lv.Version, res.ID, "accept"); err != nil {
		t.Fatal(err)
	}
	h.wantActions(t, "answer pending", order, "", d, "")
}

// GK delivery: pay the quote until its pay-by, then the Q5 choices the
// caller's grants allow; nothing to cancel once the delivery is on its way;
// the choice after an our-side failure needs move_pickup.
func TestActionsOnAGKDelivery(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d, _, _ := h.quotedOrder(t, f, pol, "gq")
	h.wantActions(t, "quoted", order, "", d, "cancel,pay_delivery")
	h.wantActions(t, "cancel link", order, "", h.capability(t, order, "read", "cancel"), "cancel")
	h.now = deliveryPayBy
	v := h.wantActions(t, "at pay-by", order, "", d, "cancel,delivery_choice")
	if strings.Join(v.DeliveryChoice.AllowedChoices, ",") != "collection,customer_courier,full_refund" {
		t.Fatalf("allowed %v", v.DeliveryChoice.AllowedChoices)
	}
	mv := h.wantActions(t, "move link at pay-by", order, "", h.capability(t, order, "read", "move_pickup"), "delivery_choice")
	if strings.Join(mv.DeliveryChoice.AllowedChoices, ",") != "collection,customer_courier" ||
		strings.Join(mv.DeliveryChoice.Choices, ",") != "collection,customer_courier,full_refund" {
		t.Fatalf("move link choices %+v", mv.DeliveryChoice)
	}
	cv := h.wantActions(t, "cancel link at pay-by", order, "", h.capability(t, order, "read", "cancel"), "cancel,delivery_choice")
	if strings.Join(cv.DeliveryChoice.AllowedChoices, ",") != "full_refund" {
		t.Fatalf("cancel link choices %+v", cv.DeliveryChoice)
	}
	h.wantActions(t, "read-only link at pay-by", order, "", h.capability(t, order, "read"), "")
	if _, err := h.payDelivery(order, d, "late", v.Version, h.currentQuote(t, order, d)); conflict(err) != "delivery_pay_by_passed" {
		t.Fatalf("delivery payment offered off: %v", err)
	}

	sent, sd, _, _, version := h.dispatchedOrder(t, f, pol, "gd")
	h.wantActions(t, "out for delivery", sent, "", sd, "")
	if _, err := h.co.PreviewCancellation(h.ctx, sent, "", sd); conflict(err) != "cancellation_not_eligible" {
		t.Fatalf("cancellation after a dispatch: %v", err)
	}
	h.now = failAt
	if _, err := h.deliveryFailed(chef, "f1", sent, version, checkout.CauseOurSide, ""); err != nil {
		t.Fatal(err)
	}
	h.wantActions(t, "failed on our side", sent, "", sd, "delivery_failure_choice")
	h.wantActions(t, "cancel link after the failure", sent, "", h.capability(t, sent, "read", "cancel", "pay"), "")
}

func (h *payHarness) currentQuote(t *testing.T, order string, d []byte) string {
	t.Helper()
	v, raw := h.deliveryView(t, order, d)
	if v.Delivery == nil || v.Delivery.Quote == nil {
		t.Fatalf("no quote: %s", raw)
	}
	return v.Delivery.Quote.ID
}

type labelledPreview struct {
	AmountMinor int64 `json:"amount_minor"`
	Allocations []struct {
		CollectionID string `json:"collection_id"`
		AmountMinor  int64  `json:"amount_minor"`
		Kind         string
	}
	DeliveryPending int64 `json:"delivery_pending_minor"`
}

func (h *payHarness) labelled(t *testing.T, order string, d []byte) (labelledPreview, string) {
	t.Helper()
	raw, err := h.co.PreviewCancellation(h.ctx, order, "", d)
	if err != nil {
		t.Fatal(err)
	}
	var p labelledPreview
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return p, string(raw)
}

func allocationsOf(p labelledPreview) string {
	var out []string
	for _, a := range p.Allocations {
		out = append(out, a.Kind+" "+itoa(a.AmountMinor))
	}
	return strings.Join(out, ", ")
}

// Labelled allocations and refund parts: a stray (returned in full beside
// the cake's 50%), a delivery waiting for the chef then sent by the chef, a
// delivery returned in full under its floor after "GK can't deliver", a
// delivery kept after nobody was available, a stray returned before a
// no-show, and prior actuals skewed by a chef exception. The parts add up
// every time (refined checks it).
func TestRefundPartsAndLabelledAllocations(t *testing.T) {
	t.Run("stray, late cancellation", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		h.withRefunds()
		order, d, cake, stray, _ := h.strayOrder(t, f, pol, "ls")
		h.now = cancelDeadline
		p, raw := h.labelled(t, order, d)
		if allocationsOf(p) != "cake 5000, stray 12000" || p.Allocations[0].CollectionID != cake || p.Allocations[1].CollectionID != stray {
			t.Fatalf("preview %s", raw)
		}
		// The hash and the cancel are unchanged by the labels.
		v, _ := h.refined(t, order, "", d)
		if _, err := h.cancel(order, d, "c", v.Version, h.preview(t, order, d)); err != nil {
			t.Fatal(err)
		}
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("cancel:\n%s", reasons(errs))
		}
		if v, raw := h.refined(t, order, "", d); partsOf(v) != "cake 5000/5000/0/0, stray 12000/12000/0/0" {
			t.Fatalf("parts %s", raw)
		}
	})
	t.Run("paid delivery waits for the chef", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		h.withRefunds()
		order, d, _, delivery := h.paidDeliveryOrder(t, f, pol, "pd")
		h.now = cancelDeadline
		p, raw := h.labelled(t, order, d)
		if p.AmountMinor != 11000 || allocationsOf(p) != "cake 5000" || p.DeliveryPending != 6000 {
			t.Fatalf("preview %s", raw)
		}
		v, _ := h.refined(t, order, "", d)
		if _, err := h.cancel(order, d, "c", v.Version, h.preview(t, order, d)); err != nil {
			t.Fatal(err)
		}
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("cancel:\n%s", reasons(errs))
		}
		v, raw = h.refined(t, order, "", d)
		if partsOf(v) != "cake 5000/5000/0/0, delivery 6000/0/0/6000" {
			t.Fatalf("parts %s", raw)
		}
		// The chef sends it; Stripe keeps it pending for a while.
		h.stripe.mu.Lock()
		h.stripe.refunds.newStatus = "pending"
		h.stripe.mu.Unlock()
		if _, err := h.co.PayDeliveryRefund(h.ctx, chef, "dr", order, checkout.DeliveryRefundInput{ExpectedVersion: v.Version,
			CollectionID: delivery}); err != nil {
			t.Fatal(err)
		}
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("delivery refund:\n%s", reasons(errs))
		}
		if v, raw := h.refined(t, order, "", d); partsOf(v) != "cake 5000/5000/0/0, delivery 6000/0/6000/0" {
			t.Fatalf("parts while the delivery refund is pending %s", raw)
		}
	})
	t.Run("delivery floor after GK can't deliver", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		h.withRefunds()
		order, d, _, _ := h.paidDeliveryOrder(t, f, pol, "nf")
		v, _ := h.refined(t, order, "", d)
		if _, err := h.notPossible("np", order, v.Version, ""); err != nil {
			t.Fatal(err)
		}
		h.now = cancelDeadline
		p, raw := h.labelled(t, order, d)
		if p.AmountMinor != 17000 || allocationsOf(p) != "cake 5000, delivery 12000" || p.DeliveryPending != 0 {
			t.Fatalf("preview %s", raw)
		}
		v, _ = h.refined(t, order, "", d)
		if _, err := h.cancel(order, d, "c", v.Version, h.preview(t, order, d)); err != nil {
			t.Fatal(err)
		}
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("cancel:\n%s", reasons(errs))
		}
		if v, raw := h.refined(t, order, "", d); partsOf(v) != "cake 5000/5000/0/0, delivery 12000/12000/0/0" {
			t.Fatalf("parts %s", raw)
		}
	})
	t.Run("nobody available keeps the delivery", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		h.staffGrants(t)
		order, d, _, _, version := h.dispatchedOrder(t, f, pol, "nk")
		h.withRefunds()
		h.now = failAt
		if _, err := h.deliveryFailed(chef, "n1", order, version, checkout.CauseNobodyAvailable, ""); err != nil {
			t.Fatal(err)
		}
		h.due(t)
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("drain:\n%s", reasons(errs))
		}
		if v, raw := h.refined(t, order, "", d); partsOf(v) != "cake 5000/5000/0/0" {
			t.Fatalf("parts %s", raw)
		}
	})
	t.Run("stray returned before a no-show", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		h.staffGrants(t)
		h.withRefunds()
		order, d, _, stray, _ := h.strayOrder(t, f, pol, "rn")
		h.returnStray(t, order, d, stray)
		paid, s := h.deliveryStarted(t, order, d, "c2")
		h.deliveryPaid(t, order, paid, s, "evt_rn_paid")
		ready := h.makeReady(t, order, d, "rn")
		if _, err := h.fulfil(fulfilment, "d1", order, checkout.ActionDispatch, ready); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		h.now = at("12:30")
		if _, err := h.fulfil(chef, "ns", order, checkout.ActionNoShow, ready+1); err != nil {
			t.Fatalf("no-show: %v", err)
		}
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("no-show:\n%s", reasons(errs))
		}
		if v, raw := h.refined(t, order, "", d); partsOf(v) != "cake 5000/5000/0/0, delivery 7000/0/0/7000, stray 12000/12000/0/0" {
			t.Fatalf("parts %s", raw)
		}
	})
	t.Run("a chef exception skews the cake", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		h.withRefunds()
		order, d, cake, _ := h.paidDeliveryOrder(t, f, pol, "sk")
		v, _ := h.refined(t, order, "", d)
		if _, err := h.co.RecordRefund(h.ctx, chef, "ex", order, refund(v.Version, 9500, "Goodwill", alloc(cake, 9500))); err != nil {
			t.Fatal(err)
		}
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("exception:\n%s", reasons(errs))
		}
		if v, raw := h.refined(t, order, "", d); partsOf(v) != "cake 9500/9500/0/0" {
			t.Fatalf("parts after the exception %s", raw)
		}
		// 50% of 22000p = 11000p: the cake already got 9500p (more than its
		// 5000p share), so the delivery line is what the goal still needs.
		h.now = cancelDeadline
		v, _ = h.refined(t, order, "", d)
		if _, err := h.cancel(order, d, "c", v.Version, h.preview(t, order, d)); err != nil {
			t.Fatal(err)
		}
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("cancel:\n%s", reasons(errs))
		}
		if v, raw := h.refined(t, order, "", d); v.Refund.TargetMinor != 11000 || partsOf(v) != "cake 9500/9500/0/0, delivery 1500/0/0/1500" {
			t.Fatalf("parts %s", raw)
		}
	})
}

type answerSummary struct {
	ID            string
	DeliveryState *string `json:"delivery_state"`
	NeedsAnswer   bool    `json:"needs_answer"`
	Attention     []string
}

func (h *payHarness) answerRow(t *testing.T, subject, order string) (answerSummary, string) {
	t.Helper()
	p, err := h.co.MyOrders(h.ctx, subject, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(p.Orders)
	for _, bad := range []string{"order_blocks", `"pay_by"`, "accept_until", "opens_at", `"late_payment":`, `"quote"`} {
		if strings.Contains(raw, bad) {
			t.Fatalf("summary leaks %s: %s", bad, raw)
		}
	}
	var rows []answerSummary
	if err := json.Unmarshal(p.Orders, &rows); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Attention == nil {
			t.Fatalf("no attention list: %s", raw)
		}
		if r.ID == order {
			return r, raw
		}
	}
	t.Fatalf("order %s not listed: %s", order, raw)
	return answerSummary{}, ""
}

func (h *payHarness) wantAnswer(t *testing.T, what, subject, order, state, attention string) {
	t.Helper()
	r, raw := h.answerRow(t, subject, order)
	got := ""
	if r.DeliveryState != nil {
		got = *r.DeliveryState
	}
	if got != state || strings.Join(r.Attention, ",") != attention || r.NeedsAnswer != (attention != "") {
		t.Fatalf("%s: delivery_state %q attention %v needs_answer %v, want %q %q: %s", what, got, r.Attention, r.NeedsAnswer,
			state, attention, raw)
	}
}

// GET /me/orders asks for an answer when the customer must act, at the API
// clock: a payable delivery quote, then the Q5 choice once its pay-by has
// passed; an acceptable late-payment offer; an open choice after a failed
// delivery. Other orders need nothing and have no delivery state. The order
// blocks behind it never leave the API.
func TestMyOrdersNeedAnswer(t *testing.T) {
	t.Run("delivery quote, then the Q5 choice", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		h.resolve(t, customerA)
		order, _, _, _ := h.quotedOrder(t, f, pol, "mq")
		h.assign(t, order, customerA, "as")
		plain := h.signedInOrder(t, f, pol, customerA, "plain")
		h.wantAnswer(t, "quoted", customerA, order, "quoted", "pay_delivery")
		h.wantAnswer(t, "an order awaiting payment", customerA, plain, "", "")
		h.wantActions(t, "owner of the delivery", order, customerA, nil, "cancel,pay_delivery")
		h.now = deliveryPayBy
		h.wantAnswer(t, "at pay-by", customerA, order, "pay_by_passed", "delivery_choice")
	})
	t.Run("late-payment offer", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		h.resolve(t, customerA)
		order, _, _ := h.lateAgreementOrder(t, f, pol, "ml")
		h.assign(t, order, customerA, "as")
		h.wantAnswer(t, "waiting", customerA, order, "", "")
		if _, err := h.offer(order, "o1", h.orderVersion(t, order), "requested", "", ""); err != nil {
			t.Fatal(err)
		}
		h.wantAnswer(t, "offered", customerA, order, "", "answer_late_payment")
		// No longer acceptable at the original handover: declining stays on
		// the order page, but nothing asks for an answer.
		h.now = time.Date(2026, 11, 14, 12, 30, 0, 0, time.UTC)
		h.wantAnswer(t, "offer no longer acceptable", customerA, order, "", "")
		h.wantActions(t, "decline still possible", order, customerA, nil, "answer_late_payment")
	})
	t.Run("choice after a failed delivery", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		h.staffGrants(t)
		h.resolve(t, customerA)
		order, _, _, _, version := h.dispatchedOrder(t, f, pol, "mf")
		h.wantAnswerless(t, order)
		h.now = failAt
		if _, err := h.deliveryFailed(chef, "f1", order, version, checkout.CauseOurSide, ""); err != nil {
			t.Fatal(err)
		}
		h.assign(t, order, customerA, "as")
		h.wantAnswer(t, "failed on our side", customerA, order, "paid", "delivery_failure_choice")
	})
}

// wantAnswerless: a guest order is in no account.
func (h *payHarness) wantAnswerless(t *testing.T, order string) {
	t.Helper()
	p, err := h.co.MyOrders(h.ctx, customerA, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(p.Orders), order) {
		t.Fatalf("guest order listed: %s", p.Orders)
	}
}

// A no-show after the chef's return of a stray was reserved but not yet
// settled: the refund line is the 50% part this no-show reserved (the
// stray's earlier reservation is neither new nor "already refunded"), and
// the stray sentence states the total. The outcome records the stray
// amounts; a no-show without a stray records none.
func TestNoShowNoticeAfterAStrayRefundReservedEarlier(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d, _, stray, _ := h.strayOrder(t, f, pol, "nr")
	paid, s := h.deliveryStarted(t, order, d, "c2")
	h.deliveryPaid(t, order, paid, s, "evt_nr_paid")
	plain, pd, _, _, pversion := h.dispatchedOrder(t, f, pol, "np")
	h.withRefunds()
	h.withNotices(t)
	h.stripe.mu.Lock()
	h.stripe.refunds.newStatus = "pending"
	h.stripe.mu.Unlock()
	v, _ := h.refined(t, order, "", d)
	if _, err := h.co.RecordRefund(h.ctx, chef, "stray", order, refund(v.Version, 12000, "Paid an old quote", alloc(stray, 12000))); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("stray refund:\n%s", reasons(errs))
	}
	if ops := h.operations(t, stray); len(ops) != 1 || ops[0].State != "pending" {
		t.Fatalf("stray refund %+v", ops)
	}
	h.now = failOrderedAt
	ready := h.makeReady(t, order, d, "nr")
	h.now = failDispatchAt
	if _, err := h.fulfil(fulfilment, "d1", order, checkout.ActionDispatch, ready); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	h.now = failAt
	if _, err := h.fulfil(chef, "ns", order, checkout.ActionNoShow, ready+1); err != nil {
		t.Fatalf("no-show: %v", err)
	}
	if _, err := h.fulfil(chef, "ns2", plain, checkout.ActionNoShow, pversion); err != nil {
		t.Fatalf("plain no-show: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("no-show:\n%s", reasons(errs))
	}
	var evidence, plainEvidence string
	if err := h.admin.QueryRow(h.ctx, `SELECT (r.outcome -> 'evidence')::text FROM business.order_command c
		JOIN business.recovery_operation r ON r.id = c.id WHERE c.order_id = $1::uuid AND c.command = 'no_show'`, order).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if err := h.admin.QueryRow(h.ctx, `SELECT (r.outcome -> 'evidence')::text FROM business.order_command c
		JOIN business.recovery_operation r ON r.id = c.id WHERE c.order_id = $1::uuid AND c.command = 'no_show'`, plain).Scan(&plainEvidence); err != nil {
		t.Fatal(err)
	}
	has(t, "evidence", evidence, `"returned_before_minor":12000`, `"new_refund_minor":5000`, `"unmatched_minor":12000`,
		`"stray_before_minor":12000,"stray_new_minor":0}`)
	hasNot(t, "evidence without a stray", plainEvidence, "stray", "unmatched")
	var text string
	for _, n := range h.notices(t, order) {
		if n.Kind == "no_show" {
			text = h.request(t, n).Text
		}
	}
	has(t, "no-show notice", text, "£120 of the £240 you paid.", "Refund requested: £50.",
		"We are also returning in full a delivery payment of £120 that could not be used for your order, so £170 is being returned to you in total.")
	hasNot(t, "no-show notice", text, "already refunded", "nothing more is due")
	// An outcome recorded before 0038 (without the stray keys) renders as
	// before: here the under-reported line the fix replaces.
	tx, err := h.db.MigratorConn(t).Begin(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(h.ctx)
	if _, err := tx.Exec(h.ctx, "SET LOCAL ROLE gk_owner"); err != nil {
		t.Fatal(err)
	}
	var legacy string
	if err := tx.QueryRow(h.ctx, `SELECT r.body_text FROM business.orders o
		JOIN business.order_command c ON c.order_id = o.id AND c.command = 'no_show'
		JOIN business.recovery_operation ro ON ro.id = c.id,
		business.order_notice_render('no_show', o, ((ro.outcome -> 'evidence')::jsonb - 'stray_before_minor' - 'stray_new_minor'
		  || '{"refund_returned": false}')::json, 'https://gk.example') r
		WHERE o.id = $1::uuid`, order).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	has(t, "pre-0038 rendering", legacy, "The £120 already refunded covers this, so nothing more is due.")
	hasNot(t, "pre-0038 rendering", legacy, "Refund requested")
	if v, raw := h.refined(t, order, "", d); partsOf(v) != "cake 5000/0/5000/0, delivery 7000/0/0/7000, stray 12000/0/12000/0" {
		t.Fatalf("parts %s", raw)
	}
	_ = pd
}
