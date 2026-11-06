// Package payments collects cake payments through Stripe Checkout (contracts
// r6.5 K04, K05, K11; migration 0014_payments.sql): the exact Checkout
// Session request stored at initiation, its protected dispatch, the signed
// webhook inbox, and reconciliation that verifies the provider's evidence
// before a paid confirmation is recorded. No personal data is sent to
// Stripe, stored as evidence or logged.
package payments

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Stripe protocol constants.
const (
	// APIVersion pins the Stripe API version every request uses (the
	// account's dashboard default, verified against the sandbox).
	APIVersion = "2026-08-26.dahlia"
	// SessionLifetime is the expiry requested for every Checkout Session,
	// counted from the first dispatch (when the request is fixed). Stripe
	// allows 30 minutes to 24 hours after creation; two hours leaves room
	// for retries of a call that may never have reached Stripe, while the
	// 15-minute attempt expiry (A3c) bounds how long a hold really waits.
	SessionLifetime = 2 * time.Hour
	// MinSessionLifetime is Stripe's minimum: a first call with less left
	// is refused before it is made (the database checks 31 minutes).
	MinSessionLifetime = 30 * time.Minute
	// ReplayWindow is how long a dispatch may be replayed with the same
	// idempotency key (Stripe keeps keys 24 hours).
	ReplayWindow = 23 * time.Hour
	// ProductName is the only line-item text sent to Stripe for a cake
	// collection (no personal data, no cake details).
	ProductName = "GK Cakes order"
	// DeliveryProductName is the line-item text of a separate GK delivery
	// collection (migration 0035).
	DeliveryProductName = "GK Cakes delivery"
	// KindDelivery is a delivery collection's kind; anything else is the
	// cake's.
	KindDelivery = "delivery"
	// ReturnQuery is appended to the order page for both return URLs; the
	// return never confirms anything.
	ReturnQuery = "?payment=returned"
	sessionPath = "/v1/checkout/sessions"
)

var (
	uuidPattern    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	envPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	accountPattern = regexp.MustCompile(`^acct_[A-Za-z0-9]{1,64}$`)
)

// ValidAccount reports whether s is a Stripe account ID (acct_...).
func ValidAccount(s string) bool { return accountPattern.MatchString(s) }

// NewID returns a random lower-case version 4 UUID.
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// SessionRequest is what a Checkout Session request is built from.
type SessionRequest struct {
	CollectionID string
	OrderID      string
	AmountMinor  int64 // GBP pence, the order total
	Environment  string
	UIOrigin     string // exact origin; return URLs go to its order page
	ExpiresAt    time.Time
	Kind         string // the collection's kind ("delivery" names the line item)
}

// productName is the line-item text for a collection kind.
func productName(kind string) string {
	if kind == KindDelivery {
		return DeliveryProductName
	}
	return ProductName
}

// SessionExpiry is the expires_at for a request fixed at now (the database
// clock of the first dispatch): its second plus SessionLifetime (the
// database checks the same).
func SessionExpiry(now time.Time) time.Time {
	return now.UTC().Truncate(time.Second).Add(SessionLifetime)
}

func (r SessionRequest) validate() error {
	switch {
	case !uuidPattern.MatchString(r.CollectionID) || !uuidPattern.MatchString(r.OrderID):
		return errors.New("payments: collection and order IDs must be lower-case UUIDs")
	case r.AmountMinor < 1:
		return errors.New("payments: the amount must be positive pence")
	case !envPattern.MatchString(r.Environment):
		return errors.New("payments: the environment must be a lower-case name")
	case r.ExpiresAt.IsZero() || r.ExpiresAt.Unix() <= 0:
		return errors.New("payments: the session expiry is required")
	}
	u, err := url.Parse(r.UIOrigin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Path != "" || u.RawQuery != "" ||
		u.Fragment != "" || u.User != nil {
		return errors.New("payments: the UI origin must be an exact origin")
	}
	return nil
}

// ReturnURL is the order page Stripe returns the customer to.
func ReturnURL(origin, orderID string) string {
	return origin + "/orders/" + orderID + ReturnQuery
}

func (r SessionRequest) values() url.Values {
	v := url.Values{}
	v.Set("mode", "payment")
	v.Set("line_items[0][price_data][currency]", "gbp")
	v.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(r.AmountMinor, 10))
	v.Set("line_items[0][price_data][product_data][name]", productName(r.Kind))
	v.Set("line_items[0][quantity]", "1")
	v.Set("client_reference_id", r.OrderID)
	for _, prefix := range []string{"metadata", "payment_intent_data[metadata]"} {
		v.Set(prefix+"[order_id]", r.OrderID)
		v.Set(prefix+"[collection_id]", r.CollectionID)
		v.Set(prefix+"[environment]", r.Environment)
	}
	ret := ReturnURL(r.UIOrigin, r.OrderID)
	v.Set("success_url", ret)
	v.Set("cancel_url", ret)
	v.Set("expires_at", strconv.FormatInt(r.ExpiresAt.Unix(), 10))
	return v
}

// Form is the exact form-encoded Checkout Session request: deterministic
// (keys sorted), one line item for the whole order, no customer data.
func (r SessionRequest) Form() (string, error) {
	if err := r.validate(); err != nil {
		return "", err
	}
	return r.values().Encode(), nil
}

