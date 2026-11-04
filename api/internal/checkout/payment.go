package checkout

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
)

// CmdPaymentStart starts collecting an order's full payment (K04).
const CmdPaymentStart = "order.payment_start"

// ErrPaymentsUnavailable means payment collection is not configured
// (STRIPE_ACCOUNT_ID); the HTTP layer answers 503.
var ErrPaymentsUnavailable = errors.New("checkout: payment collection is not configured")

// PaymentInput starts a payment at the version the customer reviewed.
type PaymentInput struct {
	ExpectedVersion int `json:"expected_version"`
}

// Validate checks the input.
func (in PaymentInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	return nil
}

// StartPayment records a payment collection for order id and enqueues its
// protected Checkout Session dispatch (migration 0014). subject is the
// signed-in owner's verified subject, or "" for a guest with the order
// capability digest. The exact Stripe request is built here once, from the
// order's stored total, and never rebuilt; the database binds it to the
// collection, order, amount, environment and expiry under the order lock.
func (s *Store) StartPayment(ctx context.Context, id, subject string, digest []byte, key string, in PaymentInput) (catalog.Result, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) || (subject == "" && digest == nil) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	if !envPattern.MatchString(s.Env) || !payments.ValidAccount(s.StripeAccount) || s.UIOrigin == "" {
		return catalog.Result{}, ErrPaymentsUnavailable
	}
	raw, err := s.GetOrder(ctx, id, subject, digest)
	if err != nil {
		return catalog.Result{}, err
	}
	var o struct {
		TotalMinor int64 `json:"total_minor"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return catalog.Result{}, err
	}
	if o.TotalMinor < 1 {
		// A free order is confirmed, never paid.
		return catalog.Result{}, &catalog.ConflictError{Code: "state_conflict"}
	}
	now := s.now().UTC()
	collection := payments.NewID()
	payload, err := payments.NewPayload(payments.SessionRequest{CollectionID: collection, OrderID: id,
		AmountMinor: o.TotalMinor, Environment: s.Env, UIOrigin: s.UIOrigin, ExpiresAt: payments.SessionExpiry(now)},
		s.StripeAccount)
	if err != nil {
		return catalog.Result{}, err
	}
	var owner *string
	if subject != "" {
		owner, digest = &subject, nil
	}
	var r catalog.Result
	err = s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.payment_initiate(
		$1::uuid, $2, $3::uuid, $4, $5, $6, $7::uuid, $8, $9, $10, $11)`,
		owner, digest, id, key, catalog.BodyHash(CmdPaymentStart, id, in), in.ExpectedVersion, collection, string(payload),
		s.Env, s.StripeAccount, now).Scan(&r.ID, &r.Replayed)
	if err != nil {
		return catalog.Result{}, gatewayError("payment", err)
	}
	return r, nil
}
