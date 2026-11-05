//go:build integration

package checkout_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
)

// GK-026 B2 part 2: the Q5 manual alternative payout (migration 0026; K05
// Q5, K11).

type manualPreview struct {
	PreviewID   string `json:"preview_id"`
	PreviewHash string `json:"preview_hash"`
	AmountMinor int64  `json:"amount_minor"`
	Failed      string `json:"failed_refund_operation_id"`
}

type manualItem struct {
	RefundOperationID string `json:"refund_operation_id"`
	State             string
	AmountMinor       int64 `json:"amount_minor"`
	Instructions      *struct {
		AmountMinor          int64  `json:"amount_minor"`
		Currency             string `json:"currency"`
		RecipientEvidenceRef string `json:"recipient_evidence_ref"`
		PaymentReference     string `json:"payment_reference"`
	}
	TransferIdentity *string `json:"transfer_identity"`
}

func (h *payHarness) manualList(t *testing.T, order string) []manualItem {
	t.Helper()
	raw, err := h.co.ManualPayouts(h.ctx, chef, order)
	if err != nil {
		t.Fatal(err)
	}
	var out []manualItem
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (h *payHarness) manualPreview(t *testing.T, order, coll, recipient string) (manualPreview, error) {
	t.Helper()
	raw, err := h.co.PreviewManualPayout(h.ctx, chef, order, checkout.ManualPayoutPreviewInput{CollectionID: coll,
		UnavailableEvidenceRef: "stripe-refund-failed/case-7", RecipientEvidenceRef: recipient})
	if err != nil {
		return manualPreview{}, err
	}
	var p manualPreview
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return p, nil
}

// failedProviderRefund dispatches the late capture's refund and Stripe
// reports it failed (definitive provider unavailability).
func (h *payHarness) failedProviderRefund(t *testing.T, coll string) refundOp {
	t.Helper()
	r := h.reservedRefund(t, coll)
	h.withRefunds()
	h.stripe.mu.Lock()
	h.stripe.refunds.newStatus = "failed"
	h.stripe.mu.Unlock()
	h.due(t)
	h.drain(t)
	h.stripe.mu.Lock()
	h.stripe.refunds.newStatus = ""
	h.stripe.mu.Unlock()
	if row := h.refundRow(t, r.ID); row.State != "failed" {
		t.Fatalf("provider refund %+v", row)
	}
	return r
}

func (h *payHarness) manualOutcome(t *testing.T, order string, d []byte, op, key string, in checkout.ManualPayoutOutcomeInput) error {
	t.Helper()
	in.ExpectedVersion = h.version(t, order, d)
	if _, err := h.co.RecordManualPayoutOutcome(h.ctx, chef, key, order, op, in); err != nil {
		return err
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("outcome %s:\n%s", in.Outcome, reasons(errs))
	}
	return nil
}

// reserveManual previews and reserves a manual payout and runs its K11
// command; it returns the manual refund operation.
func (h *payHarness) reserveManual(t *testing.T, order string, d []byte, coll, key string) string {
	t.Helper()
	p, err := h.manualPreview(t, order, coll, "recipient-verified/case-7")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.co.RequestManualPayout(h.ctx, chef, key, order, checkout.ManualPayoutInput{
		ExpectedVersion: h.version(t, order, d), PreviewID: p.PreviewID, PreviewHash: p.PreviewHash}); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("reservation:\n%s", reasons(errs))
	}
	items := h.manualList(t, order)
	return items[len(items)-1].RefundOperationID
}

