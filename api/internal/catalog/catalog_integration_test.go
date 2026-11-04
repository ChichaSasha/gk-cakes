//go:build integration

package catalog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/httpapi"
	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Owner fixtures (labelled test data, not production seeds): GK-005
// decisions BO-GK-005.flavours/allergens/cross_contact (gk019-owner-r5) and
// the GK-002 base price list (interview-r4-complete): S 6 in, 7-10 servings,
// 9000p. Recipe lines are the GK-007 VR-6 planned quantities, which GK-038
// has not yet validated.
const ownerNotice = "Allergen notice: All cakes are prepared in the same kitchen where EGGS, MILK, CEREALS CONTAINING GLUTEN (WHEAT), SOYBEANS and PEANUTS are handled. While care is taken to minimise cross-contact, we cannot guarantee that products are completely free from traces of other allergens. If you have a severe allergy, please contact us before ordering."

var commonAllergens = []catalog.Allergen{
	{Code: "eggs", Label: "EGGS"},
	{Code: "milk", Label: "MILK"},
	{Code: "cereals_containing_gluten", Label: "CEREALS CONTAINING GLUTEN (WHEAT)"},
	{Code: "soybeans", Label: "SOYBEANS"},
}

var vr6Lines = []catalog.Line{
	{Component: "Baked vanilla sponge", Quantity: "300.00", Unit: "g"},
	{Component: "Milk for soaking", Quantity: "45.00", Unit: "g"},
	{Component: "Classic cream-cheese filling", Quantity: "550.00", Unit: "g"},
	{Component: "Raspberry compote", Quantity: "200.00", Unit: "g"},
	{Component: "Swiss meringue buttercream", Quantity: "600.00", Unit: "g"},
}

