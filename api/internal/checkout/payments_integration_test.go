//go:build integration

package checkout_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
	"github.com/jackc/pgx/v5"
)

const (
	stripeAccount = "acct_1TestGkCakes"
	stripeSecret  = "whsec_test_gk_cakes_signing_secret"
	uiOrigin      = "https://gk.example"
)

// fakeStripe is a Checkout Session API: idempotent creates by key (a
// different body under a used key is an idempotency_error), retrieves with
// the payment intent and its latest charge expanded, and switches to drop a
// response after recording the request or to answer an error.
type fakeStripe struct {
	t        *testing.T
	mu       sync.Mutex
	sessions map[string]*fakeSession
	byKey    map[string]string // idempotency key -> session ID
	forms    map[string]string // idempotency key -> form
	creates  []string          // idempotency key of every create request received
	drop     int               // drop this many create responses after recording
	status   int               // answer creates with this status (error body)
	errType  string
	livemode bool
	skew     time.Duration // Stripe's clock ahead of ours (a later retry)
	// Expire requests: the idempotency key of every one received, the
	// answer replayed per key, a forced error status (no effect), and how
	// many answers to drop after expiring.
	expires      []string
	expireByKey  map[string]fakeAnswer
	expireStatus int
	expireDrop   int
	// block (with arrived) holds a create request after it reached Stripe
	// until the test lets it go.
	block, arrived chan struct{}
	// Refunds (GK-025 A3c2): see refund_worker_integration_test.go.
	refunds fakeRefunds
}

type fakeAnswer struct {
	status int
	body   []byte
}

type fakeSession struct {
	id, status, paymentStatus, currency string
	amount                              int64
	expiresAt                           int64
	metadata, piMetadata                map[string]string
	clientRef                           string
	pi                                  map[string]any
}

func newFakeStripe(t *testing.T) (*fakeStripe, *httptest.Server) {
	f := &fakeStripe{t: t, sessions: map[string]*fakeSession{}, byKey: map[string]string{}, forms: map[string]string{},
		expireByKey: map[string]fakeAnswer{}, refunds: newFakeRefunds()}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeStripe) serve(w http.ResponseWriter, r *http.Request) {
	if user, _, ok := r.BasicAuth(); !ok || user != "sk_test_fake" || r.Header.Get("Stripe-Version") != payments.APIVersion {
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"error":{"type":"authentication_error"}}`)
		return
	}
	f.mu.Lock()
	block, arrived := f.block, f.arrived
	f.mu.Unlock()
	if block != nil && r.Method == http.MethodPost && r.URL.Path == "/v1/checkout/sessions" {
		arrived <- struct{}{}
		<-block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/v1/refunds" || strings.HasPrefix(r.URL.Path, "/v1/refunds/"):
		f.serveRefunds(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/checkout/sessions/") && strings.HasSuffix(r.URL.Path, "/expire"):
		key := r.Header.Get("Idempotency-Key")
		f.expires = append(f.expires, key)
		if f.expireStatus != 0 {
			w.WriteHeader(f.expireStatus)
			_, _ = io.WriteString(w, `{"error":{"type":"api_error"}}`)
			return
		}
		a, seen := f.expireByKey[key]
		if !seen {
			s, ok := f.sessions[strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/checkout/sessions/"), "/expire")]
			switch {
			case !ok:
				a = fakeAnswer{404, []byte(`{"error":{"type":"invalid_request_error"}}`)}
			case s.status != "open":
				// Stripe: only open sessions can be expired.
				a = fakeAnswer{400, []byte(`{"error":{"type":"invalid_request_error","message":"Only Checkout Sessions with a status in [\"open\"] can be expired."}}`)}
			default:
				s.status = "expired"
				a = fakeAnswer{200, f.sessionJSON(s, false)}
			}
			if key != "" {
				f.expireByKey[key] = a
			}
		}
		if f.expireDrop > 0 {
			f.expireDrop--
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(a.status)
		_, _ = w.Write(a.body)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/checkout/sessions":
		body, _ := io.ReadAll(r.Body)
		form := string(body)
		key := r.Header.Get("Idempotency-Key")
		f.creates = append(f.creates, key)
		if f.status != 0 {
			w.WriteHeader(f.status)
			fmt.Fprintf(w, `{"error":{"type":%q,"message":"fake refusal"}}`, f.errType)
			return
		}
		id, seen := f.byKey[key]
		if seen && f.forms[key] != form {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":{"type":"idempotency_error"}}`)
			return
		}
		if !seen {
			v, err := url.ParseQuery(form)
			if err != nil {
				w.WriteHeader(400)
				return
			}
			amount, _ := strconv.ParseInt(v.Get("line_items[0][price_data][unit_amount]"), 10, 64)
			// Stripe: expires_at at least 30 minutes after creation (about
			// a minute of tolerance), at most 24 hours.
			exp, _ := strconv.ParseInt(v.Get("expires_at"), 10, 64)
			now := time.Now().Add(f.skew)
			if exp < now.Add(29*time.Minute).Unix() || exp > now.Add(24*time.Hour).Unix() {
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"The expires_at timestamp must be at least 30 minutes from Checkout Session creation."}}`)
				return
			}
			id = fmt.Sprintf("cs_test_fake%d", len(f.sessions)+1)
			f.sessions[id] = &fakeSession{id: id, status: "open", paymentStatus: "unpaid", currency: "gbp", amount: amount,
				expiresAt: exp, clientRef: v.Get("client_reference_id"),
				metadata: map[string]string{"order_id": v.Get("metadata[order_id]"), "collection_id": v.Get("metadata[collection_id]"),
					"environment": v.Get("metadata[environment]")},
				piMetadata: map[string]string{"order_id": v.Get("payment_intent_data[metadata][order_id]"),
					"collection_id": v.Get("payment_intent_data[metadata][collection_id]"),
					"environment":   v.Get("payment_intent_data[metadata][environment]")}}
			f.byKey[key], f.forms[key] = id, form
		}
		if f.drop > 0 {
			f.drop--
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		f.write(w, f.sessions[id], false)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/checkout/sessions/"):
		s, ok := f.sessions[strings.TrimPrefix(r.URL.Path, "/v1/checkout/sessions/")]
		if !ok {
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error"}}`)
			return
		}
		f.write(w, s, r.URL.Query().Get("expand[]") == "payment_intent.latest_charge")
	default:
		w.WriteHeader(404)
	}
}

func (f *fakeStripe) write(w http.ResponseWriter, s *fakeSession, expand bool) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(f.sessionJSON(s, expand))
}

func (f *fakeStripe) sessionJSON(s *fakeSession, expand bool) []byte {
	out := map[string]any{"id": s.id, "object": "checkout.session", "status": s.status, "payment_status": s.paymentStatus,
		"amount_total": s.amount, "currency": s.currency, "client_reference_id": s.clientRef, "livemode": f.livemode,
		"url": "https://checkout.stripe.test/c/pay/" + s.id, "expires_at": s.expiresAt, "metadata": s.metadata,
		"payment_intent": nil, "customer_details": map[string]any{"email": "payer@example.com"}}
	if s.pi != nil {
		if expand {
			out["payment_intent"] = s.pi
		} else {
			out["payment_intent"] = s.pi["id"]
		}
	}
	b, _ := json.Marshal(out)
	return b
}

// pay completes session id with a fully captured card payment whose charge
// was created at created.
func (f *fakeStripe) pay(id string, created time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.sessions[id]
	s.status, s.paymentStatus = "complete", "paid"
	s.pi = map[string]any{"id": "pi_fake_" + id, "object": "payment_intent", "status": "succeeded", "amount": s.amount,
		"amount_received": s.amount, "currency": "gbp", "livemode": f.livemode, "metadata": s.piMetadata,
		"latest_charge": map[string]any{"id": "ch_fake_" + id, "object": "charge", "status": "succeeded", "paid": true,
			"captured": true, "amount": s.amount, "amount_captured": s.amount, "currency": "gbp", "created": created.Unix(),
			"livemode": f.livemode, "billing_details": map[string]any{"name": "Payer Name"}}}
}

