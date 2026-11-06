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

// The Q5 choice when GK can't deliver (GK-017 r02b states 14 and 15,
// Creator decisions Q5, Q8, Q9, Q10; migration 0036).
const (
	CmdDeliveryNotPossible = "order.delivery_not_possible"
	CmdDeliveryChoice      = "order.delivery_choice"
)

// Q5 choices: collect the cake, send the customer's own courier (both at an
// opening time of the same day), or a full refund of everything paid.
const (
	ChoiceCollection = "collection"
	ChoiceCourier    = "customer_courier"
	ChoiceFullRefund = "full_refund"
)

// deliveryChoices is the block's choices list while the choice is open.
var deliveryChoices = []string{ChoiceCollection, ChoiceCourier, ChoiceFullRefund}

// DeliveryNotPossibleInput is the chef's decision that GK can't deliver the
// order at the version they saw. The reason is the chef's only (never
// audited or shown to the customer).
type DeliveryNotPossibleInput struct {
	ExpectedVersion int     `json:"expected_version"`
	Reason          *string `json:"reason,omitempty"`
}

// Validate checks the input.
func (in DeliveryNotPossibleInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	if in.Reason != nil {
		return oneLine("reason", *in.Reason, 500)
	}
	return nil
}

// DeliveryChoiceInput is the customer's Q5 choice at the version they saw:
// a time choice names one of the day's opening times shown on the order
// page; a full refund names none.
type DeliveryChoiceInput struct {
	ExpectedVersion int    `json:"expected_version"`
	Choice          string `json:"choice"`
	TargetSlotID    string `json:"target_slot_id,omitempty"`
}

// Validate checks the input.
func (in DeliveryChoiceInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	switch in.Choice {
	case ChoiceCollection, ChoiceCourier:
		if !catalog.ValidUUID(in.TargetSlotID) {
			return invalid("target_slot_id", "must be one of the opening times shown on the order page")
		}
	case ChoiceFullRefund:
		if in.TargetSlotID != "" {
			return invalid("target_slot_id", "is only for collection or customer_courier")
		}
	default:
		return invalid("choice", "must be collection, customer_courier or full_refund")
	}
	return nil
}

// RecordDeliveryNotPossible records the chef's decision that GK can't
// deliver order id (versioned, idempotent; a second decision answers the
// first as a replay). It refunds nothing: the customer's choice decides.
func (s *Store) RecordDeliveryNotPossible(ctx context.Context, subject, key, id string, in DeliveryNotPossibleInput) (catalog.Result, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.order_delivery_not_possible(
		$1::uuid, $2::uuid, $3, $4, $5, $6, $7)`,
		subject, id, key, catalog.BodyHash(CmdDeliveryNotPossible, id, in), in.ExpectedVersion, in.Reason,
		s.now().UTC()).Scan(&r.ID, &r.Replayed)
	return r, appointmentError("delivery not possible", err)
}

// ChooseDelivery records the customer's Q5 choice on order id (its owner,
// or a guest capability with "cancel" for a full refund and "move_pickup"
// for a time) and enqueues its protected commit (K11): 202, poll the order.
func (s *Store) ChooseDelivery(ctx context.Context, id, subject string, digest []byte, key string,
	in DeliveryChoiceInput) (catalog.Result, error) {
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
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.order_delivery_choice_request(
		$1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8::uuid, $9, $10)`,
		owner, digest, id, key, catalog.BodyHash(CmdDeliveryChoice, id, in), in.ExpectedVersion, in.Choice, slot,
		s.Env, s.now().UTC()).Scan(&r.ID, &r.Replayed)
	return r, appointmentError("delivery choice", err)
}

// withDeliveryChoiceClock applies the store's clock to an order
// representation's delivery_choice block (the database used its own): with
// opens_at (an unpaid quote's pay-by, no chef decision) the choice is open,
// reason pay_by_passed, from that instant (equality has passed) unless a
// protected change is pending; choices and times are empty unless open, and
// times only list starts after now. Representations without the block, or
// a block without these keys (fulfilment's), keep what they have.
func (s *Store) withDeliveryChoiceClock(raw []byte) []byte {
	if !bytes.Contains(raw, []byte(`"delivery_choice"`)) {
		return raw
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return raw
	}
	var block map[string]json.RawMessage
	if json.Unmarshal(top["delivery_choice"], &block) != nil || block == nil {
		return raw
	}
	now := s.now()
	var open, pending bool
	if json.Unmarshal(block["open"], &open) != nil || json.Unmarshal(block["pending"], &pending) != nil {
		return raw
	}
	var opensAt *time.Time
	if json.Unmarshal(block["opens_at"], &opensAt) != nil {
		return raw
	}
	if opensAt != nil {
		var reason *string
		if !now.Before(*opensAt) {
			r := "pay_by_passed"
			reason = &r
		}
		open = reason != nil && !pending
		block["open"], _ = json.Marshal(open)
		block["reason"], _ = json.Marshal(reason)
	}
	if _, ok := block["choices"]; ok {
		choices := []string{}
		if open {
			choices = deliveryChoices
		}
		block["choices"], _ = json.Marshal(choices)
	}
	if rawTimes, ok := block["times"]; ok {
		var times []map[string]json.RawMessage
		if json.Unmarshal(rawTimes, &times) != nil {
			return raw
		}
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
	if top["delivery_choice"], err = json.Marshal(block); err != nil {
		return raw
	}
	out, err := json.Marshal(top)
	if err != nil {
		return raw
	}
	return out
}
