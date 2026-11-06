package checkout

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
)

// Customer actions on the order view (GET /api/v1/orders/{id}): what this
// caller may do now (migration 0038). Each name is a customer route:
//
//	pay                      POST /orders/{id}/payment
//	free_confirmation        POST /orders/{id}/free-confirmation
//	cancel                   POST /orders/{id}/cancellation-preview, then /cancel
//	move_pickup              POST /orders/{id}/pickup
//	answer_late_payment      POST /orders/{id}/late-payment/answer
//	pay_delivery             POST /orders/{id}/delivery-payment
//	delivery_choice          POST /orders/{id}/delivery-choice
//	delivery_failure_choice  POST /orders/{id}/delivery-failure-choice
//
// It is a read-model hint (the routes still check everything): the owner by
// the order's state, a guest by the state and its capability's grants (pay:
// pay, free_confirmation, pay_delivery; cancel: cancel, answer_late_payment,
// a full_refund delivery choice; move_pickup: move_pickup, a time delivery
// choice, delivery_failure_choice).
const (
	CustomerActionPay                   = "pay"
	CustomerActionFreeConfirmation      = "free_confirmation"
	CustomerActionCancel                = "cancel"
	CustomerActionMovePickup            = "move_pickup"
	CustomerActionAnswerLatePayment     = "answer_late_payment"
	CustomerActionPayDelivery           = "pay_delivery"
	CustomerActionDeliveryChoice        = "delivery_choice"
	CustomerActionDeliveryFailureChoice = "delivery_failure_choice"
)

// errMissingBlocks: an account summary row without its order blocks (the
// database function of another release); never answered without them.
var errMissingBlocks = errors.New("checkout: account summary without order blocks")

// Guest capability grants (order_guest_capability.allowed_commands).
const (
	grantPay        = "pay"
	grantCancel     = "cancel"
	grantMovePickup = "move_pickup"
)

// actionWindow is a command's time limits for the store's clock: allowed
// while now is before Before (equality has passed) and not after NotAfter
// (equality allowed). A nil window is not allowed at all.
type actionWindow struct {
	Before   *time.Time `json:"before"`
	NotAfter *time.Time `json:"not_after"`
}

func (w *actionWindow) open(now time.Time) bool {
	return w != nil && (w.Before == nil || now.Before(*w.Before)) && (w.NotAfter == nil || !now.After(*w.NotAfter))
}

// actionFacts is business.order_customer_actions: the caller's grants and
// the state facts of the commands the order view does not show.
type actionFacts struct {
	Grants           []string      `json:"grants"`
	Pay              *actionWindow `json:"pay"`
	FreeConfirmation *actionWindow `json:"free_confirmation"`
	Cancel           bool          `json:"cancel"`
	MovePickup       *actionWindow `json:"move_pickup"`
}

// orderBlocks are the order view's blocks read for actions and account
// summaries, after the store's clock was applied to them.
type orderBlocks struct {
	LatePayment *struct {
		State string `json:"state"`
		Offer *struct {
			Acceptable *bool `json:"acceptable"`
		} `json:"offer"`
	} `json:"late_payment"`
	Delivery *struct {
		State   string `json:"state"`
		Payable *bool  `json:"payable"`
	} `json:"delivery"`
	DeliveryChoice *struct {
		Open    bool     `json:"open"`
		Choices []string `json:"choices"`
	} `json:"delivery_choice"`
	DeliveryFailure *struct {
		Open    bool     `json:"open"`
		Choices []string `json:"choices"`
	} `json:"delivery_failure"`
}

// lateOffered: an offer is open to answer (declining has no time limit).
func (b orderBlocks) lateOffered() bool {
	return b.LatePayment != nil && b.LatePayment.State == "offered" && b.LatePayment.Offer != nil
}

// lateAcceptable: the open offer can still be accepted.
func (b orderBlocks) lateAcceptable() bool {
	return b.lateOffered() && b.LatePayment.Offer.Acceptable != nil && *b.LatePayment.Offer.Acceptable
}

func (b orderBlocks) deliveryPayable() bool {
	return b.Delivery != nil && b.Delivery.Payable != nil && *b.Delivery.Payable
}

// deliveryChoices are the open Q5 choices a caller with grants may make.
func (b orderBlocks) deliveryChoices(grants map[string]bool) []string {
	out := []string{}
	if b.DeliveryChoice == nil || !b.DeliveryChoice.Open {
		return out
	}
	for _, c := range b.DeliveryChoice.Choices {
		need := grantMovePickup
		if c == ChoiceFullRefund {
			need = grantCancel
		}
		if grants[need] {
			out = append(out, c)
		}
	}
	return out
}

func (b orderBlocks) failureChoiceOpen() bool {
	return b.DeliveryFailure != nil && b.DeliveryFailure.Open && len(b.DeliveryFailure.Choices) > 0
}

// atClock applies the store's clock to an order representation's blocks
// (late payment, delivery, Q5 choice, failed delivery).
func (s *Store) atClock(raw []byte) []byte {
	return s.withDeliveryFailureClock(s.withDeliveryChoiceClock(s.withDeliveryClock(s.withAcceptable(raw))))
}

// paymentsConfigured mirrors the payment routes' own check (503 otherwise).
func (s *Store) paymentsConfigured() bool {
	return envPattern.MatchString(s.Env) && payments.ValidAccount(s.StripeAccount) && s.UIOrigin != ""
}

