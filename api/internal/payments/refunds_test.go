package payments

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
)

func testRefund(status string, amount int64) *Refund {
	return &Refund{ID: "re_1", Object: "refund", Amount: amount, Currency: "gbp", Status: status, PaymentIntent: testPI,
		Metadata: map[string]string{"refund_operation_id": testRefundOp}}
}

func TestRefundOutcomeMapping(t *testing.T) {
	cases := []struct {
		res    RefundResult
		status string
	}{
		{RefundResult{Kind: RefundSucceeded, Status: 200, Refund: testRefund("succeeded", 10000)}, "succeeded"},
		{RefundResult{Kind: RefundPending, Status: 200, Refund: testRefund("pending", 10000)}, "pending"},
		{RefundResult{Kind: RefundRequiresAction, Status: 200, Refund: testRefund("requires_action", 10000)}, "requires_action"},
		{RefundResult{Kind: RefundFailed, Status: 200, Refund: testRefund("canceled", 10000)}, "failed"},
		{RefundResult{Kind: Rejected, Status: 400, Code: "invalid_request_error"}, "rejected"},
	}
	for _, c := range cases {
		o, err := refundOutcome(c.res, false)
		if err != nil || o.Status != c.status {
			t.Fatalf("%s: %+v %v", c.res.Kind, o, err)
		}
		var ev RefundEvidence
		if err := json.Unmarshal(o.Evidence, &ev); err != nil || ev.HTTPStatus != c.res.Status {
			t.Fatalf("evidence %s %v", o.Evidence, err)
		}
		if c.res.Refund != nil && (ev.RefundID != "re_1" || ev.Status != c.res.Refund.Status || ev.AmountMinor != 10000 ||
			ev.PaymentIntentID != testPI || ev.Currency != "gbp") {
			t.Fatalf("evidence %+v", ev)
		}
		if _, _, err := o.Encode(); err != nil {
			t.Fatalf("outcome cannot be recorded: %v", err)
		}
	}
	for _, kind := range []ResultKind{Unknown, Deferred, Conflict, Refused} {
		var ce *refundCallError
		if _, err := refundOutcome(RefundResult{Kind: kind}, false); !errors.As(err, &ce) {
			t.Errorf("%s: %v, want an unknown outcome", kind, err)
		}
	}
}

func TestRefundOutcomeErrors(t *testing.T) {
	req := testRefundRequest()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	outcome := func(status string, amount int64) recovery.Outcome {
		o, _ := refundOutcome(RefundResult{Kind: ResultKind(status), Status: 200, Refund: testRefund(status, amount)}, false)
		return o
	}
	attention := func(err error) string {
		var ae *jobs.AttentionError
		if errors.As(err, &ae) {
			return ae.Reason
		}
		return ""
	}
	if err := refundOutcomeError(outcome("succeeded", 10000), req, false, log); err != nil {
		t.Fatalf("succeeded: %v", err)
	}
	if err := refundOutcomeError(outcome("pending", 10000), req, false, log); err != nil {
		t.Fatalf("pending: %v", err)
	}
	for status, want := range map[string]string{"requires_action": reasonRefundAction, "failed": reasonRefundFailed} {
		if got := attention(refundOutcomeError(outcome(status, 10000), req, false, log)); got != want {
			t.Errorf("%s: %q", status, got)
		}
	}
	if got := attention(refundOutcomeError(outcome("succeeded", 5000), req, false, log)); got != reasonRefundMismatch {
		t.Errorf("wrong amount: %q", got)
	}
	if got := attention(refundOutcomeError(outcome("succeeded", 10000), req, true, log)); got != reasonRefundMismatch {
		t.Errorf("wrong mode: %q", got)
	}
	rejected, _ := refundOutcome(RefundResult{Kind: Rejected, Status: 400}, false)
	if got := attention(refundOutcomeError(rejected, req, false, log)); got != reasonRefundRejected {
		t.Errorf("rejected: %q", got)
	}
	if err := refundOutcomeError(recovery.Outcome{Status: "settled"}, req, false, log); err != nil {
		t.Errorf("settled: %v", err)
	}
}

func TestRefundEvidenceMismatch(t *testing.T) {
	req := testRefundRequest()
	good := EvidenceOfRefund(testRefund("succeeded", 10000), false)
	if why := RefundEvidenceMismatch(good, req, false); why != "" {
		t.Fatal(why)
	}
	for name, mutate := range map[string]func(*RefundEvidence){
		"id":       func(e *RefundEvidence) { e.RefundID = "" },
		"amount":   func(e *RefundEvidence) { e.AmountMinor = 9999 },
		"currency": func(e *RefundEvidence) { e.Currency = "eur" },
		"intent":   func(e *RefundEvidence) { e.PaymentIntentID = "pi_other" },
		"mode":     func(e *RefundEvidence) { e.Livemode = true },
	} {
		e := good
		mutate(&e)
		if RefundEvidenceMismatch(e, req, false) == "" {
			t.Errorf("%s mismatch not detected", name)
		}
	}
}

func TestRefundVerdictAndMatching(t *testing.T) {
	op := reconcileOp{ID: testRefundOp, AmountMinor: 10000, PaymentIntentID: testPI}
	for status, want := range map[string]string{"succeeded": "succeeded", "pending": "pending",
		"requires_action": "requires_action", "failed": "failed", "canceled": "failed", "odd": "mismatch"} {
		if got, _ := refundVerdict(op, testRefund(status, 10000)); got != want {
			t.Errorf("%s: %s, want %s", status, got, want)
		}
	}
	if v, why := refundVerdict(op, testRefund("succeeded", 5000)); v != "mismatch" || why == "" {
		t.Errorf("amount: %s %q", v, why)
	}
	eur := testRefund("succeeded", 10000)
	eur.Currency = "eur"
	if v, _ := refundVerdict(op, eur); v != "mismatch" {
		t.Errorf("currency: %s", v)
	}
	recorded := op
	recorded.ProviderRefundID = "re_other"
	if v, _ := refundVerdict(recorded, testRefund("succeeded", 10000)); v != "mismatch" {
		t.Errorf("another refund: %s", v)
	}
	failed := testRefund("failed", 10000)
	failed.FailureReason = "expired_or_canceled_card"
	if v, why := refundVerdict(op, failed); v != "failed" || why != "expired_or_canceled_card" {
		t.Errorf("failure reason: %s %q", v, why)
	}

	other := Refund{ID: "re_dash", Amount: 5000, Currency: "gbp", Status: "succeeded", PaymentIntent: testPI}
	named := *testRefund("succeeded", 10000)
	named.ID = "re_named"
	list := []Refund{other, named}
	if r := findRefund(op, list); r == nil || r.ID != "re_named" {
		t.Fatalf("by metadata: %+v", r)
	}
	byID := op
	byID.ProviderRefundID = "re_dash"
	if r := findRefund(byID, list); r == nil || r.ID != "re_dash" {
		t.Fatalf("by recorded ID: %+v", r)
	}
	if r := findRefund(reconcileOp{ID: "00000000-0000-4000-8000-000000000000"}, list); r != nil {
		t.Fatalf("unrelated operation matched %+v", r)
	}
}
