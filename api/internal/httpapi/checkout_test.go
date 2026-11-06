package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
)

type fakeCheckout struct {
	subject  string
	digest   []byte
	calls    int
	freeErr  error
	replayed bool
	filter   checkout.StaffOrderFilter
	orderID  string
	// unchanged answers moves as already at that time.
	unchanged bool
	// order is GetOrder's representation (default {"id":"o"}).
	order string
}

func (f *fakeCheckout) CreateQuote(_ context.Context, subject string, digest []byte, _ string, _ checkout.QuoteInput) (catalog.Result, error) {
	f.calls++
	f.subject, f.digest = subject, digest
	return catalog.Result{ID: "q"}, nil
}
func (f *fakeCheckout) GetQuote(_ context.Context, _, subject string, digest []byte) (checkout.Quote, error) {
	if subject == "" && digest == nil {
		return nil, catalog.ErrNotFound
	}
	if digest != nil && f.digest != nil && string(digest) != string(f.digest) {
		return nil, catalog.ErrNotFound
	}
	return checkout.Quote(`{"id":"q"}`), nil
}
func (f *fakeCheckout) Codes(context.Context, string) ([]checkout.Code, error) {
	return []checkout.Code{{ID: "c1"}}, nil
}
func (f *fakeCheckout) Checkout(_ context.Context, subject string, digest []byte, _ string, _ checkout.CheckoutInput) (catalog.Result, error) {
	f.calls++
	f.subject, f.digest = subject, digest
	return catalog.Result{ID: "o"}, nil
}
func (f *fakeCheckout) GetOrder(_ context.Context, _, subject string, digest []byte) (checkout.Order, error) {
	if subject == "" && digest == nil {
		return nil, catalog.ErrNotFound
	}
	if f.order != "" {
		return checkout.Order(f.order), nil
	}
	return checkout.Order(`{"id":"o"}`), nil
}
func (f *fakeCheckout) FreeConfirm(_ context.Context, id, subject string, digest []byte, _ string, _ checkout.FreeConfirmInput) (catalog.Result, error) {
	f.calls++
	f.subject, f.digest = subject, digest
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "op-" + id, Replayed: f.replayed}, nil
}
func (f *fakeCheckout) StartPayment(_ context.Context, id, subject string, digest []byte, _ string, _ checkout.PaymentInput) (catalog.Result, error) {
	f.calls++
	f.subject, f.digest = subject, digest
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "pay-" + id, Replayed: f.replayed}, nil
}
func (f *fakeCheckout) PreviewCancellation(_ context.Context, _, subject string, digest []byte) (checkout.CancellationPreview, error) {
	f.calls++
	f.subject, f.digest = subject, digest
	return checkout.CancellationPreview(`{"rate":100}`), f.freeErr
}

func (f *fakeCheckout) Cancel(_ context.Context, id, subject string, digest []byte, _ string, _ checkout.CancelInput) (catalog.Result, error) {
	f.calls++
	f.subject, f.digest = subject, digest
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "cancel-" + id, Replayed: f.replayed}, nil
}

func (f *fakeCheckout) ChefCancel(_ context.Context, subject, _, _ string, _ checkout.ChefCancelInput) (catalog.Result, error) {
	f.calls++
	f.subject, f.digest = subject, nil
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "chef-cancel", Replayed: f.replayed}, nil
}

func (f *fakeCheckout) MovePickup(_ context.Context, id, subject string, digest []byte, _ string, _ checkout.PickupMoveInput) (checkout.Moved, error) {
	f.calls++
	f.subject, f.digest, f.orderID = subject, digest, id
	if f.freeErr != nil {
		return checkout.Moved{}, f.freeErr
	}
	return checkout.Moved{Result: catalog.Result{ID: "move-" + id, Replayed: f.replayed}, Unchanged: f.unchanged}, nil
}

func (f *fakeCheckout) ChangeAppointment(_ context.Context, subject, _, id string, _ checkout.AppointmentInput) (checkout.Moved, error) {
	f.calls++
	f.subject, f.digest, f.orderID = subject, nil, id
	if f.freeErr != nil {
		return checkout.Moved{}, f.freeErr
	}
	return checkout.Moved{Result: catalog.Result{ID: "appointment-" + id, Replayed: f.replayed}, Unchanged: f.unchanged}, nil
}

func (f *fakeCheckout) Transition(_ context.Context, subject, _, id string, in checkout.TransitionInput) (checkout.Transitioned, error) {
	f.calls++
	f.subject, f.orderID = subject, id
	if f.freeErr != nil {
		return checkout.Transitioned{}, f.freeErr
	}
	if in.Protected() {
		return checkout.Transitioned{Result: catalog.Result{ID: in.Action + "-" + id, Replayed: f.replayed}}, nil
	}
	return checkout.Transitioned{Result: catalog.Result{ID: id, Replayed: f.replayed}, Lifecycle: "ready",
		FulfillmentState: "awaiting_handover"}, nil
}

func (f *fakeCheckout) StartProduction(_ context.Context, subject, _, id string, _ checkout.ProductionInput) (checkout.Started, error) {
	f.calls++
	f.subject = subject
	if f.freeErr != nil {
		return checkout.Started{}, f.freeErr
	}
	lifecycle := "in_production"
	if f.replayed {
		lifecycle = "cancelled"
	}
	return checkout.Started{Result: catalog.Result{ID: id, Replayed: f.replayed}, Lifecycle: lifecycle}, nil
}

func (f *fakeCheckout) LatePayment(_ context.Context, subject, _, id string, _ checkout.LatePaymentInput) (catalog.Result, error) {
	f.calls++
	f.subject = subject
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "late-" + id, Replayed: f.replayed}, nil
}

func (f *fakeCheckout) OfferLatePayment(_ context.Context, subject, _, id string, _ checkout.LatePaymentOfferInput) (catalog.Result, error) {
	f.calls++
	f.subject, f.orderID = subject, id
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "offer-" + id, Replayed: f.replayed}, nil
}

func (f *fakeCheckout) AnswerLatePayment(_ context.Context, id, subject string, digest []byte, _ string, _ checkout.LatePaymentAnswerInput) (catalog.Result, error) {
	f.calls++
	f.subject, f.digest, f.orderID = subject, digest, id
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "answer-" + id, Replayed: f.replayed}, nil
}

func (f *fakeCheckout) RecordRefund(_ context.Context, subject, _, id string, _ checkout.RefundInput) (catalog.Result, error) {
	f.calls++
	f.subject = subject
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "exc-" + id, Replayed: f.replayed}, nil
}

func (f *fakeCheckout) decision(subject string) (catalog.Result, error) {
	f.calls++
	f.subject = subject
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "dec-1", Replayed: f.replayed}, nil
}

func (f *fakeCheckout) ResolveObservation(_ context.Context, subject, _, _, _ string, _ checkout.ResolveObservationInput) (catalog.Result, error) {
	return f.decision(subject)
}

func (f *fakeCheckout) DecideOperation(_ context.Context, subject, _, _, _ string, _ checkout.DecideOperationInput) (catalog.Result, error) {
	return f.decision(subject)
}

func (f *fakeCheckout) ClearRefundMismatch(_ context.Context, subject, _, _, _ string, _ checkout.ClearMismatchInput) (catalog.Result, error) {
	return f.decision(subject)
}

func (f *fakeCheckout) ClearPaymentMismatch(_ context.Context, subject, _, _, _ string, _ checkout.NoteInput) (catalog.Result, error) {
	return f.decision(subject)
}

func (f *fakeCheckout) ReserveRemainder(_ context.Context, subject, _, _, _ string, _ checkout.RemainderInput) (catalog.Result, error) {
	return f.decision(subject)
}

func (f *fakeCheckout) AttentionList(_ context.Context, subject string) (checkout.Attention, error) {
	f.subject = subject
	return checkout.Attention(`[{"kind":"delivery_pending"}]`), nil
}

func (f *fakeCheckout) StaffOrders(_ context.Context, subject string, filter checkout.StaffOrderFilter) (checkout.StaffOrderPage, error) {
	f.calls++
	f.subject, f.filter = subject, filter
	if f.freeErr != nil {
		return checkout.StaffOrderPage{}, f.freeErr
	}
	return checkout.StaffOrderPage{Items: json.RawMessage(`[{"id":"o1"}]`), NextCursor: "next"}, nil
}

func (f *fakeCheckout) StaffOrder(_ context.Context, subject, id string) (json.RawMessage, error) {
	f.calls++
	f.subject, f.orderID = subject, id
	if f.freeErr != nil {
		return nil, f.freeErr
	}
	return json.RawMessage(`{"id":"` + id + `"}`), nil
}

func (f *fakeCheckout) PreviewManualPayout(_ context.Context, subject, _ string, _ checkout.ManualPayoutPreviewInput) (json.RawMessage, error) {
	f.calls++
	f.subject = subject
	return json.RawMessage(`{"preview_id":"p1"}`), f.freeErr
}

func (f *fakeCheckout) RequestManualPayout(_ context.Context, subject, _, _ string, _ checkout.ManualPayoutInput) (catalog.Result, error) {
	return f.decision(subject)
}

func (f *fakeCheckout) RecordManualPayoutOutcome(_ context.Context, subject, _, _, _ string, _ checkout.ManualPayoutOutcomeInput) (catalog.Result, error) {
	return f.decision(subject)
}

func (f *fakeCheckout) ManualPayouts(_ context.Context, subject, _ string) (json.RawMessage, error) {
	f.subject = subject
	return json.RawMessage(`[]`), nil
}

func (f *fakeCheckout) PayDeliveryRefund(_ context.Context, subject, _, id string, _ checkout.DeliveryRefundInput) (catalog.Result, error) {
	f.calls++
	f.subject = subject
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "op-" + id, Replayed: f.replayed}, nil
}

func (f *fakeCheckout) QuoteDelivery(_ context.Context, subject, _, id string, _ checkout.DeliveryQuoteInput) (catalog.Result, error) {
	f.calls++
	f.subject, f.orderID = subject, id
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "quote-" + id, Replayed: f.replayed}, nil
}

func (f *fakeCheckout) WaiveDelivery(_ context.Context, subject, _, id string, _ checkout.DeliveryWaiverInput) (catalog.Result, error) {
	f.calls++
	f.subject, f.orderID = subject, id
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "waiver-" + id, Replayed: f.replayed}, nil
}

func (f *fakeCheckout) StartDeliveryPayment(_ context.Context, id, subject string, digest []byte, _ string, _ checkout.DeliveryPaymentInput) (catalog.Result, error) {
	f.calls++
	f.subject, f.digest, f.orderID = subject, digest, id
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "dpay-" + id, Replayed: f.replayed}, nil
}

func (f *fakeCheckout) RecordDeliveryNotPossible(_ context.Context, subject, _, id string, _ checkout.DeliveryNotPossibleInput) (catalog.Result, error) {
	f.calls++
	f.subject, f.orderID = subject, id
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "decision-" + id, Replayed: f.replayed}, nil
}

func (f *fakeCheckout) ChooseDelivery(_ context.Context, id, subject string, digest []byte, _ string, _ checkout.DeliveryChoiceInput) (catalog.Result, error) {
	f.calls++
	f.subject, f.digest, f.orderID = subject, digest, id
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "choice-" + id, Replayed: f.replayed}, nil
}

func (f *fakeCheckout) RecordFailedDelivery(_ context.Context, subject, _, id string, in checkout.DeliveryFailedInput) (checkout.DeliveryFailed, error) {
	f.calls++
	f.subject, f.orderID = subject, id
	if f.freeErr != nil {
		return checkout.DeliveryFailed{}, f.freeErr
	}
	prefix := "failure-"
	if in.Cause == checkout.CauseNobodyAvailable {
		prefix = "noshow-"
	}
	return checkout.DeliveryFailed{Result: catalog.Result{ID: prefix + id, Replayed: f.replayed}, Cause: in.Cause}, nil
}

func (f *fakeCheckout) ChooseAfterDeliveryFailure(_ context.Context, id, subject string, digest []byte, _ string, _ checkout.DeliveryFailureChoiceInput) (catalog.Result, error) {
	f.calls++
	f.subject, f.digest, f.orderID = subject, digest, id
	if f.freeErr != nil {
		return catalog.Result{}, f.freeErr
	}
	return catalog.Result{ID: "fchoice-" + id, Replayed: f.replayed}, nil
}

func (f *fakeCheckout) CurrentPolicy(context.Context) (checkout.Policy, error) {
	return checkout.Policy{ID: "p"}, nil
}
func (f *fakeCheckout) RecordPolicy(context.Context, string, string, checkout.PolicyInput) (catalog.Result, error) {
	f.calls++
	return catalog.Result{ID: "p"}, nil
}
func (f *fakeCheckout) CodeByID(_ context.Context, _, id string) (checkout.Code, error) {
	return checkout.Code{ID: id}, nil
}
func (f *fakeCheckout) CreateCode(context.Context, string, string, checkout.CodeInput) (catalog.Result, error) {
	f.calls++
	return catalog.Result{ID: "c1"}, nil
}
func (f *fakeCheckout) ReviseCode(context.Context, string, string, string, checkout.CodeRevision) (catalog.Result, error) {
	f.calls++
	return catalog.Result{ID: "c1"}, nil
}
func (f *fakeCheckout) ArchiveCode(context.Context, string, string, string, catalog.VersionInput) (catalog.Result, error) {
	f.calls++
	return catalog.Result{ID: "c1"}, nil
}

func serveCheckout(t *testing.T, co *fakeCheckout, id Identity, method, path, auth, key, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var logs bytes.Buffer
	h := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id, Checkout: co, CheckoutEnabled: true,
		Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if strings.Contains(logs.String(), "guest_proof") || strings.Contains(logs.String(), testProof) ||
		(hdr != nil && hdr[QuoteProofHeader] != "" && strings.Contains(logs.String(), hdr[QuoteProofHeader])) {
		t.Fatalf("a proof reached the logs: %s", logs.String())
	}
	return rec
}

