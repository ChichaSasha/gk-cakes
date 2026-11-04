package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ProviderTimeout bounds one Stripe call (architecture: provider 5s).
const ProviderTimeout = 5 * time.Second

// ResultKind classifies one Stripe call by what it proves about that call.
type ResultKind string

// Result kinds; the create kinds are stored as payment_attempt.result.
const (
	// Created: Stripe returned the Checkout Session (for a replay within the
	// idempotency window, the original session).
	Created ResultKind = "created"
	// Rejected: Stripe definitively refused this request (invalid request,
	// for example an expiry that is now too close); nothing was created.
	Rejected ResultKind = "rejected"
	// Deferred: not processed now (rate limit, a concurrent request with the
	// same key); retry later with the same key and body.
	Deferred ResultKind = "deferred"
	// Unknown: no response or an ambiguous one; a session may exist.
	Unknown ResultKind = "unknown"
	// Refused: credentials or permissions; a manager fixes the key first.
	Refused ResultKind = "refused"
	// Conflict: the idempotency key was already used with another request;
	// the outcome is unknown and goes to reconciliation.
	Conflict ResultKind = "conflict"
	// Found and NotFound classify a retrieve.
	Found    ResultKind = "found"
	NotFound ResultKind = "not_found"
)

// Result is one classified Stripe call. It never carries Stripe's error
// message (it could echo request data) or credentials.
type Result struct {
	Kind    ResultKind
	Status  int      // HTTP status, 0 without a response
	Code    string   // Stripe error type, when safe
	Session *Session // Created and Found
	Err     error    // transport or decoding problem, without secrets
}

// Provider is the Stripe API the worker needs.
type Provider interface {
	CreateSession(ctx context.Context, idempotencyKey, form string) Result
	RetrieveSession(ctx context.Context, id string) Result
}

// Session is the part of a Checkout Session the worker reads.
type Session struct {
	ID                string            `json:"id"`
	Object            string            `json:"object"`
	Status            string            `json:"status"`
	PaymentStatus     string            `json:"payment_status"`
	AmountTotal       *int64            `json:"amount_total"`
	Currency          string            `json:"currency"`
	ClientReferenceID string            `json:"client_reference_id"`
	URL               string            `json:"url"`
	ExpiresAt         int64             `json:"expires_at"`
	Livemode          bool              `json:"livemode"`
	Metadata          map[string]string `json:"metadata"`
	PaymentIntent     *PaymentIntent    `json:"payment_intent"`
}

// PaymentIntent is a session's payment intent (an ID, or expanded).
type PaymentIntent struct {
	ID             string            `json:"id"`
	Status         string            `json:"status"`
	Amount         int64             `json:"amount"`
	AmountReceived int64             `json:"amount_received"`
	Currency       string            `json:"currency"`
	Livemode       bool              `json:"livemode"`
	Metadata       map[string]string `json:"metadata"`
	LatestCharge   *Charge           `json:"latest_charge"`
	Expanded       bool              `json:"-"`
}

// UnmarshalJSON accepts an ID string or the expanded object.
func (p *PaymentIntent) UnmarshalJSON(b []byte) error {
	var id string
	if json.Unmarshal(b, &id) == nil {
		*p = PaymentIntent{ID: id}
		return nil
	}
	type plain PaymentIntent
	var v plain
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*p = PaymentIntent(v)
	p.Expanded = true
	return nil
}

// Charge is a payment intent's latest charge (an ID, or expanded).
type Charge struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	Paid           bool   `json:"paid"`
	Captured       bool   `json:"captured"`
	Amount         int64  `json:"amount"`
	AmountCaptured int64  `json:"amount_captured"`
	Currency       string `json:"currency"`
	Created        int64  `json:"created"`
	Livemode       bool   `json:"livemode"`
	Refunded       bool   `json:"refunded"`
	AmountRefunded int64  `json:"amount_refunded"`
	Disputed       bool   `json:"disputed"`
	Expanded       bool   `json:"-"`
}