type harness struct {
	ctx   context.Context
	db    *dbtest.DB
	api   *pgxpool.Pool
	admin *pgx.Conn
	store *catalog.Store
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	db := dbtest.New(t)
	if _, err := migrate.Run(ctx, db.MigratorConn(t), migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, db.LoginURL(t, "gk_api"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &harness{ctx: ctx, db: db, api: pool, admin: db.AdminConn(t), store: &catalog.Store{DB: pool}}
}

// operator runs an owner-only command as gk-identity-admin does.
func (h *harness) operator(t *testing.T, sql string, args ...any) {
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
	if _, err := tx.Exec(h.ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	if err := tx.Commit(h.ctx); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) grant(t *testing.T, subject, capability string) {
	t.Helper()
	h.operator(t, "SELECT business.staff_grant($1::uuid, $2, 'test')", subject, capability)
}

const (
	chefSubject    = "22ac712e-4e38-48bb-a56d-fa7809ccc13f"
	supportSubject = "33ac712e-4e38-48bb-a56d-fa7809ccc13f"
	fulfilSubject  = "44ac712e-4e38-48bb-a56d-fa7809ccc13f"
	customer       = "55ac712e-4e38-48bb-a56d-fa7809ccc13f"
)

type result[T any] struct {
	v   T
	err error
}

func try[T any](v T, err error) result[T] { return result[T]{v, err} }

func (r result[T]) must(t *testing.T) T {
	t.Helper()
	if r.err != nil {
		t.Fatal(r.err)
	}
	return r.v
}

type fixture struct {
	flavour, notice, cake, variant string
}

func (h *harness) seed(t *testing.T) fixture {
	t.Helper()
	h.grant(t, chefSubject, "chef")
	var f fixture
	f.flavour = try(h.store.CreateFlavour(h.ctx, chefSubject, "k-flavour", catalog.FlavourInput{Name: "Vanilla Raspberry"})).must(t).ID
	f.notice = try(h.store.CreateNotice(h.ctx, chefSubject, "k-notice", catalog.NoticeInput{Body: ownerNotice})).must(t).ID
	f.cake = try(h.store.CreateCake(h.ctx, chefSubject, "k-cake", catalog.CakeInput{Name: "Pink piping", Description: "Vanilla sponge, classic cream cheese cream, raspberry compote."})).must(t).ID
	f.variant = try(h.store.CreateVariant(h.ctx, chefSubject, "k-variant", f.cake, catalog.VariantInput{FlavourID: f.flavour, SizeCode: "s", StockMode: catalog.MadeToOrder})).must(t).ID
	return f
}

func (h *harness) version(t *testing.T, f fixture, key string, expected int, price int64, allergens []catalog.Allergen) catalog.Result {
	t.Helper()
	in := catalog.RecipeVersionInput{
		VariantID: f.variant, ExpectedVersion: expected, PriceMinor: price, ServingsMin: 7, ServingsMax: 10,
		Coating: "Swiss meringue buttercream", NoticeID: f.notice, Allergens: allergens, Lines: vr6Lines,
		Composition: []string{"Vanilla sponge", "Classic cream cheese cream", "Raspberry compote"},
	}
	if err := in.Validate(); err != nil {
		t.Fatalf("fixture input: %v", err)
	}
	return try(h.store.CreateRecipeVersion(h.ctx, chefSubject, key, in)).must(t)
}

func conflictCode(err error) string {
	var c *catalog.ConflictError
	if errors.As(err, &c) {
		return c.Code
	}
	return ""
}

// AC1: changing the active recipe and price leaves an earlier version (what
// an order pins) unchanged; archiving keeps every reference.
func TestRecipeVersionsAreImmutableSnapshots(t *testing.T) {
	h := newHarness(t)
	f := h.seed(t)

	// Not purchasable before a recipe version declares its allergens (K01).
	cake := try(h.store.StaffCake(h.ctx, chefSubject, f.cake)).must(t)
	if len(cake.Variants) != 1 || cake.Variants[0].Purchasable || cake.Variants[0].Recipe != nil {
		t.Fatalf("variant before a recipe version = %+v, want not purchasable without recipe", cake.Variants)
	}
	if _, err := h.store.PublicCake(h.ctx, f.cake); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("draft public read: %v, want not found", err)
	}

	v1 := h.version(t, f, "k-v1", 1, 9000, commonAllergens)
	try(h.store.SetCakeState(h.ctx, chefSubject, "k-publish", f.cake, catalog.CakeStateInput{ExpectedVersion: 1, State: catalog.StateActive})).must(t)

	pub := try(h.store.PublicCake(h.ctx, f.cake)).must(t)
	if len(pub.Variants) != 1 || !pub.Variants[0].Purchasable || pub.Variants[0].Recipe.PriceMinor != 9000 ||
		pub.Variants[0].Recipe.ID != v1.ID || pub.Variants[0].Recipe.Notice.Text != ownerNotice {
		t.Fatalf("public cake = %+v, want purchasable v1 at 9000 with the owner notice", pub.Variants)
	}
	if pub.State != "" || pub.Version != 0 || pub.Variants[0].Version != 0 {
		t.Fatalf("public read exposes staff fields: %+v", pub)
	}

	// A new version with a new price and an extra allergen becomes current.
	withSulphites := append(append([]catalog.Allergen{}, commonAllergens...), catalog.Allergen{Code: "sulphites", Label: "SULPHITES"})
	v2 := h.version(t, f, "k-v2", 2, 9500, withSulphites)
	pub = try(h.store.PublicCake(h.ctx, f.cake)).must(t)
	if r := pub.Variants[0].Recipe; r.ID != v2.ID || r.PriceMinor != 9500 || r.Revision != 2 || len(r.Allergens) != 5 {
		t.Fatalf("current recipe after v2 = %+v", r)
	}

	// The earlier snapshot is exactly what was bought.
	old := try(h.store.RecipeVersion(h.ctx, chefSubject, v1.ID)).must(t)
	if old.PriceMinor != 9000 || old.Revision != 1 || len(old.Allergens) != 4 || len(old.Lines) != 5 ||
		old.Lines[2].Quantity != "550.00" || old.Lines[2].Unit != "g" || old.Notice.Text != ownerNotice ||
		old.Size.Code != "s" || old.Flavour != "Vanilla Raspberry" || len(old.SourceSHA256) != 64 {
		t.Fatalf("v1 snapshot = %+v", old)
	}
	for _, a := range old.Allergens {
		if a.Code == "sulphites" {
			t.Fatalf("v1 gained an allergen added in v2: %+v", old.Allergens)
		}
	}

	// Even the owner role cannot rewrite or delete a snapshot.
	for _, sql := range []string{
		"UPDATE business.recipe_version SET price_minor = 1 WHERE id = $1",
		"DELETE FROM business.recipe_allergen WHERE recipe_version_id = $1",
		"UPDATE business.recipe_line SET quantity = 1 WHERE recipe_version_id = $1",
	} {
		_, err := h.admin.Exec(h.ctx, sql, v1.ID)
		if dbtest.SQLState(err) != "GK409" {
			t.Errorf("%s: %v, want GK409 immutable", sql, err)
		}
	}

	// Archive: public 404, staff and the snapshot remain.
	try(h.store.SetCakeState(h.ctx, chefSubject, "k-archive", f.cake, catalog.CakeStateInput{ExpectedVersion: 2, State: catalog.StateArchived})).must(t)
	if _, err := h.store.PublicCake(h.ctx, f.cake); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("archived public read: %v, want not found", err)
	}
	if c := try(h.store.StaffCake(h.ctx, chefSubject, f.cake)).must(t); c.State != "archived" || len(c.Variants) != 1 {
		t.Fatalf("archived staff read = %+v", c)
	}
	if _, err := h.store.RecipeVersion(h.ctx, chefSubject, v1.ID); err != nil {
		t.Fatalf("snapshot after archive: %v", err)
	}
	if _, err := h.admin.Exec(h.ctx, "DELETE FROM business.cake WHERE id = $1", f.cake); dbtest.SQLState(err) != "GK409" {
		t.Fatalf("delete archived cake: %v, want GK409", err)
	}

	// Archiving the variant keeps its versions; the public cake (restored)
	// shows no archived variant.
	try(h.store.SetCakeState(h.ctx, chefSubject, "k-restore", f.cake, catalog.CakeStateInput{ExpectedVersion: 3, State: catalog.StateActive})).must(t)
	try(h.store.ArchiveVariant(h.ctx, chefSubject, "k-varch", f.variant, catalog.VersionInput{ExpectedVersion: 3})).must(t)
	if c := try(h.store.PublicCake(h.ctx, f.cake)).must(t); len(c.Variants) != 0 {
		t.Fatalf("public variants after archive = %+v, want none", c.Variants)
	}
	if c := try(h.store.StaffCake(h.ctx, chefSubject, f.cake)).must(t); len(c.Variants) != 1 || c.Variants[0].ArchivedAt == nil || c.Variants[0].Purchasable {
		t.Fatalf("staff variant after archive = %+v", c.Variants)
	}
	if _, err := h.store.RecipeVersion(h.ctx, chefSubject, v2.ID); err != nil {
		t.Fatalf("snapshot after variant archive: %v", err)
	}
}

func TestCommandsAreIdempotentAndVersioned(t *testing.T) {
	h := newHarness(t)
	f := h.seed(t)

	// Same key and body: the original result, marked as a replay.
	again := try(h.store.CreateCake(h.ctx, chefSubject, "k-cake", catalog.CakeInput{Name: "Pink piping", Description: "Vanilla sponge, classic cream cheese cream, raspberry compote."})).must(t)
	if again.ID != f.cake || !again.Replayed {
		t.Fatalf("replay = %+v, want %s replayed", again, f.cake)
	}
	var cakes int
	if err := h.admin.QueryRow(h.ctx, "SELECT count(*) FROM business.cake").Scan(&cakes); err != nil || cakes != 1 {
		t.Fatalf("cakes after replay = %d, %v; want 1", cakes, err)
	}
	// Same key, changed body.
	if _, err := h.store.CreateCake(h.ctx, chefSubject, "k-cake", catalog.CakeInput{Name: "Other"}); conflictCode(err) != "key_reused" {
		t.Fatalf("changed body: %v, want key_reused", err)
	}
	// Stale expected version.
	_, err := h.store.UpdateCake(h.ctx, chefSubject, "k-upd", f.cake, catalog.CakePatch{ExpectedVersion: 7, Name: catalog.Optional[string]{Set: true, Value: "New"}})
	if conflictCode(err) != "stale_version" {
		t.Fatalf("stale update: %v, want stale_version", err)
	}
	// A refused command leaves no receipt: the key can be used again.
	upd := try(h.store.UpdateCake(h.ctx, chefSubject, "k-upd", f.cake, catalog.CakePatch{ExpectedVersion: 1, Description: catalog.Optional[string]{Set: true, Value: "Updated"}})).must(t)
	if c := try(h.store.StaffCake(h.ctx, chefSubject, upd.ID)).must(t); c.Name != "Pink piping" || c.Description != "Updated" || c.Version != 2 {
		t.Fatalf("after update = %+v", c)
	}
	// Duplicate active variant and flavour.
	if _, err := h.store.CreateVariant(h.ctx, chefSubject, "k-variant-2", f.cake, catalog.VariantInput{FlavourID: f.flavour, SizeCode: "s", StockMode: catalog.MadeToOrder}); conflictCode(err) != "duplicate" {
		t.Fatalf("duplicate variant: %v, want duplicate", err)
	}
	if _, err := h.store.CreateFlavour(h.ctx, chefSubject, "k-flavour-2", catalog.FlavourInput{Name: "vanilla raspberry"}); conflictCode(err) != "duplicate" {
		t.Fatalf("duplicate flavour: %v, want duplicate", err)
	}
	// Publishing twice is a state conflict.
	try(h.store.SetCakeState(h.ctx, chefSubject, "k-pub", f.cake, catalog.CakeStateInput{ExpectedVersion: 2, State: catalog.StateActive})).must(t)
	if _, err := h.store.SetCakeState(h.ctx, chefSubject, "k-pub-2", f.cake, catalog.CakeStateInput{ExpectedVersion: 3, State: catalog.StateActive}); conflictCode(err) != "state_conflict" {
		t.Fatalf("publish twice: %v, want state_conflict", err)
	}

	// A revoked chef cannot replay an earlier command.
	h.operator(t, "SELECT business.staff_revoke($1::uuid, 'chef', 'test')", chefSubject)
	if _, err := h.store.CreateCake(h.ctx, chefSubject, "k-cake", catalog.CakeInput{Name: "Pink piping", Description: "Vanilla sponge, classic cream cheese cream, raspberry compote."}); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("replay after revocation: %v, want forbidden", err)
	}
}

