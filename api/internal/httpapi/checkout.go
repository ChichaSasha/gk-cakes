package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

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
	LatePayment(ctx context.Context, subject, key, id string, in checkout.LatePaymentInput) (catalog.Result, error)
	RecordRefund(ctx context.Context, subject, key, id string, in checkout.RefundInput) (catalog.Result, error)
	PayDeliveryRefund(ctx context.Context, subject, key, id string, in checkout.DeliveryRefundInput) (catalog.Result, error)
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
	if checkoutEnabled {
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