// UnmarshalJSON accepts an ID string or the expanded object.
func (c *Charge) UnmarshalJSON(b []byte) error {
	var id string
	if json.Unmarshal(b, &id) == nil {
		*c = Charge{ID: id}
		return nil
	}
	type plain Charge
	var v plain
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*c = Charge(v)
	c.Expanded = true
	return nil
}

var (
	sessionIDPattern = regexp.MustCompile(`^cs_[A-Za-z0-9_]{1,250}$`)
	piIDPattern      = regexp.MustCompile(`^pi_[A-Za-z0-9_]{1,250}$`)
	objectIDPattern  = regexp.MustCompile(`^[A-Za-z0-9_]{1,255}$`)
	statusPattern    = regexp.MustCompile(`^[a-z_]{1,40}$`)
	codePattern      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// ValidSessionID reports whether s looks like a Checkout Session ID.
func ValidSessionID(s string) bool { return sessionIDPattern.MatchString(s) }

// Client is a Provider for the Stripe API.
type Client struct {
	baseURL string
	key     string
	http    *http.Client
}

// ClientConfig configures a Client.
type ClientConfig struct {
	Key string // secret or restricted key; never logged
	// Live allows a live-mode key (production only); otherwise only test
	// keys are accepted.
	Live    bool
	BaseURL string // default https://api.stripe.com
	HTTP    *http.Client
}

// NewClient returns a Stripe client. Outside production only test-mode
// keys (sk_test_, rk_test_) are accepted; in production only live keys.
func NewClient(cfg ClientConfig) (*Client, error) {
	test := strings.HasPrefix(cfg.Key, "sk_test_") || strings.HasPrefix(cfg.Key, "rk_test_")
	live := strings.HasPrefix(cfg.Key, "sk_live_") || strings.HasPrefix(cfg.Key, "rk_live_")
	switch {
	case cfg.Live && !live:
		return nil, errors.New("payments: production needs a live-mode Stripe key")
	case !cfg.Live && !test:
		return nil, errors.New("payments: a test-mode Stripe key (sk_test_ or rk_test_) is required outside production")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.stripe.com"
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 2 * ProviderTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	}
	return &Client{baseURL: strings.TrimSuffix(cfg.BaseURL, "/"), key: cfg.Key, http: cfg.HTTP}, nil
}

const maxResponseBytes = 256 << 10

func (c *Client) do(ctx context.Context, method, path, idempotencyKey, form string) (int, []byte, error) {
	var body io.Reader
	if form != "" {
		body = strings.NewReader(form)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, nil, errors.New("payments: invalid provider URL")
	}
	req.SetBasicAuth(c.key, "")
	req.Header.Set("Stripe-Version", APIVersion)
	if form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// The request may have reached Stripe. url.Error carries the URL
		// only (no credentials: basic auth is a header).
		return 0, nil, fmt.Errorf("payments: provider call: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("payments: read provider response: %w", err)
	}
	return resp.StatusCode, data, nil
}

// CreateSession posts the exact stored form under the idempotency key.
func (c *Client) CreateSession(ctx context.Context, idempotencyKey, form string) Result {
	status, data, err := c.do(ctx, http.MethodPost, sessionPath, idempotencyKey, form)
	if err != nil {
		return Result{Kind: Unknown, Status: status, Err: err}
	}
	return ClassifyCreate(status, data)
}

// RetrieveSession reads a session with its payment intent and latest charge.
func (c *Client) RetrieveSession(ctx context.Context, id string) Result {
	if !sessionIDPattern.MatchString(id) {
		return Result{Kind: Refused, Err: errors.New("payments: not a Checkout Session ID")}
	}
	status, data, err := c.do(ctx, http.MethodGet, sessionPath+"/"+id+"?"+url.Values{"expand[]": {"payment_intent.latest_charge"}}.Encode(), "", "")
	if err != nil {
		return Result{Kind: Unknown, Status: status, Err: err}
	}
	return ClassifyRetrieve(status, data)
}

// ExpireSession expires an open session (sandbox proof and later cleanup).
func (c *Client) ExpireSession(ctx context.Context, id string) Result {
	if !sessionIDPattern.MatchString(id) {
		return Result{Kind: Refused, Err: errors.New("payments: not a Checkout Session ID")}
	}
	status, data, err := c.do(ctx, http.MethodPost, sessionPath+"/"+id+"/expire", "", "")
	if err != nil {
		return Result{Kind: Unknown, Status: status, Err: err}
	}
	return ClassifyRetrieve(status, data)
}

// AccountID returns the account the key belongs to.
func (c *Client) AccountID(ctx context.Context) (string, error) {
	status, data, err := c.do(ctx, http.MethodGet, "/v1/account", "", "")
	if err != nil {
		return "", err
	}
	var a struct {
		ID string `json:"id"`
	}
	if status != http.StatusOK || json.Unmarshal(data, &a) != nil || !accountPattern.MatchString(a.ID) {
		return "", fmt.Errorf("payments: account lookup answered HTTP %d (%s)", status, errorType(data))
	}
	return a.ID, nil
}

// errorType is Stripe's error type, when it is a safe identifier.
func errorType(body []byte) string {
	var e struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	if codePattern.MatchString(e.Error.Type) {
		return e.Error.Type
	}
	return ""
}

func decodeSession(body []byte) (*Session, error) {
	var s Session
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, errors.New("payments: undecodable session")
	}
	if s.Object != "checkout.session" || !sessionIDPattern.MatchString(s.ID) {
		return nil, errors.New("payments: response is not a Checkout Session")
	}
	return &s, nil
}