// processing puts session id into an asynchronous payment still processing.
func (f *fakeStripe) processing(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.sessions[id]
	s.status, s.paymentStatus = "complete", "unpaid"
	s.pi = map[string]any{"id": "pi_fake_" + id, "object": "payment_intent", "status": "processing", "amount": s.amount,
		"amount_received": 0, "currency": "gbp", "livemode": f.livemode, "metadata": s.piMetadata,
		"latest_charge": map[string]any{"id": "ch_fake_" + id, "object": "charge", "status": "pending", "paid": false,
			"captured": false, "amount": s.amount, "amount_captured": 0, "currency": "gbp", "created": time.Now().Unix(),
			"livemode": f.livemode}}
}

func (f *fakeStripe) session(id string) *fakeSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[id]
}

type payHarness struct {
	*commandHarness
	stripe  *fakeStripe
	pay     *payments.Worker
	reviews *checkout.HoldReviews
	webhook *payments.Webhook
	refunds *payments.Refunds // chained by withRefunds
	handler jobs.Handler
}

func newPayHarness(t *testing.T) *payHarness {
	t.Helper()
	h := newCommandHarness(t)
	h.co.StripeAccount, h.co.UIOrigin = stripeAccount, uiOrigin
	fs, srv := newFakeStripe(t)
	client, err := payments.NewClient(payments.ClientConfig{Key: "sk_test_fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(h.logs, nil))
	pay := &payments.Worker{DB: h.worker, Coordinator: h.coord, Provider: client, Environment: "test", Account: stripeAccount, Logger: logger}
	wh, err := payments.NewWebhook(payments.WebhookConfig{DB: h.api, Secret: stripeSecret, Environment: "test", Account: stripeAccount, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	reviews := &checkout.HoldReviews{DB: h.worker, Logger: logger}
	refunds := &payments.Refunds{DB: h.worker, Coordinator: h.coord, Provider: client, Environment: "test", Account: stripeAccount, Logger: logger}
	return &payHarness{commandHarness: h, stripe: fs, pay: pay, reviews: reviews, webhook: wh, refunds: refunds,
		handler: jobs.Chain(h.cmds.Handle, pay.Handle, reviews.Handle)}
}

// step claims the next due job (false when none) and runs it, finalizing
// the job as the jobs worker would; it returns the handler's error.
func (h *payHarness) step(t *testing.T) (bool, error) {
	t.Helper()
	h.workers++
	job := jobs.Job{Worker: fmt.Sprintf("00000000-0000-4000-8000-%012d", h.workers)}
	err := h.worker.QueryRow(h.ctx, `SELECT job_id::text, job_operation_id::text, job_generation
		FROM business.job_claim($1::uuid, interval '60 seconds')`, job.Worker).Scan(&job.ID, &job.OperationID, &job.Generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		t.Fatal(err)
	}
	herr := h.handler(h.ctx, job)
	var (
		ae *jobs.AttentionError
		de *jobs.DeferError
		ok bool
	)
	args := []any{job.ID, job.Worker, job.Generation}
	switch {
	case herr == nil:
		err = h.worker.QueryRow(h.ctx, "SELECT business.job_complete($1::uuid, $2::uuid, $3)", args...).Scan(&ok)
	case errors.As(herr, &ae):
		err = h.worker.QueryRow(h.ctx, "SELECT business.job_attention($1::uuid, $2::uuid, $3, $4)", append(args, ae.Reason)...).Scan(&ok)
	case errors.As(herr, &de):
		err = h.worker.QueryRow(h.ctx, "SELECT business.job_release($1::uuid, $2::uuid, $3, $4)", append(args, de.After)...).Scan(&ok)
	default:
		var state string
		err = h.worker.QueryRow(h.ctx, "SELECT job_state FROM business.job_fail($1::uuid, $2::uuid, $3, $4)",
			append(args, "test failure")...).Scan(&state)
	}
	if err != nil {
		t.Fatalf("finalize %v: %v", herr, err)
	}
	return true, herr
}

// drain runs due jobs until none is left and returns their errors.
func (h *payHarness) drain(t *testing.T) []error {
	t.Helper()
	var errs []error
	for range 50 {
		ran, err := h.step(t)
		if !ran {
			return errs
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	t.Fatalf("jobs never drained:\n%s", reasons(errs[max(0, len(errs)-6):]))
	return nil
}

// due makes every waiting job of the collection (and its paid command)
// claimable now: the passage of time.
func (h *payHarness) due(t *testing.T) {
	t.Helper()
	h.owner(t, "UPDATE business.job SET next_attempt_at = now() WHERE state IN ('queued', 'retry')")
}

// withoutRefundDispatch drops the errors of refund jobs no handler takes
// (the refund worker is chained only by withRefunds, so gateway tests can
// drive a refund job call by call).
func withoutRefundDispatch(errs []error) []error {
	var out []error
	for _, err := range errs {
		if !errors.Is(err, jobs.ErrNoHandler) {
			out = append(out, err)
		}
	}
	return out
}

func attention(errs []error) []string {
	var out []string
	for _, err := range errs {
		var ae *jobs.AttentionError
		if errors.As(err, &ae) {
			out = append(out, ae.Reason)
		}
	}
	return out
}

func (h *payHarness) paidOrder(t *testing.T, f fixture, pol string) (order string, digest []byte) {
	t.Helper()
	in := checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot, FulfillmentMode: schedule.Collection,
		Lines: []checkout.QuoteLine{{VariantID: f.v1001, Quantity: 1}, {LotID: f.readyLot, Quantity: 1}}}
	quote, d := h.guestQuote(t, "q-paid", in)
	res, err := h.checkout("", d, "k-paid", quote, pol)
	if err != nil {
		t.Fatal(err)
	}
	return res.ID, d
}

// paidHolds is the order's day allocation and ready-cake reservation, and
// the lot's reserved and sold counts (a paid order has no code here).
func (h *payHarness) paidHolds(t *testing.T, order, lot string) string {
	t.Helper()
	var alloc, res string
	var reserved, sold int
	if err := h.admin.QueryRow(h.ctx, `SELECT
		(SELECT state FROM business.capacity_allocation WHERE order_ref = $1::uuid),
		(SELECT state FROM business.order_lot_reservation WHERE order_id = $1::uuid),
		l.reserved_quantity, l.sold_quantity
		FROM business.finished_cake_lot l WHERE l.id = $2::uuid`, order, lot).Scan(&alloc, &res, &reserved, &sold); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%s/%s %dr%ds", alloc, res, reserved, sold)
}

type paidOrderJSON struct {
	Version   int
	Lifecycle string
	PaidAt    *time.Time `json:"paid_at"`
	Payment   *struct {
		ID          string
		State       string
		AmountMinor int64      `json:"amount_minor"`
		CheckoutURL *string    `json:"checkout_url"`
		ExpiresAt   *time.Time `json:"expires_at"`
	}
}

func (h *payHarness) paidOrderView(t *testing.T, id string, d []byte) paidOrderJSON {
	t.Helper()
	raw, err := h.co.GetOrder(h.ctx, id, "", d)
	if err != nil {
		t.Fatal(err)
	}
	var o paidOrderJSON
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	return o
}

func (h *payHarness) start(order string, d []byte, key string, version int) (catalog.Result, error) {
	return h.co.StartPayment(h.ctx, order, "", d, key, checkout.PaymentInput{ExpectedVersion: version})
}

type collectionRow struct {
	State, SessionID string
	SucceededAt      *time.Time
	Mismatch         *string
}

func (h *payHarness) collection(t *testing.T, id string) collectionRow {
	t.Helper()
	var c collectionRow
	var session *string
	if err := h.admin.QueryRow(h.ctx, `SELECT state, provider_session_id, succeeded_at, mismatch_reason
		FROM business.payment_collection WHERE id = $1::uuid`, id).Scan(&c.State, &session, &c.SucceededAt, &c.Mismatch); err != nil {
		t.Fatal(err)
	}
	if session != nil {
		c.SessionID = *session
	}
	return c
}

// deliver posts a signed Stripe event about object (a session or payment
// intent) of collection to the webhook and returns the result.
func (h *payHarness) deliver(t *testing.T, eventID, eventType, objectType, objectID, collection, order string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"id": eventID, "object": "event", "type": eventType, "livemode": false,
		"created": time.Now().Unix(), "data": map[string]any{"object": map[string]any{"id": objectID, "object": objectType,
			"metadata":         map[string]string{"collection_id": collection, "order_id": order, "environment": "test"},
			"customer_details": map[string]string{"email": "payer@example.com"}}}})
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(stripeSecret))
	fmt.Fprintf(mac, "%d.", ts)
	mac.Write(body)
	hdr := http.Header{}
	hdr.Set(payments.SignatureHeader, fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil))))
	res, err := h.webhook.Receive(h.ctx, hdr, body)
	if err != nil {
		t.Fatalf("deliver %s: %v", eventID, err)
	}
	return res
}