// Payload is the stored dispatch payload: everything the dispatch is
// rebuilt from after a database loss (K11), including the exact request.
type Payload struct {
	V              int    `json:"v"`
	Provider       string `json:"provider"`
	Environment    string `json:"environment"`
	Account        string `json:"account"`
	Livemode       bool   `json:"livemode"`
	IdempotencyKey string `json:"idempotency_key"`
	OrderID        string `json:"order_id"`
	AmountMinor    int64  `json:"amount_minor"`
	Currency       string `json:"currency"`
	Method         string `json:"method"`
	Path           string `json:"path"`
	Request        string `json:"request"`
}

// NewPayload builds the dispatch payload bytes for a collection.
func NewPayload(r SessionRequest, account string) ([]byte, error) {
	if !accountPattern.MatchString(account) {
		return nil, errors.New("payments: a Stripe account ID (acct_...) is required")
	}
	form, err := r.Form()
	if err != nil {
		return nil, err
	}
	return encode(Payload{V: 1, Provider: "stripe", Environment: r.Environment, Account: account,
		Livemode: r.Environment == "production", IdempotencyKey: r.CollectionID, OrderID: r.OrderID,
		AmountMinor: r.AmountMinor, Currency: "gbp", Method: "POST", Path: sessionPath, Request: form})
}

func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// Expected is what the database says a collection is.
type Expected struct {
	CollectionID string
	OrderID      string
	AmountMinor  int64
	Environment  string
	Account      string
	Livemode     bool
	UIOrigin     string    // the stored return origin
	ExpiresAt    time.Time // the stored session expiry
	Kind         string    // the collection's kind (cake or delivery)
}

// Dispatch is a validated stored request.
type Dispatch struct {
	IdempotencyKey string
	Form           string
}

var allowedKeys = func() map[string]bool {
	keys := map[string]bool{}
	for k := range (SessionRequest{}).values() {
		keys[k] = true
	}
	return keys
}()

// CheckPayload validates a stored payload against its collection before any
// call: the exact request (canonical encoding, exactly the expected keys and
// values, no customer data) for this collection, order, amount, environment,
// account and mode. The environment must also be this worker's.
func CheckPayload(raw []byte, exp Expected, workerEnv string) (Dispatch, error) {
	var p Payload
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil || dec.More() {
		return Dispatch{}, errors.New("payments: stored payload is not a payment payload")
	}
	switch {
	case p.V != 1 || p.Provider != "stripe" || p.Method != "POST" || p.Path != sessionPath || p.Currency != "gbp":
		return Dispatch{}, errors.New("payments: stored payload is not a Checkout Session request")
	case p.Environment != exp.Environment || p.Environment != workerEnv:
		return Dispatch{}, errors.New("payments: stored payload belongs to another environment")
	case p.Account != exp.Account || p.Livemode != exp.Livemode || p.Livemode != (workerEnv == "production"):
		return Dispatch{}, errors.New("payments: stored payload belongs to another account or mode")
	case p.IdempotencyKey != exp.CollectionID || p.OrderID != exp.OrderID || p.AmountMinor != exp.AmountMinor:
		return Dispatch{}, errors.New("payments: stored payload does not match its collection")
	}
	v, err := url.ParseQuery(p.Request)
	if err != nil || v.Encode() != p.Request {
		return Dispatch{}, errors.New("payments: stored request is not canonical form encoding")
	}
	for k, vals := range v {
		if !allowedKeys[k] || len(vals) != 1 {
			return Dispatch{}, fmt.Errorf("payments: stored request has an unexpected field %q", k)
		}
	}
	exp1 := v.Get("expires_at")
	sec, err := strconv.ParseInt(exp1, 10, 64)
	if err != nil || sec <= 0 || strconv.FormatInt(sec, 10) != exp1 {
		return Dispatch{}, errors.New("payments: stored request has no valid expiry")
	}
	ret := v.Get("success_url")
	origin, ok := strings.CutSuffix(ret, "/orders/"+exp.OrderID+ReturnQuery)
	if !ok || origin != exp.UIOrigin {
		return Dispatch{}, errors.New("payments: stored request returns to another page")
	}
	if !time.Unix(sec, 0).Equal(exp.ExpiresAt) {
		return Dispatch{}, errors.New("payments: stored request expiry does not match its collection")
	}
	want := SessionRequest{CollectionID: exp.CollectionID, OrderID: exp.OrderID, AmountMinor: exp.AmountMinor,
		Environment: exp.Environment, UIOrigin: origin, ExpiresAt: time.Unix(sec, 0), Kind: exp.Kind}
	form, err := want.Form()
	if err == nil && form != p.Request && exp.Kind == KindDelivery {
		// A delivery request fixed by a build before the delivery line item
		// (it named it like the cake's) is still that collection's request.
		want.Kind = ""
		form, err = want.Form()
	}
	if err != nil || form != p.Request {
		return Dispatch{}, errors.New("payments: stored request does not match its collection")
	}
	return Dispatch{IdempotencyKey: p.IdempotencyKey, Form: p.Request}, nil
}
