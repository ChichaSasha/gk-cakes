package checkout

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
)

// Q5 manual alternative payout commands (K05; migration 0026).
const (
	CmdManualPayout        = "order.manual_payout"
	CmdManualPayoutOutcome = "order.manual_payout_outcome"
)

// evidenceRef is a reference to protected evidence kept outside this
// system: visible ASCII, at most 200 characters and no run of six or more
// digits, so raw bank details (sort codes, account numbers, IBANs) are
// refused (K05: no raw bank details anywhere). The database checks the
// same.
var (
	evidenceRefPattern = regexp.MustCompile(`^[\x21-\x7e]([\x20-\x7e]{0,198}[\x21-\x7e])?$`)
	digitRun           = regexp.MustCompile(`[0-9]{6,}`)
)

func evidenceRef(field, s string) error {
	if !evidenceRefPattern.MatchString(s) || digitRun.MatchString(s) {
		return invalid(field, "must be an evidence reference (1 to 200 visible characters, never bank details or a run of 6 or more digits)")
	}
	return nil
}

// ManualPayoutPreviewInput asks for a manual payout preview from one
// collection whose provider refund failed definitively.
type ManualPayoutPreviewInput struct {
	CollectionID           string `json:"collection_id"`
	UnavailableEvidenceRef string `json:"unavailable_evidence_ref"`
	RecipientEvidenceRef   string `json:"recipient_evidence_ref"`
}

// Validate checks the input.
func (in ManualPayoutPreviewInput) Validate() error {
	if !catalog.ValidUUID(in.CollectionID) {
		return invalid("collection_id", "must be a payment collection of the order")
	}
	if err := evidenceRef("unavailable_evidence_ref", in.UnavailableEvidenceRef); err != nil {
		return err
	}
	return evidenceRef("recipient_evidence_ref", in.RecipientEvidenceRef)
}

// PreviewManualPayout records (or returns again) an immutable manual payout
// preview for the order's current version.
func (s *Store) PreviewManualPayout(ctx context.Context, subject, orderID string, in ManualPayoutPreviewInput) (json.RawMessage, error) {
	if !catalog.ValidUUID(orderID) {
		return nil, catalog.ErrNotFound
	}
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT business.manual_payout_preview($1::uuid, $2::uuid, $3::uuid, $4, $5, $6)::text`,
		subject, orderID, in.CollectionID, in.UnavailableEvidenceRef, in.RecipientEvidenceRef, s.now().UTC()).Scan(&raw)
	if err != nil {
		return nil, gatewayError("manual payout preview", err)
	}
	return json.RawMessage(raw), nil
}

// ManualPayoutInput reserves a previewed manual payout.
type ManualPayoutInput struct {
	ExpectedVersion int    `json:"expected_version"`
	PreviewID       string `json:"preview_id"`
	PreviewHash     string `json:"preview_hash"`
}

// Validate checks the input.
func (in ManualPayoutInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	if !catalog.ValidUUID(in.PreviewID) || !sha256Pattern.MatchString(in.PreviewHash) {
		return invalid("preview_id", "must be a manual payout preview you requested for this order, with its preview_hash")
	}
	return nil
}

// RequestManualPayout records the reservation as a K11 order command (202):
// the instructions appear in ManualPayouts once it is secured.
func (s *Store) RequestManualPayout(ctx context.Context, subject, key, orderID string, in ManualPayoutInput) (catalog.Result, error) {
	if !catalog.ValidUUID(orderID) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	if !envPattern.MatchString(s.Env) {
		return catalog.Result{}, errors.New("checkout: the recovery journal environment is not configured")
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.manual_payout_request(
		$1::uuid, $2::uuid, $3, $4, $5, $6::uuid, $7, $8, $9)`,
		subject, orderID, key, catalog.BodyHash(CmdManualPayout, orderID, in), in.ExpectedVersion, in.PreviewID, in.PreviewHash,
		s.Env, s.now().UTC()).Scan(&r.ID, &r.Replayed)
	if err != nil {
		return catalog.Result{}, gatewayError("manual payout", err)
	}
	return r, nil
}

