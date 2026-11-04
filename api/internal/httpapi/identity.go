package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
	"github.com/jackc/pgx/v5/pgconn"
)

// Identity authenticates bearer tokens (identity.Service).
type Identity interface {
	Authenticate(ctx context.Context, token string) (identity.Actor, error)
	CheckSession(ctx context.Context, token string, a identity.Actor) error
	ListStaff(ctx context.Context, a identity.Actor) ([]identity.StaffMember, error)
}

// bearer returns the access token of an "Authorization: Bearer <token>"
// header, or false when the header is absent or malformed.
func bearer(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", false
	}
	return token, true
}

type actorHandler func(w http.ResponseWriter, r *http.Request, a identity.Actor)

// signedIn serves h for an authenticated, enabled account. Customers need
// no staff capability; a token is checked for signature, issuer, audience,
// time window, subject and session ID, never for provider metadata.
func signedIn(id Identity, logger *slog.Logger, h actorHandler) http.HandlerFunc {
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
		h(w, r, a)
	}
}

// staff serves h for an enabled account holding any of capabilities (any
// staff capability when none are named), after the provider confirms the
// session is current: a signed-out or provider-ended session is refused at
// once, and an unanswered check fails closed.
func staff(id Identity, logger *slog.Logger, capabilities []string, h actorHandler) http.HandlerFunc {
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
		allowed := len(capabilities) == 0 && a.Staff()
		for _, c := range capabilities {
			allowed = allowed || a.Has(c)
		}
		if !allowed {
			writeError(w, r, http.StatusForbidden, "FORBIDDEN", "You don't have access to this", false)
			return
		}
		if err := id.CheckSession(r.Context(), token, a); err != nil {
			identityError(w, r, logger, err)
			return
		}
		h(w, r, a)
	}
}

// identityError maps authentication failures to K00 responses. Reasons are
// logged without the token; the response never says which check failed.
func identityError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, identity.ErrSessionEnded):
		logger.Info("authentication refused", "request_id", requestID(r.Context()), "reason", err.Error())
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		writeError(w, r, http.StatusUnauthorized, "SESSION_ENDED", "Your session has ended. Sign in again.", false)
	case errors.Is(err, identity.ErrUnauthenticated):
		logger.Info("authentication refused", "request_id", requestID(r.Context()), "reason", err.Error())
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in to continue", false)
	case errors.Is(err, identity.ErrDisabled):
		writeError(w, r, http.StatusForbidden, "ACCOUNT_DISABLED", "This account is disabled", false)
	default:
		logger.Warn("identity check failed", "request_id", requestID(r.Context()), "error", err.Error())
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Service temporarily unavailable", true)
	}
}

type meResponse struct {
	AccountID        string   `json:"account_id"`
	Capabilities     []string `json:"capabilities"`
	SessionExpiresAt string   `json:"session_expires_at"`
}

func me(_ http.ResponseWriter, a identity.Actor) meResponse {
	caps := a.Capabilities
	if caps == nil {
		caps = []string{}
	}
	return meResponse{AccountID: a.AccountID, Capabilities: caps, SessionExpiresAt: a.ExpiresAt.UTC().Format(time.RFC3339)}
}

// identityRoutes registers the authenticated routes of this milestone.
func identityRoutes(mux *http.ServeMux, id Identity, logger *slog.Logger) {
	mux.HandleFunc("GET /api/v1/me", signedIn(id, logger, func(w http.ResponseWriter, _ *http.Request, a identity.Actor) {
		writeJSON(w, http.StatusOK, me(w, a))
	}))
	mux.HandleFunc("GET /api/v1/admin/me", staff(id, logger, nil, func(w http.ResponseWriter, _ *http.Request, a identity.Actor) {
		writeJSON(w, http.StatusOK, me(w, a))
	}))
	mux.HandleFunc("GET /api/v1/admin/staff", staff(id, logger, []string{identity.Chef}, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		list, err := id.ListStaff(r.Context(), a)
		var pgErr *pgconn.PgError
		switch {
		case errors.As(err, &pgErr) && pgErr.Code == "42501":
			// The database saw no active chef capability (revoked meanwhile).
			writeError(w, r, http.StatusForbidden, "FORBIDDEN", "You don't have access to this", false)
		case err != nil:
			logger.Warn("list staff failed", "request_id", requestID(r.Context()), "error", err.Error())
			writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Service temporarily unavailable", true)
		default:
			if list == nil {
				list = []identity.StaffMember{}
			}
			writeJSON(w, http.StatusOK, map[string]any{"staff": list})
		}
	}))
}
