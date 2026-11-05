//go:build integration

package checkout_test

import (
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
)

// GK-026 B2: chef refunds and recovery (migrations 0023+; contracts r6.5
// K05, K11, K00).

// auditCount counts the order's audit events of action whose detail
// contains fragment.
func (h *payHarness) auditCount(t *testing.T, order, action, fragment string) int {
	t.Helper()
	return h.count(t, `SELECT count(*) FROM business.audit_event WHERE subject_id = $1::uuid AND action = $2
		AND detail::text LIKE '%' || $3 || '%'`, order, action, fragment)
}

// B1 follow-up: a failed operation earlier in the order's history does not
// make a refund read needs_attention when all that is missing is the
// delivery share visibly waiting for the chef (delivery_pending_minor). A
// cake shortfall still does.
func TestDeliveryPendingIsNotAttention(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.tenThousandOrder(t, f, pol, "dp")
	cake := h.payNow(t, order, d, "dp")
	h.seedDelivery(t, order, 2000)
	var pi string
	if err := h.admin.QueryRow(h.ctx, "SELECT payment_intent_id FROM business.payment_collection WHERE id = $1::uuid", cake).Scan(&pi); err != nil {
		t.Fatal(err)
	}
	// An earlier goal whose only refund failed definitively.
	goal := payments.NewID()
	h.owner(t, `INSERT INTO business.refund_goal (id, order_id, target_minor, reason, source)
		VALUES ($1::uuid, $2::uuid, 1000, 'late_capture_terminal', 'system')`, goal, order)
	h.sentOperation(t, order, cake, goal, pi, "failed", 1000)
	if v, raw := h.cancelView(t, order, d); v.Refund == nil || v.Refund.State != "needs_attention" {
		t.Fatalf("a failed refund with money still owed: %s", raw)
	}
	// The customer cancels at the boundary: 50% of 12000p is 6000p, cake
	// 5000p sent, delivery 1000p waits for the chef.
	v, _ := h.cancelView(t, order, d)
	h.now = cancelDeadline
	p := h.preview(t, order, d)
	if p.AmountMinor != 6000 || p.DeliveryPending != 1000 {
		t.Fatalf("preview %+v", p)
	}
	h.withRefunds()
	if _, err := h.cancel(order, d, "c", v.Version, p); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("cancel:\n%s", reasons(errs))
	}
	if b := h.balance(t, cake); b.Succeeded != 5000 {
		t.Fatalf("cake %+v", b)
	}
	if v, raw := h.cancelView(t, order, d); v.Refund.State != "partly_returned" || v.Refund.DeliveryPending != 1000 ||
		v.Refund.TargetMinor != 6000 || v.Refund.SucceededMinor != 5000 {
		t.Fatalf("delivery pending after an old failure: %s", raw)
	}
}

// B1 follow-up: the commit-time honour_too_late refusal (defensive: the
// request refuses the same receipt instant first) is audited like the
// other refusals. The receipt is moved to the handover behind the request
// check to reach it.
func TestCommitTimeHonourTooLateIsAudited(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.lateOrder(t, f, pol, "htl")
	v, _ := h.cancelView(t, order, d)
	h.now = time.Date(2026, 11, 6, 13, 0, 0, 0, time.UTC)
	honour := checkout.LatePaymentInput{ExpectedVersion: v.Version, Decision: "honour",
		CustomerAgreement: &checkout.Agreement{Channel: "phone", AgreedAt: "2026-11-06T12:45:00Z", EvidenceRef: "Call note 7"}}
	res, err := h.co.LatePayment(h.ctx, chef, "lp", order, honour)
	if err != nil {
		t.Fatal(err)
	}
	h.owner(t, `ALTER TABLE business.order_command DISABLE TRIGGER order_command_immutable`)
	h.owner(t, `UPDATE business.order_command SET received_at = '2026-11-14T12:30:00Z' WHERE id = $1::uuid`, res.ID)
	h.owner(t, `ALTER TABLE business.order_command ENABLE TRIGGER order_command_immutable`)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("decision:\n%s", reasons(errs))
	}
	if v2, raw := h.cancelView(t, order, d); v2.Lifecycle != "paid_awaiting_agreement" || v2.Version != v.Version+1 {
		t.Fatalf("refused honour %s", raw)
	}
	if got := h.auditCount(t, order, "order.late_payment_refused", `"honour_too_late"`); got != 1 {
		t.Fatalf("honour_too_late audits %d, want 1", got)
	}
}
