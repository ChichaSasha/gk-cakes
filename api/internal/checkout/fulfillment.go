package checkout

import (
	"context"
	"errors"
	"strings"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
)

// Fulfilment transitions (K05, K06, K00; migration 0032). ready, dispatch
// and delivery_exception are plain audited transitions (chef or fulfilment);
// handover (chef or fulfilment) and no_show (chef only: its 50% refund is a
// money decision) are K11 order commands.
const (
	ActionReady             = "ready"
	ActionDispatch          = "dispatch"
	ActionDeliveryException = "delivery_exception"
	ActionHandover          = "handover"
	ActionNoShow            = "no_show"
	CmdTransition           = "order.transition"
)

// TransitionInput is one fulfilment step at the version the staff member
// saw. Reason: optional courier reference for dispatch, required for
// delivery_exception, refused otherwise.
type TransitionInput struct {
	Action          string  `json:"action"`
	ExpectedVersion int     `json:"expected_version"`
	Reason          *string `json:"reason,omitempty"`
}

// Validate checks the input.
func (in TransitionInput) Validate() error {
	switch in.Action {
	case ActionReady, ActionDispatch, ActionDeliveryException, ActionHandover, ActionNoShow:
	default:
		return invalid("action", "must be ready, dispatch, delivery_exception, handover or no_show")
	}
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	switch {
	case in.Action == ActionDeliveryException && in.Reason == nil:
		return invalid("reason", "is required: what went wrong")
	case in.Reason != nil && in.Action != ActionDispatch && in.Action != ActionDeliveryException:
		return invalid("reason", "is only for dispatch (a courier reference) and delivery_exception")
	case in.Reason != nil:
		return singleLine("reason", *in.Reason, 500)
	}
	return nil
}

// Protected reports whether the action is a K11 order command (202).
func (in TransitionInput) Protected() bool {
	return in.Action == ActionHandover || in.Action == ActionNoShow
}

// Transitioned is the answer to a fulfilment step: for a plain transition
// the order's lifecycle and fulfilment state now (Replayed also when another
// request already made the step); for a K11 command the command (ID) to poll
// the order for.
type Transitioned struct {
	catalog.Result
	Lifecycle        string
	FulfillmentState string
}

// Transition records a fulfilment step on order id.
func (s *Store) Transition(ctx context.Context, subject, key, id string, in TransitionInput) (Transitioned, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) {
		return Transitioned{}, catalog.ErrNotFound
	}
	body := catalog.BodyHash(CmdTransition, id, in)
	var t Transitioned
	if in.Protected() {
		if !envPattern.MatchString(s.Env) {
			return Transitioned{}, errors.New("checkout: the recovery journal environment is not configured")
		}
		err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.order_handover_request(
			$1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8)`,
			subject, id, key, body, in.ExpectedVersion, in.Action, s.Env, s.now().UTC()).Scan(&t.ID, &t.Replayed)
		return t, appointmentError("handover", err)
	}
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed, lifecycle, fulfillment_state
		FROM business.order_fulfillment_transition($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8)`,
		subject, id, key, body, in.ExpectedVersion, in.Action, in.Reason, s.now().UTC()).
		Scan(&t.ID, &t.Replayed, &t.Lifecycle, &t.FulfillmentState)
	return t, appointmentError("fulfillment transition", err)
}
