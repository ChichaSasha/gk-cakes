//go:build integration

package catalog_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/httpapi"
	"github.com/jackc/pgx/v5/pgconn"
)

func pos(n int) *int { return &n }

func ptr(s string) *string { return &s }

func invalidField(err error) string {
	var inv *catalog.InvalidError
	if errors.As(err, &inv) {
		return inv.Field
	}
	return ""
}

// The chef's colour list (GK-016 r01c, K01 amendment 2026-11-05): create,
// duplicate codes (archived ones included), label/position changes,
// reversible archive and optimistic versions; the code never changes.
func TestColourListCommands(t *testing.T) {
	h := newHarness(t)
	h.grant(t, chefSubject, "chef")

	// Nothing is seeded: the starting list is the chef's to create.
	if list := try(h.store.Colours(h.ctx, chefSubject)).must(t); len(list) != 0 {
		t.Fatalf("colours after migration = %+v, want none", list)
	}
	var ids []string
	for i, c := range []catalog.ColourInput{
		{Code: "pink", Label: "Pink", Position: pos(10)},
		{Code: "silver", Label: "Silver", Position: pos(20)},
		{Code: "red", Label: "Red", Position: pos(30)},
		{Code: "chocolate", Label: "Chocolate", Position: pos(40)},
		{Code: "white", Label: "White", Position: pos(0)},
	} {
		r := try(h.store.CreateColour(h.ctx, chefSubject, "k-colour-"+c.Code, c)).must(t)
		if r.Replayed {
			t.Fatalf("colour %d marked as replay", i)
		}
		ids = append(ids, r.ID)
	}
	list := try(h.store.Colours(h.ctx, chefSubject)).must(t)
	var order []string
	for _, c := range list {
		order = append(order, c.Code)
		if c.Version != 1 || c.ArchivedAt != nil {
			t.Fatalf("new colour = %+v", c)
		}
	}
	if got := string(must(json.Marshal(order))); got != `["white","pink","silver","red","chocolate"]` {
		t.Fatalf("filter order = %s, want by position", got)
	}

	// Idempotent replay; a changed body under the same key is refused.
	again := try(h.store.CreateColour(h.ctx, chefSubject, "k-colour-pink", catalog.ColourInput{Code: "pink", Label: "Pink", Position: pos(10)})).must(t)
	if again.ID != ids[0] || !again.Replayed {
		t.Fatalf("replay = %+v, want %s replayed", again, ids[0])
	}
	if _, err := h.store.CreateColour(h.ctx, chefSubject, "k-colour-pink", catalog.ColourInput{Code: "pink", Label: "Rose", Position: pos(10)}); conflictCode(err) != "key_reused" {
		t.Fatalf("changed body: %v, want key_reused", err)
	}
	// A duplicate code is refused, whatever the label.
	if _, err := h.store.CreateColour(h.ctx, chefSubject, "k-dup", catalog.ColourInput{Code: "pink", Label: "Another pink", Position: pos(1)}); conflictCode(err) != "duplicate" {
		t.Fatalf("duplicate code: %v, want duplicate", err)
	}

	// Label and position change; the version moves on; stale is refused.
	pink := ids[0]
	upd := try(h.store.UpdateColour(h.ctx, chefSubject, "k-upd", pink, catalog.ColourPatch{ExpectedVersion: 1,
		Label: catalog.Optional[string]{Set: true, Value: "Blush pink"}, Position: catalog.Optional[int]{Set: true, Value: 5}})).must(t)
	c := try(h.store.Colour(h.ctx, chefSubject, upd.ID)).must(t)
	if c.Code != "pink" || c.Label != "Blush pink" || c.Position != 5 || c.Version != 2 || c.ArchivedAt != nil {
		t.Fatalf("after update = %+v", c)
	}
	if _, err := h.store.UpdateColour(h.ctx, chefSubject, "k-stale", pink, catalog.ColourPatch{ExpectedVersion: 1,
		Position: catalog.Optional[int]{Set: true, Value: 6}}); conflictCode(err) != "stale_version" {
		t.Fatalf("stale: %v, want stale_version", err)
	}
	if _, err := h.store.UpdateColour(h.ctx, chefSubject, "k-missing", "99999999-9999-4999-8999-999999999999",
		catalog.ColourPatch{ExpectedVersion: 1, Position: catalog.Optional[int]{Set: true, Value: 6}}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("missing colour: %v, want not found", err)
	}

	// Archive (reversible); an archived colour keeps its first archive time
	// and its code stays taken.
	try(h.store.UpdateColour(h.ctx, chefSubject, "k-arch", pink, catalog.ColourPatch{ExpectedVersion: 2, Archived: catalog.Optional[bool]{Set: true, Value: true}})).must(t)
	c = try(h.store.Colour(h.ctx, chefSubject, pink)).must(t)
	if c.ArchivedAt == nil || c.Version != 3 {
		t.Fatalf("archived = %+v", c)
	}
	first := *c.ArchivedAt
	try(h.store.UpdateColour(h.ctx, chefSubject, "k-arch-2", pink, catalog.ColourPatch{ExpectedVersion: 3,
		Archived: catalog.Optional[bool]{Set: true, Value: true}, Position: catalog.Optional[int]{Set: true, Value: 7}})).must(t)
	if c = try(h.store.Colour(h.ctx, chefSubject, pink)).must(t); c.ArchivedAt == nil || !c.ArchivedAt.Equal(first) || c.Position != 7 {
		t.Fatalf("archived again = %+v, want the first archive time kept", c)
	}
	if _, err := h.store.CreateColour(h.ctx, chefSubject, "k-dup-2", catalog.ColourInput{Code: "pink", Label: "Pink", Position: pos(1)}); conflictCode(err) != "duplicate" {
		t.Fatalf("archived code reused: %v, want duplicate", err)
	}
	if list := try(h.store.Colours(h.ctx, chefSubject)).must(t); len(list) != 5 {
		t.Fatalf("staff list after archive = %d colours, want 5 (archived included)", len(list))
	}
	try(h.store.UpdateColour(h.ctx, chefSubject, "k-restore", pink, catalog.ColourPatch{ExpectedVersion: 4, Archived: catalog.Optional[bool]{Set: true, Value: false}})).must(t)
	if c = try(h.store.Colour(h.ctx, chefSubject, pink)).must(t); c.ArchivedAt != nil || c.Version != 5 {
		t.Fatalf("restored = %+v", c)
	}

	// The database refuses what the API would, even when bypassed.
	call := func(key, code, label string, position int) error {
		var id string
		var replayed bool
		return h.api.QueryRow(h.ctx, `SELECT result_id::text, replayed FROM business.catalog_colour_create($1::uuid, $2, $3, $4, $5, $6)`,
			chefSubject, key, catalog.BodyHash(catalog.CmdColourCreate, "", key), code, label, position).Scan(&id, &replayed)
	}
	for name, c := range map[string]struct {
		code, label string
		position    int
		field       string
	}{
		"code all":       {"all", "All", 1, "code"},
		"upper code":     {"Pink", "Pink", 1, "code"},
		"hyphen first":   {"-x", "X", 1, "code"},
		"long code":      {"abcdefghijabcdefghijabcdefghijabcdefghijk", "X", 1, "code"},
		"empty label":    {"x1", "", 1, "label"},
		"padded label":   {"x2", " X", 1, "label"},
		"nbsp label":     {"x3", "X\u00a0", 1, "label"},
		"bom label":      {"x4", "\ufeffX", 1, "label"},
		"long label":     {"x5", "abcdefghijabcdefghijabcdefghijabcdefghijk", 1, "label"},
		"c1 label":       {"x6", "a\u0085b", 1, "label"},
		"separator":      {"x7", "a\u2029b", 1, "label"},
		"negative":       {"x8", "X", -1, "position"},
		"position 10001": {"x9", "X", 10001, "position"},
	} {
		err := call("k-bad-"+strings.ReplaceAll(name, " ", "-"), c.code, c.label, c.position)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "GK422" || pgErr.Message != c.field+"_invalid" {
			t.Errorf("%s: %v, want GK422 %s_invalid", name, err, c.field)
		}
	}
	// 40 code points (not bytes) is accepted.
	if err := call("k-40", "forty", "éééééééééééééééééééééééééééééééééééééééé", 10000); err != nil {
		t.Fatalf("40 code points: %v", err)
	}

	// Not even the owner role may change a code or delete a colour.
	for _, sql := range []string{
		"UPDATE business.colour SET code = 'rose' WHERE id = $1",
		"DELETE FROM business.colour WHERE id = $1",
	} {
		if _, err := h.admin.Exec(h.ctx, sql, pink); dbtest.SQLState(err) != "GK409" {
			t.Errorf("%s: %v, want GK409", sql, err)
		}
	}
	if _, err := h.admin.Exec(h.ctx, "TRUNCATE business.colour CASCADE"); dbtest.SQLState(err) != "GK409" {
		t.Errorf("truncate: %v, want GK409", err)
	}

	// Every change was audited as staff, without personal data.
	var audits int
	if err := h.admin.QueryRow(h.ctx, `SELECT count(*) FROM business.audit_event WHERE subject_kind = 'colour' AND actor_kind = 'staff'
		AND action IN ('catalog.colour_created', 'catalog.colour_updated')`).Scan(&audits); err != nil || audits != 10 {
		t.Fatalf("colour audits = %d, %v; want 10 (6 created, 4 updated)", audits, err)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// A cake's one optional colour: set on create or update, cleared, refused
// when unknown or archived; customers see an active colour as {code, label,
// position} and anything else as null.
func TestCakeColour(t *testing.T) {
	h := newHarness(t)
	f := h.seed(t)
	pink := try(h.store.CreateColour(h.ctx, chefSubject, "k-pink", catalog.ColourInput{Code: "pink", Label: "Pink", Position: pos(10)})).must(t).ID
	silver := try(h.store.CreateColour(h.ctx, chefSubject, "k-silver", catalog.ColourInput{Code: "silver", Label: "Silver", Position: pos(20)})).must(t).ID
	gone := try(h.store.CreateColour(h.ctx, chefSubject, "k-gone", catalog.ColourInput{Code: "gold", Label: "Gold", Position: pos(30)})).must(t).ID
	try(h.store.UpdateColour(h.ctx, chefSubject, "k-gone-arch", gone, catalog.ColourPatch{ExpectedVersion: 1, Archived: catalog.Optional[bool]{Set: true, Value: true}})).must(t)
	unknown := "99999999-9999-4999-8999-999999999999"

	// The seeded cake (created without a colour) has none.
	if c := try(h.store.StaffCake(h.ctx, chefSubject, f.cake)).must(t); c.Colour != nil {
		t.Fatalf("cake without colour = %+v", c.Colour)
	}
	// Create with a colour; unknown or archived is refused and stores nothing.
	withColour := try(h.store.CreateCake(h.ctx, chefSubject, "k-cake-pink", catalog.CakeInput{Name: "Silver sculpture", ColourID: &silver})).must(t)
	for name, id := range map[string]string{"unknown": unknown, "archived": gone} {
		_, err := h.store.CreateCake(h.ctx, chefSubject, "k-cake-"+name, catalog.CakeInput{Name: "X", ColourID: ptr(id)})
		if invalidField(err) != "colour_id" {
			t.Errorf("create with %s colour: %v, want invalid colour_id", name, err)
		}
	}
	var cakes int
	if err := h.admin.QueryRow(h.ctx, "SELECT count(*) FROM business.cake").Scan(&cakes); err != nil || cakes != 2 {
		t.Fatalf("cakes = %d, %v; want 2", cakes, err)
	}
	if c := try(h.store.StaffCake(h.ctx, chefSubject, withColour.ID)).must(t); c.Colour == nil || c.Colour.ID != silver || c.Colour.Code != "silver" || c.Colour.ArchivedAt != nil {
		t.Fatalf("staff colour = %+v", c.Colour)
	}

	// Update: set, refuse unknown/archived (stale first), keep when absent,
	// clear with null.
	setPink := catalog.CakePatch{ExpectedVersion: 1, ColourID: catalog.Optional[string]{Set: true, Value: pink}}
	try(h.store.UpdateCake(h.ctx, chefSubject, "k-set", f.cake, setPink)).must(t)
	if again := try(h.store.UpdateCake(h.ctx, chefSubject, "k-set", f.cake, setPink)).must(t); !again.Replayed {
		t.Fatal("same key and patch not replayed")
	}
	for name, id := range map[string]string{"unknown": unknown, "archived": gone} {
		_, err := h.store.UpdateCake(h.ctx, chefSubject, "k-upd-"+name, f.cake, catalog.CakePatch{ExpectedVersion: 2, ColourID: catalog.Optional[string]{Set: true, Value: id}})
		if invalidField(err) != "colour_id" {
			t.Errorf("update to %s colour: %v, want invalid colour_id", name, err)
		}
	}
	if _, err := h.store.UpdateCake(h.ctx, chefSubject, "k-upd-stale", f.cake, catalog.CakePatch{ExpectedVersion: 1, ColourID: catalog.Optional[string]{Set: true, Null: true}}); conflictCode(err) != "stale_version" {
		t.Fatalf("stale colour update: %v, want stale_version", err)
	}
	try(h.store.UpdateCake(h.ctx, chefSubject, "k-rename", f.cake, catalog.CakePatch{ExpectedVersion: 2, Name: catalog.Optional[string]{Set: true, Value: "Pink piping II"}})).must(t)
	c := try(h.store.StaffCake(h.ctx, chefSubject, f.cake)).must(t)
	if c.Colour == nil || c.Colour.Code != "pink" || c.Version != 3 || c.Name != "Pink piping II" {
		t.Fatalf("after rename = %+v colour %+v, want pink kept", c, c.Colour)
	}

	// Public reads: active cakes show the active colour; an archived colour
	// reads as null; the deployed build's update (no colour argument) keeps
	// the colour.
	h.version(t, f, "k-v1", 1, 9000, commonAllergens)
	try(h.store.SetCakeState(h.ctx, chefSubject, "k-pub", f.cake, catalog.CakeStateInput{ExpectedVersion: 3, State: catalog.StateActive})).must(t)
	try(h.store.SetCakeState(h.ctx, chefSubject, "k-pub-2", withColour.ID, catalog.CakeStateInput{ExpectedVersion: 1, State: catalog.StateActive})).must(t)
	pub := try(h.store.PublicCake(h.ctx, f.cake)).must(t)
	if pub.Colour == nil || *pub.Colour != (catalog.CakeColour{Code: "pink", Label: "Pink", Position: 10}) {
		t.Fatalf("public colour = %+v", pub.Colour)
	}
	if b := string(must(json.Marshal(pub))); !bytes.Contains([]byte(b), []byte(`"colour":{"code":"pink","label":"Pink","position":10}`)) {
		t.Fatalf("public JSON = %s", b)
	}
	var id string
	var replayed bool
	if err := h.api.QueryRow(h.ctx, `SELECT result_id::text, replayed FROM business.catalog_cake_update($1::uuid, $2, $3, $4::uuid, $5, $6, $7)`,
		chefSubject, "k-old-build", catalog.BodyHash(catalog.CmdCakeUpdate, f.cake, "old"), f.cake, 4, nil, "Old build edit").Scan(&id, &replayed); err != nil {
		t.Fatalf("0009 update: %v", err)
	}
	if c := try(h.store.PublicCake(h.ctx, f.cake)).must(t); c.Colour == nil || c.Colour.Code != "pink" || c.Description != "Old build edit" {
		t.Fatalf("after the 0009 update = %+v colour %+v, want the colour kept", c, c.Colour)
	}

	try(h.store.UpdateColour(h.ctx, chefSubject, "k-pink-arch", pink, catalog.ColourPatch{ExpectedVersion: 1, Archived: catalog.Optional[bool]{Set: true, Value: true}})).must(t)
	page := try(h.store.PublicPage(h.ctx, "", 20)).must(t)
	got := map[string]*catalog.CakeColour{}
	for _, c := range page.Cakes {
		got[c.ID] = c.Colour
	}
	if len(got) != 2 || got[f.cake] != nil || got[withColour.ID] == nil || got[withColour.ID].Code != "silver" || got[withColour.ID].ID != "" {
		t.Fatalf("public page colours = %+v, want archived pink null and silver shown", got)
	}
	if b := string(must(json.Marshal(page))); !bytes.Contains([]byte(b), []byte(`"colour":null`)) {
		t.Fatalf("public page JSON lacks a null colour: %s", b)
	}
	// Staff still see the archived colour, with its archive time.
	if c := try(h.store.StaffCake(h.ctx, chefSubject, f.cake)).must(t); c.Colour == nil || c.Colour.ArchivedAt == nil || c.Colour.ID != pink {
		t.Fatalf("staff archived colour = %+v", c.Colour)
	}
	staffPage := try(h.store.StaffPage(h.ctx, chefSubject, "", 20)).must(t)
	for _, c := range staffPage.Cakes {
		if c.Colour == nil {
			t.Fatalf("staff page cake %s lacks its colour", c.ID)
		}
	}
	// Restoring the colour shows it again; clearing it removes it.
	try(h.store.UpdateColour(h.ctx, chefSubject, "k-pink-restore", pink, catalog.ColourPatch{ExpectedVersion: 2, Archived: catalog.Optional[bool]{Set: true, Value: false}})).must(t)
	if c := try(h.store.PublicCake(h.ctx, f.cake)).must(t); c.Colour == nil || c.Colour.Code != "pink" {
		t.Fatalf("restored colour = %+v", c.Colour)
	}
	try(h.store.UpdateCake(h.ctx, chefSubject, "k-clear", f.cake, catalog.CakePatch{ExpectedVersion: 5, ColourID: catalog.Optional[string]{Set: true, Null: true}})).must(t)
	if c := try(h.store.PublicCake(h.ctx, f.cake)).must(t); c.Colour != nil {
		t.Fatalf("cleared colour = %+v", c.Colour)
	}
	// Archiving the cake hides it with its colour.
	try(h.store.SetCakeState(h.ctx, chefSubject, "k-cake-arch", withColour.ID, catalog.CakeStateInput{ExpectedVersion: 2, State: catalog.StateArchived})).must(t)
	if _, err := h.store.PublicCake(h.ctx, withColour.ID); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("archived cake: %v", err)
	}

	// The cake changes were audited with the colour, never personal data.
	var colourAudits int
	if err := h.admin.QueryRow(h.ctx, `SELECT count(*) FROM business.audit_event WHERE subject_kind = 'cake'
		AND action IN ('catalog.cake_created', 'catalog.cake_updated') AND (detail ->> 'colour') IS NOT NULL`).Scan(&colourAudits); err != nil || colourAudits != 2 {
		t.Fatalf("cake colour audits = %d, %v; want 2 (create with silver, set pink)", colourAudits, err)
	}
	var cleared int
	if err := h.admin.QueryRow(h.ctx, `SELECT count(*) FROM business.audit_event WHERE subject_kind = 'cake'
		AND action = 'catalog.cake_updated' AND (detail::jsonb ? 'colour') AND (detail ->> 'colour') IS NULL`).Scan(&cleared); err != nil || cleared != 1 {
		t.Fatalf("cleared colour audits = %d, %v; want 1", cleared, err)
	}
}

// Colour commands are the chef's; the list is read by the chef and
// technical support only.
func TestColourAuthorization(t *testing.T) {
	h := newHarness(t)
	f := h.seed(t)
	h.grant(t, supportSubject, "technical_support")
	h.grant(t, fulfilSubject, "fulfillment")
	h.operator(t, "SELECT business.account_ensure($1::uuid, false)", customer)
	pink := try(h.store.CreateColour(h.ctx, chefSubject, "k-pink", catalog.ColourInput{Code: "pink", Label: "Pink", Position: pos(10)})).must(t).ID

	if _, err := h.store.Colours(h.ctx, supportSubject); err != nil {
		t.Fatalf("support read: %v", err)
	}
	for _, subject := range []string{supportSubject, fulfilSubject, customer, "66ac712e-4e38-48bb-a56d-fa7809ccc13f"} {
		if _, err := h.store.CreateColour(h.ctx, subject, "k1", catalog.ColourInput{Code: "red", Label: "Red", Position: pos(1)}); !errors.Is(err, catalog.ErrForbidden) {
			t.Errorf("%s create: %v, want forbidden", subject, err)
		}
		if _, err := h.store.UpdateColour(h.ctx, subject, "k2", pink, catalog.ColourPatch{ExpectedVersion: 1, Archived: catalog.Optional[bool]{Set: true, Value: true}}); !errors.Is(err, catalog.ErrForbidden) {
			t.Errorf("%s update: %v, want forbidden", subject, err)
		}
		if _, err := h.store.UpdateCake(h.ctx, subject, "k3", f.cake, catalog.CakePatch{ExpectedVersion: 1, ColourID: catalog.Optional[string]{Set: true, Value: pink}}); !errors.Is(err, catalog.ErrForbidden) {
			t.Errorf("%s cake colour: %v, want forbidden", subject, err)
		}
		if subject == supportSubject {
			continue
		}
		if _, err := h.store.Colours(h.ctx, subject); !errors.Is(err, catalog.ErrForbidden) {
			t.Errorf("%s read: %v, want forbidden", subject, err)
		}
	}
	// A revoked chef cannot replay an earlier colour command.
	h.operator(t, "SELECT business.staff_revoke($1::uuid, 'chef', 'test')", chefSubject)
	if _, err := h.store.CreateColour(h.ctx, chefSubject, "k-pink", catalog.ColourInput{Code: "pink", Label: "Pink", Position: pos(10)}); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("replay after revocation: %v, want forbidden", err)
	}
	// gk_api cannot touch the table directly.
	conn := h.db.LoginConn(t, "gk_api")
	if _, err := conn.Exec(h.ctx, "UPDATE business.colour SET label = 'x'"); dbtest.SQLState(err) != "42501" {
		t.Fatalf("direct update by gk_api: %v, want 42501", err)
	}
}

