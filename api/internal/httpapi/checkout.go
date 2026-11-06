package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
)

// Checkout serves discount codes and quotes (checkout.Store).
type Checkout interface {
	CreateQuote(ctx context.Context, subject string, digest []byte, key string, in checkout.QuoteInput) (catalog.Result, error)
	GetQuote(ctx context.Context, id, subject string, digest []byte) (checkout.Quote, error)
	Codes(ctx context.Context, subject string) ([]checkout.Code, error)
	CodeByID(ctx context.Context, subject, id string) (checkout.Code, error)
	CreateCode(ctx context.Context, subject, key string, in checkout.CodeInput) (catalog.Result, error)
	ReviseCode(ctx context.Context, subject, key, codeID string, in checkout.CodeRevision) (catalog.Result, error)
	ArchiveCode(ctx context.Context, subject, key, codeID string, in catalog.VersionInput) (catalog.Result, error)
	Checkout(ctx context.Context, subject string, digest []byte, key string, in checkout.CheckoutInput) (catalog.Result, error)
	GetOrder(ctx context.Context, id, subject string, digest []byte) (checkout.Order, error)
	FreeConfirm(ctx context.Context, id, subject string, digest []byte, key string, in checkout.FreeConfirmInput) (catalog.Result, error)
	StartPayment(ctx context.Context, id, subject string, digest []byte, key string, in checkout.PaymentInput) (catalog.Result, error)
	PreviewCancellation(ctx context.Context, id, subject string, digest []byte) (checkout.CancellationPreview, error)
	Cancel(ctx context.Context, id, subject string, digest []byte, key string, in checkout.CancelInput) (catalog.Result, error)
	ChefCancel(ctx context.Context, subject, key, id string, in checkout.ChefCancelInput) (catalog.Result, error)
	StartProduction(ctx context.Context, subject, key, id string, in checkout.ProductionInput) (checkout.Started, error)
	MovePickup(ctx context.Context, id, subject string, digest []byte, key string, in checkout.PickupMoveInput) (checkout.Moved, error)
	ChangeAppointment(ctx context.Context, subject, key, id string, in checkout.AppointmentInput) (checkout.Moved, error)
	Transition(ctx context.Context, subject, key, id string, in checkout.TransitionInput) (checkout.Transitioned, error)
	LatePayment(ctx context.Context, subject, key, id string, in checkout.LatePaymentInput) (catalog.Result, error)
	OfferLatePayment(ctx context.Context, subject, key, id string, in checkout.LatePaymentOfferInput) (catalog.Result, error)
	AnswerLatePayment(ctx context.Context, id, subject string, digest []byte, key string, in checkout.LatePaymentAnswerInput) (catalog.Result, error)
	RecordRefund(ctx context.Context, subject, key, id string, in checkout.RefundInput) (catalog.Result, error)
	ResolveObservation(ctx context.Context, subject, key, orderID, observationID string, in checkout.ResolveObservationInput) (catalog.Result, error)
	DecideOperation(ctx context.Context, subject, key, orderID, operationID string, in checkout.DecideOperationInput) (catalog.Result, error)
	ClearRefundMismatch(ctx context.Context, subject, key, orderID, operationID string, in checkout.ClearMismatchInput) (catalog.Result, error)
	ClearPaymentMismatch(ctx context.Context, subject, key, orderID, collectionID string, in checkout.NoteInput) (catalog.Result, error)
	ReserveRemainder(ctx context.Context, subject, key, orderID, operationID string, in checkout.RemainderInput) (catalog.Result, error)
	AttentionList(ctx context.Context, subject string) (checkout.Attention, error)
	StaffOrders(ctx context.Context, subject string, f checkout.StaffOrderFilter) (checkout.StaffOrderPage, error)
	StaffOrder(ctx context.Context, subject, id string) (json.RawMessage, error)
	PreviewManualPayout(ctx context.Context, subject, orderID string, in checkout.ManualPayoutPreviewInput) (json.RawMessage, error)
	RequestManualPayout(ctx context.Context, subject, key, orderID string, in checkout.ManualPayoutInput) (catalog.Result, error)
	RecordManualPayoutOutcome(ctx context.Context, subject, key, orderID, operationID string, in checkout.ManualPayoutOutcomeInput) (catalog.Result, error)
	ManualPayouts(ctx context.Context, subject, orderID string) (json.RawMessage, error)
	PayDeliveryRefund(ctx context.Context, subject, key, id string, in checkout.DeliveryRefundInput) (catalog.Result, error)
	QuoteDelivery(ctx context.Context, subject, key, id string, in checkout.DeliveryQuoteInput) (catalog.Result, error)
	WaiveDelivery(ctx context.Context, subject, key, id string, in checkout.DeliveryWaiverInput) (catalog.Result, error)
	StartDeliveryPayment(ctx context.Context, id, subject string, digest []byte, key string, in checkout.DeliveryPaymentInput) (catalog.Result, error)
	CurrentPolicy(ctx context.Context) (checkout.Policy, error)
	RecordPolicy(ctx context.Context, subject, key string, in checkout.PolicyInput) (catalog.Result, error)
}

