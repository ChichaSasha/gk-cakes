package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
)

// fakeAccounts records what the routes pass on.
type fakeAccounts struct {
	subject, cursor, state string
	limit                  int
	digest                 []byte
	authAt                 time.Time
	patch                  checkout.ProfilePatch
	claim                  checkout.ClaimInput
	calls                  int
	err                    error
	replayed               bool
}

func (f *fakeAccounts) MyOrders(_ context.Context, subject, cursor string, limit int) (checkout.MyOrderPage, error) {
	f.subject, f.cursor, f.limit = subject, cursor, limit
	return checkout.MyOrderPage{Orders: json.RawMessage(`[{"id":"o1"}]`), NextCursor: "next"}, f.err
}
func (f *fakeAccounts) Profile(_ context.Context, subject string) (json.RawMessage, error) {
	f.subject = subject
	return json.RawMessage(`{"full_name":null,"version":1}`), nil
}
func (f *fakeAccounts) UpdateProfile(_ context.Context, subject, _ string, in checkout.ProfilePatch) (catalog.Result, error) {
	f.calls++
	f.subject, f.patch = subject, in
	return catalog.Result{ID: "acc", Replayed: f.replayed}, f.err
}
func (f *fakeAccounts) SubmitClaim(_ context.Context, subject string, digest []byte, _ string, in checkout.ClaimInput) (catalog.Result, error) {
	f.calls++
	f.subject, f.digest, f.claim = subject, digest, in
	return catalog.Result{ID: "claim-1"}, f.err
}
func (f *fakeAccounts) WithdrawClaim(context.Context, string, string, string, catalog.VersionInput) (catalog.Result, error) {
	f.calls++
	return catalog.Result{ID: "claim-1"}, f.err
}
func (f *fakeAccounts) MyClaims(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`[]`), f.err
}
func (f *fakeAccounts) MyClaim(_ context.Context, _, id string) (json.RawMessage, error) {
	return json.RawMessage(`{"id":"` + id + `","state":"pending"}`), nil
}
func (f *fakeAccounts) StaffClaims(_ context.Context, subject, state, cursor string, limit int) (checkout.ClaimPage, error) {
	f.subject, f.state, f.cursor, f.limit = subject, state, cursor, limit
	return checkout.ClaimPage{Claims: json.RawMessage(`[]`)}, f.err
}
func (f *fakeAccounts) DecideClaim(context.Context, string, string, string, checkout.ClaimDecisionInput) (catalog.Result, error) {
	f.calls++
	return catalog.Result{ID: "claim-1"}, f.err
}
func (f *fakeAccounts) AssignOwner(_ context.Context, _, _, orderID string, _ checkout.OwnerAssignInput) (catalog.Result, error) {
	f.calls++
	return catalog.Result{ID: orderID}, f.err
}
func (f *fakeAccounts) PreviewDeletion(_ context.Context, subject string) (json.RawMessage, error) {
	f.calls++
	f.subject = subject
	return json.RawMessage(`{"preview_id":"p","can_delete":true}`), f.err
}
func (f *fakeAccounts) DeleteAccount(_ context.Context, subject string, authAt time.Time, _ string, _ checkout.DeletionInput) (checkout.Deleted, error) {
	f.calls++
	f.subject, f.authAt = subject, authAt
	if f.err != nil {
		return checkout.Deleted{}, f.err
	}
	return checkout.Deleted{AccountID: "acc-customer", DeletedAt: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC), OrdersUnlinked: 2,
		Replayed: f.replayed}, nil
}

var signedInAt = time.Date(2026, 11, 5, 9, 55, 0, 0, time.UTC)

func accountFixture() *fakeIdentity {
	id := identityFixture()
	id.actors[secretToken] = identity.Actor{AccountID: "acc-customer", Subject: "sub-customer", AuthenticatedAt: signedInAt}
	id.actors["support"] = identity.Actor{AccountID: "acc-sup", Subject: "sub-support", Capabilities: []string{identity.TechnicalSupport}}
	id.actors["chef"] = identity.Actor{AccountID: "acc-chef", Subject: "sub-chef", Capabilities: []string{identity.Chef}}
	id.deleted = map[string]identity.Actor{"deleted": {AccountID: "acc-gone", Subject: "sub-gone", AuthenticatedAt: signedInAt}}
	return id
}

type accountRequest struct {
	method, path, token, body, key string
	headers                        map[string]string
}