var testProof = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))

func TestQuoteRoutes(t *testing.T) {
	id := catalogIdentity()
	proof := testProof
	body := `{"date":"2026-11-14","slot_id":"aaaaaaaa-1111-4111-8111-11111111111b","fulfillment_mode":"collection",
		"lines":[{"variant_id":"bbbbbbbb-1111-4111-8111-11111111111b","quantity":1}]`
	guest := body + `,"guest_proof":"` + proof + `"}`
	co := &fakeCheckout{}
	if rec := serveCheckout(t, co, id, http.MethodPost, "/api/v1/checkout/quotes", "", "k", guest, nil); rec.Code != 201 || co.subject != "" || len(co.digest) != 32 {
		t.Fatalf("guest quote: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveCheckout(t, co, id, http.MethodPost, "/api/v1/checkout/quotes", "", "k", body+`}`, nil); rec.Code != 422 {
		t.Fatalf("guest without proof: %d", rec.Code)
	}
	if rec := serveCheckout(t, co, id, http.MethodPost, "/api/v1/checkout/quotes", secretToken, "k", body+`}`, nil); rec.Code != 201 || co.digest != nil {
		t.Fatalf("signed-in quote: %d, digest %x", rec.Code, co.digest)
	}
	if rec := serveCheckout(t, co, id, http.MethodPost, "/api/v1/checkout/quotes", secretToken, "k", guest, nil); rec.Code != 422 {
		t.Fatalf("signed in with a guest proof: %d", rec.Code)
	}
	// A sent but refused token is refused, never treated as a guest.
	if rec := serveCheckout(t, co, id, http.MethodPost, "/api/v1/checkout/quotes", "forged", "k", guest, nil); rec.Code != 401 {
		t.Fatalf("forged token: %d", rec.Code)
	}
	// Reads: the proof header, a bearer, or nothing (404, as for a wrong one).
	co.digest = nil
	if rec := serveCheckout(t, co, id, http.MethodGet, "/api/v1/checkout/quotes/q", "", "", "", map[string]string{QuoteProofHeader: proof}); rec.Code != 200 {
		t.Fatalf("read with proof: %d", rec.Code)
	}
	// The malformed proof cannot occur in hex or base64url log text (a
	// request ID), so the no-proof-in-logs check cannot trip by chance.
	for name, hdr := range map[string]map[string]string{"none": nil, "malformed": {QuoteProofHeader: "not a proof!"}} {
		if rec := serveCheckout(t, co, id, http.MethodGet, "/api/v1/checkout/quotes/q", "", "", "", hdr); rec.Code != 404 {
			t.Errorf("read with %s proof: %d", name, rec.Code)
		}
	}
	// Discount codes: chef writes, technical support reads.
	codeBody := `{"code":"SPRING10","kind":"percentage","basis_points":1000}`
	for name, c := range map[string]struct {
		auth   string
		status int
	}{"support": {"support", 403}, "customer": {secretToken, 403}, "chef": {"chef", 201}} {
		if rec := serveCheckout(t, co, id, http.MethodPost, "/api/v1/admin/discount-codes", c.auth, "k", codeBody, nil); rec.Code != c.status {
			t.Errorf("create code by %s: %d, want %d", name, rec.Code, c.status)
		}
	}
	if rec := serveCheckout(t, co, id, http.MethodGet, "/api/v1/admin/discount-codes", "support", "", "", nil); rec.Code != 200 {
		t.Fatalf("support reads codes: %d", rec.Code)
	}
}

// AC1: a client cannot send a price, currency or oversized quantity; such a
// body is malformed (K00 400: unknown fields), never priced.
func TestQuoteRefusesClientPrices(t *testing.T) {
	id := catalogIdentity()
	line := `{"variant_id":"bbbbbbbb-1111-4111-8111-11111111111b","quantity":1}`
	head := `{"date":"2026-11-14","slot_id":"aaaaaaaa-1111-4111-8111-11111111111b","fulfillment_mode":"collection","guest_proof":"` + testProof + `",`
	for name, c := range map[string]struct {
		body   string
		status int
	}{
		"unit price":   {head + `"lines":[{"variant_id":"bbbbbbbb-1111-4111-8111-11111111111b","quantity":1,"unit_price_minor":1}]}`, 400},
		"total":        {head + `"lines":[` + line + `],"total_minor":1}`, 400},
		"currency":     {head + `"lines":[` + line + `],"currency":"EUR"}`, 400},
		"null total":   {head + `"lines":[` + line + `],"total_minor":null}`, 400},
		"null price":   {head + `"lines":[{"variant_id":"bbbbbbbb-1111-4111-8111-11111111111b","quantity":1,"unit_price_minor":null}]}`, 400},
		"overflow qty": {head + `"lines":[{"variant_id":"bbbbbbbb-1111-4111-8111-11111111111b","quantity":99999999999999999999}]}`, 400},
		"21 cakes":     {head + `"lines":[{"variant_id":"bbbbbbbb-1111-4111-8111-11111111111b","quantity":21}]}`, 422},
	} {
		co := &fakeCheckout{}
		if rec := serveCheckout(t, co, id, http.MethodPost, "/api/v1/checkout/quotes", "", "k", c.body, nil); rec.Code != c.status || co.calls != 0 {
			t.Errorf("%s: %d (calls %d), want %d", name, rec.Code, co.calls, c.status)
		}
	}
	// A token without identity configured is refused, not ignored.
	co := &fakeCheckout{}
	if rec := serveCheckout(t, co, nil, http.MethodPost, "/api/v1/checkout/quotes", "anything", "k", head+`"lines":[`+line+`]}`, nil); rec.Code != 401 || co.calls != 0 {
		t.Fatalf("token without identity: %d, calls %d", rec.Code, co.calls)
	}
	// Browsers may send the quote proof header cross-origin.
	var logs bytes.Buffer
	h := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Checkout: co, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/checkout/quotes/q", nil)
	req.Header.Set("Origin", "https://gkcake.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), QuoteProofHeader) {
		t.Fatalf("preflight allows %q", rec.Header().Get("Access-Control-Allow-Headers"))
	}
}

func TestCheckoutRoutes(t *testing.T) {
	id := catalogIdentity()
	body := `{"quote_id":"aaaaaaaa-1111-4111-8111-11111111111b","quote_version":1,
		"consented_policy_version":"bbbbbbbb-1111-4111-8111-11111111111b",
		"contact":{"full_name":"Test Customer","email":"test@example.com"}`
	co := &fakeCheckout{}
	rec := serveCheckout(t, co, id, http.MethodPost, "/api/v1/checkouts", "", "k", body+`,"guest_proof":"`+testProof+`"}`, nil)
	if rec.Code != 202 || !strings.Contains(rec.Body.String(), `"state":"pending"`) || !strings.Contains(rec.Body.String(), `"poll_after_seconds":3`) ||
		len(co.digest) != 32 {
		t.Fatalf("guest checkout: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveCheckout(t, co, id, http.MethodPost, "/api/v1/checkouts", secretToken, "k", body+`}`, nil); rec.Code != 202 || co.digest != nil {
		t.Fatalf("signed-in checkout: %d", rec.Code)
	}
	for name, c := range map[string]struct {
		body   string
		status int
	}{
		"no proof":        {body + `}`, 422},
		"bad email":       {strings.Replace(body, "test@example.com", "Test <test@example.com>", 1) + `,"guest_proof":"` + testProof + `"}`, 422},
		"padded name":     {strings.Replace(body, `"Test Customer"`, `" Test"`, 1) + `,"guest_proof":"` + testProof + `"}`, 422},
		"version 2":       {strings.Replace(body, `"quote_version":1`, `"quote_version":2`, 1) + `,"guest_proof":"` + testProof + `"}`, 422},
		"client total":    {body + `,"total_minor":1,"guest_proof":"` + testProof + `"}`, 400},
		"unknown contact": {strings.Replace(body, `"email":"test@example.com"`, `"email":"test@example.com","phone":"1"`, 1) + `,"guest_proof":"` + testProof + `"}`, 400},
		// A null on an unknown field is an unknown field (400), not a null (422).
		"null total": {body + `,"total_minor":null,"guest_proof":"` + testProof + `"}`, 400},
		"null phone": {strings.Replace(body, `"email":"test@example.com"`, `"email":"test@example.com","phone":null`, 1) + `,"guest_proof":"` + testProof + `"}`, 400},
		"null proof": {body + `,"guest_proof":null}`, 422},
	} {
		co := &fakeCheckout{}
		if rec := serveCheckout(t, co, id, http.MethodPost, "/api/v1/checkouts", "", "k", c.body, nil); rec.Code != c.status || co.calls != 0 {
			t.Errorf("%s: %d (calls %d), want %d", name, rec.Code, co.calls, c.status)
		}
	}
	// Without CHECKOUT_ENABLED the checkout route is not served.
	var logs bytes.Buffer
	off := NewHandler(Options{DB: fakePinger{}, Identity: id, Checkout: co, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/checkouts", strings.NewReader(body+`}`))
	req.Header.Set("Idempotency-Key", "k")
	offRec := httptest.NewRecorder()
	off.ServeHTTP(offRec, req)
	if offRec.Code != 404 {
		t.Fatalf("checkout while disabled: %d", offRec.Code)
	}
	// Free confirmation: 202 pending (never confirmed here), by capability
	// or bearer; the expected version is required; off with checkout.
	fc := &fakeCheckout{}
	rec = serveCheckout(t, fc, id, http.MethodPost, "/api/v1/orders/o1/free-confirmation", "", "k", `{"expected_version":1}`,
		map[string]string{OrderCapabilityHeader: testProof})
	if rec.Code != 202 || rec.Body.String() != `{"order_id":"o1","operation_id":"op-o1","state":"confirmation_pending","poll_after_seconds":3}`+"\n" ||
		len(fc.digest) != 32 {
		t.Fatalf("guest free confirmation: %d %q", rec.Code, rec.Body.String())
	}
	if rec := serveCheckout(t, fc, id, http.MethodPost, "/api/v1/orders/o1/free-confirmation", secretToken, "k", `{"expected_version":1}`, nil); rec.Code != 202 || fc.digest != nil {
		t.Fatalf("signed-in free confirmation: %d", rec.Code)
	}
	for name, c := range map[string]struct {
		body, auth string
		hdr        map[string]string
		status     int
	}{
		"no capability": {`{"expected_version":1}`, "", nil, 404},
		"no version":    {`{}`, secretToken, nil, 422},
		"version 0":     {`{"expected_version":0}`, secretToken, nil, 422},
		"client amount": {`{"expected_version":1,"total_minor":0}`, secretToken, nil, 400},
		"null version":  {`{"expected_version":null}`, secretToken, nil, 422},
		"unknown null":  {`{"expected_version":1,"total_minor":null}`, secretToken, nil, 400},
		"null+unknown":  {`{"expected_version":null,"total_minor":0}`, secretToken, nil, 400},
	} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, "/api/v1/orders/o1/free-confirmation", c.auth, "k", c.body, c.hdr); rec.Code != c.status || fc.calls != 0 {
			t.Errorf("free confirmation %s: %d (calls %d), want %d", name, rec.Code, fc.calls, c.status)
		}
	}
	if rec := serveCheckout(t, &fakeCheckout{replayed: true}, id, http.MethodPost, "/api/v1/orders/o1/free-confirmation", secretToken, "k",
		`{"expected_version":1}`, nil); rec.Code != 202 || rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replayed free confirmation: %d %v", rec.Code, rec.Header())
	}
	for code, want := range map[string]string{"payment_required": "PAYMENT_REQUIRED", "lead_time": "LEAD_TIME",
		"quote_expired": "QUOTE_EXPIRED", "stale_version": "STALE_VERSION", "state_conflict": "CONFLICT"} {
		rec := serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost,
			"/api/v1/orders/o1/free-confirmation", secretToken, "k", `{"expected_version":1}`, nil)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"`+want+`"`) {
			t.Errorf("%s: %d %s, want 409 %s", code, rec.Code, rec.Body.String(), want)
		}
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/orders/o1/free-confirmation", strings.NewReader(`{"expected_version":1}`))
	req.Header.Set("Idempotency-Key", "k")
	req.Header.Set(OrderCapabilityHeader, testProof)
	offRec = httptest.NewRecorder()
	off.ServeHTTP(offRec, req)
	if offRec.Code != 404 && offRec.Code != 405 {
		t.Fatalf("free confirmation while disabled: %d", offRec.Code)
	}
	// Order reads: the capability header, a bearer, or 404.
	if rec := serveCheckout(t, co, id, http.MethodGet, "/api/v1/orders/o", "", "", "", map[string]string{OrderCapabilityHeader: testProof}); rec.Code != 200 {
		t.Fatalf("read with capability: %d", rec.Code)
	}
	if rec := serveCheckout(t, co, id, http.MethodGet, "/api/v1/orders/o", "", "", "", nil); rec.Code != 404 {
		t.Fatalf("read without capability: %d", rec.Code)
	}
	// Policies: public read, chef record.
	if rec := serveCheckout(t, co, nil, http.MethodGet, "/api/v1/policies/current", "", "", "", nil); rec.Code != 200 {
		t.Fatalf("public policy: %d", rec.Code)
	}
	pol := `{"source_decision_id":"BO-GK-003.refund_policy","source_revision_sha256":"` + strings.Repeat("ab", 32) +
		`","summary":"Terms","effective_at":"2026-11-04T12:00:00Z","approver_evidence":"Trello comment 6aeb3137"}`
	for name, c := range map[string]struct {
		auth   string
		status int
	}{"support": {"support", 403}, "customer": {secretToken, 403}, "chef": {"chef", 201}} {
		if rec := serveCheckout(t, co, id, http.MethodPost, "/api/v1/admin/policies", c.auth, "k", pol, nil); rec.Code != c.status {
			t.Errorf("record policy by %s: %d, want %d", name, rec.Code, c.status)
		}
	}
}

func TestPaymentRoute(t *testing.T) {
	id := catalogIdentity()
	const path = "/api/v1/orders/o1/payment"
	fc := &fakeCheckout{}
	rec := serveCheckout(t, fc, id, http.MethodPost, path, "", "k", `{"expected_version":1}`, map[string]string{OrderCapabilityHeader: testProof})
	if rec.Code != 202 || rec.Body.String() != `{"order_id":"o1","operation_id":"pay-o1","state":"payment_pending","poll_after_seconds":3}`+"\n" ||
		len(fc.digest) != 32 || fc.subject != "" {
		t.Fatalf("guest payment: %d %q", rec.Code, rec.Body.String())
	}
	if rec := serveCheckout(t, fc, id, http.MethodPost, path, secretToken, "k", `{"expected_version":1}`, nil); rec.Code != 202 || fc.digest != nil || fc.subject == "" {
		t.Fatalf("signed-in payment: %d", rec.Code)
	}
	for name, c := range map[string]struct {
		body, auth, key string
		hdr             map[string]string
		status          int
	}{
		"no capability":  {`{"expected_version":1}`, "", "k", nil, 404},
		"bad capability": {`{"expected_version":1}`, "", "k", map[string]string{OrderCapabilityHeader: "short"}, 404},
		"no key":         {`{"expected_version":1}`, secretToken, "", nil, 400},
		"no version":     {`{}`, secretToken, "k", nil, 422},
		"client amount":  {`{"expected_version":1,"amount_minor":1}`, secretToken, "k", nil, 400},
		"not an object":  {`[1]`, secretToken, "k", nil, 400},
	} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, path, c.auth, c.key, c.body, c.hdr); rec.Code != c.status || fc.calls != 0 {
			t.Errorf("payment %s: %d (calls %d), want %d", name, rec.Code, fc.calls, c.status)
		}
	}
	if rec := serveCheckout(t, &fakeCheckout{replayed: true}, id, http.MethodPost, path, secretToken, "k", `{"expected_version":1}`, nil); rec.Code != 202 ||
		rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replayed payment: %d %v", rec.Code, rec.Header())
	}
	for code, want := range map[string]string{"payment_in_progress": "PAYMENT_IN_PROGRESS", "lead_time": "LEAD_TIME",
		"quote_expired": "QUOTE_EXPIRED", "stale_version": "STALE_VERSION", "state_conflict": "CONFLICT", "key_reused": "KEY_REUSED"} {
		rec := serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost, path, secretToken, "k",
			`{"expected_version":1}`, nil)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"`+want+`"`) {
			t.Errorf("%s: %d %s, want 409 %s", code, rec.Code, rec.Body.String(), want)
		}
	}
	if rec := serveCheckout(t, &fakeCheckout{freeErr: checkout.ErrPaymentsUnavailable}, id, http.MethodPost, path, secretToken, "k",
		`{"expected_version":1}`, nil); rec.Code != 503 {
		t.Fatalf("payments not configured: %d", rec.Code)
	}
	if rec := serveCheckout(t, &fakeCheckout{freeErr: checkout.ErrRecoveryFrozen}, id, http.MethodPost, path, secretToken, "k",
		`{"expected_version":1}`, nil); rec.Code != 503 || !strings.Contains(rec.Body.String(), `"retryable":true`) {
		t.Fatalf("restore freeze: %d %s", rec.Code, rec.Body.String())
	}
	off := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id, Checkout: &fakeCheckout{},
		Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))})
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"expected_version":1}`))
	req.Header.Set("Idempotency-Key", "k")
	req.Header.Set(OrderCapabilityHeader, testProof)
	offRec := httptest.NewRecorder()
	off.ServeHTTP(offRec, req)
	if offRec.Code != 404 && offRec.Code != 405 {
		t.Fatalf("payment while checkout is disabled: %d", offRec.Code)
	}
}

