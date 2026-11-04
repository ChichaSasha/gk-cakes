package httpapi

import (
	"context"
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
	CreateCode(ctx context.Context, subject, key string, in checkout.CodeInput) (catalog.Result, error)
	ReviseCode(ctx context.Context, subject, key, codeID string, in checkout.CodeRevision) (catalog.Result, error)
	ArchiveCode(ctx context.Context, subject, key, codeID string, in catalog.VersionInput) (catalog.Result, error)
}

// QuoteProofHeader carries a guest's quote proof on reads; never logged.
const QuoteProofHeader = "X-Quote-Proof"

// optionalCustomer authenticates a bearer token when one is sent (a
// signed-in customer); without one the request is a guest's. A sent but
// refused token is refused, never downgraded to a guest.
func optionalCustomer(id Identity, w http.ResponseWriter, r *http.Request, logger *slog.Logger) (identity.Actor, bool, bool) {
	if r.Header.Get("Authorization") == "" || id == nil {
		return identity.Actor{}, false, true
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
func checkoutRoutes(mux *http.ServeMux, co Checkout, id Identity, logger *slog.Logger) {
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
	if id == nil {
		return
	}
	readers := []string{identity.Chef, identity.TechnicalSupport}
	chef := []string{identity.Chef}
	mux.HandleFunc("GET /api/v1/admin/discount-codes", staff(id, logger, readers, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		codes, err := co.Codes(r.Context(), a.Subject)
		respond(w, r, logger, http.StatusOK, map[string]any{"codes": codes}, err)
	}))
	codeAfter := func(ctx context.Context, subject, codeID string) (any, error) {
		codes, err := co.Codes(ctx, subject)
		if err != nil {
			return nil, err
		}
		for _, c := range codes {
			if c.ID == codeID {
				return c, nil
			}
		}
		return nil, catalog.ErrNotFound
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
