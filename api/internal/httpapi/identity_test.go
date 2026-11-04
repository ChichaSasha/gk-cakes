package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeIdentity maps tokens to actors or errors and records calls.
type fakeIdentity struct {
	actors        map[string]identity.Actor
	authErr       map[string]error
	sessionErr    error
	listErr       error
	sessionChecks int
}

func (f *fakeIdentity) Authenticate(_ context.Context, token string) (identity.Actor, error) {
	if err, ok := f.authErr[token]; ok {
		return identity.Actor{}, err
	}
	a, ok := f.actors[token]
	if !ok {
		return identity.Actor{}, fmt.Errorf("%w: unknown", identity.ErrUnauthenticated)
	}
	return a, nil
}

func (f *fakeIdentity) CheckSession(context.Context, string, identity.Actor) error {
	f.sessionChecks++
	return f.sessionErr
}

func (f *fakeIdentity) ListStaff(context.Context, identity.Actor) ([]identity.StaffMember, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return []identity.StaffMember{{AccountID: "acc-chef", Capability: identity.Chef, GrantedAt: time.Unix(0, 0).UTC()}}, nil
}

const secretToken = "eyJ.secret-token-value.sig"

func identityFixture() *fakeIdentity {
	exp := time.Date(2026, 11, 4, 4, 0, 0, 0, time.UTC)
	return &fakeIdentity{
		actors: map[string]identity.Actor{
			secretToken:   {AccountID: "acc-customer", ExpiresAt: exp},
			"fulfillment": {AccountID: "acc-ful", Capabilities: []string{identity.Fulfillment}, ExpiresAt: exp},
			"chef":        {AccountID: "acc-chef", Capabilities: []string{identity.Chef}, ExpiresAt: exp},
			"x y":         {AccountID: "acc-spaced", ExpiresAt: exp}, // never reachable: whitespace is refused
		},
		authErr: map[string]error{
			"disabled":    identity.ErrDisabled,
			"unavailable": fmt.Errorf("%w: jwks down", identity.ErrUnavailable),
			"db-down":     errors.New("identity: resolve account: connection refused"),
		},
	}
}

func serveIdentity(t *testing.T, id *fakeIdentity, logs *bytes.Buffer, method, path, auth string) *httptest.ResponseRecorder {
	t.Helper()
	h := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id,
		Logger: slog.New(slog.NewJSONHandler(logs, nil))})
	req := httptest.NewRequest(method, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMeRequiresAValidToken(t *testing.T) {
	var logs bytes.Buffer
	id := identityFixture()
	for name, c := range map[string]struct {
		auth   string
		status int
		code   string
	}{
		"no header":         {"", 401, "UNAUTHENTICATED"},
		"basic auth":        {"Basic dXNlcjpwdw==", 401, "UNAUTHENTICATED"},
		"bearer, no token":  {"Bearer ", 401, "UNAUTHENTICATED"},
		"two tokens":        {"Bearer x y", 401, "UNAUTHENTICATED"},
		"rejected token":    {"Bearer forged", 401, "UNAUTHENTICATED"},
		"disabled account":  {"Bearer disabled", 403, "ACCOUNT_DISABLED"},
		"keys unavailable":  {"Bearer unavailable", 503, "UNAVAILABLE"},
		"database down":     {"Bearer db-down", 503, "UNAVAILABLE"},
		"lower-case scheme": {"bearer " + secretToken, 200, ""},
	} {
		rec := serveIdentity(t, id, &logs, http.MethodGet, "/api/v1/me", c.auth)
		if rec.Code != c.status {
			t.Errorf("%s: status %d, want %d (%s)", name, rec.Code, c.status, rec.Body.String())
			continue
		}
		if c.code != "" {
			if got := decodeError(t, rec).Error.Code; got != c.code {
				t.Errorf("%s: code %s, want %s", name, got, c.code)
			}
		}
		if c.status == 401 && !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("%s: no WWW-Authenticate challenge", name)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: private response is cacheable", name)
		}
	}
	rec := serveIdentity(t, id, &logs, http.MethodGet, "/api/v1/me", "Bearer "+secretToken)
	var body meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.AccountID != "acc-customer" ||
		body.Capabilities == nil || len(body.Capabilities) != 0 || body.SessionExpiresAt != "2026-11-04T04:00:00Z" {
		t.Fatalf("GET /api/v1/me = %d %s", rec.Code, rec.Body.String())
	}
	if id.sessionChecks != 0 {
		t.Errorf("a customer read checked the provider session %d times; reads rely on the token", id.sessionChecks)
	}
	if strings.Contains(logs.String(), "secret-token-value") {
		t.Fatalf("a token reached the logs: %s", logs.String())
	}
}