func serveAccount(t *testing.T, id *fakeIdentity, acc *fakeAccounts, logs *bytes.Buffer, r accountRequest) *httptest.ResponseRecorder {
	t.Helper()
	h := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id, Accounts: acc,
		Logger: slog.New(slog.NewJSONHandler(logs, nil))})
	req := httptest.NewRequest(r.method, r.path, strings.NewReader(r.body))
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	if r.key != "" {
		req.Header.Set("Idempotency-Key", r.key)
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func expectStatus(t *testing.T, label string, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("%s: status %d, want %d (%s)", label, rec.Code, status, rec.Body.String())
	}
	if code != "" {
		if got := decodeError(t, rec).Error.Code; got != code {
			t.Fatalf("%s: code %s, want %s", label, got, code)
		}
	}
}

// Account reads need a signed-in, current account (no staff capability);
// a deleted account is refused; the list pages with limit and cursor.
func TestMyOrdersRoute(t *testing.T) {
	var logs bytes.Buffer
	id, acc := accountFixture(), &fakeAccounts{}
	expectStatus(t, "no token", serveAccount(t, id, acc, &logs, accountRequest{method: "GET", path: "/api/v1/me/orders"}), 401, "UNAUTHENTICATED")
	expectStatus(t, "deleted", serveAccount(t, id, acc, &logs, accountRequest{method: "GET", path: "/api/v1/me/orders", token: "deleted"}), 403, "ACCOUNT_DELETED")
	expectStatus(t, "disabled", serveAccount(t, id, acc, &logs, accountRequest{method: "GET", path: "/api/v1/me/orders", token: "disabled"}), 403, "ACCOUNT_DISABLED")
	expectStatus(t, "limit", serveAccount(t, id, acc, &logs, accountRequest{method: "GET", path: "/api/v1/me/orders?limit=51", token: secretToken}), 422, "INVALID_INPUT")
	rec := serveAccount(t, id, acc, &logs, accountRequest{method: "GET", path: "/api/v1/me/orders?limit=5&cursor=abc", token: secretToken})
	expectStatus(t, "list", rec, 200, "")
	if acc.subject != "sub-customer" || acc.limit != 5 || acc.cursor != "abc" || rec.Body.String() != `{"orders":[{"id":"o1"}],"next_cursor":"next"}`+"\n" ||
		rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("list %+v %s", acc, rec.Body.String())
	}
	acc.err = catalog.ErrBadCursor
	expectStatus(t, "bad cursor", serveAccount(t, id, acc, &logs, accountRequest{method: "GET", path: "/api/v1/me/orders?cursor=x", token: secretToken}), 400, "BAD_REQUEST")
	if id.sessionChecks != 0 {
		t.Errorf("reads checked the provider session")
	}
	if strings.Contains(logs.String(), "secret-token-value") {
		t.Fatal("token logged")
	}
}

// Profile changes are sensitive mutations: Idempotency-Key, strict JSON,
// null clears a named field, the provider session is checked (fails closed).
func TestProfileRoute(t *testing.T) {
	var logs bytes.Buffer
	id, acc := accountFixture(), &fakeAccounts{}
	patch := accountRequest{method: "PATCH", path: "/api/v1/me/profile", token: secretToken, key: "k1",
		body: `{"expected_version":1,"phone":null,"instagram":"anna.cakes"}`}
	noKey := patch
	noKey.key = ""
	expectStatus(t, "no key", serveAccount(t, id, acc, &logs, noKey), 400, "IDEMPOTENCY_KEY_REQUIRED")
	unknown := patch
	unknown.body = `{"expected_version":1,"email":"a@example.com"}`
	expectStatus(t, "unknown field", serveAccount(t, id, acc, &logs, unknown), 400, "BAD_REQUEST")
	badPhone := patch
	badPhone.body = `{"expected_version":1,"phone":"call me"}`
	expectStatus(t, "bad phone", serveAccount(t, id, acc, &logs, badPhone), 422, "INVALID_INPUT")
	checks := id.sessionChecks
	rec := serveAccount(t, id, acc, &logs, patch)
	expectStatus(t, "patch", rec, 200, "")
	if !acc.patch.Phone.Set || !acc.patch.Phone.Null || acc.patch.Instagram.Value != "anna.cakes" || acc.patch.FullName.Set ||
		id.sessionChecks != checks+1 || !strings.Contains(rec.Body.String(), `"version":1`) {
		t.Fatalf("patch %+v checks %d %s", acc.patch, id.sessionChecks, rec.Body.String())
	}
	acc.err = &catalog.ConflictError{Code: "stale_version"}
	expectStatus(t, "stale", serveAccount(t, id, acc, &logs, patch), 409, "STALE_VERSION")
	id.sessionErr = identity.ErrSessionEnded
	calls := acc.calls
	expectStatus(t, "ended session", serveAccount(t, id, acc, &logs, patch), 401, "SESSION_ENDED")
	if acc.calls != calls {
		t.Fatal("an ended session reached the store")
	}
}