// OrderCapabilityHeader carries a guest's order secret (the proof that
// created the quote) on order reads; never logged.
const OrderCapabilityHeader = "X-Order-Capability"

// QuoteProofHeader carries a guest's quote proof on reads; never logged.
const QuoteProofHeader = "X-Quote-Proof"

// optionalCustomer authenticates a bearer token when one is sent (a
// signed-in customer); without one the request is a guest's. A sent but
// refused token is refused, never downgraded to a guest.
func optionalCustomer(id Identity, w http.ResponseWriter, r *http.Request, logger *slog.Logger) (identity.Actor, bool, bool) {
	if r.Header.Get("Authorization") == "" {
		return identity.Actor{}, false, true
	}
	if id == nil {
		// Sign-in is not configured: a sent token cannot be honoured, and a
		// request that meant to be signed in is never downgraded to a guest.
		identityError(w, r, logger, identity.ErrUnauthenticated)
		return identity.Actor{}, false, false
	}
	token, ok := bearer(r)
	if !ok {
		identityError(w, r, logger, identity.ErrUnauthenticated)
		return identity.Actor{}, false, false
	}
	a, err := id.Authenticate(r.Context(), token)
	if err != nil {
		identityError(w, r, logger, err)
		return identity.Actor{}, false, false
	}
	return a, true, true
}