type fakeStripeWebhook struct {
	err   error
	calls int
}

func (f *fakeStripeWebhook) Receive(context.Context, http.Header, []byte) (string, error) {
	f.calls++
	return "recorded", f.err
}

func TestStripeWebhookRoute(t *testing.T) {
	serve := func(rcv StripeWebhook, body string) *httptest.ResponseRecorder {
		var logs bytes.Buffer
		h := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, StripeWebhook: rcv, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(body)))
		if strings.Contains(logs.String(), "evt_secret_body") {
			t.Fatalf("body logged: %s", logs.String())
		}
		return rec
	}
	for name, c := range map[string]struct {
		err    error
		status int
	}{
		"stored":        {nil, 200},
		"bad signature": {payments.ErrInvalidSignature, 400},
		"malformed":     {payments.ErrMalformedEvent, 400},
		"database down": {errors.New("db"), 503},
	} {
		if rec := serve(&fakeStripeWebhook{err: c.err}, `{"id":"evt_secret_body"}`); rec.Code != c.status {
			t.Errorf("%s: %d, want %d", name, rec.Code, c.status)
		}
	}
	rcv := &fakeStripeWebhook{}
	if rec := serve(rcv, strings.Repeat("x", payments.MaxWebhookBytes+1)); rec.Code != 413 || rcv.calls != 0 {
		t.Fatalf("oversized: %d (calls %d)", rec.Code, rcv.calls)
	}
	if rec := serve(nil, `{}`); rec.Code != 404 {
		t.Fatalf("without a secret: %d, want 404", rec.Code)
	}
}