func TestConcurrentSameKeyHasOneEffect(t *testing.T) {
	h := newHarness(t)
	h.grant(t, chefSubject, "chef")
	const n = 8
	results := make([]catalog.Result, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = h.store.CreateCake(h.ctx, chefSubject, "same-key", catalog.CakeInput{Name: "Raced"})
		}()
	}
	wg.Wait()
	created := 0
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("attempt %d: %v", i, errs[i])
		}
		if results[i].ID != results[0].ID {
			t.Fatalf("attempt %d created %s, attempt 0 %s", i, results[i].ID, results[0].ID)
		}
		if !results[i].Replayed {
			created++
		}
	}
	var cakes int
	if err := h.admin.QueryRow(h.ctx, "SELECT count(*) FROM business.cake").Scan(&cakes); err != nil || cakes != 1 || created != 1 {
		t.Fatalf("cakes = %d (created %d), %v; want exactly one", cakes, created, err)
	}
}

func TestCatalogAuthorization(t *testing.T) {
	h := newHarness(t)
	f := h.seed(t)
	h.grant(t, supportSubject, "technical_support")
	h.grant(t, fulfilSubject, "fulfillment")
	h.operator(t, "SELECT business.account_ensure($1::uuid, false)", customer)

	// Technical support reads, never writes.
	if _, err := h.store.StaffCake(h.ctx, supportSubject, f.cake); err != nil {
		t.Fatalf("support read: %v", err)
	}
	if _, err := h.store.CreateCake(h.ctx, supportSubject, "k1", catalog.CakeInput{Name: "x"}); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("support write: %v, want forbidden", err)
	}
	for _, subject := range []string{fulfilSubject, customer, "66ac712e-4e38-48bb-a56d-fa7809ccc13f"} {
		if _, err := h.store.StaffCake(h.ctx, subject, f.cake); !errors.Is(err, catalog.ErrForbidden) {
			t.Errorf("%s staff read: %v, want forbidden", subject, err)
		}
		if _, err := h.store.CreateFlavour(h.ctx, subject, "k1", catalog.FlavourInput{Name: "x"}); !errors.Is(err, catalog.ErrForbidden) {
			t.Errorf("%s write: %v, want forbidden", subject, err)
		}
	}
	// A disabled chef can do nothing.
	h.operator(t, "SELECT business.account_set_disabled($1::uuid, true, 'test')", chefSubject)
	if _, err := h.store.CreateCake(h.ctx, chefSubject, "k2", catalog.CakeInput{Name: "x"}); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("disabled chef write: %v, want forbidden", err)
	}
	// Every write was audited as staff.
	var audits int
	if err := h.admin.QueryRow(h.ctx, `SELECT count(*) FROM business.audit_event WHERE action LIKE 'catalog.%' AND actor_kind = 'staff'`).Scan(&audits); err != nil || audits != 4 {
		t.Fatalf("catalog audits = %d, %v; want 4 (flavour, notice, cake, variant)", audits, err)
	}
}