// checkoutRoutes registers the storefront quote routes (guests or signed-in
// customers) and, with identity, the chef's discount code routes.
func checkoutRoutes(mux *http.ServeMux, co Checkout, id Identity, checkoutEnabled bool, logger *slog.Logger) {
	mux.HandleFunc("POST /api/v1/checkout/quotes", func(w http.ResponseWriter, r *http.Request) {
		a, signedIn, ok := optionalCustomer(id, w, r, logger)
		if !ok {
			return
		}
		var in checkout.QuoteInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		var digest []byte
		switch {
		case signedIn && in.GuestProof != "":
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "guest_proof is for guests; you are signed in", false)
			return
		case !signedIn:
			d, err := checkout.GuestProofDigest(in.GuestProof)
			if err != nil {
				catalogError(w, r, logger, err)
				return
			}
			digest = d
		}
		res, err := co.CreateQuote(r.Context(), a.Subject, digest, key, in)
		respondCommand(w, r, logger, http.StatusCreated, res, err, func() (any, error) {
			return co.GetQuote(r.Context(), res.ID, a.Subject, digest)
		})
	})
	mux.HandleFunc("GET /api/v1/checkout/quotes/{id}", func(w http.ResponseWriter, r *http.Request) {
		a, signedIn, ok := optionalCustomer(id, w, r, logger)
		if !ok {
			return
		}
		var digest []byte
		if !signedIn {
			d, err := checkout.GuestProofDigest(r.Header.Get(QuoteProofHeader))
			if err != nil {
				// No or malformed proof: the same answer as a wrong one.
				writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Not found", false)
				return
			}
			digest = d
		}
		q, err := co.GetQuote(r.Context(), r.PathValue("id"), a.Subject, digest)
		respond(w, r, logger, http.StatusOK, q, err)
	})
	mux.HandleFunc("GET /api/v1/policies/current", func(w http.ResponseWriter, r *http.Request) {
		p, err := co.CurrentPolicy(r.Context())
		respond(w, r, logger, http.StatusOK, p, err)
	})
	postCheckout := func(w http.ResponseWriter, r *http.Request) {
		a, signedIn, ok := optionalCustomer(id, w, r, logger)
		if !ok {
			return
		}
		var in checkout.CheckoutInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		var digest []byte
		switch {
		case signedIn && in.GuestProof != "":
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "guest_proof is for guests; you are signed in", false)
			return
		case !signedIn:
			d, err := checkout.GuestProofDigest(in.GuestProof)
			if err != nil {
				catalogError(w, r, logger, err)
				return
			}
			digest = d
		}
		res, err := co.Checkout(r.Context(), a.Subject, digest, key, in)
		// K04: 202 "payment pending", never an accepted order.
		respondCommand(w, r, logger, http.StatusAccepted, res, err, func() (any, error) {
			return checkout.Placed{OrderID: res.ID, State: "pending", PollAfterSeconds: checkout.PollAfterSeconds}, nil
		})
	}
	// K03 free confirmation: 202 "confirmation pending" (K11 protection runs
	// in the worker); poll the order.
	postFreeConfirm := func(w http.ResponseWriter, r *http.Request) {
		a, signedIn, ok := optionalCustomer(id, w, r, logger)
		if !ok {
			return
		}
		var digest []byte
		if !signedIn {
			d, err := checkout.GuestProofDigest(r.Header.Get(OrderCapabilityHeader))
			if err != nil {
				writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Not found", false)
				return
			}
			digest = d
		}
		var in checkout.FreeConfirmInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := co.FreeConfirm(r.Context(), r.PathValue("id"), a.Subject, digest, key, in)
		respondCommand(w, r, logger, http.StatusAccepted, res, err, func() (any, error) {
			return checkout.Confirming{OrderID: r.PathValue("id"), OperationID: res.ID, State: "confirmation_pending",
				PollAfterSeconds: checkout.PollAfterSeconds}, nil
		})
	}
	// K04 payment: 202 "payment pending" (the Checkout Session is created by
	// the worker under K11); poll the order for its checkout URL. Never a
	// paid or accepted order.
	postPayment := func(w http.ResponseWriter, r *http.Request) {
		a, signedIn, ok := optionalCustomer(id, w, r, logger)
		if !ok {
			return
		}
		var digest []byte
		if !signedIn {
			d, err := checkout.GuestProofDigest(r.Header.Get(OrderCapabilityHeader))
			if err != nil {
				writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Not found", false)
				return
			}
			digest = d
		}
		var in checkout.PaymentInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := co.StartPayment(r.Context(), r.PathValue("id"), a.Subject, digest, key, in)
		if errors.Is(err, checkout.ErrPaymentsUnavailable) {
			why := "payments are not configured"
			if errors.Is(err, checkout.ErrRecoveryFrozen) {
				why = "a restore freeze is on"
			}
			logger.Warn("payment request refused for now", "reason", why, "request_id", requestID(r.Context()))
			writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Payments are temporarily unavailable", true)
			return
		}
		respondCommand(w, r, logger, http.StatusAccepted, res, err, func() (any, error) {
			return checkout.Confirming{OrderID: r.PathValue("id"), OperationID: res.ID, State: "payment_pending",
				PollAfterSeconds: checkout.PollAfterSeconds}, nil
		})
	}
	// orderActor is the order's owner (bearer) or a guest with the order
	// capability; a missing or malformed capability is a 404 like a wrong one.
	orderActor := func(w http.ResponseWriter, r *http.Request) (identity.Actor, []byte, bool) {
		a, signedIn, ok := optionalCustomer(id, w, r, logger)
		if !ok {
			return identity.Actor{}, nil, false
		}
		if signedIn {
			return a, nil, true
		}
		d, err := checkout.GuestProofDigest(r.Header.Get(OrderCapabilityHeader))
		if err != nil {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Not found", false)
			return identity.Actor{}, nil, false
		}
		return a, d, true
	}
	// K05 cancellation preview: the server-computed amount the customer
	// confirms (owner, or a guest capability with "cancel").
	postCancellationPreview := func(w http.ResponseWriter, r *http.Request) {
		a, digest, ok := orderActor(w, r)
		if !ok {
			return
		}
		if !emptyBody(w, r) {
			return
		}
		p, err := co.PreviewCancellation(r.Context(), r.PathValue("id"), a.Subject, digest)
		respond(w, r, logger, http.StatusOK, p, err)
	}
	// K05 cancellation: 202 "cancellation pending" (K11 protection runs in
	// the worker); poll the order. Never "refunded" until the money is back.
	postCancel := func(w http.ResponseWriter, r *http.Request) {
		a, digest, ok := orderActor(w, r)
		if !ok {
			return
		}
		var in checkout.CancelInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := co.Cancel(r.Context(), r.PathValue("id"), a.Subject, digest, key, in)
		respondCommand(w, r, logger, http.StatusAccepted, res, err, func() (any, error) {
			return checkout.Confirming{OrderID: r.PathValue("id"), OperationID: res.ID, State: "cancellation_pending",
				PollAfterSeconds: checkout.PollAfterSeconds}, nil
		})
	}
	// K06 same-day pickup move (owner, or a guest capability with
	// move_pickup): 202 "move pending" while its K11 protection runs (the
	// order shows its current time until then); 200 "unchanged" when the
	// order already has that time.
	postPickup := func(w http.ResponseWriter, r *http.Request) {
		a, digest, ok := orderActor(w, r)
		if !ok {
			return
		}
		var in checkout.PickupMoveInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := co.MovePickup(r.Context(), r.PathValue("id"), a.Subject, digest, key, in)
		respondMove(w, r, logger, res, err, "move_pending")
	}
	// K04 Q2 / GK-017 r02b Q6: the customer's answer to the chef's offer on
	// a late-paid order (owner, or a guest capability with "cancel"): 202
	// while its K11 protection runs; 409 SLOT_UNAVAILABLE when the offered
	// time can no longer take the order (the offer lapsed).
	postLatePaymentAnswer := func(w http.ResponseWriter, r *http.Request) {
		a, digest, ok := orderActor(w, r)
		if !ok {
			return
		}
		var in checkout.LatePaymentAnswerInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := co.AnswerLatePayment(r.Context(), r.PathValue("id"), a.Subject, digest, key, in)
		if errors.Is(err, checkout.ErrOrdersFrozen) {
			writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Order changes are temporarily unavailable", true)
			return
		}
		respondCommand(w, r, logger, http.StatusAccepted, res, err, func() (any, error) {
			return checkout.Confirming{OrderID: r.PathValue("id"), OperationID: res.ID, State: "answer_pending",
				PollAfterSeconds: checkout.PollAfterSeconds}, nil
		})
	}
	// K03/K06 separate GK delivery payment (owner, or a guest capability with
	// "pay", as the cake payment): 202 while the worker creates the Checkout
	// Session; poll the order's delivery block for its link. Never paid here.
	postDeliveryPayment := func(w http.ResponseWriter, r *http.Request) {
		a, digest, ok := orderActor(w, r)
		if !ok {
			return
		}
		var in checkout.DeliveryPaymentInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := co.StartDeliveryPayment(r.Context(), r.PathValue("id"), a.Subject, digest, key, in)
		if errors.Is(err, checkout.ErrPaymentsUnavailable) {
			why := "payments are not configured"
			if errors.Is(err, checkout.ErrRecoveryFrozen) {
				why = "a restore freeze is on"
			}
			logger.Warn("delivery payment request refused for now", "reason", why, "request_id", requestID(r.Context()))
			writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Payments are temporarily unavailable", true)
			return
		}
		respondCommand(w, r, logger, http.StatusAccepted, res, err, func() (any, error) {
			return checkout.Confirming{OrderID: r.PathValue("id"), OperationID: res.ID, State: "delivery_payment_pending",
				PollAfterSeconds: checkout.PollAfterSeconds}, nil
		})
	}
	if checkoutEnabled {
		mux.HandleFunc("POST /api/v1/orders/{id}/delivery-payment", postDeliveryPayment)
		mux.HandleFunc("POST /api/v1/orders/{id}/late-payment/answer", postLatePaymentAnswer)
		mux.HandleFunc("POST /api/v1/orders/{id}/pickup", postPickup)
		mux.HandleFunc("POST /api/v1/checkouts", postCheckout)
		mux.HandleFunc("POST /api/v1/orders/{id}/free-confirmation", postFreeConfirm)
		mux.HandleFunc("POST /api/v1/orders/{id}/payment", postPayment)
		mux.HandleFunc("POST /api/v1/orders/{id}/cancellation-preview", postCancellationPreview)
		mux.HandleFunc("POST /api/v1/orders/{id}/cancel", postCancel)
	}
	mux.HandleFunc("GET /api/v1/orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		a, signedIn, ok := optionalCustomer(id, w, r, logger)
		if !ok {
			return
		}
		var digest []byte
		if !signedIn {
			d, err := checkout.GuestProofDigest(r.Header.Get(OrderCapabilityHeader))
			if err != nil {
				writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Not found", false)
				return
			}
			digest = d
		}
		o, err := co.GetOrder(r.Context(), r.PathValue("id"), a.Subject, digest)
		respond(w, r, logger, http.StatusOK, o, err)
	})
	if id == nil {
		return
	}
	readers := []string{identity.Chef, identity.TechnicalSupport}
	chef := []string{identity.Chef}
	// Staff order reads (K00): chef, technical support and fulfillment, each
	// view redacted by the database; served whether or not new checkouts
	// are enabled (existing orders stay readable).
	orderReaders := []string{identity.Chef, identity.TechnicalSupport, identity.Fulfillment}
	mux.HandleFunc("GET /api/v1/admin/orders", staff(id, logger, orderReaders, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		f, ok := staffOrderFilter(w, r)
		if !ok {
			return
		}
		page, err := co.StaffOrders(r.Context(), a.Subject, f)
		respond(w, r, logger, http.StatusOK, page, err)
	}))
	mux.HandleFunc("GET /api/v1/admin/orders/{id}", staff(id, logger, orderReaders, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		o, err := co.StaffOrder(r.Context(), a.Subject, r.PathValue("id"))
		respond(w, r, logger, http.StatusOK, o, err)
	}))
	if checkoutEnabled {
		// K05 chef cancellation (100%), the same K11 command path.
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/cancel", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
			var in checkout.ChefCancelInput
			key, ok := readCommand(w, r, &in)
			if !ok {
				return
			}
			res, err := co.ChefCancel(r.Context(), a.Subject, key, r.PathValue("id"), in)
			respondCommand(w, r, logger, http.StatusAccepted, res, err, func() (any, error) {
				return checkout.Confirming{OrderID: r.PathValue("id"), OperationID: res.ID, State: "cancellation_pending",
					PollAfterSeconds: checkout.PollAfterSeconds}, nil
			})
		}))
	}
	if checkoutEnabled {
		// K05 production start (chef or fulfillment, K00): one transition per
		// order, shared with the +48 h timer.
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/start-production", staff(id, logger, []string{identity.Chef, identity.Fulfillment},
			func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
				var in checkout.ProductionInput
				key, ok := readCommand(w, r, &in)
				if !ok {
					return
				}
				res, err := co.StartProduction(r.Context(), a.Subject, key, r.PathValue("id"), in)
				if errors.Is(err, checkout.ErrOrdersFrozen) {
					writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Order changes are temporarily unavailable", true)
					return
				}
				respondCommand(w, r, logger, http.StatusOK, res.Result, err, func() (any, error) {
					return map[string]string{"order_id": res.ID, "lifecycle": res.Lifecycle}, nil
				})
			}))
		// K06 chef date/time amendment with the customer's recorded
		// agreement; 202 while its K11 protection runs.
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/appointment", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
			var in checkout.AppointmentInput
			key, ok := readCommand(w, r, &in)
			if !ok {
				return
			}
			res, err := co.ChangeAppointment(r.Context(), a.Subject, key, r.PathValue("id"), in)
			respondMove(w, r, logger, res, err, "appointment_change_pending")
		}))
		// K05/K06 fulfilment steps (chef or fulfilment; the database keeps
		// no_show to the chef): ready, dispatch and delivery_exception answer
		// 200 with the order's state; handover and no_show are K11 commands,
		// 202 while protected.
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/transitions", staff(id, logger, []string{identity.Chef, identity.Fulfillment},
			func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
				var in checkout.TransitionInput
				key, ok := readCommand(w, r, &in)
				if !ok {
					return
				}
				res, err := co.Transition(r.Context(), a.Subject, key, r.PathValue("id"), in)
				if errors.Is(err, checkout.ErrOrdersFrozen) {
					writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Order changes are temporarily unavailable", true)
					return
				}
				if in.Protected() {
					respondCommand(w, r, logger, http.StatusAccepted, res.Result, err, func() (any, error) {
						return checkout.Confirming{OrderID: r.PathValue("id"), OperationID: res.ID, State: in.Action + "_pending",
							PollAfterSeconds: checkout.PollAfterSeconds}, nil
					})
					return
				}
				respondCommand(w, r, logger, http.StatusOK, res.Result, err, func() (any, error) {
					return map[string]string{"order_id": res.ID, "lifecycle": res.Lifecycle, "fulfillment_state": res.FulfillmentState}, nil
				})
			}))
		// K04 Q2 late payment: honour (with the customer's agreement) or full
		// return; 202 while its K11 protection runs.
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/late-payment", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
			var in checkout.LatePaymentInput
			key, ok := readCommand(w, r, &in)
			if !ok {
				return
			}
			res, err := co.LatePayment(r.Context(), a.Subject, key, r.PathValue("id"), in)
			respondCommand(w, r, logger, http.StatusAccepted, res, err, func() (any, error) {
				return checkout.Confirming{OrderID: r.PathValue("id"), OperationID: res.ID, State: "decision_pending",
					PollAfterSeconds: checkout.PollAfterSeconds}, nil
			})
		}))
		// GK-017 r02b Q6: the chef's offer (the requested time or another
		// one) that the customer answers on the order page. Synchronous and
		// versioned; 200 {order_id, offer_id}.
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/late-payment/offer", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
			var in checkout.LatePaymentOfferInput
			key, ok := readCommand(w, r, &in)
			if !ok {
				return
			}
			res, err := co.OfferLatePayment(r.Context(), a.Subject, key, r.PathValue("id"), in)
			if errors.Is(err, checkout.ErrOrdersFrozen) {
				writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Order changes are temporarily unavailable", true)
				return
			}
			respondCommand(w, r, logger, http.StatusOK, res, err, func() (any, error) {
				return map[string]string{"order_id": r.PathValue("id"), "offer_id": res.ID}, nil
			})
		}))
		// K03/K06 GK delivery: the chef's quote (from £100, pay by the
		// original handover - 48 h) and free-delivery waiver (distance review).
		// Synchronous and versioned; 200 {order_id, quote_id | waiver_id}.
		chefDelivery := func(w http.ResponseWriter, r *http.Request, res catalog.Result, err error, field string) {
			if errors.Is(err, checkout.ErrOrdersFrozen) {
				writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Order changes are temporarily unavailable", true)
				return
			}
			respondCommand(w, r, logger, http.StatusOK, res, err, func() (any, error) {
				return map[string]string{"order_id": r.PathValue("id"), field: res.ID}, nil
			})
		}
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/delivery-quote", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
			var in checkout.DeliveryQuoteInput
			key, ok := readCommand(w, r, &in)
			if !ok {
				return
			}
			res, err := co.QuoteDelivery(r.Context(), a.Subject, key, r.PathValue("id"), in)
			chefDelivery(w, r, res, err, "quote_id")
		}))
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/delivery-waiver", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
			var in checkout.DeliveryWaiverInput
			key, ok := readCommand(w, r, &in)
			if !ok {
				return
			}
			res, err := co.WaiveDelivery(r.Context(), a.Subject, key, r.PathValue("id"), in)
			chefDelivery(w, r, res, err, "waiver_id")
		}))
		// K05 chef refunds: an agreed partial refund with allocations, and
		// the pending delivery share of a cancellation (chef-only). 202 while
		// the reserved refunds are dispatched; operation_id is the exception
		// or the refund operation.
		chefRefund := func(w http.ResponseWriter, r *http.Request, res catalog.Result, err error) {
			if errors.Is(err, checkout.ErrOrdersFrozen) {
				writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Order changes are temporarily unavailable", true)
				return
			}
			respondCommand(w, r, logger, http.StatusAccepted, res, err, func() (any, error) {
				return checkout.Confirming{OrderID: r.PathValue("id"), OperationID: res.ID, State: "refund_pending",
					PollAfterSeconds: checkout.PollAfterSeconds}, nil
			})
		}
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/refunds", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
			var in checkout.RefundInput
			key, ok := readCommand(w, r, &in)
			if !ok {
				return
			}
			res, err := co.RecordRefund(r.Context(), a.Subject, key, r.PathValue("id"), in)
			chefRefund(w, r, res, err)
		}))
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/delivery-refund", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
			var in checkout.DeliveryRefundInput
			key, ok := readCommand(w, r, &in)
			if !ok {
				return
			}
			res, err := co.PayDeliveryRefund(r.Context(), a.Subject, key, r.PathValue("id"), in)
			chefRefund(w, r, res, err)
		}))
		// K05 manager recovery decisions (chef): 200 {order_id, decision_id};
		// re-reserving a remainder is 202 like a refund.
		decided := func(w http.ResponseWriter, r *http.Request, res catalog.Result, err error) {
			if errors.Is(err, checkout.ErrOrdersFrozen) {
				writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Order changes are temporarily unavailable", true)
				return
			}
			respondCommand(w, r, logger, http.StatusOK, res, err, func() (any, error) {
				return map[string]string{"order_id": r.PathValue("id"), "decision_id": res.ID}, nil
			})
		}
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/refund-observations/{sub}/resolve", staff(id, logger, chef,
			func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
				var in checkout.ResolveObservationInput
				key, ok := readCommand(w, r, &in)
				if !ok {
					return
				}
				res, err := co.ResolveObservation(r.Context(), a.Subject, key, r.PathValue("id"), r.PathValue("sub"), in)
				decided(w, r, res, err)
			}))
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/refund-operations/{sub}/decide", staff(id, logger, chef,
			func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
				var in checkout.DecideOperationInput
				key, ok := readCommand(w, r, &in)
				if !ok {
					return
				}
				res, err := co.DecideOperation(r.Context(), a.Subject, key, r.PathValue("id"), r.PathValue("sub"), in)
				decided(w, r, res, err)
			}))
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/refund-operations/{sub}/clear-mismatch", staff(id, logger, chef,
			func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
				var in checkout.ClearMismatchInput
				key, ok := readCommand(w, r, &in)
				if !ok {
					return
				}
				res, err := co.ClearRefundMismatch(r.Context(), a.Subject, key, r.PathValue("id"), r.PathValue("sub"), in)
				decided(w, r, res, err)
			}))
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/payment-collections/{sub}/clear-mismatch", staff(id, logger, chef,
			func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
				var in checkout.NoteInput
				key, ok := readCommand(w, r, &in)
				if !ok {
					return
				}
				res, err := co.ClearPaymentMismatch(r.Context(), a.Subject, key, r.PathValue("id"), r.PathValue("sub"), in)
				decided(w, r, res, err)
			}))
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/refund-operations/{sub}/re-reserve", staff(id, logger, chef,
			func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
				var in checkout.RemainderInput
				key, ok := readCommand(w, r, &in)
				if !ok {
					return
				}
				res, err := co.ReserveRemainder(r.Context(), a.Subject, key, r.PathValue("id"), r.PathValue("sub"), in)
				chefRefund(w, r, res, err)
			}))
		// Q5 manual alternative payout (chef): preview, reservation and
		// outcomes as K11 order commands (202), and the protected list.
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/manual-payouts/preview", staff(id, logger, chef,
			func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
				var in checkout.ManualPayoutPreviewInput
				if !readBody(w, r, &in) {
					return
				}
				p, err := co.PreviewManualPayout(r.Context(), a.Subject, r.PathValue("id"), in)
				respond(w, r, logger, http.StatusOK, p, err)
			}))
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/manual-payouts", staff(id, logger, chef,
			func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
				var in checkout.ManualPayoutInput
				key, ok := readCommand(w, r, &in)
				if !ok {
					return
				}
				res, err := co.RequestManualPayout(r.Context(), a.Subject, key, r.PathValue("id"), in)
				respondCommand(w, r, logger, http.StatusAccepted, res, err, func() (any, error) {
					return checkout.Confirming{OrderID: r.PathValue("id"), OperationID: res.ID, State: "payout_pending",
						PollAfterSeconds: checkout.PollAfterSeconds}, nil
				})
			}))
		mux.HandleFunc("POST /api/v1/admin/orders/{id}/manual-payouts/{sub}/outcome", staff(id, logger, chef,
			func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
				var in checkout.ManualPayoutOutcomeInput
				key, ok := readCommand(w, r, &in)
				if !ok {
					return
				}
				res, err := co.RecordManualPayoutOutcome(r.Context(), a.Subject, key, r.PathValue("id"), r.PathValue("sub"), in)
				respondCommand(w, r, logger, http.StatusAccepted, res, err, func() (any, error) {
					return checkout.Confirming{OrderID: r.PathValue("id"), OperationID: res.ID, State: "outcome_pending",
						PollAfterSeconds: checkout.PollAfterSeconds}, nil
				})
			}))
		mux.HandleFunc("GET /api/v1/admin/orders/{id}/manual-payouts", staff(id, logger, chef,
			func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
				items, err := co.ManualPayouts(r.Context(), a.Subject, r.PathValue("id"))
				respond(w, r, logger, http.StatusOK, map[string]any{"items": items}, err)
			}))
		// The manager attention list: chef, or technical support read-only
		// without amounts (K00); fulfillment has no access.
		mux.HandleFunc("GET /api/v1/admin/attention", staff(id, logger, readers, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
			items, err := co.AttentionList(r.Context(), a.Subject)
			respond(w, r, logger, http.StatusOK, map[string]any{"items": items}, err)
		}))
	}
	mux.HandleFunc("POST /api/v1/admin/policies", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in checkout.PolicyInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := co.RecordPolicy(r.Context(), a.Subject, key, in)
		respondCommand(w, r, logger, http.StatusCreated, res, err, func() (any, error) {
			return map[string]string{"id": res.ID}, nil
		})
	}))
	mux.HandleFunc("GET /api/v1/admin/discount-codes", staff(id, logger, readers, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		codes, err := co.Codes(r.Context(), a.Subject)
		respond(w, r, logger, http.StatusOK, map[string]any{"codes": codes}, err)
	}))
	codeAfter := func(ctx context.Context, subject, codeID string) (any, error) {
		return co.CodeByID(ctx, subject, codeID)
	}
	mux.HandleFunc("POST /api/v1/admin/discount-codes", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in checkout.CodeInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := co.CreateCode(r.Context(), a.Subject, key, in)
		respondCommand(w, r, logger, http.StatusCreated, res, err, func() (any, error) { return codeAfter(r.Context(), a.Subject, res.ID) })
	}))
	mux.HandleFunc("POST /api/v1/admin/discount-codes/{id}/versions", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in checkout.CodeRevision
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := co.ReviseCode(r.Context(), a.Subject, key, r.PathValue("id"), in)
		respondCommand(w, r, logger, http.StatusCreated, res, err, func() (any, error) { return codeAfter(r.Context(), a.Subject, res.ID) })
	}))
	mux.HandleFunc("POST /api/v1/admin/discount-codes/{id}/archive", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in catalog.VersionInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := co.ArchiveCode(r.Context(), a.Subject, key, r.PathValue("id"), in)
		respondCommand(w, r, logger, http.StatusOK, res, err, func() (any, error) { return codeAfter(r.Context(), a.Subject, res.ID) })
	}))
}

