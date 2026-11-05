package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
)

// fakeCatalog records the last command and returns canned results.
type fakeCatalog struct {
	cmdErr   error
	readErr  error
	replayed bool
	lastKey  string
	lastSubj string
	lastBody any
	calls    int
}

func (f *fakeCatalog) PublicPage(_ context.Context, cursor string, limit int) (catalog.Page, error) {
	if cursor == "bad" {
		return catalog.Page{}, catalog.ErrBadCursor
	}
	f.lastBody = limit
	return catalog.Page{Cakes: []catalog.Cake{{ID: "c1", Name: "Pink piping", Variants: []catalog.Variant{}}}}, f.readErr
}

func (f *fakeCatalog) PublicCake(_ context.Context, id string) (catalog.Cake, error) {
	if id != "c1" {
		return catalog.Cake{}, catalog.ErrNotFound
	}
	return catalog.Cake{ID: "c1", Name: "Pink piping", Variants: []catalog.Variant{}}, f.readErr
}

func (f *fakeCatalog) StaffPage(_ context.Context, subject, _ string, _ int) (catalog.Page, error) {
	f.lastSubj = subject
	return catalog.Page{Cakes: []catalog.Cake{}}, f.readErr
}

func (f *fakeCatalog) StaffCake(_ context.Context, subject, id string) (catalog.Cake, error) {
	f.lastSubj = subject
	return catalog.Cake{ID: id, State: "draft", Version: 1, Variants: []catalog.Variant{}}, f.readErr
}

func (f *fakeCatalog) RecipeVersion(_ context.Context, _, id string) (catalog.RecipeVersion, error) {
	return catalog.RecipeVersion{Recipe: catalog.Recipe{ID: id}}, f.readErr
}

func (f *fakeCatalog) Flavours(context.Context, string) ([]catalog.Flavour, error) {
	return []catalog.Flavour{}, f.readErr
}

func (f *fakeCatalog) CurrentNotice(context.Context, string) (catalog.Notice, error) {
	return catalog.Notice{}, catalog.ErrNotFound
}

func (f *fakeCatalog) cmd(subject, key string, body any) (catalog.Result, error) {
	f.calls++
	f.lastSubj, f.lastKey, f.lastBody = subject, key, body
	return catalog.Result{ID: "new-id", Replayed: f.replayed}, f.cmdErr
}

func (f *fakeCatalog) CreateFlavour(_ context.Context, s, k string, in catalog.FlavourInput) (catalog.Result, error) {
	return f.cmd(s, k, in)
}
func (f *fakeCatalog) CreateNotice(_ context.Context, s, k string, in catalog.NoticeInput) (catalog.Result, error) {
	return f.cmd(s, k, in)
}
func (f *fakeCatalog) CreateCake(_ context.Context, s, k string, in catalog.CakeInput) (catalog.Result, error) {
	return f.cmd(s, k, in)
}
func (f *fakeCatalog) UpdateCake(_ context.Context, s, k, _ string, in catalog.CakePatch) (catalog.Result, error) {
	return f.cmd(s, k, in)
}
func (f *fakeCatalog) SetCakeState(_ context.Context, s, k, _ string, in catalog.CakeStateInput) (catalog.Result, error) {
	return f.cmd(s, k, in)
}
func (f *fakeCatalog) CreateVariant(_ context.Context, s, k, _ string, in catalog.VariantInput) (catalog.Result, error) {
	return f.cmd(s, k, in)
}
func (f *fakeCatalog) ArchiveVariant(_ context.Context, s, k, _ string, in catalog.VersionInput) (catalog.Result, error) {
	return f.cmd(s, k, in)
}
func (f *fakeCatalog) CreateRecipeVersion(_ context.Context, s, k string, in catalog.RecipeVersionInput) (catalog.Result, error) {
	return f.cmd(s, k, in)
}

func catalogIdentity() *fakeIdentity {
	id := identityFixture()
	id.actors["chef"] = identity.Actor{AccountID: "acc-chef", Subject: "sub-chef", Capabilities: []string{identity.Chef}}
	id.actors["support"] = identity.Actor{AccountID: "acc-sup", Subject: "sub-sup", Capabilities: []string{identity.TechnicalSupport}}
	customer := id.actors[secretToken]
	customer.Subject = "sub-customer"
	id.actors[secretToken] = customer
	return id
}