func TestCancellationRoutes(t *testing.T) {
	id := catalogIdentity()
	guest := map[string]string{OrderCapabilityHeader: testProof}
	const preview = "/api/v1/orders/o1/cancellation-preview"
	const cancel = "/api/v1/orders/o1/cancel"
	body := `{"expected_version":2,"preview_id":"00000000-0000-4000-8000-000000000001","preview_hash":"` + strings.Repeat("ab", 32) + `"}`
	fc := &fakeCheckout{}
	if rec := serveCheckout(t, fc, id, http.MethodPost, preview, "", "", "", guest); rec.Code != 200 || len(fc.digest) != 32 ||
		rec.Body.String() != `{"rate":100}`+"\n" {
		t.Fatalf("guest preview: %d %q", rec.Code, rec.Body.String())
	}
	if rec := serveCheckout(t, fc, id, http.MethodPost, preview, secretToken, "", "{}", nil); rec.Code != 200 || fc.digest != nil || fc.subject == "" {
		t.Fatalf("signed-in preview: %d", rec.Code)
	}
	for name, c := range map[string]struct {
		body string
		hdr  map[string]string
	}{"no capability": {"", nil}, "a body": {`{"rate":100}`, guest}} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, preview, "", "", c.body, c.hdr); rec.Code/100 != 4 || fc.calls != 0 {
			t.Errorf("preview %s: %d (calls %d)", name, rec.Code, fc.calls)
		}
	}
	fc = &fakeCheckout{}
	rec := serveCheckout(t, fc, id, http.MethodPost, cancel, "", "k", body, guest)
	if rec.Code != 202 || rec.Body.String() != `{"order_id":"o1","operation_id":"cancel-o1","state":"cancellation_pending","poll_after_seconds":3}`+"\n" {
		t.Fatalf("guest cancel: %d %q", rec.Code, rec.Body.String())
	}
	for name, c := range map[string]struct {
		body, key string
		hdr       map[string]string
		status    int
	}{
		"no capability": {body, "k", nil, 404},
		"no key":        {body, "", guest, 400},
		"no preview":    {`{"expected_version":2}`, "k", guest, 422},
		"bad hash":      {`{"expected_version":2,"preview_id":"00000000-0000-4000-8000-000000000001","preview_hash":"x"}`, "k", guest, 422},
		"client amount": {`{"expected_version":2,"amount_minor":1}`, "k", guest, 400},
	} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, cancel, "", c.key, c.body, c.hdr); rec.Code != c.status || fc.calls != 0 {
			t.Errorf("cancel %s: %d (calls %d), want %d", name, rec.Code, fc.calls, c.status)
		}
	}
	for code, want := range map[string]string{"preview_stale": "PREVIEW_STALE", "cancellation_not_eligible": "CANCELLATION_NOT_ELIGIBLE",
		"stale_version": "STALE_VERSION", "key_reused": "KEY_REUSED"} {
		rec := serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost, cancel, "", "k", body, guest)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"`+want+`"`) {
			t.Errorf("%s: %d %s, want 409 %s", code, rec.Code, rec.Body.String(), want)
		}
	}
	// Chef cancellation: the chef only.
	const chefPath = "/api/v1/admin/orders/o1/cancel"
	chefBody := `{"expected_version":2,"reason":"Oven failure"}`
	for name, c := range map[string]struct {
		auth   string
		status int
	}{"support": {"support", 403}, "customer": {secretToken, 403}, "guest": {"", 401}, "chef": {"chef", 202}} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, chefPath, c.auth, "k", chefBody, nil); rec.Code != c.status {
			t.Errorf("chef cancel by %s: %d, want %d", name, rec.Code, c.status)
		}
	}
	if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, chefPath, "chef", "k", `{"expected_version":2,"reason":" x"}`, nil); rec.Code != 422 {
		t.Fatalf("untrimmed reason: %d", rec.Code)
	}
	// Disabled checkout: no cancellation routes.
	off := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id, Checkout: &fakeCheckout{},
		Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))})
	for _, p := range []string{preview, cancel, chefPath} {
		req := httptest.NewRequest(http.MethodPost, p, strings.NewReader(chefBody))
		req.Header.Set("Idempotency-Key", "k")
		req.Header.Set("Authorization", "Bearer chef")
		rec := httptest.NewRecorder()
		off.ServeHTTP(rec, req)
		if rec.Code != 404 && rec.Code != 405 {
			t.Errorf("%s while disabled: %d", p, rec.Code)
		}
	}
}

func TestProductionAndLatePaymentRoutes(t *testing.T) {
	id := catalogIdentity()
	id.actors["fulfil"] = identity.Actor{AccountID: "acc-ful", Subject: "sub-ful", Capabilities: []string{identity.Fulfillment}}
	const start = "/api/v1/admin/orders/o1/start-production"
	for name, c := range map[string]struct {
		auth   string
		status int
	}{"support": {"support", 403}, "customer": {secretToken, 403}, "chef": {"chef", 200}, "fulfillment": {"fulfil", 200}} {
		if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, start, c.auth, "k", `{"expected_version":3}`, nil); rec.Code != c.status {
			t.Errorf("start production by %s: %d, want %d", name, rec.Code, c.status)
		}
	}
	rec := serveCheckout(t, &fakeCheckout{replayed: true}, id, http.MethodPost, start, "chef", "k", `{"expected_version":3}`, nil)
	if rec.Code != 200 || rec.Header().Get("Idempotent-Replayed") != "true" ||
		rec.Body.String() != `{"lifecycle":"cancelled","order_id":"o1"}`+"\n" {
		t.Fatalf("replayed start: %d %q", rec.Code, rec.Body.String())
	}
	if rec := serveCheckout(t, &fakeCheckout{freeErr: checkout.ErrOrdersFrozen}, id, http.MethodPost, start, "chef", "k",
		`{"expected_version":3}`, nil); rec.Code != 503 {
		t.Fatalf("frozen start: %d", rec.Code)
	}
	const late = "/api/v1/admin/orders/o1/late-payment"
	honour := `{"expected_version":3,"decision":"honour","customer_agreement":{"channel":"phone","agreed_at":"2026-11-05T09:00:00Z","evidence_ref":"Call note 12"}}`
	for name, c := range map[string]struct {
		auth, body string
		status     int
	}{
		"fulfillment":         {"fulfil", honour, 403},
		"chef honour":         {"chef", honour, 202},
		"chef full return":    {"chef", `{"expected_version":3,"decision":"full_return"}`, 202},
		"honour without":      {"chef", `{"expected_version":3,"decision":"honour"}`, 422},
		"no channel":          {"chef", `{"expected_version":3,"decision":"honour","customer_agreement":{"agreed_at":"2026-11-05T09:00:00Z","evidence_ref":"x"}}`, 422},
		"no evidence":         {"chef", `{"expected_version":3,"decision":"honour","customer_agreement":{"channel":"phone","agreed_at":"2026-11-05T09:00:00Z"}}`, 422},
		"bad time":            {"chef", `{"expected_version":3,"decision":"honour","customer_agreement":{"channel":"phone","agreed_at":"yesterday","evidence_ref":"x"}}`, 422},
		"return with consent": {"chef", `{"expected_version":3,"decision":"full_return","customer_agreement":{"channel":"phone","agreed_at":"2026-11-05T09:00:00Z","evidence_ref":"x"}}`, 422},
		"half":                {"chef", `{"expected_version":3,"decision":"half_return"}`, 422},
	} {
		if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, late, c.auth, "k", c.body, nil); rec.Code != c.status {
			t.Errorf("late payment %s: %d %s, want %d", name, rec.Code, rec.Body.String(), c.status)
		}
	}
}

func TestChefRefundRoutes(t *testing.T) {
	id := catalogIdentity()
	id.actors["fulfil"] = identity.Actor{AccountID: "acc-ful", Subject: "sub-ful", Capabilities: []string{identity.Fulfillment}}
	const refunds = "/api/v1/admin/orders/o1/refunds"
	const c1, c2 = "aaaaaaaa-1111-4111-8111-11111111111b", "22222222-2222-4222-8222-222222222222"
	good := `{"expected_version":3,"amount_minor":2500,"currency":"GBP","allocations":[{"collection_id":"` + c1 +
		`","amount_minor":2000},{"collection_id":"` + c2 + `","amount_minor":500}],"reason":"Agreed adjustment"}`
	for name, c := range map[string]struct {
		auth, body string
		status     int
	}{
		"support":        {"support", good, 403},
		"fulfillment":    {"fulfil", good, 403},
		"customer":       {secretToken, good, 403},
		"chef":           {"chef", good, 202},
		"no version":     {"chef", strings.Replace(good, `"expected_version":3`, `"expected_version":0`, 1), 422},
		"zero":           {"chef", strings.Replace(good, `"amount_minor":2500`, `"amount_minor":0`, 1), 422},
		"euro":           {"chef", strings.Replace(good, `"GBP"`, `"EUR"`, 1), 422},
		"wrong sum":      {"chef", strings.Replace(good, `"amount_minor":2500`, `"amount_minor":2400`, 1), 422},
		"duplicate":      {"chef", strings.Replace(good, c2, c1, 1), 422},
		"upper case":     {"chef", strings.Replace(good, c1, strings.ToUpper(c1), 1), 422},
		"no allocations": {"chef", `{"expected_version":3,"amount_minor":2500,"currency":"GBP","allocations":[],"reason":"x"}`, 422},
		"no reason":      {"chef", strings.Replace(good, `"Agreed adjustment"`, `""`, 1), 422},
		"two lines":      {"chef", strings.Replace(good, `"Agreed adjustment"`, `"Agreed\nadjustment"`, 1), 422},
		"unknown field":  {"chef", strings.Replace(good, `"currency"`, `"note":"x","currency"`, 1), 400},
		"null reason":    {"chef", strings.Replace(good, `"Agreed adjustment"`, `null`, 1), 422},
	} {
		if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, refunds, c.auth, "k", c.body, nil); rec.Code != c.status {
			t.Errorf("refund %s: %d %s, want %d", name, rec.Code, rec.Body.String(), c.status)
		}
	}
	rec := serveCheckout(t, &fakeCheckout{replayed: true}, id, http.MethodPost, refunds, "chef", "k", good, nil)
	if rec.Code != 202 || rec.Header().Get("Idempotent-Replayed") != "true" ||
		rec.Body.String() != `{"order_id":"o1","operation_id":"exc-o1","state":"refund_pending","poll_after_seconds":3}`+"\n" {
		t.Fatalf("replayed refund: %d %q", rec.Code, rec.Body.String())
	}
	if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, refunds, "chef", "", good, nil); rec.Code != 400 {
		t.Fatalf("no key: %d", rec.Code)
	}
	for code, want := range map[string]string{"refund_limit": "REFUND_LIMIT", "reconciliation_required": "RECONCILIATION_REQUIRED",
		"refund_in_progress": "REFUND_IN_PROGRESS", "no_refund_pending": "NO_REFUND_PENDING"} {
		rec := serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost, refunds, "chef", "k", good, nil)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"`+want+`"`) {
			t.Errorf("%s: %d %s", code, rec.Code, rec.Body.String())
		}
	}
	if rec := serveCheckout(t, &fakeCheckout{freeErr: checkout.ErrOrdersFrozen}, id, http.MethodPost, refunds, "chef", "k", good, nil); rec.Code != 503 {
		t.Fatalf("frozen refund: %d", rec.Code)
	}
	const delivery = "/api/v1/admin/orders/o1/delivery-refund"
	body := `{"expected_version":3,"collection_id":"` + c2 + `"}`
	for name, c := range map[string]struct {
		auth, body string
		status     int
	}{
		"support":       {"support", body, 403},
		"fulfillment":   {"fulfil", body, 403},
		"chef":          {"chef", body, 202},
		"no collection": {"chef", `{"expected_version":3}`, 422},
		"bad id":        {"chef", `{"expected_version":3,"collection_id":"c2"}`, 422},
		"no version":    {"chef", `{"collection_id":"` + c2 + `"}`, 422},
		"extra":         {"chef", `{"expected_version":3,"collection_id":"` + c2 + `","amount_minor":1}`, 400},
	} {
		if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, delivery, c.auth, "k", c.body, nil); rec.Code != c.status {
			t.Errorf("delivery refund %s: %d %s, want %d", name, rec.Code, rec.Body.String(), c.status)
		}
	}
	if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, delivery, "chef", "k", body, nil); rec.Body.String() !=
		`{"order_id":"o1","operation_id":"op-o1","state":"refund_pending","poll_after_seconds":3}`+"\n" {
		t.Fatalf("delivery refund answer %q", rec.Body.String())
	}
}

func TestRecoveryToolRoutes(t *testing.T) {
	id := catalogIdentity()
	id.actors["fulfil"] = identity.Actor{AccountID: "acc-ful", Subject: "sub-ful", Capabilities: []string{identity.Fulfillment}}
	const op = "bbbbbbbb-1111-4111-8111-11111111111c"
	base := "/api/v1/admin/orders/o1/"
	for name, c := range map[string]struct {
		path, auth, body string
		status           int
	}{
		"match":              {"refund-observations/x/resolve", "chef", `{"expected_version":2,"resolution":"match","refund_operation_id":"` + op + `","note":"Same refund"}`, 200},
		"accept":             {"refund-observations/x/resolve", "chef", `{"expected_version":2,"resolution":"accept_external","note":"Dashboard refund"}`, 200},
		"accept with op":     {"refund-observations/x/resolve", "chef", `{"expected_version":2,"resolution":"accept_external","refund_operation_id":"` + op + `","note":"x"}`, 422},
		"match without op":   {"refund-observations/x/resolve", "chef", `{"expected_version":2,"resolution":"match","note":"x"}`, 422},
		"delete":             {"refund-observations/x/resolve", "chef", `{"expected_version":2,"resolution":"delete","note":"x"}`, 422},
		"no note":            {"refund-observations/x/resolve", "chef", `{"expected_version":2,"resolution":"accept_external"}`, 422},
		"support resolve":    {"refund-observations/x/resolve", "support", `{"expected_version":2,"resolution":"accept_external","note":"x"}`, 403},
		"failed":             {"refund-operations/x/decide", "chef", `{"expected_version":2,"decision":"failed","note":"Complete list"}`, 200},
		"succeeded":          {"refund-operations/x/decide", "chef", `{"expected_version":2,"decision":"succeeded","provider_refund_id":"re_123","note":"Dashboard"}`, 200},
		"succeeded no id":    {"refund-operations/x/decide", "chef", `{"expected_version":2,"decision":"succeeded","note":"x"}`, 422},
		"failed with id":     {"refund-operations/x/decide", "chef", `{"expected_version":2,"decision":"failed","provider_refund_id":"re_1","note":"x"}`, 422},
		"bad id":             {"refund-operations/x/decide", "chef", `{"expected_version":2,"decision":"succeeded","provider_refund_id":"ch_1","note":"x"}`, 422},
		"fulfillment decide": {"refund-operations/x/decide", "fulfil", `{"expected_version":2,"decision":"failed","note":"x"}`, 403},
		"keep":               {"refund-operations/x/clear-mismatch", "chef", `{"expected_version":2,"decision":"keep","note":"Stale sighting"}`, 200},
		"other":              {"refund-operations/x/clear-mismatch", "chef", `{"expected_version":2,"decision":"ignore","note":"x"}`, 422},
		"payment":            {"payment-collections/x/clear-mismatch", "chef", `{"expected_version":2,"note":"Checked"}`, 200},
		"payment no note":    {"payment-collections/x/clear-mismatch", "chef", `{"expected_version":2}`, 422},
		"re-reserve":         {"refund-operations/x/re-reserve", "chef", `{"expected_version":2}`, 202},
		"re-reserve note":    {"refund-operations/x/re-reserve", "chef", `{"expected_version":2,"note":"Card replaced"}`, 202},
		"re-reserve support": {"refund-operations/x/re-reserve", "support", `{"expected_version":2}`, 403},
	} {
		if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, base+c.path, c.auth, "k", c.body, nil); rec.Code != c.status {
			t.Errorf("%s: %d %s, want %d", name, rec.Code, rec.Body.String(), c.status)
		}
	}
	rec := serveCheckout(t, &fakeCheckout{replayed: true}, id, http.MethodPost, base+"refund-operations/x/decide", "chef", "k",
		`{"expected_version":2,"decision":"failed","note":"x"}`, nil)
	if rec.Code != 200 || rec.Header().Get("Idempotent-Replayed") != "true" || rec.Body.String() != `{"decision_id":"dec-1","order_id":"o1"}`+"\n" {
		t.Fatalf("replayed decision: %d %q", rec.Code, rec.Body.String())
	}
	for code, want := range map[string]string{"identity_unproven": "IDENTITY_UNPROVEN", "window_open": "WINDOW_OPEN", "evidence_required": "EVIDENCE_REQUIRED"} {
		rec := serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost, base+"refund-operations/x/decide",
			"chef", "k", `{"expected_version":2,"decision":"failed","note":"x"}`, nil)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"`+want+`"`) {
			t.Errorf("%s: %d %s", code, rec.Code, rec.Body.String())
		}
	}
	if rec := serveCheckout(t, &fakeCheckout{freeErr: checkout.ErrOrdersFrozen}, id, http.MethodPost, base+"refund-observations/x/resolve", "chef", "k",
		`{"expected_version":2,"resolution":"accept_external","note":"x"}`, nil); rec.Code != 503 {
		t.Fatalf("frozen: %d", rec.Code)
	}
	for name, c := range map[string]struct {
		auth   string
		status int
	}{"chef": {"chef", 200}, "support": {"support", 200}, "fulfillment": {"fulfil", 403}, "customer": {secretToken, 403}} {
		rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodGet, "/api/v1/admin/attention", c.auth, "", "", nil)
		if rec.Code != c.status {
			t.Errorf("attention by %s: %d", name, rec.Code)
		}
		if c.status == 200 && rec.Body.String() != `{"items":[{"kind":"delivery_pending"}]}`+"\n" {
			t.Errorf("attention body %q", rec.Body.String())
		}
	}
}

func TestManualPayoutRoutes(t *testing.T) {
	id := catalogIdentity()
	id.actors["fulfil"] = identity.Actor{AccountID: "acc-ful", Subject: "sub-ful", Capabilities: []string{identity.Fulfillment}}
	const coll = "cccccccc-1111-4111-8111-11111111111d"
	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	base := "/api/v1/admin/orders/o1/manual-payouts"
	preview := `{"collection_id":"` + coll + `","unavailable_evidence_ref":"stripe-failure/case-1","recipient_evidence_ref":"recipient/case-1"}`
	success := `{"expected_version":4,"outcome":"succeeded","transfer_identity":"bank-statement/line-3","evidence_ref":"evidence/1","amount_minor":5000,"currency":"GBP"}`
	for name, c := range map[string]struct {
		path, auth, key, body string
		status                int
	}{
		"preview":          {"/preview", "chef", "", preview, 200},
		"preview support":  {"/preview", "support", "", preview, 403},
		"bank details":     {"/preview", "chef", "", strings.Replace(preview, "recipient/case-1", "12-34-56 12345678", 1), 422},
		"iban":             {"/preview", "chef", "", strings.Replace(preview, "recipient/case-1", "GB33BUKB20201555555555", 1), 422},
		"spaced iban":      {"/preview", "chef", "", strings.Replace(preview, "recipient/case-1", "GB33 BUKB 2020 1555 5555 55", 1), 422},
		"spaced digits":    {"/preview", "chef", "", strings.Replace(preview, "stripe-failure/case-1", "acct 12-34-56", 1), 422},
		"withdrawn":        {"/op1/outcome", "chef", "k", `{"expected_version":4,"outcome":"withdrawn","evidence_ref":"note/1"}`, 202},
		"withdrawn bare":   {"/op1/outcome", "chef", "k", `{"expected_version":4,"outcome":"withdrawn"}`, 422},
		"preview extra":    {"/preview", "chef", "", strings.Replace(preview, `"collection_id"`, `"iban":"x","collection_id"`, 1), 400},
		"reserve":          {"", "chef", "k", `{"expected_version":4,"preview_id":"` + coll + `","preview_hash":"` + hash + `"}`, 202},
		"reserve no key":   {"", "chef", "", `{"expected_version":4,"preview_id":"` + coll + `","preview_hash":"` + hash + `"}`, 400},
		"reserve bad hash": {"", "chef", "k", `{"expected_version":4,"preview_id":"` + coll + `","preview_hash":"x"}`, 422},
		"reserve fulfil":   {"", "fulfil", "k", `{"expected_version":4,"preview_id":"` + coll + `","preview_hash":"` + hash + `"}`, 403},
		"submitted":        {"/op1/outcome", "chef", "k", `{"expected_version":4,"outcome":"submitted"}`, 202},
		"succeeded":        {"/op1/outcome", "chef", "k", success, 202},
		"success no id":    {"/op1/outcome", "chef", "k", strings.Replace(success, `"transfer_identity":"bank-statement/line-3",`, "", 1), 422},
		"success euro":     {"/op1/outcome", "chef", "k", strings.Replace(success, `"GBP"`, `"EUR"`, 1), 422},
		"account number":   {"/op1/outcome", "chef", "k", strings.Replace(success, "bank-statement/line-3", "acct 12345678", 1), 422},
		"failed no proof":  {"/op1/outcome", "chef", "k", `{"expected_version":4,"outcome":"failed"}`, 422},
		"submitted amount": {"/op1/outcome", "chef", "k", `{"expected_version":4,"outcome":"submitted","amount_minor":5000}`, 422},
		"paid":             {"/op1/outcome", "chef", "k", `{"expected_version":4,"outcome":"paid"}`, 422},
	} {
		if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, base+c.path, c.auth, c.key, c.body, nil); rec.Code != c.status {
			t.Errorf("%s: %d %s, want %d", name, rec.Code, rec.Body.String(), c.status)
		}
	}
	for name, c := range map[string]struct {
		auth   string
		status int
	}{"chef": {"chef", 200}, "support": {"support", 403}, "fulfillment": {"fulfil", 403}} {
		if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodGet, base, c.auth, "", "", nil); rec.Code != c.status {
			t.Errorf("list by %s: %d", name, rec.Code)
		}
	}
}