// staffOrderFilter reads the staff order list query: from and to (London
// handover days, required), lifecycle (comma-separated and/or repeated),
// needs_attention (true or false), cursor and limit (1 to 100, default 50).
// It answers the error.
func staffOrderFilter(w http.ResponseWriter, r *http.Request) (checkout.StaffOrderFilter, bool) {
	q := r.URL.Query()
	var f checkout.StaffOrderFilter
	var ok bool
	if f.From, ok = queryDate(w, r, "from"); !ok {
		return f, false
	}
	if f.To, ok = queryDate(w, r, "to"); !ok {
		return f, false
	}
	for _, v := range q["lifecycle"] {
		for _, l := range strings.Split(v, ",") {
			f.Lifecycles = append(f.Lifecycles, strings.TrimSpace(l))
		}
	}
	if q.Has("needs_attention") {
		switch q.Get("needs_attention") {
		case "true":
			f.NeedsAttention = new(bool)
			*f.NeedsAttention = true
		case "false":
			f.NeedsAttention = new(bool)
		default:
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "needs_attention must be true or false", false)
			return f, false
		}
	}
	if raw := q.Get("limit"); raw != "" || q.Has("limit") {
		n, err := strconv.Atoi(raw)
		if !limitPattern.MatchString(raw) || err != nil || n < 1 || n > checkout.MaxStaffOrderPage {
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "limit must be 1 to 100", false)
			return f, false
		}
		f.Limit = n
	}
	f.Cursor = q.Get("cursor")
	if err := f.Validate(); err != nil {
		catalogError(w, r, slog.Default(), err)
		return f, false
	}
	return f, true
}