// started initiates a payment and runs its dispatch: the collection and
// its session.
func (h *payHarness) started(t *testing.T, order string, d []byte) (string, string) {
	t.Helper()
	res, err := h.start(order, d, "pay-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("dispatch: %v", errs)
	}
	c := h.collection(t, res.ID)
	if c.State != "pending" || c.SessionID == "" {
		t.Fatalf("after dispatch %+v", c)
	}
	return res.ID, c.SessionID
}

func (h *payHarness) noPersonalDataInLogs(t *testing.T) {
	t.Helper()
	for _, s := range []string{"example.com", "Test Customer", "Payer Name", "sk_test_fake", stripeSecret} {
		if strings.Contains(h.logs.String(), s) {
			t.Fatalf("%q in the logs: %s", s, h.logs.String())
		}
	}
}

func TestPaymentInitiationIsIdempotentAndSerialized(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)

	// Access and version first.
	_, other := proof(t)
	if _, err := h.co.StartPayment(h.ctx, order, "", other, "k", checkout.PaymentInput{ExpectedVersion: 1}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("wrong capability: %v", err)
	}
	if _, err := h.start(order, d, "k", 2); conflict(err) != "stale_version" {
		t.Fatalf("stale version: %v", err)
	}
	h.co.StripeAccount = ""
	if _, err := h.start(order, d, "k", 1); !errors.Is(err, checkout.ErrPaymentsUnavailable) {
		t.Fatalf("without an account: %v", err)
	}
	h.co.StripeAccount = stripeAccount

	first, err := h.start(order, d, "pay-a", 1)
	if err != nil || first.Replayed {
		t.Fatalf("start: %+v %v", first, err)
	}
	if again, err := h.start(order, d, "pay-a", 1); err != nil || !again.Replayed || again.ID != first.ID {
		t.Fatalf("same key: %+v %v, want the same collection", again, err)
	}
	if _, err := h.start(order, d, "pay-b", 1); conflict(err) != "payment_in_progress" {
		t.Fatalf("another key while one may succeed: %v", err)
	}
	// Another capability on the same order with the same key is not a replay.
	_, d2 := proof(t)
	h.owner(t, `INSERT INTO business.order_guest_capability (id, order_id, capability_digest, allowed_commands)
		VALUES (gen_random_uuid(), $1::uuid, $2, ARRAY['read', 'pay'])`, order, d2)
	if _, err := h.start(order, d2, "pay-a", 1); conflict(err) != "payment_in_progress" {
		t.Fatalf("another capability's key: %v", err)
	}
	o := h.paidOrderView(t, order, d)
	if o.Lifecycle != "awaiting_payment" || o.Payment == nil || o.Payment.ID != first.ID || o.Payment.State != "created" ||
		o.Payment.AmountMinor != 9501 || o.Payment.CheckoutURL != nil {
		t.Fatalf("order after the request %+v", o)
	}
	// Nothing is fixed before the first dispatch: no body, no window.
	if got := h.count(t, `SELECT count(*) FROM business.payment_collection WHERE id = $1::uuid AND payload IS NULL
		AND first_dispatch_at IS NULL AND replay_until IS NULL AND session_expires_at IS NULL AND return_origin = $2`, first.ID, uiOrigin); got != 1 {
		t.Fatal("the request was fixed at initiation")
	}

	// Stripe definitively refuses it (for example a dispatch too late for
	// its expiry): failed, and the customer may start a new collection.
	h.stripe.status, h.stripe.errType = 400, "invalid_request_error"
	errs := h.drain(t)
	if got := attention(errs); len(got) != 1 || !strings.Contains(got[0], "rejected") {
		t.Fatalf("rejected dispatch: %v", errs)
	}
	if c := h.collection(t, first.ID); c.State != "failed" {
		t.Fatalf("collection %+v, want failed", c)
	}
	if got := h.count(t, "SELECT count(*) FROM business.payment_attempt WHERE collection_id = $1::uuid AND result = 'rejected' AND provider_code = 'invalid_request_error'", first.ID); got != 1 {
		t.Fatalf("%d rejected attempts", got)
	}
	h.stripe.status = 0
	second, err := h.start(order, d, "pay-b", 1)
	if err != nil || second.ID == first.ID {
		t.Fatalf("new collection after a failure: %+v %v", second, err)
	}
	if _, err := h.start(order, d, "pay-b", 2); conflict(err) != "key_reused" {
		t.Fatalf("the same key with another body: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("second dispatch: %v", errs)
	}
	var payload string
	var fixed, expires time.Time
	if err := h.admin.QueryRow(h.ctx, `SELECT payload, first_dispatch_at, session_expires_at
		FROM business.payment_collection WHERE id = $1::uuid`, second.ID).Scan(&payload, &fixed, &expires); err != nil {
		t.Fatal(err)
	}
	if !expires.Equal(fixed.Truncate(time.Second).Add(2*time.Hour)) || time.Since(fixed) > time.Minute ||
		!strings.Contains(payload, "expires_at="+strconv.FormatInt(expires.Unix(), 10)) ||
		strings.Contains(payload, "example.com") || strings.Contains(payload, "Test Customer") {
		t.Fatalf("stored payload %s (fixed %s, expires %s)", payload, fixed, expires)
	}
	o = h.paidOrderView(t, order, d)
	if o.Payment == nil || o.Payment.ID != second.ID || o.Payment.State != "pending" || o.Payment.CheckoutURL == nil ||
		!strings.HasPrefix(*o.Payment.CheckoutURL, "https://checkout.stripe.test/") || o.Lifecycle != "awaiting_payment" {
		t.Fatalf("order with an open session %+v", o)
	}
	h.noPersonalDataInLogs(t)
}

func TestPaymentInitiationClocks(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	// The quote's 24 h window: equality expires (quote sent Thu 5 Nov 10:00).
	h.now = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	if _, err := h.start(order, d, "k", 1); conflict(err) != "quote_expired" {
		t.Fatalf("at the quote's expiry: %v", err)
	}
	// Lead time to the Sat 14 Nov 12:30 GMT handover: 192 h before is Fri
	// 6 Nov 12:30 UTC, past the quote window here; use a quote nearer the
	// boundary on Sat 21 Nov.
	h.now = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	c, err := h.sch.Choices(h.ctx, day("2026-11-21"), schedule.Collection, 1)
	if err != nil || len(c.Slots) == 0 {
		t.Fatalf("slots: %+v %v", c, err)
	}
	quote, d2 := h.guestQuote(t, "q-late", checkout.QuoteInput{Date: "2026-11-21", SlotID: c.Slots[0].ID,
		FulfillmentMode: schedule.Collection, Lines: []checkout.QuoteLine{{VariantID: f.v1000, Quantity: 1}}})
	late := must[catalog.Result](t)(h.checkout("", d2, "k-late", quote, pol)).ID
	handover := time.Date(2026, 11, 21, 12, 30, 0, 0, time.UTC)
	h.now = handover.Add(-192 * time.Hour).Add(time.Microsecond)
	if _, err := h.start(late, d2, "late", 1); conflict(err) != "lead_time" {
		t.Fatalf("one microsecond inside the lead time: %v", err)
	}
	h.now = handover.Add(-192 * time.Hour)
	if _, err := h.start(late, d2, "exact", 1); err != nil {
		t.Fatalf("exactly 192 h before: %v", err)
	}
	// A free order is never paid.
	h.now = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	free, fd := h.freeOrder(t, f, pol)
	if _, err := h.start(free, fd, "k", 1); conflict(err) != "state_conflict" {
		t.Fatalf("free order: %v", err)
	}
}