// Staff order reads: chef, technical support and fulfillment (each view is
// redacted by the database); customers and anonymous callers are refused.
// The list query is parsed strictly; reads need no Idempotency-Key and are
// served even when new checkouts are disabled.
func TestStaffOrderRoutes(t *testing.T) {
	id := catalogIdentity()
	id.actors["fulfil"] = identity.Actor{AccountID: "acc-ful", Subject: "sub-ful", Capabilities: []string{identity.Fulfillment}}
	const list = "/api/v1/admin/orders?from=2026-11-01&to=2026-11-30"
	for name, c := range map[string]struct {
		auth   string
		status int
	}{"chef": {"chef", 200}, "support": {"support", 200}, "fulfillment": {"fulfil", 200}, "customer": {secretToken, 403},
		"anonymous": {"", 401}} {
		co := &fakeCheckout{}
		rec := serveCheckout(t, co, id, http.MethodGet, list, c.auth, "", "", nil)
		if rec.Code != c.status {
			t.Errorf("list by %s: %d %s", name, rec.Code, rec.Body.String())
		}
		if c.status == 200 && (rec.Body.String() != `{"items":[{"id":"o1"}],"next_cursor":"next"}`+"\n" || co.subject != id.actors[c.auth].Subject ||
			rec.Header().Get("Cache-Control") != "no-store") {
			t.Errorf("list by %s: %q %q %s", name, rec.Body.String(), co.subject, rec.Header().Get("Cache-Control"))
		}
		rec = serveCheckout(t, co, id, http.MethodGet, "/api/v1/admin/orders/o9", c.auth, "", "", nil)
		if rec.Code != c.status || (c.status == 200 && (rec.Body.String() != `{"id":"o9"}`+"\n" || co.orderID != "o9")) {
			t.Errorf("detail by %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	co := &fakeCheckout{}
	rec := serveCheckout(t, co, id, http.MethodGet,
		list+"&lifecycle=confirmed,cancellation_pending&lifecycle=ready&needs_attention=false&limit=100&cursor=abc", "chef", "", "", nil)
	f := co.filter
	if rec.Code != 200 || f.From.Format(time.DateOnly) != "2026-11-01" || f.To.Format(time.DateOnly) != "2026-11-30" ||
		strings.Join(f.Lifecycles, " ") != "confirmed cancellation_pending ready" || f.NeedsAttention == nil || *f.NeedsAttention ||
		f.Limit != 100 || f.Cursor != "abc" {
		t.Fatalf("filter %d %+v", rec.Code, f)
	}
	serveCheckout(t, co, id, http.MethodGet, list+"&needs_attention=true", "chef", "", "", nil)
	if co.filter.NeedsAttention == nil || !*co.filter.NeedsAttention || co.filter.Limit != 0 || co.filter.Lifecycles != nil {
		t.Fatalf("needs attention %+v", co.filter)
	}
	for name, query := range map[string]string{
		"no from":         "?to=2026-11-30",
		"no to":           "?from=2026-11-01",
		"bad date":        "?from=2026-11-01&to=2026-11-31",
		"reversed":        "?from=2026-11-30&to=2026-11-01",
		"94 days":         "?from=2026-11-01&to=2027-02-02",
		"lifecycle":       "&lifecycle=paid",
		"empty lifecycle": "&lifecycle=",
		"repeated":        "&lifecycle=ready,ready",
		"attention":       "&needs_attention=yes",
		"limit 0":         "&limit=0",
		"limit 101":       "&limit=101",
		"limit empty":     "&limit=",
		"limit sign":      "&limit=+5",
	} {
		path := "/api/v1/admin/orders" + query
		if strings.HasPrefix(query, "&") {
			path = list + query
		}
		co := &fakeCheckout{}
		if rec := serveCheckout(t, co, id, http.MethodGet, path, "chef", "", "", nil); rec.Code != 422 || co.calls != 0 {
			t.Errorf("%s: %d %s (calls %d)", name, rec.Code, rec.Body.String(), co.calls)
		}
	}
	if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodGet, "/api/v1/admin/orders?from=2026-11-01&to=2027-02-01", "chef", "", "", nil); rec.Code != 200 {
		t.Fatalf("93 days: %d %s", rec.Code, rec.Body.String())
	}
	for name, c := range map[string]struct {
		err    error
		status int
	}{"bad cursor": {catalog.ErrBadCursor, 400}, "not found": {catalog.ErrNotFound, 404}, "forbidden": {catalog.ErrForbidden, 403},
		"invalid": {&catalog.InvalidError{Field: "to", Message: "x"}, 422}} {
		if rec := serveCheckout(t, &fakeCheckout{freeErr: c.err}, id, http.MethodGet, list, "chef", "", "", nil); rec.Code != c.status {
			t.Errorf("list %s: %d", name, rec.Code)
		}
		if rec := serveCheckout(t, &fakeCheckout{freeErr: c.err}, id, http.MethodGet, "/api/v1/admin/orders/o1", "chef", "", "", nil); rec.Code != c.status {
			t.Errorf("detail %s: %d", name, rec.Code)
		}
	}
	// Served without CHECKOUT_ENABLED.
	var logs bytes.Buffer
	h := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id, Checkout: &fakeCheckout{},
		Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/orders/o1", nil)
	req.Header.Set("Authorization", "Bearer fulfil")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("checkout disabled: %d", rec.Code)
	}
}

// GK-031 B1: the customer's same-day pickup move and the chef's agreed
// date/time amendment (K06).
func TestAppointmentRoutes(t *testing.T) {
	id := catalogIdentity()
	id.actors["fulfil"] = identity.Actor{AccountID: "acc-ful", Subject: "sub-ful", Capabilities: []string{identity.Fulfillment}}
	guest := map[string]string{OrderCapabilityHeader: testProof}
	const move = "/api/v1/orders/o1/pickup"
	body := `{"target_slot_id":"aaaaaaaa-1111-4111-8111-11111111111b","expected_version":2}`
	fc := &fakeCheckout{}
	rec := serveCheckout(t, fc, id, http.MethodPost, move, "", "k", body, guest)
	if rec.Code != 202 || len(fc.digest) != 32 || fc.orderID != "o1" ||
		rec.Body.String() != `{"order_id":"o1","operation_id":"move-o1","state":"move_pending","poll_after_seconds":3}`+"\n" {
		t.Fatalf("guest move: %d %q", rec.Code, rec.Body.String())
	}
	if rec := serveCheckout(t, fc, id, http.MethodPost, move, secretToken, "k", body, nil); rec.Code != 202 || fc.digest != nil || fc.subject == "" {
		t.Fatalf("signed-in move: %d", rec.Code)
	}
	rec = serveCheckout(t, &fakeCheckout{unchanged: true, replayed: true}, id, http.MethodPost, move, "", "k", body, guest)
	if rec.Code != 200 || rec.Header().Get("Idempotent-Replayed") != "true" ||
		rec.Body.String() != `{"order_id":"o1","state":"unchanged"}`+"\n" {
		t.Fatalf("same slot: %d %q", rec.Code, rec.Body.String())
	}
	for name, c := range map[string]struct {
		body, key string
		hdr       map[string]string
		status    int
	}{
		"no capability":   {body, "k", nil, 404},
		"no key":          {body, "", guest, 400},
		"no slot":         {`{"expected_version":2}`, "k", guest, 422},
		"bad slot":        {`{"target_slot_id":"x","expected_version":2}`, "k", guest, 422},
		"no version":      {`{"target_slot_id":"aaaaaaaa-1111-4111-8111-11111111111b"}`, "k", guest, 422},
		"client date":     {`{"target_slot_id":"aaaaaaaa-1111-4111-8111-11111111111b","expected_version":2,"date":"2026-11-15"}`, "k", guest, 400},
		"upper-case slot": {`{"target_slot_id":"AAAAAAAA-1111-4111-8111-11111111111B","expected_version":2}`, "k", guest, 422},
	} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, move, "", c.key, c.body, c.hdr); rec.Code != c.status || fc.calls != 0 {
			t.Errorf("move %s: %d (calls %d), want %d", name, rec.Code, fc.calls, c.status)
		}
	}
	for code, want := range map[string]string{"move_too_late": "MOVE_TOO_LATE", "move_not_eligible": "MOVE_NOT_ELIGIBLE",
		"slot_unavailable": "SLOT_UNAVAILABLE", "stale_version": "STALE_VERSION", "key_reused": "KEY_REUSED"} {
		rec := serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost, move, "", "k", body, guest)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"`+want+`"`) {
			t.Errorf("%s: %d %s, want 409 %s", code, rec.Code, rec.Body.String(), want)
		}
	}
	if rec := serveCheckout(t, &fakeCheckout{freeErr: checkout.ErrOrdersFrozen}, id, http.MethodPost, move, "", "k", body, guest); rec.Code != 503 {
		t.Fatalf("frozen move: %d", rec.Code)
	}

	// The chef's amendment: chef only, agreement required.
	const chefPath = "/api/v1/admin/orders/o1/appointment"
	chefBody := `{"expected_version":2,"target_date":"2026-11-21","target_slot_id":"aaaaaaaa-1111-4111-8111-11111111111b",
		"customer_agreement":{"channel":"phone","agreed_at":"2026-11-05T09:00:00Z","evidence_ref":"Call 5 Nov"},
		"feasibility_note":"Fresh bake for the new date"}`
	for name, c := range map[string]struct {
		auth   string
		status int
	}{"support": {"support", 403}, "fulfillment": {"fulfil", 403}, "customer": {secretToken, 403}, "guest": {"", 401}, "chef": {"chef", 202}} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, chefPath, c.auth, "k", chefBody, nil); rec.Code != c.status {
			t.Errorf("amendment by %s: %d %s, want %d", name, rec.Code, rec.Body.String(), c.status)
		}
	}
	rec = serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, chefPath, "chef", "k", chefBody, nil)
	if rec.Body.String() != `{"order_id":"o1","operation_id":"appointment-o1","state":"appointment_change_pending","poll_after_seconds":3}`+"\n" {
		t.Fatalf("amendment answer %q", rec.Body.String())
	}
	for name, b := range map[string]string{
		"no agreement":  `{"expected_version":2,"target_date":"2026-11-21","target_slot_id":"aaaaaaaa-1111-4111-8111-11111111111b","feasibility_note":"ok"}`,
		"no note":       `{"expected_version":2,"target_date":"2026-11-21","target_slot_id":"aaaaaaaa-1111-4111-8111-11111111111b","customer_agreement":{"channel":"phone","agreed_at":"2026-11-05T09:00:00Z","evidence_ref":"Call"}}`,
		"bad date":      `{"expected_version":2,"target_date":"2026-02-30","target_slot_id":"aaaaaaaa-1111-4111-8111-11111111111b","customer_agreement":{"channel":"phone","agreed_at":"2026-11-05T09:00:00Z","evidence_ref":"Call"},"feasibility_note":"ok"}`,
		"bad channel":   `{"expected_version":2,"target_date":"2026-11-21","target_slot_id":"aaaaaaaa-1111-4111-8111-11111111111b","customer_agreement":{"channel":"silence","agreed_at":"2026-11-05T09:00:00Z","evidence_ref":"Call"},"feasibility_note":"ok"}`,
		"no offset":     `{"expected_version":2,"target_date":"2026-11-21","target_slot_id":"aaaaaaaa-1111-4111-8111-11111111111b","customer_agreement":{"channel":"phone","agreed_at":"2026-11-05T09:00:00","evidence_ref":"Call"},"feasibility_note":"ok"}`,
		"note two line": `{"expected_version":2,"target_date":"2026-11-21","target_slot_id":"aaaaaaaa-1111-4111-8111-11111111111b","customer_agreement":{"channel":"phone","agreed_at":"2026-11-05T09:00:00Z","evidence_ref":"Call"},"feasibility_note":"a\nb"}`,
	} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, chefPath, "chef", "k", b, nil); rec.Code != 422 || fc.calls != 0 {
			t.Errorf("amendment %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	// Disabled checkout: no appointment routes.
	off := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id, Checkout: &fakeCheckout{},
		Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))})
	for _, p := range []string{move, chefPath} {
		req := httptest.NewRequest(http.MethodPost, p, strings.NewReader(chefBody))
		req.Header.Set("Idempotency-Key", "k")
		req.Header.Set("Authorization", "Bearer chef")
		rec := httptest.NewRecorder()
		off.ServeHTTP(rec, req)
		if rec.Code != 404 && rec.Code != 405 {
			t.Errorf("%s while disabled: %d", p, rec.Code)
		}
	}
}

// GK-031 B1 part 2: fulfilment steps (chef or fulfilment; handover and
// no_show are K11 commands answered 202).
func TestTransitionRoutes(t *testing.T) {
	id := catalogIdentity()
	id.actors["fulfil"] = identity.Actor{AccountID: "acc-ful", Subject: "sub-ful", Capabilities: []string{identity.Fulfillment}}
	const path = "/api/v1/admin/orders/o1/transitions"
	for name, c := range map[string]struct {
		auth   string
		status int
	}{"support": {"support", 403}, "customer": {secretToken, 403}, "guest": {"", 401}, "chef": {"chef", 200}, "fulfillment": {"fulfil", 200}} {
		if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, path, c.auth, "k", `{"action":"ready","expected_version":3}`, nil); rec.Code != c.status {
			t.Errorf("ready by %s: %d, want %d", name, rec.Code, c.status)
		}
	}
	rec := serveCheckout(t, &fakeCheckout{replayed: true}, id, http.MethodPost, path, "fulfil", "k", `{"action":"ready","expected_version":3}`, nil)
	if rec.Code != 200 || rec.Header().Get("Idempotent-Replayed") != "true" ||
		rec.Body.String() != `{"fulfillment_state":"awaiting_handover","lifecycle":"ready","order_id":"o1"}`+"\n" {
		t.Fatalf("ready: %d %q", rec.Code, rec.Body.String())
	}
	for action, state := range map[string]string{"handover": "handover_pending", "no_show": "no_show_pending"} {
		rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, path, "chef", "k", `{"action":"`+action+`","expected_version":3}`, nil)
		if rec.Code != 202 || rec.Body.String() != `{"order_id":"o1","operation_id":"`+action+`-o1","state":"`+state+`","poll_after_seconds":3}`+"\n" {
			t.Errorf("%s: %d %q", action, rec.Code, rec.Body.String())
		}
	}
	for name, b := range map[string]string{
		"unknown action":        `{"action":"refund","expected_version":3}`,
		"no version":            `{"action":"ready"}`,
		"exception no reason":   `{"action":"delivery_exception","expected_version":3}`,
		"reason on ready":       `{"action":"ready","expected_version":3,"reason":"x"}`,
		"reason on no_show":     `{"action":"no_show","expected_version":3,"reason":"x"}`,
		"two-line reason":       `{"action":"delivery_exception","expected_version":3,"reason":"a\nb"}`,
		"untrimmed courier ref": `{"action":"dispatch","expected_version":3,"reason":" ref"}`,
	} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, path, "chef", "k", b, nil); rec.Code != 422 || fc.calls != 0 {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, path, "chef", "k", `{"action":"ready","expected_version":3,"amount_minor":1}`, nil); rec.Code != 400 {
		t.Fatalf("unknown field: %d", rec.Code)
	}
	for code, want := range map[string]string{"delivery_not_paid": "DELIVERY_NOT_PAID", "handover_not_due": "HANDOVER_NOT_DUE",
		"transition_not_allowed": "TRANSITION_NOT_ALLOWED", "stale_version": "STALE_VERSION"} {
		rec := serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost, path, "fulfil", "k",
			`{"action":"dispatch","expected_version":3,"reason":"Courier GK-1"}`, nil)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"`+want+`"`) {
			t.Errorf("%s: %d %s", code, rec.Code, rec.Body.String())
		}
	}
	if rec := serveCheckout(t, &fakeCheckout{freeErr: catalog.ErrForbidden}, id, http.MethodPost, path, "fulfil", "k",
		`{"action":"no_show","expected_version":3}`, nil); rec.Code != 403 {
		t.Fatalf("no_show refused by the database for fulfilment: %d", rec.Code)
	}
	if rec := serveCheckout(t, &fakeCheckout{freeErr: checkout.ErrOrdersFrozen}, id, http.MethodPost, path, "chef", "k",
		`{"action":"handover","expected_version":3}`, nil); rec.Code != 503 {
		t.Fatalf("frozen: %d", rec.Code)
	}
}

