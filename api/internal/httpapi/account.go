package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
)

// Accounts serves the customer account routes (GK-016 r02b; checkout.Store,
// migration 0033). No analytics run on these routes (K10); responses are
// private no-store like every API response.
type Accounts interface {
	MyOrders(ctx context.Context, subject, cursor string, limit int) (checkout.MyOrderPage, error)
	Profile(ctx context.Context, subject string) (json.RawMessage, error)
	UpdateProfile(ctx context.Context, subject, key string, in checkout.ProfilePatch) (catalog.Result, error)
	SubmitClaim(ctx context.Context, subject string, guestDigest []byte, key string, in checkout.ClaimInput) (catalog.Result, error)
	WithdrawClaim(ctx context.Context, subject, key, id string, in catalog.VersionInput) (catalog.Result, error)
	MyClaims(ctx context.Context, subject string) (json.RawMessage, error)
	MyClaim(ctx context.Context, subject, id string) (json.RawMessage, error)
	StaffClaims(ctx context.Context, subject, state, cursor string, limit int) (checkout.ClaimPage, error)
	DecideClaim(ctx context.Context, subject, key, id string, in checkout.ClaimDecisionInput) (catalog.Result, error)
	AssignOwner(ctx context.Context, subject, key, orderID string, in checkout.OwnerAssignInput) (catalog.Result, error)
	PreviewDeletion(ctx context.Context, subject string) (json.RawMessage, error)
	DeleteAccount(ctx context.Context, subject string, authenticatedAt time.Time, key string, in checkout.DeletionInput) (checkout.Deleted, error)
}

// sensitiveCustomer serves h for an authenticated, enabled account after
// the provider confirms the session is current (K00: authenticated
// sensitive mutations fail closed on a provider outage).
func sensitiveCustomer(id Identity, logger *slog.Logger, h actorHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		if !ok {
			identityError(w, r, logger, identity.ErrUnauthenticated)
			return
		}
		a, err := id.Authenticate(r.Context(), token)
		if err != nil {
			identityError(w, r, logger, err)
			return
		}
		if err := id.CheckSession(r.Context(), token, a); err != nil {
			identityError(w, r, logger, err)
			return
		}
		h(w, r, a)
	}
}

// accountLimit reads ?limit= (1 to 50; 0 means the default).
func accountLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if !limitPattern.MatchString(raw) || err != nil || n < 1 || n > checkout.MaxAccountPage {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "limit must be 1 to 50", false)
		return 0, false
	}
	return n, true
}

// frozen answers a restore freeze of order changes.
func frozen(w http.ResponseWriter, r *http.Request, err error) bool {
	if errors.Is(err, checkout.ErrOrdersFrozen) {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Order changes are temporarily unavailable", true)
		return true
	}
	return false
}

// reauthenticate answers a command that needs a recent sign-in: 401 with
// the RFC 9470 step-up challenge (max_age in seconds).
func reauthenticate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_user_authentication", error_description="Sign in again to continue", max_age="600"`)
	writeError(w, r, http.StatusUnauthorized, "REAUTHENTICATION_REQUIRED", "Please sign in again to confirm it's you.", false)
}

