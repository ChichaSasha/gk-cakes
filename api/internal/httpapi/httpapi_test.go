package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/notifications"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func newTestHandler(db Pinger) http.Handler {
	return NewHandler(Options{
		UIOrigin: "https://gkcake.com",
		DB:       db,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

type errorBody struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body is not JSON: %v: %s", err, rec.Body.String())
	}
	return body
}

func TestHealthzIsLivenessOnly(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler(fakePinger{err: errors.New("db down")}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want 200 even when the database is down", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestReadyzReportsDatabaseWithoutLeakingDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler(fakePinger{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /readyz with healthy database = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	leak := "password=c4n4ry host=10.0.0.5"
	newTestHandler(fakePinger{err: errors.New(leak)}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /readyz with failing database = %d, want 503", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Error.Code != "UNAVAILABLE" || !body.Error.Retryable {
		t.Errorf("unexpected error body: %+v", body.Error)
	}
	if strings.Contains(rec.Body.String(), "c4n4ry") || strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Errorf("readiness error leaks internal detail: %s", rec.Body.String())
	}
}

func TestUnknownRouteUsesContractErrorBody(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler(fakePinger{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown route = %d, want 404", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Error.Code != "NOT_FOUND" || body.Error.Retryable || body.Error.Message == "" {
		t.Errorf("unexpected error body: %+v", body.Error)
	}
	if body.Error.RequestID == "" || body.Error.RequestID != rec.Header().Get("X-Request-Id") {
		t.Errorf("request_id %q does not match X-Request-Id %q", body.Error.RequestID, rec.Header().Get("X-Request-Id"))
	}
}

func TestPanicBecomesOpaqueInternalError(t *testing.T) {
	h := withRecovery(slog.New(slog.NewTextHandler(io.Discard, nil)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("sql: secret detail")
	}))
	rec := httptest.NewRecorder()
	withRequestID(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic = %d, want 500", rec.Code)
	}
	if body := decodeError(t, rec); body.Error.Code != "INTERNAL" || strings.Contains(rec.Body.String(), "secret detail") {
		t.Errorf("unexpected internal error body: %s", rec.Body.String())
	}
}

func TestCORSAllowsOnlyTheConfiguredOrigin(t *testing.T) {
	h := newTestHandler(fakePinger{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://gkcake.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://gkcake.com" {
		t.Errorf("allowed origin got Access-Control-Allow-Origin %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("foreign origin got Access-Control-Allow-Origin %q", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("foreign origin response Vary = %q, want Origin so caches key on it", got)
	}

	req = httptest.NewRequest(http.MethodOptions, "/api/v1/orders", nil)
	req.Header.Set("Origin", "https://gkcake.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight = %d, want 204", rec.Code)
	}
	allowed := rec.Header().Get("Access-Control-Allow-Headers")
	for _, header := range []string{"Authorization", "Content-Type", "Idempotency-Key"} {
		if !strings.Contains(allowed, header) {
			t.Errorf("preflight Access-Control-Allow-Headers %q lacks %s", allowed, header)
		}
	}
}

func TestServeDrainsInFlightRequestsOnShutdown(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, ln, h, 5*time.Second) }()

	result := make(chan int, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/")
		if err != nil {
			result <- 0
			return
		}
		resp.Body.Close()
		result <- resp.StatusCode
	}()
	<-started
	cancel()
	time.Sleep(50 * time.Millisecond)
	select {
	case err := <-served:
		t.Fatalf("Serve returned %v before the in-flight request finished", err)
	default:
	}
	close(release)
	if code := <-result; code != http.StatusOK {
		t.Fatalf("in-flight request status = %d, want 200", code)
	}
	if err := <-served; err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if _, err := http.Get("http://" + ln.Addr().String() + "/"); err == nil {
		t.Fatal("server still accepts connections after shutdown")
	}
}

type fakeWebhook struct {
	result string
	err    error
	got    []byte
	header http.Header
}

func (f *fakeWebhook) Receive(_ context.Context, h http.Header, body []byte) (string, error) {
	f.got, f.header = body, h
	return f.result, f.err
}

func webhookHandler(w EmailWebhook) http.Handler {
	return NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), EmailWebhook: w})
}

func TestEmailWebhookAcknowledgesOnlyStoredDeliveries(t *testing.T) {
	cases := []struct {
		name      string
		rcv       *fakeWebhook
		status    int
		code      string
		retryable bool
	}{
		{"recorded", &fakeWebhook{result: "recorded"}, http.StatusOK, "", false},
		{"duplicate", &fakeWebhook{result: "duplicate"}, http.StatusOK, "", false},
		{"invalid signature", &fakeWebhook{err: fmt.Errorf("wrapped: %w", notifications.ErrInvalidSignature)}, http.StatusUnauthorized, "UNAUTHORIZED", false},
		{"malformed event", &fakeWebhook{err: notifications.ErrMalformedEvent}, http.StatusBadRequest, "BAD_REQUEST", false},
		{"storage failure", &fakeWebhook{err: errors.New("database down: ms.example@example.org")}, http.StatusServiceUnavailable, "UNAVAILABLE", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/webhooks/email", strings.NewReader(`{"type":"email.sent"}`))
			req.Header.Set("svix-id", "msg_1")
			rec := httptest.NewRecorder()
			webhookHandler(tc.rcv).ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d (%s)", rec.Code, tc.status, rec.Body.String())
			}
			if string(tc.rcv.got) != `{"type":"email.sent"}` || tc.rcv.header.Get("svix-id") != "msg_1" {
				t.Fatalf("receiver got body %q, headers %v; want the raw body and headers", tc.rcv.got, tc.rcv.header)
			}
			if tc.code == "" {
				var ok map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &ok); err != nil || ok["status"] != tc.rcv.result {
					t.Fatalf("body %s, want status %q", rec.Body.String(), tc.rcv.result)
				}
				return
			}
			body := decodeError(t, rec)
			if body.Error.Code != tc.code || body.Error.Retryable != tc.retryable || strings.Contains(rec.Body.String(), "example.org") {
				t.Fatalf("error body %s", rec.Body.String())
			}
		})
	}
}

func TestEmailWebhookRefusesOversizedBodiesAndIsAbsentWhenUnconfigured(t *testing.T) {
	rcv := &fakeWebhook{result: "recorded"}
	rec := httptest.NewRecorder()
	big := strings.Repeat("x", notifications.MaxWebhookBytes+1)
	webhookHandler(rcv).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/email", strings.NewReader(big)))
	if rec.Code != http.StatusRequestEntityTooLarge || rcv.got != nil {
		t.Fatalf("oversized body: status %d, receiver called %v", rec.Code, rcv.got != nil)
	}
	rec = httptest.NewRecorder()
	webhookHandler(rcv).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/webhooks/email", nil))
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /webhooks/email = %d, want 404 or 405", rec.Code)
	}
	rec = httptest.NewRecorder()
	newTestHandler(fakePinger{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/email", strings.NewReader("{}")))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unconfigured webhook: status %d, want 404", rec.Code)
	}
}