func serveCatalog(t *testing.T, cat *fakeCatalog, id Identity, method, path, auth, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	var logs bytes.Buffer
	h := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id, Catalog: cat,
		Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPublicCatalogNeedsNoSignIn(t *testing.T) {
	cat := &fakeCatalog{}
	for name, c := range map[string]struct {
		path   string
		status int
		code   string
	}{
		"list":          {"/api/v1/cakes", 200, ""},
		"list limit 50": {"/api/v1/cakes?limit=50", 200, ""},
		"limit 51":      {"/api/v1/cakes?limit=51", 422, "INVALID_INPUT"},
		"limit 0":       {"/api/v1/cakes?limit=0", 422, "INVALID_INPUT"},
		"limit text":    {"/api/v1/cakes?limit=ten", 422, "INVALID_INPUT"},
		"limit +5":      {"/api/v1/cakes?limit=+5", 422, "INVALID_INPUT"},
		"bad cursor":    {"/api/v1/cakes?cursor=bad", 400, "BAD_REQUEST"},
		"detail":        {"/api/v1/cakes/c1", 200, ""},
		"missing":       {"/api/v1/cakes/c2", 404, "NOT_FOUND"},
	} {
		// No Identity configured: the public catalog is still served.
		rec := serveCatalog(t, cat, nil, http.MethodGet, c.path, "", "", "")
		if rec.Code != c.status {
			t.Errorf("%s: %d %s, want %d", name, rec.Code, rec.Body.String(), c.status)
			continue
		}
		if c.code != "" && decodeError(t, rec).Error.Code != c.code {
			t.Errorf("%s: %s, want %s", name, rec.Body.String(), c.code)
		}
	}
	// Staff routes are not served without identity.
	if rec := serveCatalog(t, cat, nil, http.MethodGet, "/api/v1/admin/cakes", "chef", "", ""); rec.Code != 404 {
		t.Fatalf("admin without identity: %d", rec.Code)
	}
	cat.readErr = errors.New("catalog: public page: connection refused")
	if rec := serveCatalog(t, cat, nil, http.MethodGet, "/api/v1/cakes", "", "", ""); rec.Code != 503 ||
		strings.Contains(rec.Body.String(), "connection refused") {
		t.Fatalf("database down: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCatalogCommandsAreChefOnly(t *testing.T) {
	cat := &fakeCatalog{}
	id := catalogIdentity()
	body := `{"name":"Vanilla Raspberry"}`
	for name, c := range map[string]struct {
		auth   string
		status int
	}{
		"anonymous":         {"", 401},
		"customer":          {secretToken, 403},
		"fulfillment":       {"fulfillment", 403},
		"technical support": {"support", 403},
		"chef":              {"chef", 201},
	} {
		rec := serveCatalog(t, cat, id, http.MethodPost, "/api/v1/admin/flavours", c.auth, "key-1", body)
		if rec.Code != c.status {
			t.Errorf("%s: %d %s, want %d", name, rec.Code, rec.Body.String(), c.status)
		}
	}
	if cat.calls != 1 || cat.lastSubj != "sub-chef" || cat.lastKey != "key-1" {
		t.Fatalf("command calls = %d subject %q key %q; want one by the chef", cat.calls, cat.lastSubj, cat.lastKey)
	}
	// Reads: chef and technical support; not fulfillment or customers.
	for name, c := range map[string]struct {
		auth   string
		status int
	}{"support": {"support", 200}, "chef": {"chef", 200}, "fulfillment": {"fulfillment", 403}, "customer": {secretToken, 403}} {
		if rec := serveCatalog(t, cat, id, http.MethodGet, "/api/v1/admin/cakes/c1", c.auth, "", ""); rec.Code != c.status {
			t.Errorf("read by %s: %d, want %d", name, rec.Code, c.status)
		}
	}
	// A signed-out chef session is refused before the command runs.
	id.sessionErr = identity.ErrSessionEnded
	if rec := serveCatalog(t, cat, id, http.MethodPost, "/api/v1/admin/flavours", "chef", "key-2", body); rec.Code != 401 || cat.calls != 1 {
		t.Fatalf("ended session: %d, calls %d", rec.Code, cat.calls)
	}
}

func TestCatalogCommandInput(t *testing.T) {
	id := catalogIdentity()
	recipe := `{"variant_id":"11111111-1111-4111-8111-111111111111","expected_version":1,"price_minor":9000,
		"servings_min":7,"servings_max":10,"coating":"Swiss meringue buttercream","composition":["Vanilla sponge"],
		"allergen_notice_id":"22222222-2222-4222-8222-222222222222",
		"allergens":[{"code":"eggs","label":"EGGS","prominent":false}],"lines":[{"component":"Sponge","quantity":"300.00","unit":"g"}]}`
	for name, c := range map[string]struct {
		path, key, body string
		status          int
		code            string
	}{
		"no key":           {"/api/v1/admin/cakes", "", `{"name":"A","description":""}`, 400, "IDEMPOTENCY_KEY_REQUIRED"},
		"key with space":   {"/api/v1/admin/cakes", "a b", `{"name":"A","description":""}`, 400, "IDEMPOTENCY_KEY_REQUIRED"},
		"unknown field":    {"/api/v1/admin/cakes", "k", `{"name":"A","price":1}`, 400, "BAD_REQUEST"},
		"repeated key":     {"/api/v1/admin/cakes", "k", `{"name":"A","name":"B"}`, 400, "BAD_REQUEST"},
		"nested repeat":    {"/api/v1/admin/recipes/versions", "k", strings.Replace(recipe, `"code":"eggs"`, `"code":"eggs","code":"milk"`, 1), 400, "BAD_REQUEST"},
		"trailing data":    {"/api/v1/admin/cakes", "k", `{"name":"A"} {}`, 400, "BAD_REQUEST"},
		"array body":       {"/api/v1/admin/cakes", "k", `[{"name":"A"}]`, 400, "BAD_REQUEST"},
		"empty body":       {"/api/v1/admin/cakes", "k", ``, 400, "BAD_REQUEST"},
		"string price":     {"/api/v1/admin/recipes/versions", "k", strings.Replace(recipe, `9000`, `"9000"`, 1), 400, "BAD_REQUEST"},
		"fractional price": {"/api/v1/admin/recipes/versions", "k", strings.Replace(recipe, `9000`, `90.5`, 1), 400, "BAD_REQUEST"},
		"invalid name":     {"/api/v1/admin/cakes", "k", `{"name":"","description":""}`, 422, "INVALID_INPUT"},
		"no allergens":     {"/api/v1/admin/recipes/versions", "k", strings.Replace(recipe, `[{"code":"eggs","label":"EGGS","prominent":false}]`, `[]`, 1), 422, "INVALID_INPUT"},
		"valid recipe":     {"/api/v1/admin/recipes/versions", "k", recipe, 201, ""},
		"null description": {"/api/v1/admin/cakes", "k", `{"name":"A","description":null}`, 422, "INVALID_INPUT"},
		"nested null":      {"/api/v1/admin/recipes/versions", "k", strings.Replace(recipe, `"prominent":false`, `"prominent":null`, 1), 422, "INVALID_INPUT"},
		// The unknown-field check wins over the null check (K00).
		"unknown null":        {"/api/v1/admin/cakes", "k", `{"name":"A","price":null}`, 400, "BAD_REQUEST"},
		"null then unknown":   {"/api/v1/admin/cakes", "k", `{"name":"A","description":null,"price":1}`, 400, "BAD_REQUEST"},
		"nested unknown null": {"/api/v1/admin/recipes/versions", "k", strings.Replace(recipe, `"prominent":false`, `"prominent":false,"note":null`, 1), 400, "BAD_REQUEST"},
		"null then repeat":    {"/api/v1/admin/cakes", "k", `{"description":null,"name":"A","name":"B"}`, 400, "BAD_REQUEST"},
		"null then bad key":   {"/api/v1/admin/cakes", "k", `{"description":null,"Name":"A"}`, 400, "BAD_REQUEST"},
		"null lines":          {"/api/v1/admin/recipes/versions", "k", strings.Replace(recipe, `"lines":[{"component":"Sponge","quantity":"300.00","unit":"g"}]`, `"lines":null`, 1), 422, "INVALID_INPUT"},
		"upper-case key":      {"/api/v1/admin/cakes", "k", `{"NAME":"A","description":""}`, 400, "BAD_REQUEST"},
		"case repeat":         {"/api/v1/admin/cakes", "k", `{"name":"a","Name":"b","description":""}`, 400, "BAD_REQUEST"},
		"long-s key":          {"/api/v1/admin/cakes", "k", "{\"name\":\"a\",\"de\u017fcription\":\"b\"}", 400, "BAD_REQUEST"},
		"huge version":        {"/api/v1/admin/cakes/c1/state", "k", `{"expected_version":3000000000,"state":"active"}`, 422, "INVALID_INPUT"},
	} {
		cat := &fakeCatalog{}
		rec := serveCatalog(t, cat, id, http.MethodPost, c.path, "chef", c.key, c.body)
		if rec.Code != c.status {
			t.Errorf("%s: %d %s, want %d", name, rec.Code, rec.Body.String(), c.status)
			continue
		}
		if c.code != "" {
			if got := decodeError(t, rec).Error.Code; got != c.code {
				t.Errorf("%s: code %s, want %s", name, got, c.code)
			}
			if cat.calls != 0 {
				t.Errorf("%s: a refused input reached the store", name)
			}
		}
	}
	// Invalid UTF-8 is refused, not replaced.
	cat0 := &fakeCatalog{}
	if rec := serveCatalog(t, cat0, id, http.MethodPost, "/api/v1/admin/flavours", "chef", "k", "{\"name\":\"a\xff\"}"); rec.Code != 400 || cat0.calls != 0 {
		t.Fatalf("invalid UTF-8: %d, calls %d", rec.Code, cat0.calls)
	}
	// Oversized body.
	cat := &fakeCatalog{}
	big := `{"name":"` + strings.Repeat("a", maxCommandBytes) + `"}`
	if rec := serveCatalog(t, cat, id, http.MethodPost, "/api/v1/admin/cakes", "chef", "k", big); rec.Code != 413 {
		t.Fatalf("oversized: %d", rec.Code)
	}
	// PATCH: an explicit null on a non-nullable field is invalid (no catalog
	// field is nullable); omitted fields are unchanged.
	if rec := serveCatalog(t, cat, id, http.MethodPatch, "/api/v1/admin/cakes/c1", "chef", "k", `{"expected_version":1,"name":null}`); rec.Code != 422 {
		t.Fatalf("null name: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveCatalog(t, cat, id, http.MethodPatch, "/api/v1/admin/cakes/c1", "chef", "k", `{"expected_version":1,"description":"New"}`); rec.Code != 200 {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	if p := cat.lastBody.(catalog.CakePatch); p.Name.Set || !p.Description.Set || p.Description.Value != "New" {
		t.Fatalf("patch decoded as %+v", p)
	}
}

func TestCatalogRefusalsAndReplays(t *testing.T) {
	id := catalogIdentity()
	for name, c := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"stale":     {&catalog.ConflictError{Code: "stale_version"}, 409, "STALE_VERSION"},
		"key reuse": {&catalog.ConflictError{Code: "key_reused"}, 409, "KEY_REUSED"},
		"duplicate": {&catalog.ConflictError{Code: "duplicate"}, 409, "DUPLICATE"},
		"state":     {&catalog.ConflictError{Code: "state_conflict"}, 409, "CONFLICT"},
		"invalid":   {&catalog.InvalidError{Field: "flavour_id", Message: "is not an active flavour"}, 422, "INVALID_INPUT"},
		"forbidden": {catalog.ErrForbidden, 403, "FORBIDDEN"},
		"not found": {catalog.ErrNotFound, 404, "NOT_FOUND"},
		"db down":   {errors.New("catalog: cake create: connection refused"), 503, "UNAVAILABLE"},
	} {
		cat := &fakeCatalog{cmdErr: c.err}
		rec := serveCatalog(t, cat, id, http.MethodPost, "/api/v1/admin/cakes/c1/state", "chef", "k", `{"expected_version":1,"state":"active"}`)
		if rec.Code != c.status || decodeError(t, rec).Error.Code != c.code {
			t.Errorf("%s: %d %s, want %d %s", name, rec.Code, rec.Body.String(), c.status, c.code)
		}
		if strings.Contains(rec.Body.String(), "connection refused") {
			t.Errorf("%s: internal detail in the response", name)
		}
	}
	cat := &fakeCatalog{replayed: true}
	rec := serveCatalog(t, cat, id, http.MethodPost, "/api/v1/admin/cakes", "chef", "k", `{"name":"A","description":""}`)
	if rec.Code != 201 || rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d, header %q", rec.Code, rec.Header().Get("Idempotent-Replayed"))
	}
	cat.replayed = false
	rec = serveCatalog(t, cat, id, http.MethodPost, "/api/v1/admin/cakes", "chef", "k2", `{"name":"A","description":""}`)
	if rec.Header().Get("Idempotent-Replayed") != "" {
		t.Fatal("first use marked as replay")
	}
}
