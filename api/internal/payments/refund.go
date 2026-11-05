package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

// GK-025 A3c2: Stripe refunds for the minimum full-return core (contracts
// r6.5 K05, K11; migration 0016_full_return.sql). A refund operation's ID is
// its Stripe idempotency key; its exact POST /v1/refunds request is fixed at
// the first dispatch and replayed unchanged; reconciliation retrieves a
// refund by ID or lists a payment intent's refunds completely (every page)
// before anything is treated as absent. No personal data is sent, stored or
// logged.

// Refund protocol constants.
const (
	refundPath = "/v1/refunds"
	// RefundReason is the only reason sent: the customer is owed the money.
	RefundReason = "requested_by_customer"
	// RefundPageSize is Stripe's maximum list page.
	RefundPageSize = 100
	// MaxRefundPages bounds one complete listing (2000 refunds of one
	// payment intent); beyond it the list is incomplete (Unknown), never
	// treated as complete.
	MaxRefundPages = 20
)

// Refund result kinds of a create call (with Rejected, Deferred, Unknown,
// Refused and Conflict); each is stored as refund_attempt.result. Stripe's
// canceled is RefundFailed.
const (
	RefundSucceeded      ResultKind = "succeeded"
	RefundPending        ResultKind = "pending"
	RefundRequiresAction ResultKind = "requires_action"
	RefundFailed         ResultKind = "failed"
)

var refundIDPattern = regexp.MustCompile(`^(re|pyr)_[A-Za-z0-9_]{1,250}$`)

// ValidRefundID reports whether s looks like a Stripe refund ID.
func ValidRefundID(s string) bool { return refundIDPattern.MatchString(s) }

// Refund is the part of a Stripe refund the worker reads.
type Refund struct {
	ID            string            `json:"id"`
	Object        string            `json:"object"`
	Amount        int64             `json:"amount"`
	Currency      string            `json:"currency"`
	Status        string            `json:"status"`
	PaymentIntent string            `json:"-"`
	Created       int64             `json:"created"`
	FailureReason string            `json:"failure_reason"`
	Metadata      map[string]string `json:"metadata"`
}

