package checkout

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
	"github.com/jackc/pgx/v5/pgconn"
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

// ErrRecoveryFrozen means a restore freeze refuses new collections for now
// (503, retryable).
var ErrRecoveryFrozen = fmt.Errorf("%w: a restore is in progress", ErrPaymentsUnavailable)

// StartPayment records a payment collection for order id and enqueues its
// protected Checkout Session dispatch (migration 0014). subject is the
// signed-in owner's verified subject, or "" for a guest with the order
// capability digest. The exact Stripe request is fixed by the worker at the
// first dispatch, from the order total and UIOrigin stored here.
func (s *Store) StartPayment(ctx context.Context, id, subject string, digest []byte, key string, in PaymentInput) (catalog.Result, error) {
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
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.payment_initiate(
		$1::uuid, $2, $3::uuid, $4, $5, $6, $7::uuid, $8, $9, $10, $11)`,
		owner, digest, id, key, catalog.BodyHash(CmdPaymentStart, id, in), in.ExpectedVersion, payments.NewID(), s.UIOrigin,
		s.Env, s.StripeAccount, s.now().UTC()).Scan(&r.ID, &r.Replayed)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "GK503" {
		return catalog.Result{}, ErrRecoveryFrozen
	}
	if err != nil {
		return catalog.Result{}, gatewayError("payment", err)
	}
	return r, nil
}