// Q5: only after a definitive provider failure; the whole remaining
// allocation under one manual_alternative operation, protected under K11
// before its instructions are shown; submitted is pending; unknown blocks
// every route; a verified success with a unique transfer identity reduces
// the liability once.
func TestManualAlternativePayout(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d, coll := h.lateCapture(t, f, pol, "q5")
	if _, err := h.manualPreview(t, order, coll, "recipient-verified/case-7"); conflict(err) != "refund_in_progress" {
		t.Fatalf("while the provider refund is reserved: %v", err)
	}
	failed := h.failedProviderRefund(t, coll)

	// Evidence references only: never bank details.
	for _, ref := range []string{"12-34-56 12345678", "GB33BUKB20201555555555", "", " lead"} {
		if _, err := h.manualPreview(t, order, coll, ref); err == nil || conflict(err) != "" {
			t.Errorf("recipient reference %q: %v", ref, err)
		}
	}
	if _, err := h.co.PreviewManualPayout(h.ctx, fulfilment, order, checkout.ManualPayoutPreviewInput{CollectionID: coll,
		UnavailableEvidenceRef: "x", RecipientEvidenceRef: "y"}); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("fulfillment preview: %v", err)
	}
	p, err := h.manualPreview(t, order, coll, "recipient-verified/case-7")
	if err != nil {
		t.Fatal(err)
	}
	if p.AmountMinor != 10000 || p.Failed != failed.ID {
		t.Fatalf("preview %+v", p)
	}
	if again, _ := h.manualPreview(t, order, coll, "recipient-verified/case-7"); again.PreviewID != p.PreviewID || again.PreviewHash != p.PreviewHash {
		t.Fatalf("identical preview not reused: %+v", again)
	}
	other, _ := h.manualPreview(t, order, coll, "recipient-verified/case-8")
	if other.PreviewID == p.PreviewID {
		t.Fatal("a changed recipient reused the preview")
	}
	v := h.version(t, order, d)
	in := checkout.ManualPayoutInput{ExpectedVersion: v, PreviewID: p.PreviewID, PreviewHash: other.PreviewHash}
	if _, err := h.co.RequestManualPayout(h.ctx, chef, "m1", order, in); !strings.HasPrefix(invalidField(err), "preview_id:") {
		t.Fatalf("wrong hash: %v", err)
	}
	in.PreviewHash = p.PreviewHash
	res, err := h.co.RequestManualPayout(h.ctx, chef, "m1", order, in)
	if err != nil || res.Replayed {
		t.Fatalf("reserve %+v %v", res, err)
	}
	if again, err := h.co.RequestManualPayout(h.ctx, chef, "m1", order, in); err != nil || !again.Replayed || again.ID != res.ID {
		t.Fatalf("replay %+v %v", again, err)
	}
	if len(h.manualList(t, order)) != 0 {
		t.Fatal("instructions before the reservation is committed")
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("reservation:\n%s", reasons(errs))
	}
	items := h.manualList(t, order)
	if len(items) != 1 || items[0].State != "reserved" || items[0].AmountMinor != 10000 || items[0].Instructions == nil ||
		items[0].Instructions.RecipientEvidenceRef != "recipient-verified/case-7" ||
		items[0].Instructions.PaymentReference != items[0].RefundOperationID {
		t.Fatalf("manual payouts %+v", items)
	}
	op := items[0].RefundOperationID
	// Instructions only while the reservation's K11 outcome is secured.
	h.owner(t, `UPDATE business.recovery_operation SET state = 'committed', outcome_ack_at = NULL WHERE id = $1::uuid`, res.ID)
	if got := h.manualList(t, order); got[0].State != "protecting" || got[0].Instructions != nil {
		t.Fatalf("unprotected instructions %+v", got)
	}
	h.owner(t, `UPDATE business.recovery_operation SET state = 'secured', outcome_ack_at = now() WHERE id = $1::uuid`, res.ID)
	// The reservation blocks provider refunds and other manual payouts.
	if _, err := h.co.ReserveRemainder(h.ctx, chef, "rr", order, failed.ID, checkout.RemainderInput{ExpectedVersion: h.version(t, order, d)}); conflict(err) != "refund_in_progress" {
		t.Fatalf("provider re-reserve: %v", err)
	}
	if _, err := h.manualPreview(t, order, coll, "recipient-verified/case-9"); conflict(err) != "refund_in_progress" {
		t.Fatalf("second manual payout: %v", err)
	}
	if b := h.balance(t, coll); b.Reserved != 10000 || b.Available != 0 {
		t.Fatalf("reserved %+v", b)
	}

	success := checkout.ManualPayoutOutcomeInput{Outcome: "succeeded", TransferIdentity: "bank-statement/2026-11/line-3",
		EvidenceRef: "evidence/case-7", AmountMinor: 10000, Currency: "GBP"}
	if err := h.manualOutcome(t, order, d, op, "early", success); conflict(err) != "state_conflict" {
		t.Fatalf("success before submission: %v", err)
	}
	if err := h.manualOutcome(t, order, d, op, "sub", checkout.ManualPayoutOutcomeInput{Outcome: "submitted"}); err != nil {
		t.Fatal(err)
	}
	if view, raw := h.view(t, order, d); view.Refund.State != "pending" || view.Refund.InProgress != 10000 || view.Refund.SucceededMinor != 0 {
		t.Fatalf("submitted %s", raw)
	}
	if err := h.manualOutcome(t, order, d, op, "unk", checkout.ManualPayoutOutcomeInput{Outcome: "unknown"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.manualPreview(t, order, coll, "recipient-verified/case-9"); conflict(err) != "refund_in_progress" {
		t.Fatalf("manual payout while unknown: %v", err)
	}
	if _, err := h.co.ReserveRemainder(h.ctx, chef, "rr2", order, failed.ID, checkout.RemainderInput{ExpectedVersion: h.version(t, order, d)}); conflict(err) != "refund_in_progress" {
		t.Fatalf("provider re-reserve while unknown: %v", err)
	}
	wrong := success
	wrong.AmountMinor = 9999
	if err := h.manualOutcome(t, order, d, op, "wrong", wrong); !strings.HasPrefix(invalidField(err), "amount_minor:") {
		t.Fatalf("another amount: %v", err)
	}
	if err := h.manualOutcome(t, order, d, op, "ok", success); err != nil {
		t.Fatal(err)
	}
	if b := h.balance(t, coll); b.Succeeded != 10000 || b.Reserved != 0 || b.Exposure != 10000 {
		t.Fatalf("returned once %+v", b)
	}
	if view, raw := h.view(t, order, d); view.Refund.State != "returned" || view.Refund.SucceededMinor != 10000 {
		t.Fatalf("returned %s", raw)
	}
	// Until the outcome is secured, the success is not published.
	var outcomeCmd string
	if err := h.admin.QueryRow(h.ctx, `SELECT command_id::text FROM business.manual_payout_command
		WHERE refund_operation_id = $1::uuid AND outcome = 'succeeded'`, op).Scan(&outcomeCmd); err != nil {
		t.Fatal(err)
	}
	h.owner(t, `UPDATE business.recovery_operation SET state = 'committed', outcome_ack_at = NULL WHERE id = $1::uuid`, outcomeCmd)
	if view, raw := h.view(t, order, d); view.Refund.State == "returned" || view.Refund.SucceededMinor != 0 || view.Refund.InProgress != 10000 {
		t.Fatalf("published before protection %s", raw)
	}
	h.owner(t, `UPDATE business.recovery_operation SET state = 'secured', outcome_ack_at = now() WHERE id = $1::uuid`, outcomeCmd)
	if len(h.stripe.refundCreates()) != 1 {
		t.Fatalf("Stripe called again: %v", h.stripe.refundCreates())
	}
	if got := h.manualList(t, order); got[0].State != "succeeded" || got[0].TransferIdentity == nil {
		t.Fatalf("after success %+v", got)
	}
	if h.count(t, "SELECT count(*) FROM business.manual_payout_event WHERE refund_operation_id = $1::uuid", op) != 3 {
		t.Fatal("outcome events")
	}
	if strings.Contains(h.logs.String(), "recipient-verified") || strings.Contains(h.logs.String(), "bank-statement") {
		t.Fatal("evidence references in the logs")
	}

	// The same transfer evidence cannot reduce another liability.
	h.handler = jobs.Chain(h.cmds.Handle, h.pay.Handle, h.reviews.Handle, h.productions.Handle)
	order2, d2, coll2 := h.lateCapture(t, f, pol, "q5b")
	h.failedProviderRefund(t, coll2)
	op2 := h.reserveManual(t, order2, d2, coll2, "m2")
	if err := h.manualOutcome(t, order2, d2, op2, "sub", checkout.ManualPayoutOutcomeInput{Outcome: "submitted"}); err != nil {
		t.Fatal(err)
	}
	if err := h.manualOutcome(t, order2, d2, op2, "dup", success); conflict(err) != "duplicate" {
		t.Fatalf("duplicate transfer identity: %v", err)
	}
	// A failed transfer reopens the remainder; a withdrawn one (never paid)
	// lets a new manual payout follow.
	if err := h.manualOutcome(t, order2, d2, op2, "fail", checkout.ManualPayoutOutcomeInput{Outcome: "failed", EvidenceRef: "bank-return/case-9"}); err != nil {
		t.Fatal(err)
	}
	if b := h.balance(t, coll2); b.Succeeded != 0 || b.Reserved != 0 || b.Room != 10000 {
		t.Fatalf("after a failed transfer %+v", b)
	}
	op3 := h.reserveManual(t, order2, d2, coll2, "m3")
	if err := h.manualOutcome(t, order2, d2, op3, "wd", checkout.ManualPayoutOutcomeInput{Outcome: "withdrawn"}); err != nil {
		t.Fatal(err)
	}
	if b := h.balance(t, coll2); b.Reserved != 0 {
		t.Fatalf("after withdrawal %+v", b)
	}
	if fa := findKind(h.attention(t, chef, order2), "refund_failed_owed"); fa == nil {
		t.Fatalf("owed after the failed transfer %+v", h.attention(t, chef, order2))
	}
}
