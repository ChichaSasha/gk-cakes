// Package httpapi serves the versioned HTTP API. Responses use the K00 error
// body and never expose SQL, provider or private detail.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/notifications"
	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
)

// RequestDeadline is the proposed overall API request budget (architecture.md).
const RequestDeadline = 10 * time.Second

const readinessTimeout = 2 * time.Second

// Pinger checks database reachability.
type Pinger interface {
	Ping(ctx context.Context) error
}

// EmailWebhook verifies and stores one email provider webhook delivery
// (notifications.Webhook).
type EmailWebhook interface {
	Receive(ctx context.Context, h http.Header, body []byte) (string, error)
}

// StripeWebhook verifies and stores one Stripe webhook delivery
// (payments.Webhook).
type StripeWebhook interface {
	Receive(ctx context.Context, h http.Header, body []byte) (string, error)
}

// Options configures the handler.
type Options struct {
	// UIOrigin is the single exact browser origin allowed by CORS.
	UIOrigin string
	DB       Pinger
	Logger   *slog.Logger
	// EmailWebhook serves POST /webhooks/email when set (EMAIL_WEBHOOK_SECRET).
	EmailWebhook EmailWebhook
	// StripeWebhook serves POST /webhooks/stripe when set
	// (STRIPE_WEBHOOK_SECRET).
	StripeWebhook StripeWebhook
	// Identity serves the authenticated /api/v1 routes when set (AUTH_ISSUER).
	Identity Identity
	// Catalog serves the public catalog when set, and the chef catalog when
	// Identity is set too.
	Catalog Catalog
	// Schedule serves the public schedule and ready cakes when set, and the
	// staff schedule, day and lot routes when Identity is set too.
	Schedule Schedule
	// Checkout serves storefront quotes when set (signed-in quotes and the
	// chef's discount codes need Identity too).
	Checkout Checkout
	// CheckoutEnabled also serves POST /api/v1/checkouts (config
	// CHECKOUT_ENABLED; off until payments exist).
	CheckoutEnabled bool
}

// NewHandler returns the API handler with its middleware chain.
func NewHandler(opts Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()
		if err := opts.DB.Ping(ctx); err != nil {
			opts.Logger.Warn("readiness check failed", "request_id", requestID(r.Context()))
			writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Service temporarily unavailable", true)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	if opts.EmailWebhook != nil {
		mux.HandleFunc("POST /webhooks/email", emailWebhook(opts.EmailWebhook))
	}
	if opts.StripeWebhook != nil {
		mux.HandleFunc("POST /webhooks/stripe", stripeWebhook(opts.StripeWebhook))
	}
	if opts.Identity != nil {
		identityRoutes(mux, opts.Identity, opts.Logger)
	}
	if opts.Catalog != nil {
		catalogPublicRoutes(mux, opts.Catalog, opts.Logger)
		if opts.Identity != nil {
			catalogStaffRoutes(mux, opts.Catalog, opts.Identity, opts.Logger)
		}
	}
	if opts.Checkout != nil {
		checkoutRoutes(mux, opts.Checkout, opts.Identity, opts.CheckoutEnabled, opts.Logger)
	}
	if opts.Schedule != nil {
		schedulePublicRoutes(mux, opts.Schedule, opts.Logger)
		if opts.Identity != nil {
			scheduleStaffRoutes(mux, opts.Schedule, opts.Identity, opts.Logger)
		}
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Not found", false)
	})

	var h http.Handler = mux
	h = withRecovery(opts.Logger, h)
	h = withCORS(opts.UIOrigin, h)
	h = withDeadline(h)
	h = withAccessLog(opts.Logger, h)
	return withRequestID(h)
}

// emailWebhook acknowledges a delivery only after it is stored: an invalid
// signature is 401, an unusable signed event 400, a storage failure 503 (the
// provider retries with the same svix-id). Bodies are never logged.
func emailWebhook(rcv EmailWebhook) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, notifications.MaxWebhookBytes))
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			writeError(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "Payload too large", false)
			return
		case err != nil:
			writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Unreadable request body", false)
			return
		}
		result, err := rcv.Receive(r.Context(), r.Header, body)
		switch {
		case errors.Is(err, notifications.ErrInvalidSignature):
			writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid webhook signature", false)
		case errors.Is(err, notifications.ErrMalformedEvent):
			writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Malformed webhook event", false)
		case err != nil:
			writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Service temporarily unavailable", true)
		default:
			writeJSON(w, http.StatusOK, map[string]string{"status": result})
		}
	}
}

// stripeWebhook acknowledges a delivery only after it is stored: an invalid
// signature or an unusable signed event is 400 (Stripe retries non-2xx), a
// storage failure 503. Bodies are never logged.
func stripeWebhook(rcv StripeWebhook) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, payments.MaxWebhookBytes))
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			writeError(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "Payload too large", false)
			return
		case err != nil:
			writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Unreadable request body", false)
			return
		}
		result, err := rcv.Receive(r.Context(), r.Header, body)
		switch {
		case errors.Is(err, payments.ErrInvalidSignature):
			writeError(w, r, http.StatusBadRequest, "BAD_SIGNATURE", "Invalid webhook signature", false)
		case errors.Is(err, payments.ErrMalformedEvent):
			writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Malformed webhook event", false)
		case err != nil:
			writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Service temporarily unavailable", true)
		default:
			writeJSON(w, http.StatusOK, map[string]string{"status": result})
		}
	}
}

// Serve runs h on ln until ctx is cancelled, then stops accepting connections
// and waits up to shutdownTimeout for in-flight requests to finish.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, shutdownTimeout time.Duration) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       RequestDeadline,
		WriteTimeout:      RequestDeadline + 5*time.Second,
		IdleTimeout:       60 * time.Second,
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

type ctxKey struct{}

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b [16]byte
		_, _ = rand.Read(b[:])
		id := hex.EncodeToString(b[:])
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

func withDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), RequestDeadline)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func withCORS(origin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Add("Vary", "Origin")
		if r.Header.Get("Origin") != origin {
			next.ServeHTTP(w, r)
			return
		}
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Expose-Headers", "X-Request-Id, Idempotent-Replayed")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Quote-Proof, X-Order-Capability")
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withRecovery(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				logger.Error("handler panic", "request_id", requestID(r.Context()))
				writeError(w, r, http.StatusInternalServerError, "INTERNAL", "Something went wrong", true)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// withAccessLog logs the route path only; query strings are never logged.
func withAccessLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Info("request",
			"request_id", requestID(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Retryable bool   `json:"retryable"`
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, retryable bool) {
	writeJSON(w, status, map[string]apiError{"error": {
		Code:      code,
		Message:   message,
		RequestID: requestID(r.Context()),
		Retryable: retryable,
	}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