// accountRoutes registers the customer account routes and the chef's claim
// routes.
func accountRoutes(mux *http.ServeMux, acc Accounts, id Identity, logger *slog.Logger) {
	chef := []string{identity.Chef}

	// K02: the signed-in customer's orders (summaries, newest first).
	mux.HandleFunc("GET /api/v1/me/orders", signedIn(id, logger, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		limit, ok := accountLimit(w, r)
		if !ok {
			return
		}
		p, err := acc.MyOrders(r.Context(), a.Subject, r.URL.Query().Get("cursor"), limit)
		respond(w, r, logger, http.StatusOK, p, err)
	}))

	// Profile (optional name, phone, Instagram name).
	mux.HandleFunc("GET /api/v1/me/profile", signedIn(id, logger, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		p, err := acc.Profile(r.Context(), a.Subject)
		respond(w, r, logger, http.StatusOK, p, err)
	}))
	mux.HandleFunc("PATCH /api/v1/me/profile", sensitiveCustomer(id, logger, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in checkout.ProfilePatch
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := acc.UpdateProfile(r.Context(), a.Subject, key, in)
		respondCommand(w, r, logger, http.StatusOK, res, err, func() (any, error) {
			return acc.Profile(r.Context(), a.Subject)
		})
	}))

	// K00 claims: the customer asks; the chef decides.
	mux.HandleFunc("GET /api/v1/me/order-claims", signedIn(id, logger, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		list, err := acc.MyClaims(r.Context(), a.Subject)
		if err != nil {
			catalogError(w, r, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]json.RawMessage{"claims": list})
	}))
	mux.HandleFunc("GET /api/v1/me/order-claims/{id}", signedIn(id, logger, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		c, err := acc.MyClaim(r.Context(), a.Subject, r.PathValue("id"))
		respond(w, r, logger, http.StatusOK, c, err)
	}))
	mux.HandleFunc("POST /api/v1/me/order-claims", sensitiveCustomer(id, logger, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var digest []byte
		if raw := r.Header.Get(OrderCapabilityHeader); raw != "" {
			d, err := checkout.GuestProofDigest(raw)
			if err != nil {
				// A malformed capability answers as a wrong one (K00 generic 404).
				writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Not found", false)
				return
			}
			digest = d
		}
		var in checkout.ClaimInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := acc.SubmitClaim(r.Context(), a.Subject, digest, key, in)
		respondCommand(w, r, logger, http.StatusCreated, res, err, func() (any, error) {
			return acc.MyClaim(r.Context(), a.Subject, res.ID)
		})
	}))
	mux.HandleFunc("POST /api/v1/me/order-claims/{id}/withdraw", sensitiveCustomer(id, logger, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in catalog.VersionInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := acc.WithdrawClaim(r.Context(), a.Subject, key, r.PathValue("id"), in)
		respondCommand(w, r, logger, http.StatusOK, res, err, func() (any, error) {
			return acc.MyClaim(r.Context(), a.Subject, res.ID)
		})
	}))
	mux.HandleFunc("GET /api/v1/admin/order-claims", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		limit, ok := accountLimit(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()
		p, err := acc.StaffClaims(r.Context(), a.Subject, q.Get("state"), q.Get("cursor"), limit)
		respond(w, r, logger, http.StatusOK, p, err)
	}))
	mux.HandleFunc("POST /api/v1/admin/order-claims/{id}/decision", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in checkout.ClaimDecisionInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := acc.DecideClaim(r.Context(), a.Subject, key, r.PathValue("id"), in)
		if frozen(w, r, err) {
			return
		}
		respondCommand(w, r, logger, http.StatusOK, res, err, func() (any, error) {
			state := map[string]string{"approve": "approved", "decline": "declined"}[in.Decision]
			out := map[string]string{"claim_id": res.ID, "state": state}
			if in.OrderID != "" {
				out["order_id"] = in.OrderID
			}
			return out, nil
		})
	}))
	mux.HandleFunc("POST /api/v1/admin/orders/{id}/owner", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in checkout.OwnerAssignInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := acc.AssignOwner(r.Context(), a.Subject, key, r.PathValue("id"), in)
		if frozen(w, r, err) {
			return
		}
		respondCommand(w, r, logger, http.StatusOK, res, err, func() (any, error) {
			return map[string]any{"order_id": res.ID, "linked": true}, nil
		})
	}))

	// K09 deletion: preview, then delete with a fresh sign-in.
	mux.HandleFunc("POST /api/v1/me/deletion-preview", sensitiveCustomer(id, logger, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		if !emptyBody(w, r) {
			return
		}
		p, err := acc.PreviewDeletion(r.Context(), a.Subject)
		respond(w, r, logger, http.StatusOK, p, err)
	}))
	mux.HandleFunc("POST /api/v1/me/deletion", func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		if !ok {
			identityError(w, r, logger, identity.ErrUnauthenticated)
			return
		}
		// A deleted account may only replay its deletion (same key and body).
		a, err := id.Authenticate(r.Context(), token)
		if err != nil && !(errors.Is(err, identity.ErrDeleted) && a.Subject != "") {
			identityError(w, r, logger, err)
			return
		}
		if err := id.CheckSession(r.Context(), token, a); err != nil {
			identityError(w, r, logger, err)
			return
		}
		var in checkout.DeletionInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		d, err := acc.DeleteAccount(r.Context(), a.Subject, a.AuthenticatedAt, key, in)
		switch {
		case errors.Is(err, checkout.ErrReauthenticate):
			reauthenticate(w, r)
			return
		case errors.Is(err, checkout.ErrAccountDeleted):
			identityError(w, r, logger, identity.ErrDeleted)
			return
		case frozen(w, r, err):
			return
		}
		respondCommand(w, r, logger, http.StatusOK, catalog.Result{ID: d.AccountID, Replayed: d.Replayed}, err, func() (any, error) {
			return map[string]any{"account_id": d.AccountID, "deleted": true, "deleted_at": d.DeletedAt.UTC().Format(time.RFC3339),
				"orders_unlinked": d.OrdersUnlinked, "provider_deletion": "pending"}, nil
		})
	})
}