// ClassifyCreate interprets a create response per Stripe's error reference.
// Anything not proven otherwise is Unknown.
func ClassifyCreate(status int, body []byte) Result {
	if status >= 200 && status < 300 {
		s, err := decodeSession(body)
		if err != nil {
			return Result{Kind: Unknown, Status: status, Err: err}
		}
		if !strings.HasPrefix(s.URL, "https://") || s.ExpiresAt <= 0 {
			return Result{Kind: Unknown, Status: status, Err: errors.New("payments: session without a checkout URL or expiry")}
		}
		return Result{Kind: Created, Status: status, Session: s}
	}
	res := Result{Status: status, Code: errorType(body)}
	switch {
	case status == http.StatusBadRequest && res.Code == "idempotency_error":
		res.Kind = Conflict
	case (status == http.StatusBadRequest || status == http.StatusPaymentRequired) &&
		(res.Code == "invalid_request_error" || res.Code == "card_error"):
		res.Kind = Rejected
	case status == http.StatusConflict, status == http.StatusTooManyRequests:
		res.Kind = Deferred
	case status >= 400 && status < 500:
		// Credentials, permissions or an unrecognised refusal: nothing
		// final; a manager checks the configuration.
		res.Kind = Refused
	default: // 5xx and anything unexpected
		res.Kind = Unknown
	}
	return res
}

// ClassifyRetrieve interprets a retrieve (or expire) response.
func ClassifyRetrieve(status int, body []byte) Result {
	if status == http.StatusOK {
		s, err := decodeSession(body)
		if err != nil {
			return Result{Kind: Unknown, Status: status, Err: err}
		}
		return Result{Kind: Found, Status: status, Session: s}
	}
	res := Result{Status: status, Code: errorType(body)}
	switch {
	case status == http.StatusNotFound:
		res.Kind = NotFound
	case status == http.StatusTooManyRequests, status == http.StatusConflict:
		res.Kind = Deferred
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		res.Kind = Refused
	case status >= 400 && status < 500:
		res.Kind = Rejected
	default:
		res.Kind = Unknown
	}
	return res
}

// String describes a result without secrets or Stripe messages.
func (r Result) String() string {
	msg := "stripe call " + string(r.Kind)
	if r.Status != 0 {
		msg += fmt.Sprintf(" (HTTP %d", r.Status)
		if r.Code != "" {
			msg += " " + r.Code
		}
		msg += ")"
	}
	if r.Err != nil {
		msg += ": " + r.Err.Error()
	}
	return msg
}

// sanitize keeps a provider identifier or status only when it matches its
// expected shape (never free text).
func sanitize(p *regexp.Regexp, s string) string {
	if p.MatchString(s) {
		return s
	}
	return ""
}
