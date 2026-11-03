package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ProviderTimeout bounds one provider call (architecture: provider 5s).
const ProviderTimeout = 5 * time.Second

// ResultKind classifies one provider call by what it proves about that call.
type ResultKind string

// Result kinds, stored as notification_attempt.result.
const (
	// Accepted: the provider returned the message ID (for a replay within
	// the idempotency window, the original message's).
	Accepted ResultKind = "accepted"
	// Rejected: the provider definitively refused this request; it was not
	// sent and an identical request will not succeed.
	Rejected ResultKind = "rejected"
	// Deferred: not processed now (rate limit, quota, a concurrent request
	// with the same key); retry later with the same key.
	Deferred ResultKind = "deferred"
	// Unknown: no response or an ambiguous one; the email may have been sent.
	Unknown ResultKind = "unknown"
	// Refused: credential, permission or sender configuration; a manager must
	// fix the configuration before the same request is retried.
	Refused ResultKind = "refused"
	// Conflict: the key was already used with a different body. A request
	// under this key existed, so the outcome is unknown; never resend with
	// another key.
	Conflict ResultKind = "conflict"
)

// Result is the classified outcome of one provider call. It carries no
// provider error message (those can echo recipients).
type Result struct {
	Kind      ResultKind
	Status    int    // HTTP status, 0 without a response
	Code      string // provider error name, when safe
	MessageID string // provider message ID when Accepted
	Err       error  // transport or decoding problem, without secrets
}

// Sender sends one exact request body under an idempotency key.
type Sender interface {
	Send(ctx context.Context, idempotencyKey string, body []byte) Result
}

// Resend is a Sender for the Resend API.
type Resend struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// ResendConfig configures a Resend client.
type ResendConfig struct {
	APIKey  string // a sending key; never logged
	BaseURL string // default https://api.resend.com
	HTTP    *http.Client
}

// NewResend returns a Resend client.
func NewResend(cfg ResendConfig) (*Resend, error) {
	if !strings.HasPrefix(cfg.APIKey, "re_") {
		return nil, errors.New("notifications: a Resend API key (re_...) is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.resend.com"
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 2 * ProviderTimeout}
	}
	return &Resend{baseURL: strings.TrimSuffix(cfg.BaseURL, "/"), apiKey: cfg.APIKey, http: cfg.HTTP}, nil
}

var (
	messageIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	codePattern      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	// Resend error names for a request whose content is invalid (400/422).
	// Resend also answers 400 validation_error "API key is invalid" for a
	// malformed key; that is recognised by its message and refused instead.
	rejectionCodes = map[string]bool{"validation_error": true, "missing_required_field": true,
		"missing_required_parameter": true, "invalid_parameter": true, "invalid_attachment": true}
)

const maxResponseBytes = 64 << 10

// Send posts body to /emails with the Idempotency-Key header.
func (r *Resend) Send(ctx context.Context, idempotencyKey string, body []byte) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/emails", bytes.NewReader(body))
	if err != nil {
		return Result{Kind: Refused, Err: errors.New("notifications: invalid provider URL")}
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	resp, err := r.http.Do(req)
	if err != nil {
		// The request may have reached the provider.
		return Result{Kind: Unknown, Err: fmt.Errorf("notifications: provider call: %w", err)}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return Result{Kind: Unknown, Status: resp.StatusCode, Err: fmt.Errorf("notifications: read provider response: %w", err)}
	}
	return Classify(resp.StatusCode, data)
}

// Classify interprets a Resend response (status and body) per the Resend
// error reference. Anything not proven otherwise is Unknown.
func Classify(status int, body []byte) Result {
	if status >= 200 && status < 300 {
		var ok struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(body, &ok); err != nil || !messageIDPattern.MatchString(ok.ID) {
			return Result{Kind: Unknown, Status: status, Err: errors.New("notifications: success response without a usable message ID")}
		}
		return Result{Kind: Accepted, Status: status, MessageID: ok.ID}
	}
	// The message is read only to recognise credential problems; it can echo
	// recipients, so it is never stored or logged.
	var failure struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &failure)
	code := ""
	if codePattern.MatchString(failure.Name) {
		code = failure.Name
	}
	credential := strings.Contains(strings.ToLower(failure.Message), "api key")
	res := Result{Status: status, Code: code}
	switch {
	case (status == http.StatusBadRequest || status == http.StatusUnprocessableEntity) && rejectionCodes[code] && !credential:
		res.Kind = Rejected
	case status == http.StatusBadRequest, status == http.StatusUnprocessableEntity:
		// An invalid key, an invalid idempotency key or an unrecognised
		// error: nothing final; a manager checks the configuration.
		res.Kind = Refused
	case status == http.StatusConflict && code == "invalid_idempotent_request":
		res.Kind = Conflict
	case status == http.StatusConflict, status == http.StatusTooManyRequests:
		res.Kind = Deferred
	case status == http.StatusUnauthorized, status == http.StatusForbidden,
		status == http.StatusNotFound, status == http.StatusMethodNotAllowed:
		res.Kind = Refused
	default: // 5xx and anything unexpected
		res.Kind = Unknown
	}
	return res
}