// The create response is lost after Stripe recorded it: the retry sends the
// same key and body and gets the same session; exactly one session exists.
func TestLostCreateResponseIsRetriedWithTheSameKeyAndBody(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	res, err := h.start(order, d, "pay-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	h.stripe.drop = 1
	errs := h.drain(t)
	if len(errs) != 1 || len(attention(errs)) != 0 {
		t.Fatalf("lost response: %v, want one retryable error", errs)
	}
	if c := h.collection(t, res.ID); c.State != "unknown" || h.recoveryState(t, res.ID) != "prepared" {
		t.Fatalf("after the lost response %+v (%s)", c, h.recoveryState(t, res.ID))
	}
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("retry: %v", errs)
	}
	c := h.collection(t, res.ID)
	if c.State != "pending" || len(h.stripe.sessions) != 1 || len(h.stripe.creates) != 2 ||
		h.stripe.creates[0] != res.ID || h.stripe.creates[1] != res.ID || c.SessionID != "cs_test_fake1" {
		t.Fatalf("collection %+v, sessions %d, creates %v", c, len(h.stripe.sessions), h.stripe.creates)
	}
	if got := h.count(t, "SELECT count(*) FROM business.payment_attempt WHERE collection_id = $1::uuid", res.ID); got != 2 {
		t.Fatalf("%d attempts, want 2", got)
	}
	// The journal holds the exact request, once.
	key, _ := h.coord.EnvelopeKey(recovery.Operation{ID: res.ID, AggregateID: res.ID})
	if h.journal.Puts(key+".prepared") != 1 || h.journal.Puts(key+".outcome") != 1 {
		t.Fatalf("envelopes %d/%d", h.journal.Puts(key+".prepared"), h.journal.Puts(key+".outcome"))
	}
	if h.recoveryState(t, res.ID) != "secured" {
		t.Fatalf("dispatch %s, want secured", h.recoveryState(t, res.ID))
	}
	var started *time.Time
	if err := h.admin.QueryRow(h.ctx, "SELECT dispatch_started_at FROM business.payment_collection WHERE id = $1::uuid", res.ID).Scan(&started); err != nil || started == nil {
		t.Fatalf("dispatch_started_at %v %v", started, err)
	}
}

// The customer returns before any webhook: nothing confirms until the
// reconcile verifies Stripe's evidence; the order is confirmation_pending
// until the outcome is secured, then confirmed with paid_at = the charge's
// time.
func TestReturnBeforeWebhookAndConfirmationPendingUntilSecured(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	charged := time.Date(2026, 11, 5, 10, 7, 3, 0, time.UTC)
	h.stripe.pay(session, charged)
	// Return (the order page is read): nothing confirmed.
	if o := h.paidOrderView(t, order, d); o.Lifecycle != "awaiting_payment" || o.PaidAt != nil || o.Payment.State != "pending" {
		t.Fatalf("after the return %+v", o)
	}
	if got := h.paidHolds(t, order, f.readyLot); got != "tentative/reserved 1r0s" {
		t.Fatalf("holds %s", got)
	}
	// The webhook arrives; the outcome envelope fails once.
	if r := h.deliver(t, "evt_completed", "checkout.session.completed", "checkout.session", session, coll, order); r != payments.WebhookRecorded {
		t.Fatalf("webhook %s", r)
	}
	h.journal.Fail = func(op, name string) error {
		if op == "put" && strings.HasSuffix(name, ".outcome") && !strings.Contains(name, coll) {
			return errors.New("b2 unavailable")
		}
		return nil
	}
	errs := h.drain(t)
	if len(errs) != 1 {
		t.Fatalf("with the outcome envelope failing: %v", errs)
	}
	c := h.collection(t, coll)
	if c.State != "succeeded" || c.SucceededAt == nil || !c.SucceededAt.Equal(charged) {
		t.Fatalf("collection %+v", c)
	}
	o := h.paidOrderView(t, order, d)
	if o.Lifecycle != "confirmation_pending" || o.PaidAt != nil || o.Payment.State != "succeeded" || o.Payment.CheckoutURL != nil {
		t.Fatalf("committed but unsecured %+v", o)
	}
	if got := h.paidHolds(t, order, f.readyLot); got != "committed/sold 0r1s" {
		t.Fatalf("holds after the commit %s", got)
	}
	h.journal.Fail = nil
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("securing: %v", errs)
	}
	o = h.paidOrderView(t, order, d)
	if o.Lifecycle != "confirmed" || o.PaidAt == nil || !o.PaidAt.Equal(charged) {
		t.Fatalf("secured %+v; want confirmed with paid_at %s", o, charged)
	}
	var cmdPayload string
	if err := h.admin.QueryRow(h.ctx, "SELECT payload FROM business.order_command WHERE collection_id = $1::uuid", coll).Scan(&cmdPayload); err != nil {
		t.Fatal(err)
	}
	var p struct {
		Command    string
		ReceivedAt string `json:"received_at"`
		Actor      struct{ Kind string }
		Collection struct {
			ID          string
			AmountMinor int64  `json:"amount_minor"`
			SessionID   string `json:"session_id"`
			Evidence    payments.Evidence
		}
	}
	if err := json.Unmarshal([]byte(cmdPayload), &p); err != nil || p.Command != "order.paid_confirm" || p.Actor.Kind != "system" ||
		p.ReceivedAt != charged.Format("2006-01-02T15:04:05.000000Z") || p.Collection.ID != coll || p.Collection.AmountMinor != 9501 ||
		p.Collection.SessionID != session || p.Collection.Evidence.ChargeStatus != "succeeded" {
		t.Fatalf("command payload %s (%v)", cmdPayload, err)
	}
	if strings.Contains(cmdPayload, "payer@example.com") || strings.Contains(cmdPayload, "Payer Name") {
		t.Fatalf("payer data in the command payload: %s", cmdPayload)
	}
	h.noPersonalDataInLogs(t)
}

// Out-of-order and duplicate events (the payment intent succeeds before the
// session completes; the same event twice) make exactly one paid
// confirmation.
func TestDuplicateAndOutOfOrderEventsConfirmOnce(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	// The payment intent event arrives while Stripe still shows processing.
	h.stripe.processing(session)
	pi := "pi_fake_" + session
	if r := h.deliver(t, "evt_pi_processing", "payment_intent.processing", "payment_intent", pi, coll, order); r != payments.WebhookRecorded {
		t.Fatal(r)
	}
	errs := h.drain(t)
	if len(errs) != 1 || h.collection(t, coll).State != "pending" {
		t.Fatalf("processing: %v %+v", errs, h.collection(t, coll))
	}
	var de *jobs.DeferError
	if !errors.As(errs[0], &de) {
		t.Fatalf("processing reconcile: %v, want a recheck later", errs[0])
	}
	h.stripe.pay(session, time.Date(2026, 11, 5, 10, 9, 0, 0, time.UTC))
	h.deliver(t, "evt_pi_succeeded", "payment_intent.succeeded", "payment_intent", pi, coll, order)
	if r := h.deliver(t, "evt_pi_succeeded", "payment_intent.succeeded", "payment_intent", pi, coll, order); r != payments.WebhookDuplicate {
		t.Fatalf("duplicate event: %s", r)
	}
	// The waiting reconcile was brought forward rather than duplicated.
	if got := h.count(t, `SELECT count(*) FROM business.payment_job pj JOIN business.job j ON j.id = pj.job_id
		WHERE pj.collection_id = $1::uuid AND pj.purpose = 'reconcile' AND j.state IN ('queued', 'retry')`, coll); got != 1 {
		t.Fatalf("%d waiting reconcile jobs, want 1", got)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("paid: %v", errs)
	}
	// The session completes later; then the same again.
	h.deliver(t, "evt_completed", "checkout.session.completed", "checkout.session", session, coll, order)
	h.deliver(t, "evt_completed", "checkout.session.completed", "checkout.session", session, coll, order)
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("after the late events: %v", errs)
	}
	if got := h.count(t, "SELECT count(*) FROM business.order_command WHERE order_id = $1::uuid", order); got != 1 {
		t.Fatalf("%d order commands, want 1", got)
	}
	if got := h.count(t, "SELECT count(*) FROM business.audit_event WHERE action = 'order.paid_confirmed' AND subject_id = $1::uuid", order); got != 1 {
		t.Fatalf("%d paid confirmations, want 1", got)
	}
	if got := h.count(t, "SELECT count(*) FROM business.stripe_event WHERE collection_id = $1::uuid", coll); got != 3 {
		t.Fatalf("%d stored events, want 3", got)
	}
	if o := h.paidOrderView(t, order, d); o.Lifecycle != "confirmed" || o.Version != 2 {
		t.Fatalf("order %+v", o)
	}
}

