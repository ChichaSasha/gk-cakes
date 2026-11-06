//go:build integration

package identity_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/httpapi"
	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type harness struct {
	ctx   context.Context
	db    *dbtest.DB
	api   *pgxpool.Pool // gk_api login
	admin *pgx.Conn
	dir   *identity.Directory
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	db := dbtest.NewMigrated(t, "")
	pool, err := pgxpool.New(ctx, db.LoginURL(t, "gk_api"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &harness{ctx: ctx, db: db, api: pool, admin: db.AdminConn(t), dir: &identity.Directory{DB: pool}}
}

// operator runs an owner-only identity command as gk-identity-admin does:
// the migration credential, SET LOCAL ROLE gk_owner, in one transaction.
func (h *harness) operator(t *testing.T, sql string, args ...any) bool {
	t.Helper()
	conn := h.db.MigratorConn(t)
	tx, err := conn.Begin(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(h.ctx)
	if _, err := tx.Exec(h.ctx, "SET LOCAL ROLE gk_owner"); err != nil {
		t.Fatal(err)
	}
	var changed bool
	if err := tx.QueryRow(h.ctx, sql, args...).Scan(&changed); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	if err := tx.Commit(h.ctx); err != nil {
		t.Fatal(err)
	}
	return changed
}

func (h *harness) audits(t *testing.T, action string) int {
	t.Helper()
	var n int
	if err := h.admin.QueryRow(h.ctx, "SELECT count(*) FROM business.audit_event WHERE action = $1", action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func claims(subject string) identity.Claims {
	return identity.Claims{Subject: subject, SessionID: "66666666-7777-4888-9999-aaaaaaaaaaaa", ExpiresAt: time.Now().Add(time.Hour)}
}

const (
	alice = "aaaaaaaa-0000-4000-8000-000000000001"
	bob   = "bbbbbbbb-0000-4000-8000-000000000002"
	chef  = "cccccccc-0000-4000-8000-000000000003"
)

// The first authenticated request creates the account exactly once, even
// when several arrive together, and audits it.
func TestResolveCreatesTheAccountOnce(t *testing.T) {
	h := newHarness(t)
	var wg sync.WaitGroup
	ids := make([]string, 8)
	errs := make([]error, 8)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := h.dir.Resolve(h.ctx, claims(alice))
			ids[i], errs[i] = a.AccountID, err
		}()
	}
	wg.Wait()
	for i := range ids {
		if errs[i] != nil || ids[i] != ids[0] || ids[0] == "" {
			t.Fatalf("concurrent first use: ids %v, errors %v", ids, errs)
		}
	}
	a, err := h.dir.Resolve(h.ctx, claims(alice))
	if err != nil || a.AccountID != ids[0] || a.Staff() || a.Subject != alice {
		t.Fatalf("second resolve: %+v, %v", a, err)
	}
	if n := h.audits(t, "account.created"); n != 1 {
		t.Fatalf("account.created audits = %d, want 1", n)
	}
	b, err := h.dir.Resolve(h.ctx, claims(bob))
	if err != nil || b.AccountID == a.AccountID {
		t.Fatalf("another subject: %+v, %v; want its own account", b, err)
	}
}

// Capabilities come only from operator grants, are idempotent, revocable and
// audited; a disabled account can do nothing.
func TestOperatorCommandsGrantRevokeAndDisable(t *testing.T) {
	h := newHarness(t)
	if !h.operator(t, "SELECT business.staff_grant($1, 'chef', 'first manager approved in GK-010')", chef) {
		t.Fatal("first grant reported no change")
	}
	if h.operator(t, "SELECT business.staff_grant($1, 'chef', 'again')", chef) {
		t.Fatal("repeated grant reported a change")
	}
	a, err := h.dir.Resolve(h.ctx, claims(chef))
	if err != nil || !slices.Equal(a.Capabilities, []string{identity.Chef}) {
		t.Fatalf("granted chef: %+v, %v", a, err)
	}
	h.operator(t, "SELECT business.staff_grant($1, 'fulfillment', 'also packs')", chef)
	if a, _ = h.dir.Resolve(h.ctx, claims(chef)); !slices.Equal(a.Capabilities, []string{identity.Chef, identity.Fulfillment}) {
		t.Fatalf("two capabilities in name order: %v", a.Capabilities)
	}
	if !h.operator(t, "SELECT business.staff_revoke($1, 'fulfillment', 'no longer packs')", chef) ||
		h.operator(t, "SELECT business.staff_revoke($1, 'fulfillment', 'again')", chef) {
		t.Fatal("revoke is not reported once")
	}
	if a, _ = h.dir.Resolve(h.ctx, claims(chef)); !slices.Equal(a.Capabilities, []string{identity.Chef}) {
		t.Fatalf("after revoking fulfillment: %v", a.Capabilities)
	}
	if !h.operator(t, "SELECT business.account_set_disabled($1, true, 'left the business')", chef) ||
		h.operator(t, "SELECT business.account_set_disabled($1, true, 'again')", chef) {
		t.Fatal("disable is not reported once")
	}
	if _, err := h.dir.Resolve(h.ctx, claims(chef)); !errors.Is(err, identity.ErrDisabled) {
		t.Fatalf("disabled account: %v, want ErrDisabled", err)
	}
	h.operator(t, "SELECT business.account_set_disabled($1, false, 'returned')", chef)
	if _, err := h.dir.Resolve(h.ctx, claims(chef)); err != nil {
		t.Fatalf("re-enabled: %v", err)
	}
	// Enabling never creates an account (a mistyped subject leaves no row).
	if h.operator(t, "SELECT business.account_set_disabled($1, false, 'typo')", "dddddddd-0000-4000-8000-000000000004") {
		t.Fatal("enabling an unknown subject reported a change")
	}
	var accounts int
	if err := h.admin.QueryRow(h.ctx, "SELECT count(*) FROM business.account WHERE provider_subject = 'dddddddd-0000-4000-8000-000000000004'").Scan(&accounts); err != nil || accounts != 0 {
		t.Fatalf("enabling an unknown subject created %d accounts (%v)", accounts, err)
	}
	// A subject can be disabled before its first sign-in.
	if !h.operator(t, "SELECT business.account_set_disabled($1, true, 'blocked before sign-up')", bob) {
		t.Fatal("disabling a subject with no account reported no change")
	}
	if _, err := h.dir.Resolve(h.ctx, claims(bob)); !errors.Is(err, identity.ErrDisabled) {
		t.Fatalf("first sign-in of a disabled subject: %v, want ErrDisabled", err)
	}
	for action, want := range map[string]int{"staff.capability_granted": 2, "staff.capability_revoked": 1,
		"account.disabled": 2, "account.enabled": 1, "account.created": 2} {
		if n := h.audits(t, action); n != want {
			t.Errorf("%s audits = %d, want %d", action, n, want)
		}
	}
	var via string
	if err := h.admin.QueryRow(h.ctx, `SELECT actor_kind || ':' || (detail->>'via') FROM business.audit_event
		WHERE action = 'account.created' ORDER BY occurred_at LIMIT 1`).Scan(&via); err != nil || via != "system:operator" {
		t.Errorf("an account created by a grant is audited as %q (%v), want system:operator", via, err)
	}
	for name, sql := range map[string]string{
		"unknown capability": "SELECT business.staff_grant('" + bob + "', 'owner', 'x')",
		"empty note":         "SELECT business.staff_grant('" + bob + "', 'chef', '')",
		"long note":          "SELECT business.staff_revoke('" + bob + "', 'chef', repeat('x', 201))",
		"null disabled":      "SELECT business.account_set_disabled('" + bob + "', NULL, 'x')",
	} {
		conn := h.db.MigratorConn(t)
		_, err := conn.Exec(h.ctx, "BEGIN; SET LOCAL ROLE gk_owner; "+sql+"; COMMIT")
		if dbtest.SQLState(err) != "22023" {
			t.Errorf("%s: %v, want 22023", name, err)
		}
	}
}

// Only an active chef on an enabled account lists staff, and the database
// checks this itself.
func TestStaffListNeedsAnActiveChef(t *testing.T) {
	h := newHarness(t)
	h.operator(t, "SELECT business.staff_grant($1, 'chef', 'manager')", chef)
	h.operator(t, "SELECT business.staff_grant($1, 'fulfillment', 'packer')", bob)
	c, _ := h.dir.Resolve(h.ctx, claims(chef))
	list, err := h.dir.ListStaff(h.ctx, c)
	if err != nil || len(list) != 2 || list[0].Capability != identity.Chef || list[1].Capability != identity.Fulfillment {
		t.Fatalf("chef lists staff: %+v, %v", list, err)
	}
	b, _ := h.dir.Resolve(h.ctx, claims(bob))
	if _, err := h.dir.ListStaff(h.ctx, b); dbtest.SQLState(err) != "GK403" {
		t.Fatalf("fulfillment lists staff: %v, want GK403", err)
	}
	// A disabled staff member's grants are not listed.
	h.operator(t, "SELECT business.account_set_disabled($1, true, 'away')", bob)
	if list, err := h.dir.ListStaff(h.ctx, c); err != nil || len(list) != 1 || list[0].Capability != identity.Chef {
		t.Fatalf("staff list with a disabled packer: %+v, %v; want only the chef", list, err)
	}
	h.operator(t, "SELECT business.account_set_disabled($1, true, 'away')", chef)
	if _, err := h.dir.ListStaff(h.ctx, c); dbtest.SQLState(err) != "GK403" {
		t.Fatalf("disabled chef lists staff: %v, want GK403", err)
	}
	h.operator(t, "SELECT business.account_set_disabled($1, false, 'back')", chef)
	h.operator(t, "SELECT business.staff_revoke($1, 'chef', 'stepped down')", chef)
	if _, err := h.dir.ListStaff(h.ctx, c); dbtest.SQLState(err) != "GK403" {
		t.Fatalf("revoked chef lists staff: %v, want GK403", err)
	}
}

// The API login resolves and lists through the gateway only: it cannot read
// identity tables or run operator commands.
func TestAPICannotBypassTheIdentityGateway(t *testing.T) {
	h := newHarness(t)
	conn := h.db.LoginConn(t, "gk_api")
	for _, sql := range []string{
		"SELECT count(*) FROM business.account",
		"SELECT count(*) FROM business.staff_capability",
		"INSERT INTO business.staff_capability (id, account_id, capability) VALUES (gen_random_uuid(), gen_random_uuid(), 'chef')",
		"UPDATE business.account SET disabled_at = NULL",
		"SELECT business.staff_grant('" + alice + "', 'chef', 'self-service')",
		"SELECT business.staff_revoke('" + alice + "', 'chef', 'x')",
		"SELECT business.account_set_disabled('" + alice + "', false, 'x')",
		"SELECT business.account_ensure('" + alice + "', true)",
	} {
		if _, err := conn.Exec(h.ctx, sql); dbtest.SQLState(err) != "42501" {
			t.Errorf("gk_api: %s: %v, want 42501", sql, err)
		}
	}
	worker := h.db.LoginConn(t, "gk_worker")
	if _, err := worker.Exec(h.ctx, "SELECT * FROM business.identity_resolve('"+alice+"')"); dbtest.SQLState(err) != "42501" {
		t.Errorf("gk_worker resolves identities: %v, want 42501", err)
	}
}

// --- End to end: real token verification, database and routes (AC1, AC2).

type provider struct {
	key     *ecdsa.PrivateKey
	srv     *httptest.Server
	issuer  string
	mu      sync.Mutex
	ended   map[string]bool // subjects whose provider session ended
	checked int
}

func newProvider(t *testing.T) *provider {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p := &provider{key: k, ended: map[string]bool{}}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/v1/.well-known/jwks.json":
			pub, _ := k.PublicKey.Bytes()
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kty": "EC", "crv": "P-256", "alg": "ES256",
				"kid": "k1", "x": base64.RawURLEncoding.EncodeToString(pub[1:33]), "y": base64.RawURLEncoding.EncodeToString(pub[33:])}}})
		case "/auth/v1/user":
			p.mu.Lock()
			defer p.mu.Unlock()
			p.checked++
			sub := subjectOf(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if p.ended[sub] {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error_code":"session_not_found"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id": sub})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.srv.Close)
	p.issuer = p.srv.URL + "/auth/v1"
	return p
}