// respondMove answers an appointment change: 503 during a restore freeze,
// 200 unchanged, or 202 with the command while it is protected.
func respondMove(w http.ResponseWriter, r *http.Request, logger *slog.Logger, res checkout.Moved, err error, pending string) {
	if errors.Is(err, checkout.ErrOrdersFrozen) {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Order changes are temporarily unavailable", true)
		return
	}
	if err == nil && res.Unchanged {
		respondCommand(w, r, logger, http.StatusOK, res.Result, nil, func() (any, error) {
			return map[string]string{"order_id": r.PathValue("id"), "state": "unchanged"}, nil
		})
		return
	}
	respondCommand(w, r, logger, http.StatusAccepted, res.Result, err, func() (any, error) {
		return checkout.Confirming{OrderID: r.PathValue("id"), OperationID: res.ID, State: pending,
			PollAfterSeconds: checkout.PollAfterSeconds}, nil
	})
}

// emptyBody accepts a request without a body or with an empty JSON object
// (a read-like POST with no input); anything else is a 400.
func emptyBody(w http.ResponseWriter, r *http.Request) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err == nil && len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	var none struct{}
	if err != nil || decodeStrict(body, &none) != nil {
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "This request takes no body other than {}", false)
		return false
	}
	return true
}

// readBody decodes a strict JSON command body without an Idempotency-Key (a
// read-like POST such as a preview) and validates it; it answers the error.
func readBody(w http.ResponseWriter, r *http.Request, in validator) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCommandBytes))
	if err != nil || decodeStrict(body, in) != nil {
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "The request body must be one JSON object with known fields", false)
		return false
	}
	if err := in.Validate(); err != nil {
		var inv *catalog.InvalidError
		if errors.As(err, &inv) {
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", inv.Field+" "+inv.Message, false)
			return false
		}
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "The request body is not valid", false)
		return false
	}
	return true
}