// The webhook is lost: the reconcile scheduled after the session was
// created confirms the payment.
func TestLostWebhookIsCoveredByTheScheduledReconcile(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	var due time.Time
	if err := h.admin.QueryRow(h.ctx, `SELECT j.next_attempt_at FROM business.payment_job pj JOIN business.job j ON j.id = pj.job_id
		WHERE pj.collection_id = $1::uuid AND pj.purpose = 'reconcile'`, coll).Scan(&due); err != nil {
		t.Fatal(err)
	}
	if d := time.Until(due); d < 14*time.Minute || d > 16*time.Minute {
		t.Fatalf("reconcile due in %s, want about 15 minutes", d)
	}
	h.stripe.pay(session, time.Date(2026, 11, 5, 10, 20, 0, 0, time.UTC))
	if ran, _ := h.step(t); ran {
		t.Fatal("a job was due before the reconcile time")
	}
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("scheduled reconcile: %v", errs)
	}
	if o := h.paidOrderView(t, order, d); o.Lifecycle != "confirmed" || o.PaidAt == nil {
		t.Fatalf("order %+v", o)
	}
}

// A paid session for another amount is never confirmed: unknown and
// attention. A session still processing stays pending.
func TestWrongAmountGoesToAttentionAndProcessingStaysPending(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	h.stripe.processing(session)
	h.due(t)
	errs := h.drain(t)
	var de *jobs.DeferError
	if len(errs) != 1 || !errors.As(errs[0], &de) || h.collection(t, coll).State != "pending" {
		t.Fatalf("processing: %v %+v", errs, h.collection(t, coll))
	}
	h.stripe.pay(session, time.Date(2026, 11, 5, 10, 9, 0, 0, time.UTC))
	h.stripe.session(session).amount = 100
	h.deliver(t, "evt_completed", "checkout.session.completed", "checkout.session", session, coll, order)
	errs = h.drain(t)
	if got := attention(errs); len(got) != 1 || !strings.Contains(got[0], "does not match") {
		t.Fatalf("wrong amount: %v", errs)
	}
	c := h.collection(t, coll)
	if c.State != "unknown" || c.Mismatch == nil || h.count(t, "SELECT count(*) FROM business.order_command WHERE order_id = $1::uuid", order) != 0 {
		t.Fatalf("collection %+v; want unknown and no confirmation", c)
	}
	if o := h.paidOrderView(t, order, d); o.Lifecycle != "awaiting_payment" {
		t.Fatalf("order %+v", o)
	}
	// A new collection is not allowed while this one is unknown.
	if _, err := h.start(order, d, "pay-2", 1); conflict(err) != "payment_in_progress" {
		t.Fatalf("new collection while unknown: %v", err)
	}
}

// Success after the 192 h lead time (K04 Q2): paid_awaiting_agreement, the
// money kept, the job in attention.
func TestPaymentAfterTheLeadTimeAwaitsAgreement(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	// 192 h before the Sat 14 Nov 12:30 GMT handover is Fri 6 Nov 12:30.
	charged := time.Date(2026, 11, 6, 12, 30, 1, 0, time.UTC)
	h.stripe.pay(session, charged)
	h.deliver(t, "evt_completed", "checkout.session.completed", "checkout.session", session, coll, order)
	errs := h.drain(t)
	if got := attention(errs); len(got) != 1 || !strings.Contains(got[0], "paid_awaiting_agreement") {
		t.Fatalf("late payment: %v", errs)
	}
	o := h.paidOrderView(t, order, d)
	if o.Lifecycle != "paid_awaiting_agreement" || o.PaidAt == nil || !o.PaidAt.Equal(charged) {
		t.Fatalf("order %+v", o)
	}
}

func TestPaymentExactlyAtTheLeadTimeIsConfirmed(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	h.stripe.pay(session, time.Date(2026, 11, 6, 12, 30, 0, 0, time.UTC))
	h.deliver(t, "evt_completed", "checkout.session.completed", "checkout.session", session, coll, order)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("at the boundary: %v", errs)
	}
	if o := h.paidOrderView(t, order, d); o.Lifecycle != "confirmed" {
		t.Fatalf("order %+v", o)
	}
}

// A paid success after the order lost its holds: refused, the collection
// stays succeeded and the job goes to attention (full return is A3c2).
func TestPaidSuccessAfterTheOrderChangedIsRefused(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	h.stripe.pay(session, time.Date(2026, 11, 5, 10, 9, 0, 0, time.UTC))
	h.deliver(t, "evt_completed", "checkout.session.completed", "checkout.session", session, coll, order)
	// The reconcile records the success; the hold is released before the
	// command commits.
	ran, err := h.step(t)
	if !ran || err != nil || h.collection(t, coll).State != "succeeded" {
		t.Fatalf("reconcile: %v %v", ran, err)
	}
	// K11: payment received, confirmation being secured.
	if o := h.paidOrderView(t, order, d); o.Lifecycle != "confirmation_pending" || o.PaidAt != nil || o.Payment.State != "succeeded" {
		t.Fatalf("recorded but not committed %+v", o)
	}
	h.owner(t, "UPDATE business.capacity_allocation SET state = 'released', expires_at = NULL WHERE order_ref = $1::uuid", order)
	errs := h.drain(t)
	if got := attention(errs); len(got) != 1 || !strings.Contains(got[0], "full return") {
		t.Fatalf("refused paid confirmation: %v", errs)
	}
	o := h.paidOrderView(t, order, d)
	if o.Lifecycle != "awaiting_payment" || o.PaidAt != nil || o.Payment.State != "succeeded" || o.Version != 2 {
		t.Fatalf("order %+v", o)
	}
	if c := h.collection(t, coll); c.State != "succeeded" {
		t.Fatalf("collection %+v, want still succeeded", c)
	}
	// No second collection is possible for this order.
	if _, err := h.start(order, d, "pay-2", 2); conflict(err) != "state_conflict" {
		t.Fatalf("new collection after a success: %v", err)
	}
	// The order is not cancelled: no automatic full return (GK-025 A3c2).
	if h.count(t, "SELECT count(*) FROM business.refund_goal WHERE order_id = $1::uuid", order) != 0 ||
		h.count(t, "SELECT count(*) FROM business.refund_operation WHERE order_id = $1::uuid", order) != 0 ||
		h.count(t, "SELECT count(*) FROM business.refund_late_capture($1::uuid) g WHERE g IS NOT NULL", order) != 0 {
		t.Fatal("a full return was recorded for an order that is not cancelled")
	}
}