// The database refuses invalid versions even when the API's validation is
// bypassed: no allergen declaration (missing disclosure), more than two
// decimals, a fractional count, an unknown notice.
func TestGatewayRefusesInvalidRecipes(t *testing.T) {
	h := newHarness(t)
	f := h.seed(t)
	call := func(key, allergens, lines string, notice string) error {
		var id string
		var replayed bool
		return h.api.QueryRow(h.ctx, `SELECT result_id::text, replayed FROM business.catalog_recipe_version_create(
			$1::uuid, $2, $3, $4::uuid, 1, 9000, 7, 10, 'Swiss meringue buttercream', ARRAY['Vanilla sponge'],
			$5::uuid, $6::json, $7::json, $8)`, chefSubject, key, strings.Repeat("a", 64), f.variant, notice,
			allergens, lines, strings.Repeat("b", 64)).Scan(&id, &replayed)
	}
	ok := `[{"code":"eggs","label":"EGGS","prominent":false}]`
	cases := map[string]error{
		"no allergens":     call("k1", `[]`, `[]`, f.notice),
		"unknown allergen": call("k2", `[{"code":"gluten","label":"GLUTEN"}]`, `[]`, f.notice),
		"three decimals":   call("k3", ok, `[{"component":"Sponge","quantity":"1.005","unit":"g"}]`, f.notice),
		"fractional count": call("k4", ok, `[{"component":"Eggs","quantity":"2.5","unit":"count"}]`, f.notice),
		"zero quantity":    call("k5", ok, `[{"component":"Sponge","quantity":"0.00","unit":"g"}]`, f.notice),
		"unknown notice":   call("k6", ok, `[]`, "77ac712e-4e38-48bb-a56d-fa7809ccc13f"),
		"repeated code":    call("k7", `[{"code":"eggs","label":"EGGS"},{"code":"eggs","label":"EGGS"}]`, `[]`, f.notice),
	}
	for name, err := range cases {
		if dbtest.SQLState(err) != "GK422" {
			t.Errorf("%s: %v, want GK422", name, err)
		}
	}
	var versions int
	if err := h.admin.QueryRow(h.ctx, "SELECT count(*) FROM business.recipe_version").Scan(&versions); err != nil || versions != 0 {
		t.Fatalf("recipe versions = %d, %v; want 0", versions, err)
	}
}

