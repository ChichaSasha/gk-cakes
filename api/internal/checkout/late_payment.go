package checkout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
)

// The late-payment agreement on the order page (GK-017 r02b Q6; migration
// 0034): the chef offers the requested date or another date, and the
// customer accepts or declines that offer on the order page.
const (
	CmdLatePaymentOffer  = "order.late_payment_offer"
	CmdLatePaymentAnswer = "order.late_payment_answer"
)

// Offer kinds.
const (
	OfferRequested = "requested"
	OfferOther     = "other"
)

// LatePaymentOfferInput is the chef's offer on a paid_awaiting_agreement
// order: the customer's requested appointment, or another published start
// (target_date and target_slot_id, as the chef's appointment change takes
// them). An offer changes nothing until the customer accepts it.
type LatePaymentOfferInput struct {
	ExpectedVersion int    `json:"expected_version"`
	Kind            string `json:"kind"`
	TargetDate      string `json:"target_date,omitempty"`
	TargetSlotID    string `json:"target_slot_id,omitempty"`
}

// Validate checks the input.
func (in LatePaymentOfferInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	switch in.Kind {
	case OfferRequested:
		if in.TargetDate != "" || in.TargetSlotID != "" {
			return invalid("target_slot_id", "is only for kind other: requested offers the order's own time")
		}
	case OfferOther:
		if d, err := time.Parse(time.DateOnly, in.TargetDate); err != nil || d.Format(time.DateOnly) != in.TargetDate {
			return invalid("target_date", "must be a date (YYYY-MM-DD)")
		}
		if !catalog.ValidUUID(in.TargetSlotID) {
			return invalid("target_slot_id", "must be a slot ID of the target date")
		}
	default:
		return invalid("kind", "must be requested or other")
	}
	return nil
}

// OfferLatePayment records the chef's offer on order id (versioned,
// idempotent, not a K11 command: it holds no capacity and changes no
// commitment). The result is the offer.
func (s *Store) OfferLatePayment(ctx context.Context, subject, key, id string, in LatePaymentOfferInput) (catalog.Result, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	var day, slot *string
	if in.Kind == OfferOther {
		day, slot = &in.TargetDate, &in.TargetSlotID
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.order_late_payment_offer(
		$1::uuid, $2::uuid, $3, $4, $5, $6, $7::date, $8::uuid, $9)`,
		subject, id, key, catalog.BodyHash(CmdLatePaymentOffer, id, in), in.ExpectedVersion, in.Kind, day, slot,
		s.now().UTC()).Scan(&r.ID, &r.Replayed)
	return r, appointmentError("late payment offer", err)
}

// LatePaymentAnswerInput is the customer's answer to the current offer at
// the version they saw: accept (explicit agreement, recorded as evidence) or
// decline (everything paid back).
type LatePaymentAnswerInput struct {
	ExpectedVersion int    `json:"expected_version"`
	OfferID         string `json:"offer_id"`
	Answer          string `json:"answer"`
}

// Validate checks the input.
func (in LatePaymentAnswerInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	if !catalog.ValidUUID(in.OfferID) {
		return invalid("offer_id", "must be the offer shown on the order page")
	}
	if in.Answer != "accept" && in.Answer != "decline" {
		return invalid("answer", "must be accept or decline")
	}
	return nil
}

// ErrOfferLapsed means the offered time can no longer take the order: the
// offer lapsed and nothing else changed (409 SLOT_UNAVAILABLE). It wraps a
// slot_unavailable conflict.
var ErrOfferLapsed = &catalog.ConflictError{Code: "slot_unavailable"}

// AnswerLatePayment records the customer's answer to offer in.OfferID of
// order id (its owner, or a guest capability with "cancel") and enqueues its
// protected commit (K11): 202, poll the order. When the offered time can no
// longer take the order the offer lapses and the answer is ErrOfferLapsed
// (also on a replay).
func (s *Store) AnswerLatePayment(ctx context.Context, id, subject string, digest []byte, key string,
	in LatePaymentAnswerInput) (catalog.Result, error) {
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
	var outcome string
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed, outcome FROM business.order_late_payment_answer(
		$1::uuid, $2, $3::uuid, $4, $5, $6, $7::uuid, $8, $9, $10)`,
		owner, digest, id, key, catalog.BodyHash(CmdLatePaymentAnswer, id, in), in.ExpectedVersion, in.OfferID, in.Answer,
		s.Env, s.now().UTC()).Scan(&r.ID, &r.Replayed, &outcome)
	if err != nil {
		return catalog.Result{}, appointmentError("late payment answer", err)
	}
	if outcome == "lapsed" {
		return r, ErrOfferLapsed
	}
	return r, nil
}

// withAcceptable adds late_payment.offer.acceptable to an order
// representation: whether the open offer can still be accepted at the
// store's clock (state offered and now before its accept_until). Declining
// has no such limit. Representations without an offer are returned as they
// are.
func (s *Store) withAcceptable(raw []byte) []byte {
	if !bytes.Contains(raw, []byte(`"accept_until"`)) {
		return raw
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return raw
	}
	var block map[string]json.RawMessage
	if json.Unmarshal(top["late_payment"], &block) != nil || block == nil {
		return raw
	}
	var offer map[string]json.RawMessage
	if json.Unmarshal(block["offer"], &offer) != nil || offer == nil {
		return raw
	}
	var state string
	var until time.Time
	if json.Unmarshal(block["state"], &state) != nil || json.Unmarshal(offer["accept_until"], &until) != nil {
		return raw
	}
	offer["acceptable"], _ = json.Marshal(state == "offered" && s.now().Before(until))
	var err error
	if block["offer"], err = json.Marshal(offer); err != nil {
		return raw
	}
	if top["late_payment"], err = json.Marshal(block); err != nil {
		return raw
	}
	out, err := json.Marshal(top)
	if err != nil {
		return raw
	}
	return out
}