// Manual payout outcomes.
var manualOutcomes = map[string]bool{"submitted": true, "succeeded": true, "failed": true, "unknown": true, "withdrawn": true}

// ManualPayoutOutcomeInput records what happened to a manual transfer.
type ManualPayoutOutcomeInput struct {
	ExpectedVersion  int    `json:"expected_version"`
	Outcome          string `json:"outcome"`
	TransferIdentity string `json:"transfer_identity,omitempty"`
	EvidenceRef      string `json:"evidence_ref,omitempty"`
	AmountMinor      int64  `json:"amount_minor,omitempty"`
	Currency         string `json:"currency,omitempty"`
}

// Validate checks the input.
func (in ManualPayoutOutcomeInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	if !manualOutcomes[in.Outcome] {
		return invalid("outcome", "must be submitted, succeeded, failed, unknown or withdrawn")
	}
	success := in.Outcome == "succeeded"
	if success != (in.TransferIdentity != "") || success != (in.AmountMinor != 0) || success != (in.Currency != "") {
		return invalid("outcome", "succeeded needs transfer_identity, evidence_ref, amount_minor and currency; other outcomes take none of the transfer fields")
	}
	if success && in.Currency != "GBP" {
		return invalid("currency", "must be GBP")
	}
	if success && (in.AmountMinor < 1 || in.AmountMinor > maxRefundMinor) {
		return invalid("amount_minor", "must be the reserved amount in pence")
	}
	if (success || in.Outcome == "failed") && in.EvidenceRef == "" {
		return invalid("evidence_ref", "is required for succeeded and failed")
	}
	if in.TransferIdentity != "" {
		if err := evidenceRef("transfer_identity", in.TransferIdentity); err != nil {
			return err
		}
	}
	if in.EvidenceRef != "" {
		return evidenceRef("evidence_ref", in.EvidenceRef)
	}
	return nil
}

// RecordManualPayoutOutcome records the outcome as a K11 order command
// (202).
func (s *Store) RecordManualPayoutOutcome(ctx context.Context, subject, key, orderID, operationID string, in ManualPayoutOutcomeInput) (catalog.Result, error) {
	order, op, ok := ids(orderID, operationID)
	if !ok {
		return catalog.Result{}, catalog.ErrNotFound
	}
	if !envPattern.MatchString(s.Env) {
		return catalog.Result{}, errors.New("checkout: the recovery journal environment is not configured")
	}
	var amount *int64
	if in.AmountMinor != 0 {
		amount = &in.AmountMinor
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.manual_payout_outcome_request(
		$1::uuid, $2::uuid, $3, $4, $5, $6::uuid, $7, $8, $9, $10, $11, $12, $13)`,
		subject, order, key, catalog.BodyHash(CmdManualPayoutOutcome, op, in), in.ExpectedVersion, op, in.Outcome,
		optional(in.TransferIdentity), optional(in.EvidenceRef), amount, optional(in.Currency), s.Env, s.now().UTC(),
	).Scan(&r.ID, &r.Replayed)
	if err != nil {
		return catalog.Result{}, gatewayError("manual payout outcome", err)
	}
	return r, nil
}

// ManualPayouts lists the order's manual payouts (chef); instructions only
// once protected.
func (s *Store) ManualPayouts(ctx context.Context, subject, orderID string) (json.RawMessage, error) {
	if !catalog.ValidUUID(orderID) {
		return nil, catalog.ErrNotFound
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT business.manual_payout_list($1::uuid, $2::uuid)::text`, subject, orderID).Scan(&raw); err != nil {
		return nil, gatewayError("manual payouts", err)
	}
	return json.RawMessage(raw), nil
}