// A restore freeze defers dispatch and reconcile; nothing is reserved,
// called or recorded until it lifts.
func TestRestoreFreezeDefersPayments(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	res, err := h.start(order, d, "pay-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	errs := h.drain(t)
	var de *jobs.DeferError
	if len(errs) != 1 || !errors.As(errs[0], &de) || len(h.stripe.creates) != 0 || h.count(t, "SELECT count(*) FROM business.recovery_operation WHERE id = $1::uuid", res.ID) != 0 {
		t.Fatalf("frozen dispatch: %v, creates %v", errs, h.stripe.creates)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("after the freeze: %v", errs)
	}
	session := h.collection(t, res.ID).SessionID
	h.stripe.pay(session, time.Date(2026, 11, 5, 10, 9, 0, 0, time.UTC))
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	h.deliver(t, "evt_completed", "checkout.session.completed", "checkout.session", session, res.ID, order)
	errs = h.drain(t)
	if len(errs) != 1 || !errors.As(errs[0], &de) || h.collection(t, res.ID).State != "pending" {
		t.Fatalf("frozen reconcile: %v %+v", errs, h.collection(t, res.ID))
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("after the freeze: %v", errs)
	}
	if o := h.paidOrderView(t, order, d); o.Lifecycle != "confirmed" {
		t.Fatalf("order %+v", o)
	}
}

// An expired unpaid session cancels the collection and allows a new one;
// the holds stay until the order's hold has ended (the hold review, GK-025
// A3c1: see expiry_integration_test.go).
func TestExpiredSessionCancelsTheCollection(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	h.stripe.session(session).status = "expired"
	h.deliver(t, "evt_expired", "checkout.session.expired", "checkout.session", session, coll, order)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("expired: %v", errs)
	}
	if c := h.collection(t, coll); c.State != "cancelled" {
		t.Fatalf("collection %+v", c)
	}
	if got := h.paidHolds(t, order, f.readyLot); got != "tentative/reserved 1r0s" {
		t.Fatalf("holds %s, want untouched", got)
	}
	// Events after the decision schedule nothing.
	h.deliver(t, "evt_late", "checkout.session.expired", "checkout.session", session, coll, order)
	if ran, _ := h.step(t); ran {
		t.Fatal("a job was scheduled for a cancelled collection")
	}
	if _, err := h.start(order, d, "pay-2", 1); err != nil {
		t.Fatalf("new collection after an expiry: %v", err)
	}
}

