package checkout

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
)

// Cancellation commands (K05, migration 0021).
const (
	CmdCancel     = "order.cancel"
	CmdChefCancel = "order.chef_cancel"
)

// CancellationPreview is what cancelling the order now would return: the
// server-computed K05 rate (100 strictly before full_refund_deadline_at, 50
// at or after), the paid aggregate, the half-up amount, its allocation per
// paid collection and a short expiry. The customer confirms it by ID and
// hash (CancelInput).
type CancellationPreview = json.RawMessage

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// CancelInput is the customer's confirmation of a preview at the version
// they reviewed (K00 expected_version).
type CancelInput struct {
	ExpectedVersion int    `json:"expected_version"`
	PreviewID       string `json:"preview_id"`
	PreviewHash     string `json:"preview_hash"`
}

// Validate checks the input.
func (in CancelInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	if !catalog.ValidUUID(in.PreviewID) {
		return invalid("preview_id", "must be a cancellation preview ID")
	}
	if !hashPattern.MatchString(in.PreviewHash) {
		return invalid("preview_hash", "must be the preview's preview_hash")
	}
	return nil
}

// ChefCancelInput is the chef's cancellation (100%) with a reason note.
type ChefCancelInput struct {
	ExpectedVersion int    `json:"expected_version"`
	Reason          string `json:"reason"`
}

// Validate checks the input.
func (in ChefCancelInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	r := in.Reason
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

// PreviewCancellation computes and records a cancellation preview of order
// id for its owner (subject) or a guest capability holder with "cancel".
func (s *Store) PreviewCancellation(ctx context.Context, id, subject string, digest []byte) (CancellationPreview, error) {
	if !catalog.ValidUUID(id) || (subject == "" && digest == nil) {
		return nil, catalog.ErrNotFound
	}
	var owner *string
	if subject != "" {
		owner, digest = &subject, nil
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT business.order_cancellation_preview($1::uuid, $2, $3::uuid, $4)`,
		owner, digest, id, s.now().UTC()).Scan(&raw); err != nil {
		return nil, gatewayError("cancellation preview", err)
	}
	return CancellationPreview(raw), nil
}

// Cancel records the customer's cancellation of order id confirming a
// preview, and enqueues its protected commit (K11): 202, poll the order.
func (s *Store) Cancel(ctx context.Context, id, subject string, digest []byte, key string, in CancelInput) (catalog.Result, error) {
	if !catalog.ValidUUID(id) || (subject == "" && digest == nil) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	if !envPattern.MatchString(s.Env) {
		return catalog.Result{}, errors.New("checkout: the recovery journal environment is not configured")
	}
	var owner *string
	if subject != "" {
		owner, digest = &subject, nil
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.order_cancel_request(
		$1::uuid, $2, $3::uuid, $4, $5, $6, $7::uuid, $8, $9, $10)`,
		owner, digest, id, key, catalog.BodyHash(CmdCancel, id, in), in.ExpectedVersion, in.PreviewID, in.PreviewHash,
		s.Env, s.now().UTC(),
	).Scan(&r.ID, &r.Replayed)
	if err != nil {
		return catalog.Result{}, gatewayError("cancellation", err)
	}
	return r, nil
}

// ChefCancel records the chef's cancellation of order id (100% return) and
// enqueues its protected commit (K11).
func (s *Store) ChefCancel(ctx context.Context, subject, key, id string, in ChefCancelInput) (catalog.Result, error) {
	if !catalog.ValidUUID(id) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	if !envPattern.MatchString(s.Env) {
		return catalog.Result{}, errors.New("checkout: the recovery journal environment is not configured")
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.order_chef_cancel_request(
		$1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8)`,
		subject, id, key, catalog.BodyHash(CmdChefCancel, id, in), in.ExpectedVersion, in.Reason, s.Env, s.now().UTC(),
	).Scan(&r.ID, &r.Replayed)
	if err != nil {
		return catalog.Result{}, gatewayError("chef cancellation", err)
	}
	return r, nil
}
