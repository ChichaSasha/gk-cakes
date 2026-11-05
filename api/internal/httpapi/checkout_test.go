package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
)

type fakeCheckout struct {
	subject  string
	digest   []byte
	calls    int
	freeErr  error
	replayed bool
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
	for name, hdr := range map[string]map[string]string{"none": nil, "malformed": {QuoteProofHeader: "abc"}} {
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