// Claims: the order capability header is passed as its digest; a malformed
// one answers 404 like a wrong one; the chef queue is chef-only.
func TestClaimRoutes(t *testing.T) {
	var logs bytes.Buffer
	id, acc := accountFixture(), &fakeAccounts{}
	body := `{"channel":"instagram","reference":"IG-0613","note":"Pink cake"}`
	rec := serveAccount(t, id, acc, &logs, accountRequest{method: "POST", path: "/api/v1/me/order-claims", token: secretToken, key: "c1", body: body})
	expectStatus(t, "submit", rec, 201, "")
	if acc.digest != nil || acc.claim.Reference != "IG-0613" || !strings.Contains(rec.Body.String(), `"state":"pending"`) {
		t.Fatalf("submit %+v %s", acc, rec.Body.String())
	}
	proof := strings.Repeat("A", 43)
	rec = serveAccount(t, id, acc, &logs, accountRequest{method: "POST", path: "/api/v1/me/order-claims", token: secretToken, key: "c2",
		body: `{"channel":"website_email_link"}`, headers: map[string]string{OrderCapabilityHeader: proof}})
	want, _ := checkout.GuestProofDigest(proof)
	if expectStatus(t, "link claim", rec, 201, ""); !bytes.Equal(acc.digest, want) {
		t.Fatalf("digest %x, want %x", acc.digest, want)
	}
	calls := acc.calls
	expectStatus(t, "malformed capability", serveAccount(t, id, acc, &logs, accountRequest{method: "POST", path: "/api/v1/me/order-claims",
		token: secretToken, key: "c3", body: `{"channel":"website_email_link"}`, headers: map[string]string{OrderCapabilityHeader: "short"}}), 404, "NOT_FOUND")
	if acc.calls != calls {
		t.Fatal("a malformed capability reached the store")
	}
	acc.err = &catalog.ConflictError{Code: "claim_pending"}
	expectStatus(t, "pending", serveAccount(t, id, acc, &logs, accountRequest{method: "POST", path: "/api/v1/me/order-claims", token: secretToken,
		key: "c4", body: body}), 409, "CLAIM_PENDING")
	acc.err = nil
	for _, token := range []string{secretToken, "fulfillment", "support"} {
		expectStatus(t, token+" queue", serveAccount(t, id, acc, &logs, accountRequest{method: "GET", path: "/api/v1/admin/order-claims", token: token}), 403, "FORBIDDEN")
		expectStatus(t, token+" decides", serveAccount(t, id, acc, &logs, accountRequest{method: "POST", path: "/api/v1/admin/order-claims/x/decision",
			token: token, key: "d", body: `{"expected_version":1,"decision":"decline"}`}), 403, "FORBIDDEN")
		expectStatus(t, token+" adds", serveAccount(t, id, acc, &logs, accountRequest{method: "POST", path: "/api/v1/admin/orders/x/owner",
			token: token, key: "a", body: `{"expected_version":1,"account_id":"11111111-1111-4111-8111-111111111111","evidence":"x"}`}), 403, "FORBIDDEN")
	}
	rec = serveAccount(t, id, acc, &logs, accountRequest{method: "GET", path: "/api/v1/admin/order-claims?state=declined&limit=10", token: "chef"})
	if expectStatus(t, "chef queue", rec, 200, ""); acc.state != "declined" || acc.limit != 10 || acc.subject != "sub-chef" {
		t.Fatalf("queue %+v", acc)
	}
	rec = serveAccount(t, id, acc, &logs, accountRequest{method: "POST", path: "/api/v1/admin/order-claims/c/decision", token: "chef", key: "d",
		body: `{"expected_version":1,"decision":"approve","order_id":"11111111-1111-4111-8111-111111111111"}`})
	if expectStatus(t, "approve", rec, 200, ""); !strings.Contains(rec.Body.String(), `"state":"approved"`) {
		t.Fatalf("approve %s", rec.Body.String())
	}
	acc.err = checkout.ErrOrdersFrozen
	expectStatus(t, "frozen", serveAccount(t, id, acc, &logs, accountRequest{method: "POST", path: "/api/v1/admin/orders/o/owner", token: "chef",
		key: "a", body: `{"expected_version":1,"account_id":"11111111-1111-4111-8111-111111111111","evidence":"x"}`}), 503, "UNAVAILABLE")
}