// AC3 (API part): the public page uses its keyset index on a 500-product
// fixture and every cake appears exactly once across the pages.
func TestPublicPagesOn500Products(t *testing.T) {
	h := newHarness(t)
	h.grant(t, chefSubject, "chef")
	// Bulk fixture through the owner role (500 commands through the API
	// path would only slow the test): 500 active and 20 draft cakes.
	h.operator(t, `INSERT INTO business.cake (id, name, state, published_at)
		SELECT gen_random_uuid(), 'Cake ' || g, 'active', now() - g * interval '1 minute' FROM generate_series(1, 500) g`)
	h.operator(t, `INSERT INTO business.cake (id, name) SELECT gen_random_uuid(), 'Draft ' || g FROM generate_series(1, 20) g`)
	if _, err := h.admin.Exec(h.ctx, "ANALYZE business.cake"); err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	cursor := ""
	pages := 0
	start := time.Now()
	for {
		page := try(h.store.PublicPage(h.ctx, cursor, catalog.MaxPageSize)).must(t)
		pages++
		for _, c := range page.Cakes {
			if seen[c.ID] {
				t.Fatalf("cake %s on two pages", c.ID)
			}
			seen[c.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	elapsed := time.Since(start)
	if len(seen) != 500 || pages != 10 {
		t.Fatalf("pages = %d, cakes = %d; want 10 pages of 500 active cakes", pages, len(seen))
	}
	t.Logf("500-product fixture: 10 pages of 50 in %s", elapsed)

	var plan []byte
	if err := h.admin.QueryRow(h.ctx, `EXPLAIN (FORMAT JSON) SELECT c.id FROM business.cake c
		WHERE c.state = 'active' AND (c.published_at, c.id) < (now(), gen_random_uuid())
		ORDER BY c.published_at DESC, c.id DESC LIMIT 51`).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plan), "cake_public_page_idx") {
		t.Fatalf("public page plan does not use cake_public_page_idx: %s", plan)
	}
	var out json.RawMessage = plan
	t.Logf("plan: %s", out)

	if _, err := h.store.PublicPage(h.ctx, "not-a-cursor", 20); !errors.Is(err, catalog.ErrBadCursor) {
		t.Fatalf("bad cursor: %v", err)
	}
}

// stubIdentity authenticates the token "chef" as the chef subject; the real
// token checks are tested in package identity.
type stubIdentity struct{}

func (stubIdentity) Authenticate(_ context.Context, token string) (identity.Actor, error) {
	if token != "chef" {
		return identity.Actor{}, identity.ErrUnauthenticated
	}
	return identity.Actor{AccountID: "acc", Subject: chefSubject, Capabilities: []string{identity.Chef}}, nil
}
func (stubIdentity) CheckSession(context.Context, string, identity.Actor) error { return nil }
func (stubIdentity) ListStaff(context.Context, identity.Actor) ([]identity.StaffMember, error) {
	return nil, nil
}

// The HTTP routes against the real gateway: a chef builds a purchasable
// cake and a customer reads it without signing in.
func TestCatalogOverHTTP(t *testing.T) {
	h := newHarness(t)
	h.grant(t, chefSubject, "chef")
	srv := httptest.NewServer(httpapi.NewHandler(httpapi.Options{DB: h.api, Logger: slog.New(slog.DiscardHandler),
		Identity: stubIdentity{}, Catalog: h.store}))
	t.Cleanup(srv.Close)

	call := func(method, path, key string, body any, want int) map[string]any {
		t.Helper()
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, srv.URL+path, rd)
		if key != "" {
			req.Header.Set("Authorization", "Bearer chef")
			req.Header.Set("Idempotency-Key", key)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if resp.StatusCode != want {
			t.Fatalf("%s %s = %d %v, want %d", method, path, resp.StatusCode, out, want)
		}
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s is cacheable", method, path)
		}
		return out
	}

	flavour := call("POST", "/api/v1/admin/flavours", "f", map[string]any{"name": "Snickers"}, 201)["id"].(string)
	notice := call("POST", "/api/v1/admin/allergen-notices", "n", map[string]any{"body": ownerNotice}, 201)["id"].(string)
	cake := call("POST", "/api/v1/admin/cakes", "c", map[string]any{"name": "Chocolate swirl", "description": ""}, 201)
	cakeID := cake["id"].(string)
	if again := call("POST", "/api/v1/admin/cakes", "c", map[string]any{"name": "Chocolate swirl", "description": ""}, 201); again["id"] != cakeID {
		t.Fatalf("replay created %v, want %s", again["id"], cakeID)
	}
	call("POST", "/api/v1/admin/cakes", "c", map[string]any{"name": "Other", "description": ""}, 409)
	variant := call("POST", "/api/v1/admin/cakes/"+cakeID+"/variants", "v",
		map[string]any{"flavour_id": flavour, "size_code": "s", "stock_mode": "ready_stock"}, 201)["variant_id"].(string)
	allergens := append(append([]catalog.Allergen{}, commonAllergens...), catalog.Allergen{Code: "peanuts", Label: "PEANUTS", Prominent: true})
	version := call("POST", "/api/v1/admin/recipes/versions", "r", catalog.RecipeVersionInput{
		VariantID: variant, ExpectedVersion: 1, PriceMinor: 9000, ServingsMin: 7, ServingsMax: 10,
		Coating: "Swiss meringue buttercream", Composition: []string{"Chocolate sponge", "Peanut cream cheese cream", "Peanuts", "Caramel"},
		NoticeID: notice, Allergens: allergens, Lines: []catalog.Line{},
	}, 201)
	if version["price_minor"].(float64) != 9000 || version["currency"] != "GBP" {
		t.Fatalf("version = %v", version)
	}
	call("POST", "/api/v1/admin/cakes/"+cakeID+"/state", "p", map[string]any{"expected_version": 1, "state": "active"}, 200)
	call("POST", "/api/v1/admin/cakes/"+cakeID+"/state", "p2", map[string]any{"expected_version": 1, "state": "archived"}, 409)

	page := call("GET", "/api/v1/cakes", "", nil, 200)
	cakes := page["cakes"].([]any)
	if len(cakes) != 1 {
		t.Fatalf("public cakes = %v", cakes)
	}
	v := cakes[0].(map[string]any)["variants"].([]any)[0].(map[string]any)
	r := v["recipe"].(map[string]any)
	first := r["allergens"].([]any)[0].(map[string]any)
	if v["purchasable"] != true || v["stock_mode"] != "ready_stock" || first["code"] != "peanuts" || first["prominent"] != true {
		t.Fatalf("public variant = %v (peanuts must be listed first and prominent)", v)
	}
	if _, staffField := v["version"]; staffField {
		t.Fatalf("public variant exposes its version: %v", v)
	}
	call("GET", "/api/v1/cakes/"+cakeID, "", nil, 200)
	call("GET", "/api/v1/cakes/not-a-uuid", "", nil, 404)
}
