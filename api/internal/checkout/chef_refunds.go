package checkout

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/jackc/pgx/v5/pgconn"
)

// Chef refund commands (K05; migration 0024).
const (
	CmdRefundException = "refund.exception"
	CmdDeliveryRefund  = "refund.delivery_pay"
)

// Refund bounds checked before the database: at most 10 allocations and
// £1,000,000 in pence (the database checks the same).
const (
	maxRefundAllocations = 10
	maxRefundMinor       = 100_000_000
)

// RefundAllocation is the part of an agreed exception returned from one of
// the order's succeeded payment collections.
type RefundAllocation struct {
	CollectionID string `json:"collection_id"`
	AmountMinor  int64  `json:"amount_minor"`
}

// RefundInput is the chef's agreed partial refund (K05: amount,
// allocations, reason, version and key mandatory).
type RefundInput struct {
	ExpectedVersion int                `json:"expected_version"`
	AmountMinor     int64              `json:"amount_minor"`
	Currency        string             `json:"currency"`
	Allocations     []RefundAllocation `json:"allocations"`
	Reason          string             `json:"reason"`
}

// Validate checks the input (the database checks the collections and the
// limits).
func (in RefundInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	if in.AmountMinor < 1 || in.AmountMinor > maxRefundMinor {
		return invalid("amount_minor", "must be 1 to %d pence", maxRefundMinor)
	}
	if in.Currency != "GBP" {
		return invalid("currency", "must be GBP")
	}
	if len(in.Allocations) < 1 || len(in.Allocations) > maxRefundAllocations {
		return invalid("allocations", "must list 1 to %d collections", maxRefundAllocations)
	}
	seen := map[string]bool{}
	var sum int64
	for _, a := range in.Allocations {
		if !catalog.ValidUUID(a.CollectionID) || seen[a.CollectionID] {
			return invalid("allocations", "must name each payment collection of the order once")
		}
		if a.AmountMinor < 1 || a.AmountMinor > maxRefundMinor {
			return invalid("allocations", "must each return at least 1 penny")
		}
		seen[a.CollectionID] = true
		sum += a.AmountMinor
	}
	if sum != in.AmountMinor {
		return invalid("allocations", "must add up to amount_minor")
	}
	return reasonLine(in.Reason)
}

// reasonLine checks a chef's reason note: 1 to 500 characters on one line.
func reasonLine(r string) error {
	if !utf8.ValidString(r) || r != strings.TrimSpace(r) || utf8.RuneCountInString(r) < 1 || utf8.RuneCountInString(r) > 500 {
		return invalid("reason", "must be 1 to 500 characters without surrounding spaces")
	}
	for _, c := range r {
		if unicode.IsControl(c) || c == '\u2028' || c == '\u2029' {
			return invalid("reason", "must be one line without control characters")
		}
	}
	return nil
}

// RecordRefund records the chef's agreed exception on order id: the goal is
// raised by the amount and exactly the allocations are reserved for
// dispatch (202; the order's refund block reports progress). The result ID
// is the exception.
func (s *Store) RecordRefund(ctx context.Context, subject, key, id string, in RefundInput) (catalog.Result, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	collections := make([]string, len(in.Allocations))
	amounts := make([]int64, len(in.Allocations))
	for i, a := range in.Allocations {
		collections[i], amounts[i] = a.CollectionID, a.AmountMinor
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.refund_exception_record(
		$1::uuid, $2, $3, $4::uuid, $5, $6, $7, $8::uuid[], $9::bigint[], $10)`,
		subject, key, catalog.BodyHash(CmdRefundException, id, in), id, in.ExpectedVersion, in.AmountMinor, in.Currency,
		collections, amounts, in.Reason).Scan(&r.ID, &r.Replayed)
	return refundResult("refund exception", r, err)
}

// DeliveryRefundInput pays the delivery share of the order's refund goal
// that waits for the chef, from one delivery collection.
type DeliveryRefundInput struct {
	ExpectedVersion int    `json:"expected_version"`
	CollectionID    string `json:"collection_id"`
}

// Validate checks the input.
func (in DeliveryRefundInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	if !catalog.ValidUUID(in.CollectionID) {
		return invalid("collection_id", "must be a delivery payment collection of the order")
	}
	return nil
}

// PayDeliveryRefund reserves the pending delivery share of order id (K05:
// delivery refunds are chef-only). The result ID is the refund operation.
func (s *Store) PayDeliveryRefund(ctx context.Context, subject, key, id string, in DeliveryRefundInput) (catalog.Result, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.refund_delivery_pay(
		$1::uuid, $2, $3, $4::uuid, $5, $6::uuid)`,
		subject, key, catalog.BodyHash(CmdDeliveryRefund, id, in), id, in.ExpectedVersion, in.CollectionID).Scan(&r.ID, &r.Replayed)
	return refundResult("delivery refund", r, err)
}

func refundResult(op string, r catalog.Result, err error) (catalog.Result, error) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "GK503" {
		return catalog.Result{}, ErrOrdersFrozen
	}
	if err != nil {
		return catalog.Result{}, gatewayError(op, err)
	}
	return r, nil
}