// Deletion: the session is checked, the token's sign-in instant is passed
// on, a stale sign-in is a 401 step-up challenge, blocked is 409, and a
// deleted account may replay its deletion but nothing else.
func TestDeletionRoutes(t *testing.T) {
	var logs bytes.Buffer
	id, acc := accountFixture(), &fakeAccounts{}
	expectStatus(t, "preview with a body", serveAccount(t, id, acc, &logs, accountRequest{method: "POST", path: "/api/v1/me/deletion-preview",
		token: secretToken, body: `{"x":1}`}), 400, "BAD_REQUEST")
	expectStatus(t, "preview", serveAccount(t, id, acc, &logs, accountRequest{method: "POST", path: "/api/v1/me/deletion-preview", token: secretToken}), 200, "")
	del := accountRequest{method: "POST", path: "/api/v1/me/deletion", token: secretToken, key: "del",
		body: `{"preview_id":"11111111-1111-4111-8111-111111111111","preview_hash":"` + strings.Repeat("a", 64) + `"}`}
	checks := id.sessionChecks
	rec := serveAccount(t, id, acc, &logs, del)
	expectStatus(t, "delete", rec, 200, "")
	if !acc.authAt.Equal(signedInAt) || acc.subject != "sub-customer" || id.sessionChecks != checks+1 ||
		!strings.Contains(rec.Body.String(), `"provider_deletion":"pending"`) || !strings.Contains(rec.Body.String(), `"orders_unlinked":2`) {
		t.Fatalf("delete %+v %s", acc, rec.Body.String())
	}
	acc.err = checkout.ErrReauthenticate
	rec = serveAccount(t, id, acc, &logs, del)
	if expectStatus(t, "stale sign-in", rec, 401, "REAUTHENTICATION_REQUIRED"); !strings.Contains(rec.Header().Get("WWW-Authenticate"), `error="insufficient_user_authentication"`) ||
		!strings.Contains(rec.Header().Get("WWW-Authenticate"), `max_age="600"`) {
		t.Fatalf("challenge %q", rec.Header().Get("WWW-Authenticate"))
	}
	acc.err = &catalog.ConflictError{Code: "account_deletion_blocked"}
	expectStatus(t, "blocked", serveAccount(t, id, acc, &logs, del), 409, "ACCOUNT_DELETION_BLOCKED")
	// A deleted account: its deletion replays; a new deletion is refused.
	acc.err, acc.replayed = nil, true
	gone := del
	gone.token = "deleted"
	rec = serveAccount(t, id, acc, &logs, gone)
	if expectStatus(t, "replay", rec, 200, ""); rec.Header().Get("Idempotent-Replayed") != "true" || acc.subject != "sub-gone" {
		t.Fatalf("replay %+v %v", acc, rec.Header())
	}
	acc.err = checkout.ErrAccountDeleted
	expectStatus(t, "deleted, new key", serveAccount(t, id, acc, &logs, gone), 403, "ACCOUNT_DELETED")
	expectStatus(t, "deleted preview", serveAccount(t, id, acc, &logs, accountRequest{method: "POST", path: "/api/v1/me/deletion-preview", token: "deleted"}), 403, "ACCOUNT_DELETED")
	id.sessionErr = identity.ErrUnavailable
	acc.err = nil
	calls := acc.calls
	expectStatus(t, "session check down", serveAccount(t, id, acc, &logs, del), 503, "UNAVAILABLE")
	if acc.calls != calls {
		t.Fatal("deletion ran without a confirmed session")
	}
}

// Without identity the account routes are not served.
func TestAccountRoutesNeedIdentity(t *testing.T) {
	h := NewHandler(Options{DB: fakePinger{}, Accounts: &fakeAccounts{}, Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me/orders", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}