// GK-017 r02b Q6: the chef's late-payment offer (chef only) and the
// customer's answer on the order page (owner or guest capability).
func TestLatePaymentAgreementRoutes(t *testing.T) {
	id := catalogIdentity()
	id.actors["fulfil"] = identity.Actor{AccountID: "acc-ful", Subject: "sub-ful", Capabilities: []string{identity.Fulfillment}}
	const offer = "/api/v1/admin/orders/o1/late-payment/offer"
	requested := `{"expected_version":3,"kind":"requested"}`
	other := `{"expected_version":3,"kind":"other","target_date":"2026-11-21","target_slot_id":"aaaaaaaa-1111-4111-8111-11111111111b"}`
	for name, c := range map[string]struct {
		auth   string
		status int
	}{"support": {"support", 403}, "fulfillment": {"fulfil", 403}, "customer": {secretToken, 403}, "guest": {"", 401}, "chef": {"chef", 200}} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, offer, c.auth, "k", requested, nil); rec.Code != c.status {
			t.Errorf("offer by %s: %d %s, want %d", name, rec.Code, rec.Body.String(), c.status)
		}
	}
	fc := &fakeCheckout{}
	rec := serveCheckout(t, fc, id, http.MethodPost, offer, "chef", "k", other, nil)
	if rec.Code != 200 || fc.orderID != "o1" || rec.Body.String() != `{"offer_id":"offer-o1","order_id":"o1"}`+"\n" {
		t.Fatalf("other offer: %d %q", rec.Code, rec.Body.String())
	}
	rec = serveCheckout(t, &fakeCheckout{replayed: true}, id, http.MethodPost, offer, "chef", "k", requested, nil)
	if rec.Code != 200 || rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replayed offer: %d %v", rec.Code, rec.Header())
	}
	for name, c := range map[string]struct {
		body, key string
		status    int
	}{
		"no key":               {requested, "", 400},
		"unknown field":        {`{"expected_version":3,"kind":"requested","slot_id":"aaaaaaaa-1111-4111-8111-11111111111b"}`, "k", 400},
		"null unknown field":   {`{"expected_version":3,"kind":"requested","start":null}`, "k", 400},
		"null kind":            {`{"expected_version":3,"kind":null}`, "k", 422},
		"no version":           {`{"kind":"requested"}`, "k", 422},
		"bad kind":             {`{"expected_version":3,"kind":"tomorrow"}`, "k", 422},
		"requested with slot":  {`{"expected_version":3,"kind":"requested","target_date":"2026-11-21","target_slot_id":"aaaaaaaa-1111-4111-8111-11111111111b"}`, "k", 422},
		"other without slot":   {`{"expected_version":3,"kind":"other","target_date":"2026-11-21"}`, "k", 422},
		"other without date":   {`{"expected_version":3,"kind":"other","target_slot_id":"aaaaaaaa-1111-4111-8111-11111111111b"}`, "k", 422},
		"other upper-case":     {`{"expected_version":3,"kind":"other","target_date":"2026-11-21","target_slot_id":"AAAAAAAA-1111-4111-8111-11111111111B"}`, "k", 422},
		"other impossible day": {`{"expected_version":3,"kind":"other","target_date":"2026-02-30","target_slot_id":"aaaaaaaa-1111-4111-8111-11111111111b"}`, "k", 422},
	} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, offer, "chef", c.key, c.body, nil); rec.Code != c.status || fc.calls != 0 {
			t.Errorf("offer %s: %d %s (calls %d), want %d", name, rec.Code, rec.Body.String(), fc.calls, c.status)
		}
	}
	for code, want := range map[string]string{"honour_too_late": "HONOUR_TOO_LATE", "slot_unavailable": "SLOT_UNAVAILABLE",
		"stale_version": "STALE_VERSION", "state_conflict": "CONFLICT", "key_reused": "KEY_REUSED"} {
		rec := serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost, offer, "chef", "k", requested, nil)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"`+want+`"`) {
			t.Errorf("offer %s: %d %s, want 409 %s", code, rec.Code, rec.Body.String(), want)
		}
	}
	if rec := serveCheckout(t, &fakeCheckout{freeErr: checkout.ErrOrdersFrozen}, id, http.MethodPost, offer, "chef", "k", requested, nil); rec.Code != 503 {
		t.Fatalf("frozen offer: %d", rec.Code)
	}

	// The customer's answer: the order's owner or the guest capability.
	guest := map[string]string{OrderCapabilityHeader: testProof}
	const answer = "/api/v1/orders/o1/late-payment/answer"
	accept := `{"expected_version":4,"offer_id":"bbbbbbbb-1111-4111-8111-11111111111b","answer":"accept"}`
	fc = &fakeCheckout{}
	rec = serveCheckout(t, fc, id, http.MethodPost, answer, "", "k", accept, guest)
	if rec.Code != 202 || len(fc.digest) != 32 || fc.subject != "" || fc.orderID != "o1" ||
		rec.Body.String() != `{"order_id":"o1","operation_id":"answer-o1","state":"answer_pending","poll_after_seconds":3}`+"\n" {
		t.Fatalf("guest answer: %d %q", rec.Code, rec.Body.String())
	}
	fc = &fakeCheckout{}
	if rec := serveCheckout(t, fc, id, http.MethodPost, answer, secretToken, "k", strings.Replace(accept, "accept", "decline", 1), nil); rec.Code != 202 ||
		fc.digest != nil || fc.subject == "" {
		t.Fatalf("signed-in decline: %d", rec.Code)
	}
	rec = serveCheckout(t, &fakeCheckout{replayed: true}, id, http.MethodPost, answer, "", "k", accept, guest)
	if rec.Code != 202 || rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replayed answer: %d", rec.Code)
	}
	for name, c := range map[string]struct {
		body, key string
		hdr       map[string]string
		status    int
	}{
		"no capability":    {accept, "k", nil, 404},
		"bad capability":   {accept, "k", map[string]string{OrderCapabilityHeader: "short"}, 404},
		"no key":           {accept, "", guest, 400},
		"unknown field":    {`{"expected_version":4,"offer_id":"bbbbbbbb-1111-4111-8111-11111111111b","answer":"accept","channel":"order_page"}`, "k", guest, 400},
		"null unknown":     {`{"expected_version":4,"offer_id":"bbbbbbbb-1111-4111-8111-11111111111b","answer":"accept","agreed_at":null}`, "k", guest, 400},
		"null answer":      {`{"expected_version":4,"offer_id":"bbbbbbbb-1111-4111-8111-11111111111b","answer":null}`, "k", guest, 422},
		"no offer":         {`{"expected_version":4,"answer":"accept"}`, "k", guest, 422},
		"upper-case offer": {`{"expected_version":4,"offer_id":"BBBBBBBB-1111-4111-8111-11111111111B","answer":"accept"}`, "k", guest, 422},
		"maybe":            {`{"expected_version":4,"offer_id":"bbbbbbbb-1111-4111-8111-11111111111b","answer":"maybe"}`, "k", guest, 422},
		"no version":       {`{"offer_id":"bbbbbbbb-1111-4111-8111-11111111111b","answer":"accept"}`, "k", guest, 422},
	} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, answer, "", c.key, c.body, c.hdr); rec.Code != c.status || fc.calls != 0 {
			t.Errorf("answer %s: %d %s (calls %d), want %d", name, rec.Code, rec.Body.String(), fc.calls, c.status)
		}
	}
	for code, want := range map[string]string{"offer_not_current": "OFFER_NOT_CURRENT", "slot_unavailable": "SLOT_UNAVAILABLE",
		"honour_too_late": "HONOUR_TOO_LATE", "stale_version": "STALE_VERSION", "key_reused": "KEY_REUSED", "state_conflict": "CONFLICT"} {
		rec := serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost, answer, "", "k", accept, guest)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"`+want+`"`) {
			t.Errorf("answer %s: %d %s, want 409 %s", code, rec.Code, rec.Body.String(), want)
		}
	}
	if rec := serveCheckout(t, &fakeCheckout{freeErr: checkout.ErrOfferLapsed}, id, http.MethodPost, answer, "", "k", accept, guest); rec.Code != 409 ||
		!strings.Contains(rec.Body.String(), `"code":"SLOT_UNAVAILABLE"`) {
		t.Fatalf("lapsed offer: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveCheckout(t, &fakeCheckout{freeErr: checkout.ErrOrdersFrozen}, id, http.MethodPost, answer, "", "k", accept, guest); rec.Code != 503 {
		t.Fatalf("frozen answer: %d", rec.Code)
	}
	// Disabled checkout: neither route.
	off := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id, Checkout: &fakeCheckout{},
		Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))})
	for _, p := range []string{offer, answer} {
		req := httptest.NewRequest(http.MethodPost, p, strings.NewReader(requested))
		req.Header.Set("Idempotency-Key", "k")
		req.Header.Set("Authorization", "Bearer chef")
		rec := httptest.NewRecorder()
		off.ServeHTTP(rec, req)
		if rec.Code != 404 && rec.Code != 405 {
			t.Errorf("%s while disabled: %d", p, rec.Code)
		}
	}
}