// The colour routes against the real gateway.
func TestColoursOverHTTP(t *testing.T) {
	h := newHarness(t)
	h.grant(t, chefSubject, "chef")
	srv := httptest.NewServer(httpapi.NewHandler(httpapi.Options{DB: h.api, Logger: slog.New(slog.DiscardHandler),
		Identity: stubIdentity{}, Catalog: h.store}))
	t.Cleanup(srv.Close)
	call := func(method, path, key, body string, want int) (map[string]any, http.Header) {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = bytes.NewReader([]byte(body))
		}
		req, _ := http.NewRequest(method, srv.URL+path, rd)
		if key != "" {
			req.Header.Set("Authorization", "Bearer chef")
			req.Header.Set("Idempotency-Key", key)
		} else if method == "GET" && len(path) > 13 && path[:13] == "/api/v1/admin" {
			req.Header.Set("Authorization", "Bearer chef")
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
		return out, resp.Header
	}

	pink, _ := call("POST", "/api/v1/admin/colours", "c1", `{"code":"pink","label":"Pink","position":10}`, 201)
	pinkID := pink["id"].(string)
	if pink["code"] != "pink" || pink["version"].(float64) != 1 || pink["archived_at"] != nil {
		t.Fatalf("created colour = %v", pink)
	}
	if again, hdr := call("POST", "/api/v1/admin/colours", "c1", `{"code":"pink","label":"Pink","position":10}`, 201); again["id"] != pinkID || hdr.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay = %v %v", again, hdr)
	}
	dup, _ := call("POST", "/api/v1/admin/colours", "c2", `{"code":"pink","label":"Pink 2","position":11}`, 409)
	if dup["error"].(map[string]any)["code"] != "DUPLICATE" {
		t.Fatalf("duplicate = %v", dup)
	}
	call("PATCH", "/api/v1/admin/colours/"+pinkID, "p1", `{"expected_version":1,"label":"Blush"}`, 200)
	stale, _ := call("PATCH", "/api/v1/admin/colours/"+pinkID, "p2", `{"expected_version":1,"archived":true}`, 409)
	if stale["error"].(map[string]any)["code"] != "STALE_VERSION" {
		t.Fatalf("stale = %v", stale)
	}
	list, _ := call("GET", "/api/v1/admin/colours", "", "", 200)
	if cs := list["colours"].([]any); len(cs) != 1 || cs[0].(map[string]any)["label"] != "Blush" {
		t.Fatalf("list = %v", list)
	}

	cake, _ := call("POST", "/api/v1/admin/cakes", "k1", `{"name":"Pink piping","description":"","colour_id":"`+pinkID+`"}`, 201)
	cakeID := cake["id"].(string)
	if col := cake["colour"].(map[string]any); col["id"] != pinkID || col["code"] != "pink" {
		t.Fatalf("staff cake colour = %v", cake["colour"])
	}
	bad, _ := call("POST", "/api/v1/admin/cakes", "k2", `{"name":"X","description":"","colour_id":"99999999-9999-4999-8999-999999999999"}`, 422)
	if bad["error"].(map[string]any)["message"] != "colour_id is not an active colour" {
		t.Fatalf("unknown colour = %v", bad)
	}
	call("POST", "/api/v1/admin/cakes/"+cakeID+"/state", "s1", `{"expected_version":1,"state":"active"}`, 200)
	pub, _ := call("GET", "/api/v1/cakes/"+cakeID, "", "", 200)
	if col, ok := pub["colour"].(map[string]any); !ok || len(col) != 3 || col["code"] != "pink" || col["label"] != "Blush" || col["position"].(float64) != 10 {
		t.Fatalf("public colour = %v, want exactly {code, label, position}", pub["colour"])
	}
	call("PATCH", "/api/v1/admin/colours/"+pinkID, "p3", `{"expected_version":2,"archived":true}`, 200)
	page, _ := call("GET", "/api/v1/cakes", "", "", 200)
	first := page["cakes"].([]any)[0].(map[string]any)
	if v, ok := first["colour"]; !ok || v != nil {
		t.Fatalf("archived colour on the public page = %v (present %v), want null", v, ok)
	}
	call("PATCH", "/api/v1/admin/cakes/"+cakeID, "u1", `{"expected_version":2,"colour_id":"`+pinkID+`"}`, 422)
	cleared, _ := call("PATCH", "/api/v1/admin/cakes/"+cakeID, "u2", `{"expected_version":2,"colour_id":null}`, 200)
	if v, ok := cleared["colour"]; !ok || v != nil || cleared["version"].(float64) != 3 {
		t.Fatalf("cleared = %v", cleared)
	}
}