// An idempotency conflict is never answered with a new session: the
// dispatch goes to reconciliation and the collection is unknown.
func TestIdempotencyConflictGoesToReconciliation(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	res, err := h.start(order, d, "pay-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	h.stripe.status, h.stripe.errType = 400, "idempotency_error"
	errs := h.drain(t)
	if got := attention(errs); len(got) != 1 || !strings.Contains(got[0], "reconciliation") {
		t.Fatalf("conflict: %v", errs)
	}
	if c := h.collection(t, res.ID); c.State != "unknown" || h.recoveryState(t, res.ID) != "reconcile" {
		t.Fatalf("collection %+v (%s)", c, h.recoveryState(t, res.ID))
	}
}

// Events are stored once and only start work for a collection of this
// environment and account; unsigned deliveries are refused and not stored.
func TestStripeWebhookInbox(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	h.owner(t, "UPDATE business.job SET state = 'completed', lease_owner = NULL, lease_until = NULL WHERE state IN ('queued', 'retry')")
	unknown := "99999999-8888-4777-8666-555555555555"
	h.deliver(t, "evt_other_collection", "checkout.session.completed", "checkout.session", "cs_test_other", unknown, order)
	h.deliver(t, "evt_refund", "charge.refunded", "charge", "ch_1", "", "")
	if ran, _ := h.step(t); ran {
		t.Fatal("an unrelated event scheduled work")
	}
	body := []byte(`{"id":"evt_forged","object":"event","type":"checkout.session.completed","livemode":false,"data":{"object":{"id":"` +
		session + `","metadata":{"collection_id":"` + coll + `","environment":"test"}}}}`)
	hdr := http.Header{}
	hdr.Set(payments.SignatureHeader, fmt.Sprintf("t=%d,v1=%s", time.Now().Unix(), strings.Repeat("ab", 32)))
	if _, err := h.webhook.Receive(h.ctx, hdr, body); !errors.Is(err, payments.ErrInvalidSignature) {
		t.Fatalf("forged: %v", err)
	}
	var matched, total int
	if err := h.admin.QueryRow(h.ctx, `SELECT count(*) FILTER (WHERE collection_id IS NOT NULL), count(*) FROM business.stripe_event`).Scan(&matched, &total); err != nil {
		t.Fatal(err)
	}
	if matched != 0 || total != 2 {
		t.Fatalf("stored %d events (%d matched), want 2 unmatched", total, matched)
	}
	// The API login records events only through the gateway.
	conn := h.db.LoginConn(t, "gk_api")
	for _, sql := range []string{"SELECT count(*) FROM business.stripe_event", "SELECT count(*) FROM business.payment_collection"} {
		if _, err := conn.Exec(h.ctx, sql); err == nil || !strings.Contains(err.Error(), "42501") {
			t.Errorf("gk_api: %s: %v, want 42501", sql, err)
		}
	}
	h.noPersonalDataInLogs(t)
}

// The request is fixed at the first dispatch, not at the initiation: a
// collection whose first dispatch waits (no Stripe key, a freeze, a backlog)
// for hours still gets a session; a worker without a key fixes nothing.
func TestDelayedFirstDispatchStillCreatesTheSession(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	res, err := h.start(order, d, "pay-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	keyless := *h.pay
	keyless.Provider = nil
	h.handler = jobs.Chain(h.cmds.Handle, keyless.Handle, h.reviews.Handle)
	errs := h.drain(t)
	var de *jobs.DeferError
	if len(errs) != 1 || !errors.As(errs[0], &de) {
		t.Fatalf("without a key: %v, want a deferral", errs)
	}
	if got := h.count(t, "SELECT count(*) FROM business.payment_collection WHERE id = $1::uuid AND first_dispatch_at IS NULL AND payload IS NULL", res.ID); got != 1 {
		t.Fatal("a worker without a key started the window")
	}
	// Three hours later (the initiation instant is far behind).
	h.owner(t, "UPDATE business.payment_collection SET initiated_at = initiated_at - interval '3 hours' WHERE id = $1::uuid", res.ID)
	h.handler = jobs.Chain(h.cmds.Handle, h.pay.Handle, h.reviews.Handle)
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("delayed first dispatch: %v", errs)
	}
	if c := h.collection(t, res.ID); c.State != "pending" || len(h.stripe.sessions) != 1 {
		t.Fatalf("collection %+v", c)
	}
}

// A first call that never reached Stripe (connection refused) is retried
// with the same key and body an hour later, and Stripe still accepts it.
func TestRetryAfterARefusedConnectionAnHourLater(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	res, err := h.start(order, d, "pay-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	down, err := payments.NewClient(payments.ClientConfig{Key: "sk_test_fake", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	unreachable := *h.pay
	unreachable.Provider = down
	h.handler = jobs.Chain(h.cmds.Handle, unreachable.Handle, h.reviews.Handle)
	errs := h.drain(t)
	if len(errs) != 1 || len(attention(errs)) != 0 || h.collection(t, res.ID).State != "unknown" || len(h.stripe.creates) != 0 {
		t.Fatalf("refused connection: %v %+v", errs, h.collection(t, res.ID))
	}
	h.handler = jobs.Chain(h.cmds.Handle, h.pay.Handle, h.reviews.Handle)
	h.stripe.skew = 61 * time.Minute
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("retry an hour later: %v", errs)
	}
	if c := h.collection(t, res.ID); c.State != "pending" || len(h.stripe.creates) != 1 || h.stripe.creates[0] != res.ID {
		t.Fatalf("collection %+v, creates %v", c, h.stripe.creates)
	}
}

// A first call that would come too late for its session (the prepared
// envelope kept failing for over 1.5 hours) is refused before it is made:
// the collection fails without a Stripe call.
func TestFirstCallTooLateFailsWithoutCallingStripe(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	res, err := h.start(order, d, "pay-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	h.journal.Fail = func(op, name string) error {
		if op == "put" && strings.HasSuffix(name, ".prepared") {
			return errors.New("b2 unavailable")
		}
		return nil
	}
	if errs := h.drain(t); len(errs) != 1 || h.recoveryState(t, res.ID) != "reserved" {
		t.Fatalf("journal down: %v (%s)", errs, h.recoveryState(t, res.ID))
	}
	// 100 minutes pass while the journal is down: move the fixed request
	// (and its operation, never journalled) back in time.
	var fixed time.Time
	if err := h.admin.QueryRow(h.ctx, "SELECT first_dispatch_at FROM business.payment_collection WHERE id = $1::uuid", res.ID).Scan(&fixed); err != nil {
		t.Fatal(err)
	}
	earlier := fixed.Add(-100 * time.Minute)
	payload, err := payments.NewPayload(payments.SessionRequest{CollectionID: res.ID, OrderID: order, AmountMinor: 9501,
		Environment: "test", UIOrigin: uiOrigin, ExpiresAt: payments.SessionExpiry(earlier)}, stripeAccount)
	if err != nil {
		t.Fatal(err)
	}
	h.owner(t, `UPDATE business.payment_collection SET first_dispatch_at = $2::timestamptz, replay_until = $2::timestamptz + interval '23 hours',
		session_expires_at = $3, payload = $4::text, payload_sha256 = encode(sha256(convert_to($4::text, 'UTF8')), 'hex')
		WHERE id = $1::uuid`, res.ID, earlier, payments.SessionExpiry(earlier), string(payload))
	h.owner(t, `UPDATE business.recovery_operation SET replay_until = $2::timestamptz + interval '23 hours',
		payload_hash = encode(sha256(convert_to($3::text, 'UTF8')), 'hex') WHERE id = $1::uuid`, res.ID, earlier, string(payload))
	h.journal.Fail = nil
	h.due(t)
	errs := h.drain(t)
	if got := attention(errs); len(got) != 1 || !strings.Contains(got[0], "without a Stripe call") {
		t.Fatalf("too late: %v", errs)
	}
	if c := h.collection(t, res.ID); c.State != "failed" || len(h.stripe.creates) != 0 || h.recoveryState(t, res.ID) != "secured" ||
		h.count(t, "SELECT count(*) FROM business.payment_attempt WHERE collection_id = $1::uuid", res.ID) != 0 {
		t.Fatalf("collection %+v, creates %v", c, h.stripe.creates)
	}
	if _, err := h.start(order, d, "pay-2", 1); err != nil {
		t.Fatalf("new collection after the failure: %v", err)
	}
}

// A restore freeze refuses new collections (503, retry later); a replay of
// an existing request still answers.
func TestPaymentInitiationRefusedWhileFrozen(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	first, err := h.start(order, d, "pay-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	h.stripe.status, h.stripe.errType = 400, "invalid_request_error"
	h.drain(t) // failed: a new collection would be allowed
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if _, err := h.start(order, d, "pay-2", 1); !errors.Is(err, checkout.ErrRecoveryFrozen) || !errors.Is(err, checkout.ErrPaymentsUnavailable) {
		t.Fatalf("while frozen: %v", err)
	}
	if again, err := h.start(order, d, "pay-1", 1); err != nil || !again.Replayed || again.ID != first.ID {
		t.Fatalf("replay while frozen: %+v %v", again, err)
	}
	if got := h.count(t, "SELECT count(*) FROM business.payment_collection WHERE order_id = $1::uuid", order); got != 1 {
		t.Fatalf("%d collections, want 1", got)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	if _, err := h.start(order, d, "pay-2", 1); err != nil {
		t.Fatalf("after the freeze: %v", err)
	}
}

// Concurrent deliveries for one collection leave one waiting reconcile job.
func TestConcurrentEventsScheduleOneReconcile(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	for round := range 10 {
		h.owner(t, `UPDATE business.job SET state = 'completed', lease_owner = NULL, lease_until = NULL
			WHERE state IN ('queued', 'retry')`)
		var wg sync.WaitGroup
		for i := range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				h.deliver(t, fmt.Sprintf("evt_r%d_%d", round, i), "checkout.session.completed", "checkout.session", session, coll, order)
			}()
		}
		wg.Wait()
		if got := h.count(t, `SELECT count(*) FROM business.payment_job pj JOIN business.job j ON j.id = pj.job_id
			WHERE pj.collection_id = $1::uuid AND pj.purpose = 'reconcile' AND j.state IN ('queued', 'retry')`, coll); got != 1 {
			t.Fatalf("round %d: %d waiting reconcile jobs, want 1", round, got)
		}
	}
}

// insertOrderOperation records another protected operation on the order at
// its current version (a competing command), in state.
func (h *payHarness) insertOrderOperation(t *testing.T, order string, version int) string {
	t.Helper()
	id := payments.NewID()
	h.owner(t, `INSERT INTO business.recovery_operation (id, kind, aggregate_id, prior_version, payload_hash, envelope_key, replay_until)
		VALUES ($1::uuid, 'command', $2::uuid, $3, repeat('a', 64), 'recovery/test/op/' || $2 || '/x-' || $1, 'infinity')`, id, order, version)
	return id
}

// A paid record waits (counted retries, then attention) while another
// protected operation on the order is unresolved, and goes to attention at
// once when the order already has an operation at its version.
func TestPaidRecordWaitsWhileBusyAndEscalatesWhenStale(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	h.stripe.pay(session, time.Date(2026, 11, 5, 10, 9, 0, 0, time.UTC))
	// An unresolved operation at an earlier version holds the order.
	other := h.insertOrderOperation(t, order, 0)
	h.deliver(t, "evt_completed", "checkout.session.completed", "checkout.session", session, coll, order)
	errs := h.drain(t)
	var de *jobs.DeferError
	if len(errs) != 1 || errors.As(errs[0], &de) || len(attention(errs)) != 0 {
		t.Fatalf("busy: %v, want one counted retry", errs)
	}
	if c := h.collection(t, coll); c.State != "pending" || h.count(t, "SELECT count(*) FROM business.order_command WHERE order_id = $1::uuid", order) != 0 {
		t.Fatalf("busy recorded something: %+v", c)
	}
	if got := h.count(t, `SELECT count(*) FROM business.payment_job pj JOIN business.job j ON j.id = pj.job_id
		WHERE pj.collection_id = $1::uuid AND j.state = 'retry' AND j.failures = 1`, coll); got != 1 {
		t.Fatalf("%d reconcile jobs with one counted failure, want 1", got)
	}
	// The competing operation resolves at the order's version: stale.
	h.owner(t, `UPDATE business.recovery_operation SET prior_version = 1, state = 'secured', prepared_ack_at = now(), outcome_ack_at = now(),
		outcome = '{"status":"refused"}', outcome_hash = repeat('b', 64) WHERE id = $1::uuid`, other)
	h.due(t)
	errs = h.drain(t)
	if got := attention(errs); len(got) != 1 || !strings.Contains(got[0], "later version") {
		t.Fatalf("stale: %v", errs)
	}
	if c := h.collection(t, coll); c.State != "pending" {
		t.Fatalf("stale recorded something: %+v", c)
	}
}

// recordDirect runs payment_reconcile_record for collection on a freshly
// scheduled reconcile job, as a worker that began before a decision would.
func (h *payHarness) recordDirect(t *testing.T, coll, session, verdict string, evidence string, successAt *time.Time, reason *string) string {
	t.Helper()
	h.owner(t, "SELECT business.payment_schedule_reconcile($1::uuid, now())", coll)
	h.workers++
	job := jobs.Job{Worker: fmt.Sprintf("00000000-0000-4000-8000-%012d", h.workers)}
	if err := h.worker.QueryRow(h.ctx, `SELECT job_id::text, job_operation_id::text, job_generation
		FROM business.job_claim($1::uuid, interval '60 seconds')`, job.Worker).Scan(&job.ID, &job.OperationID, &job.Generation); err != nil {
		t.Fatal(err)
	}
	var state string
	var cmd *string
	if err := h.worker.QueryRow(context.Background(), `SELECT collection_state, command_id::text FROM business.payment_reconcile_record(
		$1::uuid, $2::uuid, $3, $4::uuid, $5, $6, 'open', 'unpaid', NULL, $7::json, $8, $9)`,
		job.ID, job.Worker, job.Generation, coll, verdict, session, evidence, successAt, reason).Scan(&state, &cmd); err != nil {
		t.Fatalf("record %s: %v", verdict, err)
	}
	return state
}

// A stale pending observation never reopens a cancelled collection, and a
// later mismatch on a succeeded collection keeps its paid evidence.
func TestDecidedCollectionsKeepTheirDecisionAndEvidence(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	h.stripe.session(session).status = "expired"
	h.deliver(t, "evt_expired", "checkout.session.expired", "checkout.session", session, coll, order)
	if errs := h.drain(t); len(errs) != 0 || h.collection(t, coll).State != "cancelled" {
		t.Fatalf("expired: %v %+v", errs, h.collection(t, coll))
	}
	if got := h.recordDirect(t, coll, session, "pending", `{"session_id":"`+session+`","session_status":"open"}`, nil, nil); got != "cancelled" {
		t.Fatalf("stale pending moved a cancelled collection to %s", got)
	}

	// A second order, paid; then a contradicting observation.
	h2 := h
	coll2, session2 := func() (string, string) {
		quote, d2 := h2.guestQuote(t, "q-paid-2", checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot,
			FulfillmentMode: schedule.Collection, Lines: []checkout.QuoteLine{{VariantID: f.v1000, Quantity: 1}}})
		o2 := must[catalog.Result](t)(h2.checkout("", d2, "k-paid-2", quote, pol)).ID
		res, err := h2.start(o2, d2, "pay-x", 1)
		if err != nil {
			t.Fatal(err)
		}
		if errs := h2.drain(t); len(errs) != 0 {
			t.Fatalf("dispatch: %v", errs)
		}
		return res.ID, h2.collection(t, res.ID).SessionID
	}()
	h.stripe.pay(session2, time.Date(2026, 11, 5, 10, 9, 0, 0, time.UTC))
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 || h.collection(t, coll2).State != "succeeded" {
		t.Fatalf("paid: %v %+v", errs, h.collection(t, coll2))
	}
	var before string
	if err := h.admin.QueryRow(h.ctx, "SELECT evidence::text FROM business.payment_collection WHERE id = $1::uuid", coll2).Scan(&before); err != nil {
		t.Fatal(err)
	}
	reason := "charge was refunded"
	if got := h.recordDirect(t, coll2, session2, "mismatch", `{"session_id":"`+session2+`","charge_refunded":true}`, nil, &reason); got != "succeeded" {
		t.Fatalf("mismatch moved a succeeded collection to %s", got)
	}
	var after, mismatch string
	if err := h.admin.QueryRow(h.ctx, `SELECT evidence::text, mismatch_evidence::text FROM business.payment_collection
		WHERE id = $1::uuid`, coll2).Scan(&after, &mismatch); err != nil {
		t.Fatal(err)
	}
	if after != before || !strings.Contains(before, `"charge_status":"succeeded"`) || !strings.Contains(mismatch, "charge_refunded") {
		t.Fatalf("evidence %s -> %s, mismatch %s", before, after, mismatch)
	}
}