// K03/K06 GK delivery: the chef's quote and waiver (chef only, strict
// input, 200 with the record, 503 while frozen) and the customer's delivery
// payment (owner or guest capability, 202, 503 while frozen or without
// payments).
func TestDeliveryRoutes(t *testing.T) {
	id := catalogIdentity()
	id.actors["fulfil"] = identity.Actor{AccountID: "acc-ful", Subject: "sub-ful", Capabilities: []string{identity.Fulfillment}}
	const quote = "/api/v1/admin/orders/o1/delivery-quote"
	const waiver = "/api/v1/admin/orders/o1/delivery-waiver"
	quoteBody := `{"expected_version":3,"amount_minor":12000,"note":"Measured route, ULEZ included"}`
	waiverBody := `{"expected_version":4,"distance_m":8200,"distance_source":"Google Maps driving route"}`
	for _, c := range []struct{ path, body, field string }{{quote, quoteBody, "quote_id"}, {waiver, waiverBody, "waiver_id"}} {
		for name, who := range map[string]struct {
			auth   string
			status int
		}{"support": {"support", 403}, "fulfillment": {"fulfil", 403}, "customer": {secretToken, 403}, "guest": {"", 401}, "chef": {"chef", 200}} {
			fc := &fakeCheckout{}
			if rec := serveCheckout(t, fc, id, http.MethodPost, c.path, who.auth, "k", c.body, nil); rec.Code != who.status {
				t.Errorf("%s by %s: %d %s, want %d", c.path, name, rec.Code, rec.Body.String(), who.status)
			}
		}
		fc := &fakeCheckout{}
		rec := serveCheckout(t, fc, id, http.MethodPost, c.path, "chef", "k", c.body, nil)
		want, _ := json.Marshal(map[string]string{"order_id": "o1", c.field: strings.TrimSuffix(c.field, "_id") + "-o1"})
		if rec.Code != 200 || fc.orderID != "o1" || rec.Body.String() != string(want)+"\n" {
			t.Fatalf("%s: %d %q", c.path, rec.Code, rec.Body.String())
		}
		if rec := serveCheckout(t, &fakeCheckout{replayed: true}, id, http.MethodPost, c.path, "chef", "k", c.body, nil); rec.Code != 200 ||
			rec.Header().Get("Idempotent-Replayed") != "true" {
			t.Fatalf("%s replayed: %d %v", c.path, rec.Code, rec.Header())
		}
		if rec := serveCheckout(t, &fakeCheckout{freeErr: checkout.ErrOrdersFrozen}, id, http.MethodPost, c.path, "chef", "k", c.body, nil); rec.Code != 503 ||
			!strings.Contains(rec.Body.String(), `"retryable":true`) {
			t.Fatalf("%s frozen: %d %s", c.path, rec.Code, rec.Body.String())
		}
	}
	for name, c := range map[string]struct {
		path, body, key string
		status          int
	}{
		"quote no key":           {quote, quoteBody, "", 400},
		"quote unknown field":    {quote, `{"expected_version":3,"amount_minor":12000,"currency":"GBP"}`, "k", 400},
		"quote address":          {quote, `{"expected_version":3,"amount_minor":12000,"address":"1 Road"}`, "k", 400},
		"quote no version":       {quote, `{"amount_minor":12000}`, "k", 422},
		"quote under £100":       {quote, `{"expected_version":3,"amount_minor":9999}`, "k", 422},
		"quote over the bound":   {quote, `{"expected_version":3,"amount_minor":100000000}`, "k", 422},
		"quote fractional":       {quote, `{"expected_version":3,"amount_minor":12000.5}`, "k", 400},
		"quote as text":          {quote, `{"expected_version":3,"amount_minor":"12000"}`, "k", 400},
		"quote empty note":       {quote, `{"expected_version":3,"amount_minor":12000,"note":""}`, "k", 422},
		"quote two-line note":    {quote, `{"expected_version":3,"amount_minor":12000,"note":"a\nb"}`, "k", 422},
		"waiver no key":          {waiver, waiverBody, "", 400},
		"waiver unknown field":   {waiver, `{"expected_version":4,"distance_m":8200,"distance_source":"map","radius_m":10000}`, "k", 400},
		"waiver no distance":     {waiver, `{"expected_version":4,"distance_source":"map"}`, "k", 422},
		"waiver zero distance":   {waiver, `{"expected_version":4,"distance_m":0,"distance_source":"map"}`, "k", 422},
		"waiver too far":         {waiver, `{"expected_version":4,"distance_m":1000001,"distance_source":"map"}`, "k", 422},
		"waiver no source":       {waiver, `{"expected_version":4,"distance_m":8200}`, "k", 422},
		"waiver padded source":   {waiver, `{"expected_version":4,"distance_m":8200,"distance_source":" map"}`, "k", 422},
		"waiver no version":      {waiver, `{"distance_m":8200,"distance_source":"map"}`, "k", 422},
		"waiver negative metres": {waiver, `{"expected_version":4,"distance_m":-5,"distance_source":"map"}`, "k", 422},
	} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, c.path, "chef", c.key, c.body, nil); rec.Code != c.status || fc.calls != 0 {
			t.Errorf("%s: %d %s (calls %d), want %d", name, rec.Code, rec.Body.String(), fc.calls, c.status)
		}
	}
	for code, want := range map[string]string{"delivery_pay_by_passed": "DELIVERY_PAY_BY_PASSED", "payment_in_progress": "PAYMENT_IN_PROGRESS",
		"waiver_not_eligible": "WAIVER_NOT_ELIGIBLE", "stale_version": "STALE_VERSION", "state_conflict": "CONFLICT", "key_reused": "KEY_REUSED"} {
		for _, c := range []struct{ path, body string }{{quote, quoteBody}, {waiver, waiverBody}} {
			rec := serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost, c.path, "chef", "k", c.body, nil)
			if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"`+want+`"`) {
				t.Errorf("%s %s: %d %s, want 409 %s", c.path, code, rec.Code, rec.Body.String(), want)
			}
		}
	}

	// The customer's delivery payment.
	guest := map[string]string{OrderCapabilityHeader: testProof}
	const pay = "/api/v1/orders/o1/delivery-payment"
	payBody := `{"expected_version":5,"quote_id":"cccccccc-1111-4111-8111-11111111111c"}`
	fc := &fakeCheckout{}
	rec := serveCheckout(t, fc, id, http.MethodPost, pay, "", "k", payBody, guest)
	if rec.Code != 202 || len(fc.digest) != 32 || fc.subject != "" || fc.orderID != "o1" ||
		rec.Body.String() != `{"order_id":"o1","operation_id":"dpay-o1","state":"delivery_payment_pending","poll_after_seconds":3}`+"\n" {
		t.Fatalf("guest delivery payment: %d %q", rec.Code, rec.Body.String())
	}
	fc = &fakeCheckout{}
	if rec := serveCheckout(t, fc, id, http.MethodPost, pay, secretToken, "k", payBody, nil); rec.Code != 202 || fc.digest != nil || fc.subject == "" {
		t.Fatalf("signed-in delivery payment: %d", rec.Code)
	}
	if rec := serveCheckout(t, &fakeCheckout{replayed: true}, id, http.MethodPost, pay, "", "k", payBody, guest); rec.Code != 202 ||
		rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replayed delivery payment: %d", rec.Code)
	}
	for name, c := range map[string]struct {
		body, key string
		hdr       map[string]string
		status    int
	}{
		"no capability":    {payBody, "k", nil, 404},
		"bad capability":   {payBody, "k", map[string]string{OrderCapabilityHeader: "short"}, 404},
		"no key":           {payBody, "", guest, 400},
		"client amount":    {`{"expected_version":5,"quote_id":"cccccccc-1111-4111-8111-11111111111c","amount_minor":1}`, "k", guest, 400},
		"no quote":         {`{"expected_version":5}`, "k", guest, 422},
		"upper-case quote": {`{"expected_version":5,"quote_id":"CCCCCCCC-1111-4111-8111-11111111111C"}`, "k", guest, 422},
		"no version":       {`{"quote_id":"cccccccc-1111-4111-8111-11111111111c"}`, "k", guest, 422},
	} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, pay, "", c.key, c.body, c.hdr); rec.Code != c.status || fc.calls != 0 {
			t.Errorf("delivery payment %s: %d %s (calls %d), want %d", name, rec.Code, rec.Body.String(), fc.calls, c.status)
		}
	}
	for code, want := range map[string]string{"delivery_pay_by_passed": "DELIVERY_PAY_BY_PASSED", "quote_not_current": "QUOTE_NOT_CURRENT",
		"payment_in_progress": "PAYMENT_IN_PROGRESS", "stale_version": "STALE_VERSION", "state_conflict": "CONFLICT", "key_reused": "KEY_REUSED"} {
		rec := serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost, pay, "", "k", payBody, guest)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"`+want+`"`) {
			t.Errorf("delivery payment %s: %d %s, want 409 %s", code, rec.Code, rec.Body.String(), want)
		}
	}
	for _, err := range []error{checkout.ErrPaymentsUnavailable, checkout.ErrRecoveryFrozen} {
		if rec := serveCheckout(t, &fakeCheckout{freeErr: err}, id, http.MethodPost, pay, "", "k", payBody, guest); rec.Code != 503 ||
			!strings.Contains(rec.Body.String(), `"retryable":true`) {
			t.Fatalf("delivery payment %v: %d %s", err, rec.Code, rec.Body.String())
		}
	}
	// Disabled checkout: none of the routes.
	off := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id, Checkout: &fakeCheckout{},
		Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))})
	for _, p := range []string{quote, waiver, pay} {
		req := httptest.NewRequest(http.MethodPost, p, strings.NewReader(quoteBody))
		req.Header.Set("Idempotency-Key", "k")
		req.Header.Set("Authorization", "Bearer chef")
		rec := httptest.NewRecorder()
		off.ServeHTTP(rec, req)
		if rec.Code != 404 && rec.Code != 405 {
			t.Errorf("%s while disabled: %d", p, rec.Code)
		}
	}
}

