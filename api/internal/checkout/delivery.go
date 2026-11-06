package checkout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
	"github.com/jackc/pgx/v5/pgconn"
)

// GK delivery quote, payment and waiver (K03, K06; migration 0035).
const (
	CmdDeliveryQuote   = "order.delivery_quote"
	CmdDeliveryWaiver  = "order.delivery_waiver"
	CmdDeliveryPayment = "order.delivery_payment"
)

// Delivery bounds the database checks too: a quote is whole pence from
// £100 (K03 "from £100") to 99999999 (Stripe's GBP maximum for one charge:
// the quote is paid as one Checkout Session); a measured driving distance is
// 1 m to 1000 km.
const (
	MinDeliveryMinor = 10_000
	MaxDeliveryMinor = 99_999_999
	MaxDistanceM     = 1_000_000
)

// DeliveryQuoteInput is the chef's quote for a GK delivery order at the
// version they saw. The note is staff-only.
type DeliveryQuoteInput struct {
	ExpectedVersion int     `json:"expected_version"`
	AmountMinor     int64   `json:"amount_minor"`
	Note            *string `json:"note,omitempty"`
}

// Validate checks the input.
func (in DeliveryQuoteInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	if in.AmountMinor < MinDeliveryMinor || in.AmountMinor > MaxDeliveryMinor {
		return invalid("amount_minor", "must be whole pence from %d (£100) to %d", MinDeliveryMinor, MaxDeliveryMinor)
	}
	if in.Note != nil {
		if err := oneLine("note", *in.Note, 500); err != nil {
			return err
		}
	}
	return nil
}

// DeliveryWaiverInput is the chef's free-delivery eligibility review of the
// order's current quote: the measured driving distance and its source.
type DeliveryWaiverInput struct {
	ExpectedVersion int    `json:"expected_version"`
	DistanceM       int    `json:"distance_m"`
	DistanceSource  string `json:"distance_source"`
}

// Validate checks the input.
func (in DeliveryWaiverInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	if in.DistanceM < 1 || in.DistanceM > MaxDistanceM {
		return invalid("distance_m", "must be the measured driving distance in whole metres (1 to %d)", MaxDistanceM)
	}
	return oneLine("distance_source", in.DistanceSource, 200)
}

// DeliveryPaymentInput pays the order's current delivery quote at the
// version the customer saw.
type DeliveryPaymentInput struct {
	ExpectedVersion int    `json:"expected_version"`
	QuoteID         string `json:"quote_id"`
}

// Validate checks the input.
func (in DeliveryPaymentInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	if !catalog.ValidUUID(in.QuoteID) {
		return invalid("quote_id", "must be the delivery quote shown on the order page")
	}
	return nil
}

// oneLine checks a short staff text: 1 to max characters on one line
// without surrounding spaces or control characters.
func oneLine(field, s string, max int) error {
	n := utf8.RuneCountInString(s)
	if !utf8.ValidString(s) || s != strings.TrimSpace(s) || n < 1 || n > max {
		return invalid(field, "must be 1 to %d characters without surrounding spaces", max)
	}
	for _, c := range s {
		if unicode.IsControl(c) || c == '\u2028' || c == '\u2029' {
			return invalid(field, "must be one line without control characters")
		}
	}
	return nil
}

// QuoteDelivery records the chef's delivery quote on order id (versioned,
// idempotent; a new quote replaces an unpaid one). The result is the quote.
func (s *Store) QuoteDelivery(ctx context.Context, subject, key, id string, in DeliveryQuoteInput) (catalog.Result, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.order_delivery_quote(
		$1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8)`,
		subject, id, key, catalog.BodyHash(CmdDeliveryQuote, id, in), in.ExpectedVersion, in.AmountMinor, in.Note,
		s.now().UTC()).Scan(&r.ID, &r.Replayed)
	return r, appointmentError("delivery quote", err)
}

// WaiveDelivery records the chef's free-delivery waiver of order id's
// current quote (versioned, idempotent). The result is the waiver.
func (s *Store) WaiveDelivery(ctx context.Context, subject, key, id string, in DeliveryWaiverInput) (catalog.Result, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.order_delivery_waiver(
		$1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8)`,
		subject, id, key, catalog.BodyHash(CmdDeliveryWaiver, id, in), in.ExpectedVersion, in.DistanceM, in.DistanceSource,
		s.now().UTC()).Scan(&r.ID, &r.Replayed)
	return r, appointmentError("delivery waiver", err)
}

// StartDeliveryPayment records a delivery collection for the current quote
// of order id and enqueues its protected Checkout Session dispatch, as
// StartPayment does for the cake (the same access, worker and rules). The
// result is the collection.
func (s *Store) StartDeliveryPayment(ctx context.Context, id, subject string, digest []byte, key string,
	in DeliveryPaymentInput) (catalog.Result, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) || (subject == "" && digest == nil) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	if !envPattern.MatchString(s.Env) || !payments.ValidAccount(s.StripeAccount) || s.UIOrigin == "" {
		return catalog.Result{}, ErrPaymentsUnavailable
	}
	var owner *string
	if subject != "" {
		owner, digest = &subject, nil
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.delivery_payment_initiate(
		$1::uuid, $2, $3::uuid, $4, $5, $6, $7::uuid, $8::uuid, $9, $10, $11, $12)`,
		owner, digest, id, key, catalog.BodyHash(CmdDeliveryPayment, id, in), in.ExpectedVersion, in.QuoteID,
		payments.NewID(), s.UIOrigin, s.Env, s.StripeAccount, s.now().UTC()).Scan(&r.ID, &r.Replayed)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "GK503" {
		return catalog.Result{}, ErrRecoveryFrozen
	}
	if err != nil {
		return catalog.Result{}, gatewayError("delivery payment", err)
	}
	return r, nil
}

// withDeliveryClock applies the store's clock to an order representation's
// delivery block (the database used its own): state quoted or
// pay_by_passed (now at or after quote.pay_by), and payable only before
// pay_by. Blocks without a quote pay-by (awaiting a quote, the fulfilment
// view) are returned as they are.
func (s *Store) withDeliveryClock(raw []byte) []byte {
	if !bytes.Contains(raw, []byte(`"pay_by"`)) {
		return raw
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return raw
	}
	var block map[string]json.RawMessage
	if json.Unmarshal(top["delivery"], &block) != nil || block == nil {
		return raw
	}
	var quote struct {
		PayBy *time.Time `json:"pay_by"`
	}
	var state string
	var payable bool
	if json.Unmarshal(block["quote"], &quote) != nil || quote.PayBy == nil || json.Unmarshal(block["state"], &state) != nil {
		return raw
	}
	passed := !s.now().Before(*quote.PayBy)
	if state == "quoted" || state == "pay_by_passed" {
		state = "quoted"
		if passed {
			state = "pay_by_passed"
		}
		block["state"], _ = json.Marshal(state)
	}
	if _, ok := block["payable"]; ok {
		_ = json.Unmarshal(block["payable"], &payable)
		block["payable"], _ = json.Marshal(payable && !passed)
	}
	var err error
	if top["delivery"], err = json.Marshal(block); err != nil {
		return raw
	}
	out, err := json.Marshal(top)
	if err != nil {
		return raw
	}
	return out
}
