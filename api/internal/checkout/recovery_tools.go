package checkout

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
)

// Manager recovery commands (K05; migration 0025).
const (
	CmdObservationResolve = "refund.observation_resolve"
	CmdOperationDecide    = "refund.operation_decide"
	CmdMismatchClear      = "refund.mismatch_clear"
	CmdPaymentMismatch    = "payment.mismatch_clear"
	CmdRemainderReserve   = "refund.remainder_reserve"
)

var refundIDPattern = regexp.MustCompile(`^(re|pyr)_[A-Za-z0-9_]{1,250}$`)

// noteLine checks a manager decision note: 1 to 500 characters on one line
// (required unless optional and empty).
func noteLine(note string, optional bool) error {
	if optional && note == "" {
		return nil
	}
	if err := reasonLine(note); err != nil {
		return invalid("note", "must be 1 to 500 characters on one line without surrounding spaces")
	}
	return nil
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ResolveObservationInput resolves a quarantined refund observation:
// "match" (with refund_operation_id) or "accept_external".
type ResolveObservationInput struct {
	ExpectedVersion   int    `json:"expected_version"`
	Resolution        string `json:"resolution"`
	RefundOperationID string `json:"refund_operation_id,omitempty"`
	Note              string `json:"note"`
}

// Validate checks the input.
func (in ResolveObservationInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	switch in.Resolution {
	case "match":
		if !catalog.ValidUUID(in.RefundOperationID) {
			return invalid("refund_operation_id", "must name the refund operation to match")
		}
	case "accept_external":
		if in.RefundOperationID != "" {
			return invalid("refund_operation_id", "is only for match")
		}
	default:
		return invalid("resolution", "must be match or accept_external")
	}
	return noteLine(in.Note, false)
}

// DecideOperationInput decides an unknown refund past its replay window:
// "failed" (complete-list evidence of non-execution) or "succeeded" (with
// the provider refund ID, verified at Stripe).
type DecideOperationInput struct {
	ExpectedVersion  int    `json:"expected_version"`
	Decision         string `json:"decision"`
	ProviderRefundID string `json:"provider_refund_id,omitempty"`
	Note             string `json:"note"`
}

// Validate checks the input.
func (in DecideOperationInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	switch in.Decision {
	case "succeeded":
		if !refundIDPattern.MatchString(in.ProviderRefundID) {
			return invalid("provider_refund_id", "must be the Stripe refund ID (re_...)")
		}
	case "failed":
		if in.ProviderRefundID != "" {
			return invalid("provider_refund_id", "is only for succeeded")
		}
	default:
		return invalid("decision", "must be failed or succeeded")
	}
	return noteLine(in.Note, false)
}

// ClearMismatchInput clears the contradiction on a refund operation: keep
// its state, or correct it to failed or succeeded.
type ClearMismatchInput struct {
	ExpectedVersion int    `json:"expected_version"`
	Decision        string `json:"decision"`
	Note            string `json:"note"`
}

// Validate checks the input.
func (in ClearMismatchInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	if in.Decision != "keep" && in.Decision != "failed" && in.Decision != "succeeded" {
		return invalid("decision", "must be keep, failed or succeeded")
	}
	return noteLine(in.Note, false)
}

// NoteInput is a versioned decision with a note (optional where stated).
type NoteInput struct {
	ExpectedVersion int    `json:"expected_version"`
	Note            string `json:"note,omitempty"`
}

// Validate checks the input (the note is required).
func (in NoteInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	return noteLine(in.Note, false)
}

// RemainderInput re-reserves a failed refund's remainder (note optional).
type RemainderInput NoteInput

// Validate checks the input.
func (in RemainderInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	return noteLine(in.Note, true)
}

func ids(order, sub string) (string, string, bool) {
	order, sub = strings.ToLower(order), strings.ToLower(sub)
	return order, sub, catalog.ValidUUID(order) && catalog.ValidUUID(sub)
}

// ResolveObservation resolves a quarantined refund observation of the order.
// The result ID is the decision.
func (s *Store) ResolveObservation(ctx context.Context, subject, key, orderID, observationID string, in ResolveObservationInput) (catalog.Result, error) {
	order, obs, ok := ids(orderID, observationID)
	if !ok {
		return catalog.Result{}, catalog.ErrNotFound
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.refund_observation_resolve(
		$1::uuid, $2, $3, $4::uuid, $5, $6::uuid, $7, $8::uuid, $9)`,
		subject, key, catalog.BodyHash(CmdObservationResolve, obs, in), order, in.ExpectedVersion, obs, in.Resolution,
		optional(in.RefundOperationID), in.Note).Scan(&r.ID, &r.Replayed)
	return refundResult("observation resolve", r, err)
}

// DecideOperation decides an unknown refund operation of the order past its
// replay window. The result ID is the decision.
func (s *Store) DecideOperation(ctx context.Context, subject, key, orderID, operationID string, in DecideOperationInput) (catalog.Result, error) {
	order, op, ok := ids(orderID, operationID)
	if !ok {
		return catalog.Result{}, catalog.ErrNotFound
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.refund_operation_decide(
		$1::uuid, $2, $3, $4::uuid, $5, $6::uuid, $7, $8, $9)`,
		subject, key, catalog.BodyHash(CmdOperationDecide, op, in), order, in.ExpectedVersion, op, in.Decision,
		optional(in.ProviderRefundID), in.Note).Scan(&r.ID, &r.Replayed)
	return refundResult("operation decide", r, err)
}

// ClearRefundMismatch clears the contradiction recorded on a refund
// operation of the order. The result ID is the decision.
func (s *Store) ClearRefundMismatch(ctx context.Context, subject, key, orderID, operationID string, in ClearMismatchInput) (catalog.Result, error) {
	order, op, ok := ids(orderID, operationID)
	if !ok {
		return catalog.Result{}, catalog.ErrNotFound
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.refund_mismatch_clear(
		$1::uuid, $2, $3, $4::uuid, $5, $6::uuid, $7, $8)`,
		subject, key, catalog.BodyHash(CmdMismatchClear, op, in), order, in.ExpectedVersion, op, in.Decision,
		in.Note).Scan(&r.ID, &r.Replayed)
	return refundResult("refund mismatch clear", r, err)
}

// ClearPaymentMismatch clears the contradiction recorded on a decided
// payment collection of the order. The result ID is the decision.
func (s *Store) ClearPaymentMismatch(ctx context.Context, subject, key, orderID, collectionID string, in NoteInput) (catalog.Result, error) {
	order, coll, ok := ids(orderID, collectionID)
	if !ok {
		return catalog.Result{}, catalog.ErrNotFound
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.payment_mismatch_clear(
		$1::uuid, $2, $3, $4::uuid, $5, $6::uuid, $7)`,
		subject, key, catalog.BodyHash(CmdPaymentMismatch, coll, in), order, in.ExpectedVersion, coll,
		in.Note).Scan(&r.ID, &r.Replayed)
	return refundResult("payment mismatch clear", r, err)
}

// ReserveRemainder re-reserves a failed provider refund's remainder on its
// collection. The result ID is the decision.
func (s *Store) ReserveRemainder(ctx context.Context, subject, key, orderID, operationID string, in RemainderInput) (catalog.Result, error) {
	order, op, ok := ids(orderID, operationID)
	if !ok {
		return catalog.Result{}, catalog.ErrNotFound
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.refund_remainder_reserve(
		$1::uuid, $2, $3, $4::uuid, $5, $6::uuid, $7)`,
		subject, key, catalog.BodyHash(CmdRemainderReserve, op, in), order, in.ExpectedVersion, op,
		optional(in.Note)).Scan(&r.ID, &r.Replayed)
	return refundResult("remainder reserve", r, err)
}

// Attention is the manager attention list as the database renders it
// (amounts only for the chef).
type Attention = json.RawMessage

// AttentionList reads the manager attention list (chef or technical
// support; a support read is audited).
func (s *Store) AttentionList(ctx context.Context, subject string) (Attention, error) {
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT business.refund_attention_list($1::uuid)::text`, subject).Scan(&raw); err != nil {
		return nil, gatewayError("attention list", err)
	}
	return json.RawMessage(raw), nil
}
