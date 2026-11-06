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

// A failed GK delivery (GK-017 r02b state 19, Creator decisions Q17, Q18,
// Q10; migration 0037).
const (
	CmdDeliveryFailed        = "order.delivery_failed"
	CmdDeliveryFailureChoice = "order.delivery_failure_choice"
)

// Causes of a failed GK delivery: GK's side (the customer then chooses
// redelivery or collection), or nobody available to receive it (Q18: the
// chef's no-show with the delivery payment kept).
const (
	CauseOurSide         = "our_side"
	CauseNobodyAvailable = "nobody_available"
)

// Choices after an our-side failure: deliver again (the chef agrees the new
// time) or collect today with the delivery refunded in full.
const (
	FailureRedelivery = "redelivery"
	FailureCollection = "collection"
)

// failureChoices is the block's choices list while the choice is open.
var failureChoices = []string{FailureRedelivery, FailureCollection}

// DeliveryFailedInput is the chef's record that the order's dispatched GK
// delivery failed, at the version they saw. The reason is staff-only (the
// chef's view; never audited or shown to the customer).
type DeliveryFailedInput struct {
	ExpectedVersion int     `json:"expected_version"`
	Cause           string  `json:"cause"`
	Reason          *string `json:"reason,omitempty"`
}

// Validate checks the input.
func (in DeliveryFailedInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	switch in.Cause {
	case CauseOurSide, CauseNobodyAvailable:
	default:
		return invalid("cause", "must be our_side or nobody_available")
	}
	if in.Reason != nil {
		return oneLine("reason", *in.Reason, 500)
	}
	return nil
}

// DeliveryFailed answers a recorded failure: for our_side the failure (ID),
// for nobody_available the no-show command to poll the order for (K11).
// Cause is the recorded failure's (also on a replay).
type DeliveryFailed struct {
	catalog.Result
	Cause string
}

// DeliveryFailureChoiceInput is the customer's choice after an our-side
// delivery failure at the version they saw: collection names one of the
// opening times shown on the order page; redelivery names none.
type DeliveryFailureChoiceInput struct {
	ExpectedVersion int    `json:"expected_version"`
	Choice          string `json:"choice"`
	TargetSlotID    string `json:"target_slot_id,omitempty"`
}

// Validate checks the input.
func (in DeliveryFailureChoiceInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	switch in.Choice {
	case FailureCollection:
		if !catalog.ValidUUID(in.TargetSlotID) {
			return invalid("target_slot_id", "must be one of the opening times shown on the order page")
		}
	case FailureRedelivery:
		if in.TargetSlotID != "" {
			return invalid("target_slot_id", "is only for collection")
		}
	default:
		return invalid("choice", "must be redelivery or collection")
	}
	return nil
}

// RecordFailedDelivery records the chef's failed delivery on order id
// (versioned, idempotent). our_side is a plain command (the result is the
// failure); nobody_available is the chef's no-show K11 command (202, poll
// the order).
func (s *Store) RecordFailedDelivery(ctx context.Context, subject, key, id string, in DeliveryFailedInput) (DeliveryFailed, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) {
		return DeliveryFailed{}, catalog.ErrNotFound
	}
	if in.Cause == CauseNobodyAvailable && !envPattern.MatchString(s.Env) {
		return DeliveryFailed{}, errors.New("checkout: the recovery journal environment is not configured")
	}
	var (
		d     DeliveryFailed
		cause *string
	)
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed, cause FROM business.order_delivery_failed(
		$1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9)`,
		subject, id, key, catalog.BodyHash(CmdDeliveryFailed, id, in), in.ExpectedVersion, in.Cause, in.Reason, s.Env,
		s.now().UTC()).Scan(&d.ID, &d.Replayed, &cause)
	if err != nil {
		return DeliveryFailed{}, appointmentError("delivery failed", err)
	}
	d.Cause = in.Cause
	if cause != nil {
		d.Cause = *cause
	}
	return d, nil
}

// ChooseAfterDeliveryFailure records the customer's choice after an our-side
// delivery failure on order id (its owner, or a guest capability with
// "move_pickup") and enqueues its protected commit (K11): 202, poll the
// order.
func (s *Store) ChooseAfterDeliveryFailure(ctx context.Context, id, subject string, digest []byte, key string,
	in DeliveryFailureChoiceInput) (catalog.Result, error) {
	id = strings.ToLower(id)
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
	var slot *string
	if in.TargetSlotID != "" {
		slot = &in.TargetSlotID
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.order_delivery_failure_choice_request(
		$1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8::uuid, $9, $10)`,
		owner, digest, id, key, catalog.BodyHash(CmdDeliveryFailureChoice, id, in), in.ExpectedVersion, in.Choice, slot,
		s.Env, s.now().UTC()).Scan(&r.ID, &r.Replayed)
	return r, appointmentError("delivery failure choice", err)
}

// withDeliveryFailureClock applies the store's clock to an order
// representation's delivery_failure block: choices are listed only while
// the choice is open, and times only while open and starting after now (the
// opening times still ahead today, Q10). Representations without the block,
// or a block without these keys (fulfilment's), keep what they have.
func (s *Store) withDeliveryFailureClock(raw []byte) []byte {
	if !bytes.Contains(raw, []byte(`"delivery_failure"`)) {
		return raw
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return raw
	}
	var block map[string]json.RawMessage
	if json.Unmarshal(top["delivery_failure"], &block) != nil || block == nil {
		return raw
	}
	var open bool
	if json.Unmarshal(block["open"], &open) != nil {
		return raw
	}
	if _, ok := block["choices"]; ok {
		choices := []string{}
		if open {
			choices = failureChoices
		}
		block["choices"], _ = json.Marshal(choices)
	}
	if rawTimes, ok := block["times"]; ok {
		var times []map[string]json.RawMessage
		if json.Unmarshal(rawTimes, &times) != nil {
			return raw
		}
		now := s.now()
		kept := []map[string]json.RawMessage{}
		for _, t := range times {
			var starts time.Time
			if open && json.Unmarshal(t["starts_at"], &starts) == nil && starts.After(now) {
				kept = append(kept, t)
			}
		}
		block["times"], _ = json.Marshal(kept)
	}
	var err error
	if top["delivery_failure"], err = json.Marshal(block); err != nil {
		return raw
	}
	out, err := json.Marshal(top)
	if err != nil {
		return raw
	}
	return out
}