// GK-017 r02b Q5: the chef's "GK can't deliver" (chef only, synchronous,
// versioned) and the customer's choice (owner or guest capability, 202).
func TestDeliveryChoiceRoutes(t *testing.T) {
	id := catalogIdentity()
	id.actors["fulfil"] = identity.Actor{AccountID: "acc-ful", Subject: "sub-ful", Capabilities: []string{identity.Fulfillment}}
	const np = "/api/v1/admin/orders/o1/delivery-not-possible"
	npBody := `{"expected_version":3,"reason":"Van off the road on Saturday"}`
	for name, who := range map[string]struct {
		auth   string
		status int
	}{"support": {"support", 403}, "fulfillment": {"fulfil", 403}, "customer": {secretToken, 403}, "guest": {"", 401}, "chef": {"chef", 200}} {
		if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, np, who.auth, "k", npBody, nil); rec.Code != who.status {
			t.Errorf("not possible by %s: %d %s, want %d", name, rec.Code, rec.Body.String(), who.status)
		}
	}
	fc := &fakeCheckout{}
	rec := serveCheckout(t, fc, id, http.MethodPost, np, "chef", "k", npBody, nil)
	if rec.Code != 200 || fc.orderID != "o1" || rec.Body.String() != `{"decision_id":"decision-o1","order_id":"o1"}`+"\n" {
		t.Fatalf("not possible: %d %q", rec.Code, rec.Body.String())
	}
	if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, np, "chef", "k", `{"expected_version":3}`, nil); rec.Code != 200 {
		t.Fatalf("not possible without a reason: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveCheckout(t, &fakeCheckout{replayed: true}, id, http.MethodPost, np, "chef", "k", npBody, nil); rec.Code != 200 ||
		rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("not possible replayed: %d %v", rec.Code, rec.Header())
	}
	if rec := serveCheckout(t, &fakeCheckout{freeErr: checkout.ErrOrdersFrozen}, id, http.MethodPost, np, "chef", "k", npBody, nil); rec.Code != 503 ||
		!strings.Contains(rec.Body.String(), `"retryable":true`) {
		t.Fatalf("not possible frozen: %d %s", rec.Code, rec.Body.String())
	}
	for name, c := range map[string]struct {
		body, key string
		status    int
	}{
		"no key":          {npBody, "", 400},
		"unknown field":   {`{"expected_version":3,"refund":true}`, "k", 400},
		"no version":      {`{"reason":"x"}`, "k", 422},
		"empty reason":    {`{"expected_version":3,"reason":""}`, "k", 422},
		"two-line reason": {`{"expected_version":3,"reason":"a\nb"}`, "k", 422},
		"padded reason":   {`{"expected_version":3,"reason":" a"}`, "k", 422},
	} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, np, "chef", c.key, c.body, nil); rec.Code != c.status || fc.calls != 0 {
			t.Errorf("not possible %s: %d %s (calls %d), want %d", name, rec.Code, rec.Body.String(), fc.calls, c.status)
		}
	}
	for code, want := range map[string]string{"payment_in_progress": "PAYMENT_IN_PROGRESS", "stale_version": "STALE_VERSION",
		"state_conflict": "CONFLICT", "key_reused": "KEY_REUSED"} {
		rec := serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost, np, "chef", "k", npBody, nil)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"`+want+`"`) {
			t.Errorf("not possible %s: %d %s, want 409 %s", code, rec.Code, rec.Body.String(), want)
		}
	}

	// The customer's choice.
	guest := map[string]string{OrderCapabilityHeader: testProof}
	const choose = "/api/v1/orders/o1/delivery-choice"
	collect := `{"expected_version":5,"choice":"collection","target_slot_id":"8d3f0f8e-5d55-4c3e-9a49-34a0e2a9f001"}`
	refund := `{"expected_version":5,"choice":"full_refund"}`
	for _, body := range []string{collect, refund,
		`{"expected_version":5,"choice":"customer_courier","target_slot_id":"8d3f0f8e-5d55-4c3e-9a49-34a0e2a9f001"}`} {
		fc := &fakeCheckout{}
		rec := serveCheckout(t, fc, id, http.MethodPost, choose, "", "k", body, guest)
		if rec.Code != 202 || len(fc.digest) != 32 || fc.subject != "" || fc.orderID != "o1" ||
			rec.Body.String() != `{"order_id":"o1","operation_id":"choice-o1","state":"choice_pending","poll_after_seconds":3}`+"\n" {
			t.Fatalf("guest choice %s: %d %q", body, rec.Code, rec.Body.String())
		}
	}
	fc = &fakeCheckout{}
	if rec := serveCheckout(t, fc, id, http.MethodPost, choose, secretToken, "k", refund, nil); rec.Code != 202 || fc.digest != nil || fc.subject == "" {
		t.Fatalf("signed-in choice: %d", rec.Code)
	}
	if rec := serveCheckout(t, &fakeCheckout{replayed: true}, id, http.MethodPost, choose, "", "k", refund, guest); rec.Code != 202 ||
		rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replayed choice: %d", rec.Code)
	}
	if rec := serveCheckout(t, &fakeCheckout{freeErr: checkout.ErrOrdersFrozen}, id, http.MethodPost, choose, "", "k", refund, guest); rec.Code != 503 ||
		!strings.Contains(rec.Body.String(), `"retryable":true`) {
		t.Fatalf("frozen choice: %d %s", rec.Code, rec.Body.String())
	}
	for name, c := range map[string]struct {
		body, key string
		hdr       map[string]string
		status    int
	}{
		"no capability":         {refund, "k", nil, 404},
		"bad capability":        {refund, "k", map[string]string{OrderCapabilityHeader: "short"}, 404},
		"no key":                {refund, "", guest, 400},
		"unknown field":         {`{"expected_version":5,"choice":"full_refund","amount_minor":1}`, "k", guest, 400},
		"unknown choice":        {`{"expected_version":5,"choice":"redeliver"}`, "k", guest, 422},
		"no choice":             {`{"expected_version":5}`, "k", guest, 422},
		"time without slot":     {`{"expected_version":5,"choice":"collection"}`, "k", guest, 422},
		"refund with slot":      {`{"expected_version":5,"choice":"full_refund","target_slot_id":"8d3f0f8e-5d55-4c3e-9a49-34a0e2a9f001"}`, "k", guest, 422},
		"upper-case slot":       {`{"expected_version":5,"choice":"collection","target_slot_id":"8D3F0F8E-5D55-4C3E-9A49-34A0E2A9F001"}`, "k", guest, 422},
		"no version":            {`{"choice":"full_refund"}`, "k", guest, 422},
		"version as text":       {`{"expected_version":"5","choice":"full_refund"}`, "k", guest, 400},
		"two objects":           {refund + refund, "k", guest, 400},
		"choice in wrong case":  {`{"expected_version":5,"choice":"Full_Refund"}`, "k", guest, 422},
		"duplicate choice keys": {`{"expected_version":5,"choice":"full_refund","choice":"collection"}`, "k", guest, 400},
	} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, choose, "", c.key, c.body, c.hdr); rec.Code != c.status || fc.calls != 0 {
			t.Errorf("choice %s: %d %s (calls %d), want %d", name, rec.Code, rec.Body.String(), fc.calls, c.status)
		}
	}
	for code, want := range map[string]string{"choice_not_open": "CHOICE_NOT_OPEN", "slot_unavailable": "SLOT_UNAVAILABLE",
		"stale_version": "STALE_VERSION", "state_conflict": "CONFLICT", "key_reused": "KEY_REUSED"} {
		rec := serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost, choose, "", "k", refund, guest)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"`+want+`"`) {
			t.Errorf("choice %s: %d %s, want 409 %s", code, rec.Code, rec.Body.String(), want)
		}
	}
	// The dispatch refusal's code.
	rec = serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: "delivery_not_possible"}}, id, http.MethodPost,
		"/api/v1/admin/orders/o1/transitions", "chef", "k", `{"action":"dispatch","expected_version":3}`, nil)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"DELIVERY_NOT_POSSIBLE"`) {
		t.Errorf("dispatch refused: %d %s", rec.Code, rec.Body.String())
	}
	// Disabled checkout: none of the routes.
	off := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id, Checkout: &fakeCheckout{},
		Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))})
	for _, p := range []string{np, choose} {
		req := httptest.NewRequest(http.MethodPost, p, strings.NewReader(refund))
		req.Header.Set("Idempotency-Key", "k")
		req.Header.Set("Authorization", "Bearer chef")
		rec := httptest.NewRecorder()
		off.ServeHTTP(rec, req)
		if rec.Code != 404 && rec.Code != 405 {
			t.Errorf("%s while disabled: %d", p, rec.Code)
		}
	}
}

func TestDeliveryFailureRoutes(t *testing.T) {
	id := catalogIdentity()
	id.actors["fulfil"] = identity.Actor{AccountID: "acc-ful", Subject: "sub-ful", Capabilities: []string{identity.Fulfillment}}
	const failed = "/api/v1/admin/orders/o1/delivery-failed"
	ourSide := `{"expected_version":7,"cause":"our_side","reason":"Van broke down"}`
	nobody := `{"expected_version":7,"cause":"nobody_available"}`
	// Chef only: fulfilment records a delivery exception instead.
	for name, who := range map[string]struct {
		auth   string
		status int
	}{"support": {"support", 403}, "fulfillment": {"fulfil", 403}, "customer": {secretToken, 403}, "guest": {"", 401}, "chef": {"chef", 200}} {
		if rec := serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, failed, who.auth, "k", ourSide, nil); rec.Code != who.status {
			t.Errorf("delivery failed by %s: %d %s, want %d", name, rec.Code, rec.Body.String(), who.status)
		}
	}
	fc := &fakeCheckout{}
	rec := serveCheckout(t, fc, id, http.MethodPost, failed, "chef", "k", ourSide, nil)
	if rec.Code != 200 || fc.orderID != "o1" || rec.Body.String() != `{"failure_id":"failure-o1","order_id":"o1"}`+"\n" {
		t.Fatalf("our side: %d %q", rec.Code, rec.Body.String())
	}
	// Nobody available is the chef's no-show K11 command: 202.
	rec = serveCheckout(t, &fakeCheckout{}, id, http.MethodPost, failed, "chef", "k", nobody, nil)
	if rec.Code != 202 || rec.Body.String() != `{"order_id":"o1","operation_id":"noshow-o1","state":"no_show_pending","poll_after_seconds":3}`+"\n" {
		t.Fatalf("nobody available: %d %q", rec.Code, rec.Body.String())
	}
	for _, body := range []string{ourSide, nobody} {
		if rec := serveCheckout(t, &fakeCheckout{replayed: true}, id, http.MethodPost, failed, "chef", "k", body, nil); rec.Code/100 != 2 ||
			rec.Header().Get("Idempotent-Replayed") != "true" {
			t.Fatalf("replayed %s: %d %v", body, rec.Code, rec.Header())
		}
		if rec := serveCheckout(t, &fakeCheckout{freeErr: checkout.ErrOrdersFrozen}, id, http.MethodPost, failed, "chef", "k", body, nil); rec.Code != 503 ||
			!strings.Contains(rec.Body.String(), `"retryable":true`) {
			t.Fatalf("frozen %s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	for name, c := range map[string]struct {
		body, key string
		status    int
	}{
		"no key":          {ourSide, "", 400},
		"unknown field":   {`{"expected_version":7,"cause":"our_side","refund":true}`, "k", 400},
		"no version":      {`{"cause":"our_side"}`, "k", 422},
		"no cause":        {`{"expected_version":7}`, "k", 422},
		"unknown cause":   {`{"expected_version":7,"cause":"weather"}`, "k", 422},
		"empty reason":    {`{"expected_version":7,"cause":"our_side","reason":""}`, "k", 422},
		"two-line reason": {`{"expected_version":7,"cause":"our_side","reason":"a\nb"}`, "k", 422},
		"duplicate cause": {`{"expected_version":7,"cause":"our_side","cause":"nobody_available"}`, "k", 400},
	} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, failed, "chef", c.key, c.body, nil); rec.Code != c.status || fc.calls != 0 {
			t.Errorf("delivery failed %s: %d %s (calls %d), want %d", name, rec.Code, rec.Body.String(), fc.calls, c.status)
		}
	}
	for code, want := range map[string]string{"transition_not_allowed": "TRANSITION_NOT_ALLOWED", "handover_not_due": "HANDOVER_NOT_DUE",
		"stale_version": "STALE_VERSION", "state_conflict": "CONFLICT", "key_reused": "KEY_REUSED"} {
		rec := serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost, failed, "chef", "k", nobody, nil)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"`+want+`"`) {
			t.Errorf("delivery failed %s: %d %s, want 409 %s", code, rec.Code, rec.Body.String(), want)
		}
	}

	// The customer's choice after an our-side failure.
	guest := map[string]string{OrderCapabilityHeader: testProof}
	const choose = "/api/v1/orders/o1/delivery-failure-choice"
	collect := `{"expected_version":8,"choice":"collection","target_slot_id":"8d3f0f8e-5d55-4c3e-9a49-34a0e2a9f001"}`
	redeliver := `{"expected_version":8,"choice":"redelivery"}`
	for _, body := range []string{collect, redeliver} {
		fc := &fakeCheckout{}
		rec := serveCheckout(t, fc, id, http.MethodPost, choose, "", "k", body, guest)
		if rec.Code != 202 || len(fc.digest) != 32 || fc.subject != "" || fc.orderID != "o1" ||
			rec.Body.String() != `{"order_id":"o1","operation_id":"fchoice-o1","state":"choice_pending","poll_after_seconds":3}`+"\n" {
			t.Fatalf("guest choice %s: %d %q", body, rec.Code, rec.Body.String())
		}
	}
	fc = &fakeCheckout{}
	if rec := serveCheckout(t, fc, id, http.MethodPost, choose, secretToken, "k", redeliver, nil); rec.Code != 202 || fc.digest != nil || fc.subject == "" {
		t.Fatalf("signed-in choice: %d", rec.Code)
	}
	if rec := serveCheckout(t, &fakeCheckout{replayed: true}, id, http.MethodPost, choose, "", "k", redeliver, guest); rec.Code != 202 ||
		rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replayed choice: %d", rec.Code)
	}
	if rec := serveCheckout(t, &fakeCheckout{freeErr: checkout.ErrOrdersFrozen}, id, http.MethodPost, choose, "", "k", redeliver, guest); rec.Code != 503 ||
		!strings.Contains(rec.Body.String(), `"retryable":true`) {
		t.Fatalf("frozen choice: %d %s", rec.Code, rec.Body.String())
	}
	for name, c := range map[string]struct {
		body, key string
		hdr       map[string]string
		status    int
	}{
		"no capability":           {redeliver, "k", nil, 404},
		"no key":                  {redeliver, "", guest, 400},
		"unknown field":           {`{"expected_version":8,"choice":"redelivery","amount_minor":1}`, "k", guest, 400},
		"unknown choice":          {`{"expected_version":8,"choice":"full_refund"}`, "k", guest, 422},
		"no choice":               {`{"expected_version":8}`, "k", guest, 422},
		"collection without slot": {`{"expected_version":8,"choice":"collection"}`, "k", guest, 422},
		"redelivery with slot":    {`{"expected_version":8,"choice":"redelivery","target_slot_id":"8d3f0f8e-5d55-4c3e-9a49-34a0e2a9f001"}`, "k", guest, 422},
		"no version":              {`{"choice":"redelivery"}`, "k", guest, 422},
		"two objects":             {redeliver + redeliver, "k", guest, 400},
	} {
		fc := &fakeCheckout{}
		if rec := serveCheckout(t, fc, id, http.MethodPost, choose, "", c.key, c.body, c.hdr); rec.Code != c.status || fc.calls != 0 {
			t.Errorf("choice %s: %d %s (calls %d), want %d", name, rec.Code, rec.Body.String(), fc.calls, c.status)
		}
	}
	for code, want := range map[string]string{"choice_not_open": "CHOICE_NOT_OPEN", "slot_unavailable": "SLOT_UNAVAILABLE",
		"stale_version": "STALE_VERSION", "state_conflict": "CONFLICT", "key_reused": "KEY_REUSED"} {
		rec := serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost, choose, "", "k", redeliver, guest)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"`+want+`"`) {
			t.Errorf("choice %s: %d %s, want 409 %s", code, rec.Code, rec.Body.String(), want)
		}
	}
	// The dispatch refusals' codes.
	for code, want := range map[string]string{"delivery_failure_open": "DELIVERY_FAILURE_OPEN", "redelivery_time_not_set": "REDELIVERY_TIME_NOT_SET"} {
		rec = serveCheckout(t, &fakeCheckout{freeErr: &catalog.ConflictError{Code: code}}, id, http.MethodPost,
			"/api/v1/admin/orders/o1/transitions", "chef", "k", `{"action":"dispatch","expected_version":3}`, nil)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"`+want+`"`) {
			t.Errorf("dispatch refused %s: %d %s", code, rec.Code, rec.Body.String())
		}
	}
	// Disabled checkout: none of the routes.
	off := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id, Checkout: &fakeCheckout{},
		Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))})
	for _, p := range []string{failed, choose} {
		req := httptest.NewRequest(http.MethodPost, p, strings.NewReader(redeliver))
		req.Header.Set("Idempotency-Key", "k")
		req.Header.Set("Authorization", "Bearer chef")
		rec := httptest.NewRecorder()
		off.ServeHTTP(rec, req)
		if rec.Code != 404 && rec.Code != 405 {
			t.Errorf("%s while disabled: %d", p, rec.Code)
		}
	}
}

// The order's actions are a hint for the order page: without
// CHECKOUT_ENABLED no customer command is served, so none is offered.
func TestOrderActionsNeedCheckout(t *testing.T) {
	id := catalogIdentity()
	order := `{"actions":["cancel","delivery_choice"],"delivery_choice":{"open":true,"choices":["full_refund"],"allowed_choices":["full_refund"]},"id":"o"}`
	hdr := map[string]string{OrderCapabilityHeader: testProof}
	rec := serveCheckout(t, &fakeCheckout{order: order}, id, http.MethodGet, "/api/v1/orders/o", "", "", "", hdr)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"actions":["cancel","delivery_choice"]`) {
		t.Fatalf("with checkout: %d %s", rec.Code, rec.Body)
	}
	var logs bytes.Buffer
	off := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id, Checkout: &fakeCheckout{order: order},
		Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/o", nil)
	req.Header.Set(OrderCapabilityHeader, testProof)
	offRec := httptest.NewRecorder()
	off.ServeHTTP(offRec, req)
	body := offRec.Body.String()
	if offRec.Code != 200 || !strings.Contains(body, `"actions":[]`) || !strings.Contains(body, `"allowed_choices":[]`) ||
		!strings.Contains(body, `"choices":["full_refund"]`) {
		t.Fatalf("without checkout: %d %s", offRec.Code, body)
	}
}