// UnmarshalJSON accepts payment_intent as an ID or an expanded object.
func (r *Refund) UnmarshalJSON(b []byte) error {
	type plain Refund
	var v struct {
		plain
		PaymentIntent json.RawMessage `json:"payment_intent"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*r = Refund(v.plain)
	if len(v.PaymentIntent) > 0 && string(v.PaymentIntent) != "null" {
		var id string
		if json.Unmarshal(v.PaymentIntent, &id) != nil {
			var obj struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(v.PaymentIntent, &obj); err != nil {
				return err
			}
			id = obj.ID
		}
		r.PaymentIntent = id
	}
	return nil
}

// OperationID is the refund's metadata[refund_operation_id] when it is a
// lower-case UUID ("" otherwise: never trusted on its own).
func (r *Refund) OperationID() string {
	if id := r.Metadata["refund_operation_id"]; uuidPattern.MatchString(id) {
		return id
	}
	return ""
}

// RefundResult is one classified refund call.
type RefundResult struct {
	Kind    ResultKind
	Status  int
	Code    string
	Refund  *Refund  // create and retrieve
	Refunds []Refund // list: every refund of the payment intent
	Pages   int      // list: pages read
	Err     error
}

// String describes a result without secrets or Stripe messages.
func (r RefundResult) String() string {
	return Result{Kind: r.Kind, Status: r.Status, Code: r.Code, Err: r.Err}.String()
}

// RefundProvider is the Stripe refund API the worker needs.
type RefundProvider interface {
	CreateRefund(ctx context.Context, idempotencyKey, form string) RefundResult
	RetrieveRefund(ctx context.Context, id string) RefundResult
	ListRefunds(ctx context.Context, paymentIntentID string) RefundResult
}

var _ RefundProvider = (*Client)(nil)

// CreateRefund posts the exact stored form under the idempotency key (the
// refund operation's ID).
func (c *Client) CreateRefund(ctx context.Context, idempotencyKey, form string) RefundResult {
	if !uuidPattern.MatchString(idempotencyKey) || form == "" {
		return RefundResult{Kind: Refused, Err: errors.New("payments: a refund needs its operation key and request")}
	}
	status, data, err := c.do(ctx, http.MethodPost, refundPath, idempotencyKey, form)
	if err != nil {
		return RefundResult{Kind: Unknown, Status: status, Err: err}
	}
	return ClassifyRefundCreate(status, data)
}

// RetrieveRefund reads one refund.
func (c *Client) RetrieveRefund(ctx context.Context, id string) RefundResult {
	if !refundIDPattern.MatchString(id) {
		return RefundResult{Kind: Refused, Err: errors.New("payments: not a refund ID")}
	}
	status, data, err := c.do(ctx, http.MethodGet, refundPath+"/"+id, "", "")
	if err != nil {
		return RefundResult{Kind: Unknown, Status: status, Err: err}
	}
	return ClassifyRefundRetrieve(status, data)
}

// ListRefunds reads every refund of the payment intent, following has_more
// page by page (starting_after the last ID). Found means the list is
// complete; any failed, undecodable or over-long listing is not (its Kind
// says why), so nothing may be concluded from absence.
func (c *Client) ListRefunds(ctx context.Context, paymentIntentID string) RefundResult {
	if !piIDPattern.MatchString(paymentIntentID) {
		return RefundResult{Kind: Refused, Err: errors.New("payments: not a payment intent ID")}
	}
	var all []Refund
	after := ""
	for page := 1; page <= MaxRefundPages; page++ {
		q := url.Values{"payment_intent": {paymentIntentID}, "limit": {strconv.Itoa(RefundPageSize)}}
		if after != "" {
			q.Set("starting_after", after)
		}
		status, data, err := c.do(ctx, http.MethodGet, refundPath+"?"+q.Encode(), "", "")
		if err != nil {
			return RefundResult{Kind: Unknown, Status: status, Pages: page - 1, Err: err}
		}
		if status != http.StatusOK {
			res := ClassifyRefundRetrieve(status, data)
			res.Refund, res.Pages = nil, page-1
			if res.Kind == NotFound {
				// The payment intent itself is unknown to this account.
				res.Kind = Rejected
			}
			return res
		}
		var list struct {
			Object  string            `json:"object"`
			Data    []json.RawMessage `json:"data"`
			HasMore *bool             `json:"has_more"`
		}
		if json.Unmarshal(data, &list) != nil || list.Object != "list" || list.HasMore == nil {
			return RefundResult{Kind: Unknown, Status: status, Pages: page, Err: errors.New("payments: undecodable refund list")}
		}
		for _, raw := range list.Data {
			r, err := decodeRefund(raw)
			if err != nil {
				return RefundResult{Kind: Unknown, Status: status, Pages: page, Err: err}
			}
			if r.PaymentIntent != paymentIntentID {
				return RefundResult{Kind: Unknown, Status: status, Pages: page,
					Err: errors.New("payments: refund list holds another payment intent's refund")}
			}
			all = append(all, *r)
		}
		if !*list.HasMore {
			return RefundResult{Kind: Found, Status: status, Refunds: all, Pages: page}
		}
		if len(list.Data) == 0 {
			return RefundResult{Kind: Unknown, Status: status, Pages: page, Err: errors.New("payments: empty page with more to come")}
		}
		after = all[len(all)-1].ID
	}
	return RefundResult{Kind: Unknown, Status: http.StatusOK, Pages: MaxRefundPages,
		Err: fmt.Errorf("payments: more than %d refund pages: listing incomplete", MaxRefundPages)}
}

func decodeRefund(body []byte) (*Refund, error) {
	var r Refund
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, errors.New("payments: undecodable refund")
	}
	if r.Object != "refund" || !refundIDPattern.MatchString(r.ID) || r.Amount < 0 || !currencyPattern.MatchString(r.Currency) ||
		!statusPattern.MatchString(r.Status) {
		return nil, errors.New("payments: response is not a refund")
	}
	if r.PaymentIntent != "" && !piIDPattern.MatchString(r.PaymentIntent) {
		return nil, errors.New("payments: refund with an invalid payment intent")
	}
	r.FailureReason = sanitize(codePattern, r.FailureReason)
	return &r, nil
}

var currencyPattern = regexp.MustCompile(`^[a-z]{3}$`)

// refundStatusKind maps a refund's status to its result kind ("" when it is
// not one Stripe documents).
func refundStatusKind(status string) ResultKind {
	switch status {
	case "succeeded":
		return RefundSucceeded
	case "pending":
		return RefundPending
	case "requires_action":
		return RefundRequiresAction
	case "failed", "canceled":
		return RefundFailed
	}
	return ""
}

// ClassifyRefundCreate interprets a refund create response. A 2xx refund
// object is classified by its status; 400 idempotency_error is Conflict;
// 400/402 invalid_request_error or card_error is Rejected (definitive:
// nothing was created, for example an amount above what is refundable);
// 409 and 429 are Deferred; other 4xx (401, 403, ...) Refused; anything
// else (5xx, no or undecodable body) Unknown.
func ClassifyRefundCreate(status int, body []byte) RefundResult {
	if status >= 200 && status < 300 {
		r, err := decodeRefund(body)
		if err != nil {
			return RefundResult{Kind: Unknown, Status: status, Err: err}
		}
		kind := refundStatusKind(r.Status)
		if kind == "" {
			return RefundResult{Kind: Unknown, Status: status, Refund: r, Err: errors.New("payments: unknown refund status")}
		}
		return RefundResult{Kind: kind, Status: status, Refund: r}
	}
	res := RefundResult{Status: status, Code: errorType(body)}
	switch {
	case status == http.StatusBadRequest && res.Code == "idempotency_error":
		res.Kind = Conflict
	case (status == http.StatusBadRequest || status == http.StatusPaymentRequired) &&
		(res.Code == "invalid_request_error" || res.Code == "card_error"):
		res.Kind = Rejected
	case status == http.StatusConflict, status == http.StatusTooManyRequests:
		res.Kind = Deferred
	case status >= 400 && status < 500:
		res.Kind = Refused
	default:
		res.Kind = Unknown
	}
	return res
}

// ClassifyRefundRetrieve interprets a refund retrieve response: 200 is
// Found; 404 NotFound; 409/429 Deferred; 401/403 Refused; other 4xx
// Rejected; anything else Unknown.
func ClassifyRefundRetrieve(status int, body []byte) RefundResult {
	if status == http.StatusOK {
		r, err := decodeRefund(body)
		if err != nil {
			return RefundResult{Kind: Unknown, Status: status, Err: err}
		}
		return RefundResult{Kind: Found, Status: status, Refund: r}
	}
	res := RefundResult{Status: status, Code: errorType(body)}
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

// RefundEvidence is the bounded outcome evidence of a refund (IDs, status,
// amount, currency only), in the shape business.refund_recovery_committed
// and refund_reconcile_record verify. Livemode is the client key's mode
// (Stripe's refund object carries none; NewClient refuses live keys outside
// production).
type RefundEvidence struct {
	RefundID        string `json:"refund_id,omitempty"`
	Status          string `json:"status,omitempty"`
	AmountMinor     int64  `json:"amount_minor"`
	Currency        string `json:"currency,omitempty"`
	PaymentIntentID string `json:"payment_intent_id,omitempty"`
	Livemode        bool   `json:"livemode"`
	FailureReason   string `json:"failure_reason,omitempty"`
	HTTPStatus      int    `json:"http_status,omitempty"`
	ProviderCode    string `json:"provider_code,omitempty"`
}

// EvidenceOfRefund is r's evidence.
func EvidenceOfRefund(r *Refund, livemode bool) RefundEvidence {
	return RefundEvidence{RefundID: sanitize(refundIDPattern, r.ID), Status: sanitize(statusPattern, r.Status),
		AmountMinor: r.Amount, Currency: sanitize(currencyPattern, r.Currency),
		PaymentIntentID: sanitize(piIDPattern, r.PaymentIntent), Livemode: livemode,
		FailureReason: sanitize(codePattern, r.FailureReason)}
}

// RefundOutcomeStatus is the dispatch outcome status of a create result
// (business.refund_recovery_committed), or "" when the result is not a
// recordable outcome (the dispatch stays prepared and is replayed).
func RefundOutcomeStatus(kind ResultKind) string {
	switch kind {
	case RefundSucceeded, RefundPending, RefundRequiresAction, RefundFailed, Rejected:
		return string(kind)
	}
	return ""
}

// RefundRequest is what a refund request is built from.
type RefundRequest struct {
	OperationID     string
	OrderID         string
	CollectionID    string
	PaymentIntentID string
	AmountMinor     int64 // GBP pence
	Environment     string
}

func (r RefundRequest) validate() error {
	switch {
	case !uuidPattern.MatchString(r.OperationID) || !uuidPattern.MatchString(r.OrderID) ||
		!uuidPattern.MatchString(r.CollectionID):
		return errors.New("payments: refund operation, order and collection IDs must be lower-case UUIDs")
	case !piIDPattern.MatchString(r.PaymentIntentID):
		return errors.New("payments: a refund needs the collection's payment intent")
	case r.AmountMinor < 1:
		return errors.New("payments: the refund amount must be positive pence")
	case !envPattern.MatchString(r.Environment):
		return errors.New("payments: the environment must be a lower-case name")
	}
	return nil
}

func (r RefundRequest) values() url.Values {
	v := url.Values{}
	v.Set("payment_intent", r.PaymentIntentID)
	v.Set("amount", strconv.FormatInt(r.AmountMinor, 10))
	v.Set("reason", RefundReason)
	v.Set("metadata[order_id]", r.OrderID)
	v.Set("metadata[collection_id]", r.CollectionID)
	v.Set("metadata[refund_operation_id]", r.OperationID)
	v.Set("metadata[environment]", r.Environment)
	return v
}

// Form is the exact form-encoded refund request: deterministic (keys
// sorted), no customer data.
func (r RefundRequest) Form() (string, error) {
	if err := r.validate(); err != nil {
		return "", err
	}
	return r.values().Encode(), nil
}

// RefundPayload is the stored refund dispatch payload (K11: everything the
// dispatch is rebuilt from, including the exact request).
type RefundPayload struct {
	V                 int    `json:"v"`
	Provider          string `json:"provider"`
	Kind              string `json:"kind"`
	Environment       string `json:"environment"`
	Account           string `json:"account"`
	Livemode          bool   `json:"livemode"`
	IdempotencyKey    string `json:"idempotency_key"`
	RefundOperationID string `json:"refund_operation_id"`
	OrderID           string `json:"order_id"`
	CollectionID      string `json:"collection_id"`
	PaymentIntentID   string `json:"payment_intent_id"`
	AmountMinor       int64  `json:"amount_minor"`
	Currency          string `json:"currency"`
	Method            string `json:"method"`
	Path              string `json:"path"`
	Request           string `json:"request"`
}

// NewRefundPayload builds the refund dispatch payload bytes.
func NewRefundPayload(r RefundRequest, account string) ([]byte, error) {
	if !accountPattern.MatchString(account) {
		return nil, errors.New("payments: a Stripe account ID (acct_...) is required")
	}
	form, err := r.Form()
	if err != nil {
		return nil, err
	}
	return encode(RefundPayload{V: 1, Provider: "stripe", Kind: "refund", Environment: r.Environment, Account: account,
		Livemode: r.Environment == "production", IdempotencyKey: r.OperationID, RefundOperationID: r.OperationID,
		OrderID: r.OrderID, CollectionID: r.CollectionID, PaymentIntentID: r.PaymentIntentID, AmountMinor: r.AmountMinor,
		Currency: "gbp", Method: "POST", Path: refundPath, Request: form})
}

// RefundExpected is what the database says a refund operation is.
type RefundExpected struct {
	RefundRequest
	Account  string
	Livemode bool
}

// CheckRefundPayload validates a stored refund payload against its
// operation before any call: exactly the request this operation, order,
// collection, payment intent, amount, environment, account and mode make
// (canonical encoding, no other field). The environment must also be this
// worker's.
func CheckRefundPayload(raw []byte, exp RefundExpected, workerEnv string) (Dispatch, error) {
	var p RefundPayload
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil || dec.More() {
		return Dispatch{}, errors.New("payments: stored payload is not a refund payload")
	}
	switch {
	case p.V != 1 || p.Provider != "stripe" || p.Kind != "refund" || p.Method != "POST" || p.Path != refundPath ||
		p.Currency != "gbp":
		return Dispatch{}, errors.New("payments: stored payload is not a refund request")
	case p.Environment != exp.Environment || p.Environment != workerEnv:
		return Dispatch{}, errors.New("payments: stored refund payload belongs to another environment")
	case p.Account != exp.Account || p.Livemode != exp.Livemode || p.Livemode != (workerEnv == "production"):
		return Dispatch{}, errors.New("payments: stored refund payload belongs to another account or mode")
	case p.IdempotencyKey != exp.OperationID || p.RefundOperationID != exp.OperationID || p.OrderID != exp.OrderID ||
		p.CollectionID != exp.CollectionID || p.PaymentIntentID != exp.PaymentIntentID || p.AmountMinor != exp.AmountMinor:
		return Dispatch{}, errors.New("payments: stored refund payload does not match its operation")
	}
	form, err := exp.RefundRequest.Form()
	if err != nil || form != p.Request {
		return Dispatch{}, errors.New("payments: stored refund request does not match its operation")
	}
	return Dispatch{IdempotencyKey: p.IdempotencyKey, Form: p.Request}, nil
}