func subjectOf(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	b, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c struct {
		Sub string `json:"sub"`
	}
	_ = json.Unmarshal(b, &c)
	return c.Sub
}

func (p *provider) token(t *testing.T, subject string, extra map[string]any) string {
	t.Helper()
	c := map[string]any{"iss": p.issuer, "aud": "authenticated", "sub": subject, "role": "authenticated",
		"session_id": "66666666-7777-4888-9999-aaaaaaaaaaaa", "exp": time.Now().Add(time.Hour).Unix()}
	for k, v := range extra {
		c[k] = v
	}
	enc := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	input := enc(map[string]string{"alg": "ES256", "kid": "k1", "typ": "JWT"}) + "." + enc(c)
	d := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, p.key, d[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestRoutesEndToEnd(t *testing.T) {
	h := newHarness(t)
	p := newProvider(t)
	svc := &identity.Service{
		Verifier:  &identity.Verifier{Issuer: p.issuer, HTTP: p.srv.Client()},
		Sessions:  &identity.SessionChecker{Issuer: p.issuer, PublishableKey: "sb_publishable_test", HTTP: p.srv.Client()},
		Directory: h.dir,
	}
	var logs bytes.Buffer
	handler := httpapi.NewHandler(httpapi.Options{UIOrigin: "https://gkcake.com", DB: h.api, Identity: svc,
		Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	call := func(path, token string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		body, _ := io.ReadAll(rec.Body)
		return rec.Code, string(body)
	}
	h.operator(t, "SELECT business.staff_grant($1, 'chef', 'manager')", chef)
	h.operator(t, "SELECT business.staff_grant($1, 'fulfillment', 'packer')", bob)

	// A customer reads their own account; metadata naming a staff role grants
	// nothing (AC1: a metadata-forged role cannot act).
	forged := p.token(t, alice, map[string]any{
		"app_metadata":  map[string]any{"role": "chef", "roles": []string{"chef"}, "capabilities": []string{"chef"}},
		"user_metadata": map[string]any{"role": "chef"}, "user_role": "chef"})
	if code, body := call("/api/v1/me", forged); code != 200 || !strings.Contains(body, `"capabilities":[]`) {
		t.Fatalf("customer /me: %d %s", code, body)
	}
	for _, path := range []string{"/api/v1/admin/me", "/api/v1/admin/staff"} {
		if code, body := call(path, forged); code != 403 || !strings.Contains(body, "FORBIDDEN") {
			t.Errorf("metadata-forged role on %s: %d %s, want 403", path, code, body)
		}
	}
	// Fulfillment reaches the staff shell but not the chef's staff list.
	ful := p.token(t, bob, nil)
	if code, _ := call("/api/v1/admin/me", ful); code != 200 {
		t.Errorf("fulfillment /admin/me: %d", code)
	}
	if code, _ := call("/api/v1/admin/staff", ful); code != 403 {
		t.Errorf("fulfillment /admin/staff: %d, want 403", code)
	}
	chefToken := p.token(t, chef, nil)
	if code, body := call("/api/v1/admin/staff", chefToken); code != 200 || !strings.Contains(body, `"capability":"fulfillment"`) {
		t.Fatalf("chef /admin/staff: %d %s", code, body)
	}
	// Inactive staff (AC1): a revoked capability stops at once.
	h.operator(t, "SELECT business.staff_revoke($1, 'fulfillment', 'left')", bob)
	if code, _ := call("/api/v1/admin/me", ful); code != 403 {
		t.Errorf("revoked fulfillment /admin/me: %d, want 403", code)
	}
	// Sign-out: the provider ends the session; the same unexpired token is
	// refused on staff routes at once, while customer reads rely on the
	// token until it expires (sign-out cannot revoke an issued JWT).
	p.mu.Lock()
	p.ended[chef] = true
	p.mu.Unlock()
	if code, body := call("/api/v1/admin/staff", chefToken); code != 401 || !strings.Contains(body, "SESSION_ENDED") {
		t.Errorf("signed-out chef: %d %s, want 401 SESSION_ENDED", code, body)
	}
	// A disabled account can do nothing, not even read itself.
	h.operator(t, "SELECT business.account_set_disabled($1, true, 'abuse')", alice)
	if code, body := call("/api/v1/me", forged); code != 403 || !strings.Contains(body, "ACCOUNT_DISABLED") {
		t.Errorf("disabled customer: %d %s", code, body)
	}
	// Replay with another issuer's token or a tampered subject is refused.
	if code, _ := call("/api/v1/me", p.token(t, bob, map[string]any{"iss": "https://evil.example/auth/v1"})); code != 401 {
		t.Errorf("foreign issuer: %d, want 401", code)
	}
	parts := strings.Split(ful, ".")
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"`+chef+`"}`)) + "." + parts[2]
	if code, _ := call("/api/v1/admin/staff", tampered); code != 401 {
		t.Errorf("tampered subject: %d, want 401", code)
	}
	if strings.Contains(logs.String(), parts[2]) {
		t.Fatal("a token signature reached the logs")
	}
}

// GK-016 r02b (0033): resolving records sign-in activity at most once an
// hour (K09 inactivity), passes the token's sign-in instant on, and a
// deleted account resolves to ErrDeleted with its actor; the previous API
// revision's identity_resolve still refuses it as disabled.
func TestResolveRecordsActivityAndDeletedAccounts(t *testing.T) {
	h := newHarness(t)
	c := claims(alice)
	c.AuthenticatedAt = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	a, err := h.dir.Resolve(h.ctx, c)
	if err != nil || !a.AuthenticatedAt.Equal(c.AuthenticatedAt) {
		t.Fatalf("resolve: %+v %v", a, err)
	}
	active := func() time.Time {
		t.Helper()
		var at time.Time
		if err := h.admin.QueryRow(h.ctx, "SELECT last_active_at FROM business.account WHERE id = $1::uuid", a.AccountID).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	first := active()
	if _, err := h.dir.Resolve(h.ctx, c); err != nil || !active().Equal(first) {
		t.Fatalf("activity rewritten within the hour: %v", err)
	}
	if _, err := h.admin.Exec(h.ctx, "UPDATE business.account SET last_active_at = now() - interval '2 hours' WHERE id = $1::uuid", a.AccountID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.dir.Resolve(h.ctx, c); err != nil || !active().After(first.Add(-time.Minute)) {
		t.Fatalf("activity not refreshed after an hour: %v", err)
	}
	if _, err := h.admin.Exec(h.ctx, "UPDATE business.account SET disabled_at = now(), deleted_at = now() WHERE id = $1::uuid", a.AccountID); err != nil {
		t.Fatal(err)
	}
	gone, err := h.dir.Resolve(h.ctx, c)
	if !errors.Is(err, identity.ErrDeleted) || gone.AccountID != a.AccountID || gone.Subject != alice {
		t.Fatalf("deleted account: %+v %v", gone, err)
	}
	var disabled bool
	if err := h.api.QueryRow(h.ctx, "SELECT account_disabled FROM business.identity_resolve($1::uuid)", alice).Scan(&disabled); err != nil || !disabled {
		t.Fatalf("previous identity_resolve on a deleted account: disabled=%v %v", disabled, err)
	}
}