// A stale mismatch verdict on a cancelled collection keeps it cancelled and
// records only the mismatch (the worker sends the job to attention); the
// money it may show is never hidden: the holds stay, no second collection
// is allowed and the customer reads unknown ("still checking payment").
func TestStaleMismatchKeepsACancelledCollectionCancelled(t *testing.T) {
	cancelled := func(t *testing.T) (*payHarness, fixture, string, []byte, string, string) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		order, d := h.paidOrder(t, f, pol)
		coll, session := h.started(t, order, d)
		h.stripe.session(session).status = "expired"
		h.deliver(t, "evt_expired", "checkout.session.expired", "checkout.session", session, coll, order)
		if errs := h.drain(t); len(errs) != 0 || h.collection(t, coll).State != "cancelled" {
			t.Fatalf("expired: %v %+v", errs, h.collection(t, coll))
		}
		return h, f, order, d, coll, session
	}
	mismatch := func(t *testing.T, h *payHarness, coll, session string) {
		t.Helper()
		var before string
		if err := h.admin.QueryRow(h.ctx, "SELECT evidence::text FROM business.payment_collection WHERE id = $1::uuid", coll).Scan(&before); err != nil {
			t.Fatal(err)
		}
		reason := "amount does not match the collection"
		ev := `{"session_id":"` + session + `","charge_status":"succeeded","amount_minor":100}`
		if got := h.recordDirect(t, coll, session, "mismatch", ev, nil, &reason); got != "cancelled" {
			t.Fatalf("stale mismatch moved a cancelled collection to %s", got)
		}
		var after, mismatchEv string
		var mismatchReason *string
		if err := h.admin.QueryRow(h.ctx, `SELECT evidence::text, mismatch_evidence::text, mismatch_reason
			FROM business.payment_collection WHERE id = $1::uuid`, coll).Scan(&after, &mismatchEv, &mismatchReason); err != nil {
			t.Fatal(err)
		}
		if after != before || !strings.Contains(mismatchEv, `"charge_status":"succeeded"`) || mismatchReason == nil || *mismatchReason != reason {
			t.Fatalf("evidence %s -> %s, mismatch %v %s", before, after, mismatchReason, mismatchEv)
		}
		if got := h.count(t, `SELECT count(*) FROM business.audit_event WHERE action = 'payment.mismatch' AND subject_kind = 'order'
			AND detail ->> 'collection_id' = $1::text`, coll); got != 1 {
			t.Fatalf("%d payment.mismatch audits", got)
		}
	}
	t.Run("before the release", func(t *testing.T) {
		h, f, order, d, coll, session := cancelled(t)
		mismatch(t, h, coll, session)
		v, raw := h.view(t, order, d)
		if v.Lifecycle != "awaiting_payment" || v.Payment == nil || v.Payment.State != "unknown" || v.Payment.CheckoutURL != nil {
			t.Fatalf("customer view %s", raw)
		}
		if _, err := h.start(order, d, "pay-2", 1); conflict(err) != "payment_in_progress" {
			t.Fatalf("new collection beside a mismatch: %v", err)
		}
		// The hold ends: the review keeps the holds and keeps looking.
		h.endHold(t, order)
		for range 3 {
			h.due(t)
			if errs := h.drain(t); len(withoutRefundDispatch(errs)) != 0 {
				t.Fatal(errs)
			}
		}
		if o := h.orderRow(t, order); o.Lifecycle != "awaiting_payment" {
			t.Fatalf("order %+v", o)
		}
		if got := h.paidHolds(t, order, f.readyLot); got != "tentative/reserved 1r0s" {
			t.Fatalf("holds %s", got)
		}
		if id, _ := h.reviewJob(t, order); id == "" {
			t.Fatal("the review stopped looking at a collection under reconciliation")
		}
		if c := h.collection(t, coll); c.State != "cancelled" {
			t.Fatalf("collection %+v", c)
		}
	})
	t.Run("after the release", func(t *testing.T) {
		h, f, order, d, coll, session := cancelled(t)
		h.endHold(t, order)
		h.due(t)
		if errs := h.drain(t); len(errs) != 0 || h.orderRow(t, order).Lifecycle != "cancelled" {
			t.Fatalf("release: %v %+v", errs, h.orderRow(t, order))
		}
		mismatch(t, h, coll, session)
		if o := h.orderRow(t, order); o.Lifecycle != "cancelled" || o.CancelReason != "payment_not_completed" {
			t.Fatalf("order %+v", o)
		}
		if got := h.paidHolds(t, order, f.readyLot); got != "released/returned 0r0s" {
			t.Fatalf("holds %s", got)
		}
		if v, raw := h.view(t, order, d); v.Payment == nil || v.Payment.State != "unknown" {
			t.Fatalf("customer view %s", raw)
		}
	})
}