// withActions adds actions (and delivery_choice.allowed_choices, the subset
// of its choices this caller may make) to an order representation already
// at the store's clock. Without a Q5 block the representation keeps its
// bytes and actions is appended; anything that is not a readable JSON
// object (order_get never returns one) is an error, never an answer
// without actions.
func (s *Store) withActions(raw []byte, factsRaw []byte) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	var facts actionFacts
	if err := json.Unmarshal(factsRaw, &facts); err != nil {
		return nil, err
	}
	var blocks orderBlocks
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	grants := map[string]bool{}
	for _, g := range facts.Grants {
		grants[g] = true
	}
	now := s.now()
	pays := s.paymentsConfigured()
	actions := []string{}
	add := func(name string, ok bool) {
		if ok {
			actions = append(actions, name)
		}
	}
	allowed := blocks.deliveryChoices(grants)
	add(CustomerActionPay, grants[grantPay] && pays && facts.Pay.open(now))
	add(CustomerActionFreeConfirmation, grants[grantPay] && facts.FreeConfirmation.open(now))
	add(CustomerActionCancel, grants[grantCancel] && facts.Cancel)
	add(CustomerActionMovePickup, grants[grantMovePickup] && facts.MovePickup.open(now))
	add(CustomerActionAnswerLatePayment, grants[grantCancel] && blocks.lateOffered())
	add(CustomerActionPayDelivery, grants[grantPay] && pays && blocks.deliveryPayable())
	add(CustomerActionDeliveryChoice, len(allowed) > 0)
	add(CustomerActionDeliveryFailureChoice, grants[grantMovePickup] && blocks.failureChoiceOpen())
	list, err := json.Marshal(actions)
	if err != nil {
		return nil, err
	}
	if _, ok := top["actions"]; ok {
		return nil, errors.New("checkout: order representation already has actions")
	}
	if changed, err := setAllowedChoices(top, allowed); err != nil {
		return nil, err
	} else if changed {
		top["actions"] = list
		return json.Marshal(top)
	}
	// Otherwise the representation keeps its bytes: actions are appended as
	// its last key (it is a JSON object with at least one key).
	trimmed := bytes.TrimRight(raw, " \t\r\n")
	if len(trimmed) < 2 || trimmed[len(trimmed)-1] != '}' || len(top) == 0 {
		return nil, errors.New("checkout: order representation is not a JSON object")
	}
	out := make([]byte, 0, len(trimmed)+len(list)+16)
	out = append(out, trimmed[:len(trimmed)-1]...)
	out = append(out, `, "actions" : `...)
	out = append(out, list...)
	return append(out, '}'), nil
}

// setAllowedChoices sets delivery_choice.allowed_choices on a customer
// block (one that lists choices) and reports whether it did; other blocks
// are left as they are.
func setAllowedChoices(top map[string]json.RawMessage, allowed []string) (bool, error) {
	var block map[string]json.RawMessage
	if json.Unmarshal(top["delivery_choice"], &block) != nil || block == nil {
		return false, nil
	}
	if _, ok := block["choices"]; !ok {
		return false, nil
	}
	var err error
	if block["allowed_choices"], err = json.Marshal(allowed); err != nil {
		return false, err
	}
	top["delivery_choice"], err = json.Marshal(block)
	return err == nil, err
}

// WithoutActions empties an order representation's actions and
// delivery_choice.allowed_choices: the API serves no customer command
// (CHECKOUT_ENABLED is off), so none can be offered. Other representations
// are returned as they are.
func WithoutActions(raw json.RawMessage) json.RawMessage {
	if !bytes.Contains(raw, []byte(`"actions"`)) {
		return raw
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return raw
	}
	top["actions"] = json.RawMessage(`[]`)
	if _, err := setAllowedChoices(top, []string{}); err != nil {
		return raw
	}
	out, err := json.Marshal(top)
	if err != nil {
		return raw
	}
	return out
}

// summaryAttention is what the customer must answer, of an account summary's
// blocks at the store's clock (the owner may use every command): an
// acceptable late-payment offer, a payable delivery quote, an open Q5 choice,
// an open choice after a failed delivery. Names are the order view's actions.
func summaryAttention(b orderBlocks) []string {
	out := []string{}
	if b.lateAcceptable() {
		out = append(out, CustomerActionAnswerLatePayment)
	}
	if b.deliveryPayable() {
		out = append(out, CustomerActionPayDelivery)
	}
	if b.DeliveryChoice != nil && b.DeliveryChoice.Open && len(b.DeliveryChoice.Choices) > 0 {
		out = append(out, CustomerActionDeliveryChoice)
	}
	if b.failureChoiceOpen() {
		out = append(out, CustomerActionDeliveryFailureChoice)
	}
	return out
}

// withSummaryAttention replaces an account summary row's internal
// order_blocks (always removed) with delivery_state (the GK delivery's
// state at the store's clock, null for other orders), needs_answer and
// attention.
func (s *Store) withSummaryAttention(row json.RawMessage) (json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(row, &top); err != nil {
		return nil, err
	}
	raw, ok := top["order_blocks"]
	delete(top, "order_blocks")
	if !ok {
		return nil, errMissingBlocks
	}
	var blocks orderBlocks
	if err := json.Unmarshal(s.atClock(raw), &blocks); err != nil {
		return nil, err
	}
	attention := summaryAttention(blocks)
	var state *string
	if blocks.Delivery != nil {
		state = &blocks.Delivery.State
	}
	var err error
	if top["delivery_state"], err = json.Marshal(state); err != nil {
		return nil, err
	}
	if top["needs_answer"], err = json.Marshal(len(attention) > 0); err != nil {
		return nil, err
	}
	if top["attention"], err = json.Marshal(attention); err != nil {
		return nil, err
	}
	return json.Marshal(top)
}