// Staff routes need a locally granted capability (never provider metadata)
// and a provider-confirmed current session; the check fails closed.
func TestStaffRoutesNeedACapabilityAndACurrentSession(t *testing.T) {
	var logs bytes.Buffer
	for name, c := range map[string]struct {
		path, auth string
		session    error
		list       error
		status     int
		code       string
		checked    bool
	}{
		"customer on admin/me":          {"/api/v1/admin/me", "Bearer " + secretToken, nil, nil, 403, "FORBIDDEN", false},
		"fulfillment on admin/me":       {"/api/v1/admin/me", "Bearer fulfillment", nil, nil, 200, "", true},
		"fulfillment on staff list":     {"/api/v1/admin/staff", "Bearer fulfillment", nil, nil, 403, "FORBIDDEN", false},
		"chef on staff list":            {"/api/v1/admin/staff", "Bearer chef", nil, nil, 200, "", true},
		"signed-out chef":               {"/api/v1/admin/staff", "Bearer chef", fmt.Errorf("%w: 403", identity.ErrSessionEnded), nil, 401, "SESSION_ENDED", true},
		"session check down":            {"/api/v1/admin/me", "Bearer chef", fmt.Errorf("%w: 502", identity.ErrUnavailable), nil, 503, "UNAVAILABLE", true},
		"chef revoked in the meantime":  {"/api/v1/admin/staff", "Bearer chef", nil, &pgconn.PgError{Code: "GK403"}, 403, "FORBIDDEN", true},
		"missing grant (deploy fault)":  {"/api/v1/admin/staff", "Bearer chef", nil, &pgconn.PgError{Code: "42501"}, 503, "UNAVAILABLE", true},
		"staff list database error":     {"/api/v1/admin/staff", "Bearer chef", nil, errors.New("timeout"), 503, "UNAVAILABLE", true},
		"disabled staff":                {"/api/v1/admin/me", "Bearer disabled", nil, nil, 403, "ACCOUNT_DISABLED", false},
		"no token on staff list":        {"/api/v1/admin/staff", "", nil, nil, 401, "UNAUTHENTICATED", false},
		"customer token on staff list":  {"/api/v1/admin/staff", "Bearer " + secretToken, nil, nil, 403, "FORBIDDEN", false},
		"provider keys down on staff":   {"/api/v1/admin/staff", "Bearer unavailable", nil, nil, 503, "UNAVAILABLE", false},
		"rejected token on staff route": {"/api/v1/admin/me", "Bearer forged", nil, nil, 401, "UNAUTHENTICATED", false},
	} {
		id := identityFixture()
		id.sessionErr, id.listErr = c.session, c.list
		rec := serveIdentity(t, id, &logs, http.MethodGet, c.path, c.auth)
		if rec.Code != c.status {
			t.Errorf("%s: status %d, want %d (%s)", name, rec.Code, c.status, rec.Body.String())
			continue
		}
		if c.code != "" {
			if got := decodeError(t, rec).Error.Code; got != c.code {
				t.Errorf("%s: code %s, want %s", name, got, c.code)
			}
		}
		if (id.sessionChecks > 0) != c.checked {
			t.Errorf("%s: session checks %d, want checked=%v", name, id.sessionChecks, c.checked)
		}
	}
	id := identityFixture()
	rec := serveIdentity(t, id, &logs, http.MethodGet, "/api/v1/admin/staff", "Bearer chef")
	var body struct {
		Staff []identity.StaffMember `json:"staff"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Staff) != 1 || body.Staff[0].Capability != identity.Chef {
		t.Fatalf("staff list = %s", rec.Body.String())
	}
}

func TestIdentityRoutesNeedConfiguration(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler(fakePinger{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/v1/me without identity = %d, want 404", rec.Code)
	}
}
