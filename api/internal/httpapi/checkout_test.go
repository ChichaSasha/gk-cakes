package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
)

type fakeCheckout struct {
	subject string
	digest  []byte
	calls   int
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
	h := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id, Checkout: co,
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
	if strings.Contains(logs.String(), "guest_proof") || (hdr != nil && hdr[QuoteProofHeader] != "" && strings.Contains(logs.String(), hdr[QuoteProofHeader])) {
		t.Fatalf("a proof reached the logs: %s", logs.String())
	}
	return rec
}

func TestQuoteRoutes(t *testing.T) {
	id := catalogIdentity()
	proof := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
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
