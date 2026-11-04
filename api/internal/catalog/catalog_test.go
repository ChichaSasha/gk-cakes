package catalog

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func validRecipe() RecipeVersionInput {
	return RecipeVersionInput{
		VariantID: "aaaaaaaa-1111-4111-8111-11111111111b", ExpectedVersion: 1, PriceMinor: 9000,
		ServingsMin: 7, ServingsMax: 10, Coating: "Swiss meringue buttercream",
		Composition: []string{"Vanilla sponge", "Raspberry compote"},
		NoticeID:    "22222222-2222-4222-8222-222222222222",
		Allergens:   []Allergen{{Code: "eggs", Label: "EGGS"}, {Code: "peanuts", Label: "PEANUTS", Prominent: true}},
		Lines:       []Line{{Component: "Sponge", Quantity: "300.00", Unit: "g"}, {Component: "Eggs", Quantity: "3", Unit: "count"}},
	}
}

func TestRecipeVersionValidation(t *testing.T) {
	if err := validRecipe().Validate(); err != nil {
		t.Fatalf("valid recipe: %v", err)
	}
	for name, c := range map[string]struct {
		mutate func(*RecipeVersionInput)
		field  string
	}{
		"bad variant":       {func(in *RecipeVersionInput) { in.VariantID = "x" }, "variant_id"},
		"upper-case uuid":   {func(in *RecipeVersionInput) { in.VariantID = strings.ToUpper(in.VariantID) }, "variant_id"},
		"no version":        {func(in *RecipeVersionInput) { in.ExpectedVersion = 0 }, "expected_version"},
		"zero price":        {func(in *RecipeVersionInput) { in.PriceMinor = 0 }, "price_minor"},
		"negative price":    {func(in *RecipeVersionInput) { in.PriceMinor = -1 }, "price_minor"},
		"huge price":        {func(in *RecipeVersionInput) { in.PriceMinor = MaxPriceMinor + 1 }, "price_minor"},
		"servings order":    {func(in *RecipeVersionInput) { in.ServingsMax = 6 }, "servings_max"},
		"servings zero":     {func(in *RecipeVersionInput) { in.ServingsMin = 0 }, "servings_min"},
		"empty coating":     {func(in *RecipeVersionInput) { in.Coating = "" }, "coating"},
		"padded coating":    {func(in *RecipeVersionInput) { in.Coating = " x" }, "coating"},
		"no composition":    {func(in *RecipeVersionInput) { in.Composition = nil }, "composition"},
		"control char":      {func(in *RecipeVersionInput) { in.Composition = []string{"a\u0007"} }, "composition"},
		"newline in single": {func(in *RecipeVersionInput) { in.Composition = []string{"a\nb"} }, "composition"},
		"no notice":         {func(in *RecipeVersionInput) { in.NoticeID = "" }, "allergen_notice_id"},
		"no allergens":      {func(in *RecipeVersionInput) { in.Allergens = nil }, "allergens"},
		"unknown allergen":  {func(in *RecipeVersionInput) { in.Allergens = []Allergen{{Code: "gluten", Label: "GLUTEN"}} }, "allergens"},
		"repeated allergen": {func(in *RecipeVersionInput) { in.Allergens = append(in.Allergens, in.Allergens[0]) }, "allergens"},
		"empty label":       {func(in *RecipeVersionInput) { in.Allergens[0].Label = "" }, "allergens"},
		"three decimals":    {func(in *RecipeVersionInput) { in.Lines[0].Quantity = "1.005" }, "lines"},
		"zero quantity":     {func(in *RecipeVersionInput) { in.Lines[0].Quantity = "0.00" }, "lines"},
		"negative quantity": {func(in *RecipeVersionInput) { in.Lines[0].Quantity = "-1" }, "lines"},
		"exponent":          {func(in *RecipeVersionInput) { in.Lines[0].Quantity = "1e3" }, "lines"},
		"fractional count":  {func(in *RecipeVersionInput) { in.Lines[1].Quantity = "2.5" }, "lines"},
		"unknown unit":      {func(in *RecipeVersionInput) { in.Lines[0].Unit = "kg" }, "lines"},
		"too many lines":    {func(in *RecipeVersionInput) { in.Lines = make([]Line, 101) }, "lines"},
	} {
		in := validRecipe()
		c.mutate(&in)
		var inv *InvalidError
		if err := in.Validate(); !errors.As(err, &inv) || inv.Field != c.field {
			t.Errorf("%s: %v, want invalid %s", name, err, c.field)
		}
	}
	// A whole count may be written with zero decimals.
	in := validRecipe()
	in.Lines[1].Quantity = "3.00"
	if err := in.Validate(); err != nil {
		t.Errorf("3.00 eggs: %v", err)
	}
}

func TestOtherInputValidation(t *testing.T) {
	for name, err := range map[string]error{
		"empty flavour":    FlavourInput{Name: ""}.Validate(),
		"long flavour":     FlavourInput{Name: strings.Repeat("a", 81)}.Validate(),
		"empty notice":     NoticeInput{}.Validate(),
		"padded cake":      CakeInput{Name: " Cake"}.Validate(),
		"no patch fields":  CakePatch{ExpectedVersion: 1}.Validate(),
		"null name":        CakePatch{ExpectedVersion: 1, Name: Optional[string]{Set: true, Null: true}}.Validate(),
		"null description": CakePatch{ExpectedVersion: 1, Description: Optional[string]{Set: true, Null: true}}.Validate(),
		"patch no version": CakePatch{Name: Optional[string]{Set: true, Value: "x"}}.Validate(),
		"draft state":      CakeStateInput{ExpectedVersion: 1, State: "draft"}.Validate(),
		"bad size":         VariantInput{FlavourID: "11111111-1111-4111-8111-111111111111", SizeCode: "5in", StockMode: MadeToOrder}.Validate(),
		"bad stock mode":   VariantInput{FlavourID: "11111111-1111-4111-8111-111111111111", SizeCode: "s", StockMode: "resale"}.Validate(),
		"archive version":  VersionInput{}.Validate(),
	} {
		var inv *InvalidError
		if !errors.As(err, &inv) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
	for name, err := range map[string]error{
		"multi-line notice": NoticeInput{Body: "Line one\nLine two"}.Validate(),
		"empty description": CakeInput{Name: "Pink piping"}.Validate(),
		"patch description": CakePatch{ExpectedVersion: 2, Description: Optional[string]{Set: true, Value: ""}}.Validate(),
		"tiered variant":    VariantInput{FlavourID: "11111111-1111-4111-8111-111111111111", SizeCode: "tiers_6_9_11", StockMode: ReadyStock}.Validate(),
	} {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestBodyHashIsCanonical(t *testing.T) {
	a := BodyHash(CmdCakeCreate, "", CakeInput{Name: "A"})
	if a != BodyHash(CmdCakeCreate, "", CakeInput{Name: "A"}) || len(a) != 64 {
		t.Fatalf("hash not stable: %s", a)
	}
	distinct := []string{
		a,
		BodyHash(CmdCakeCreate, "", CakeInput{Name: "B"}),
		BodyHash(CmdFlavourCreate, "", CakeInput{Name: "A"}),
		BodyHash(CmdCakeCreate, "11111111-1111-4111-8111-111111111111", CakeInput{Name: "A"}),
	}
	seen := map[string]bool{}
	for _, h := range distinct {
		if seen[h] {
			t.Fatalf("hash collision across commands, resources or bodies: %v", distinct)
		}
		seen[h] = true
	}
	// Absent, null and empty PATCH fields differ.
	unset := BodyHash(CmdCakeUpdate, "x", CakePatch{ExpectedVersion: 1})
	null := BodyHash(CmdCakeUpdate, "x", CakePatch{ExpectedVersion: 1, Description: Optional[string]{Set: true, Null: true}})
	empty := BodyHash(CmdCakeUpdate, "x", CakePatch{ExpectedVersion: 1, Description: Optional[string]{Set: true}})
	if unset == null || null == empty || unset == empty {
		t.Fatal("absent, null and empty optional fields hash the same")
	}
	// The source hash ignores the expected version only.
	r1, r2 := validRecipe(), validRecipe()
	r2.ExpectedVersion = 5
	if r1.SourceHash() != r2.SourceHash() {
		t.Fatal("source hash depends on the expected version")
	}
	r2.PriceMinor++
	if r1.SourceHash() == r2.SourceHash() {
		t.Fatal("source hash ignores the price")
	}
}

func TestOptionalDecoding(t *testing.T) {
	var p CakePatch
	if err := json.Unmarshal([]byte(`{"expected_version":3,"description":null}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.Name.Set || !p.Description.Set || !p.Description.Null || p.ExpectedVersion != 3 {
		t.Fatalf("decoded %+v", p)
	}
	if err := json.Unmarshal([]byte(`{"name":"New"}`), &p); err != nil || !p.Name.Set || p.Name.Value != "New" {
		t.Fatalf("decoded %+v, %v", p, err)
	}
}

func TestCursor(t *testing.T) {
	ts := time.Date(2026, 11, 4, 12, 30, 0, 123456000, time.UTC)
	id := "11111111-1111-4111-8111-111111111111"
	gotT, gotID, err := decodeCursor(encodeCursor(ts, id))
	if err != nil || !gotT.Equal(ts) || *gotID != id {
		t.Fatalf("round trip = %v %v %v", gotT, gotID, err)
	}
	if a, b, err := decodeCursor(""); a != nil || b != nil || err != nil {
		t.Fatal("empty cursor is the first page")
	}
	for _, bad := range []string{"%%%", "bm90LWEtY3Vyc29y", encodeCursor(ts, "not-a-uuid"),
		"MjAyNi0xMS0wNHwxMTExMTExMS0xMTExLTQxMTEtODExMS0xMTExMTExMTExMTE", strings.Repeat("A", 200)} {
		if _, _, err := decodeCursor(bad); !errors.Is(err, ErrBadCursor) {
			t.Errorf("cursor %q: %v, want ErrBadCursor", bad, err)
		}
	}
	if pageSize(0) != DefaultPageSize || pageSize(500) != MaxPageSize || pageSize(7) != 7 {
		t.Fatal("page size bounds")
	}
}

func TestGatewayErrorMapping(t *testing.T) {
	pg := func(code, msg string) error { return &pgconn.PgError{Code: code, Message: msg} }
	if !errors.Is(gatewayError("x", pg("GK403", "no")), ErrForbidden) {
		t.Error("GK403")
	}
	if !errors.Is(gatewayError("x", pg("GK404", "no")), ErrNotFound) {
		t.Error("GK404")
	}
	for msg, want := range map[string]string{"stale_version": "stale_version", "key_reused": "key_reused",
		"duplicate": "duplicate", "recipe_version rows are immutable": "state_conflict"} {
		var c *ConflictError
		if err := gatewayError("x", pg("GK409", msg)); !errors.As(err, &c) || c.Code != want {
			t.Errorf("GK409 %q: %v, want %s", msg, err, want)
		}
	}
	var inv *InvalidError
	if err := gatewayError("x", pg("GK422", "flavour_unknown")); !errors.As(err, &inv) || inv.Field != "flavour_id" {
		t.Errorf("GK422: %v", err)
	}
	// A missing grant or a constraint failure is a fault, not a refusal.
	for _, code := range []string{"42501", "23514", "08006"} {
		err := gatewayError("x", pg(code, "detail"))
		var c *ConflictError
		if errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) || errors.As(err, &c) || errors.As(err, &inv) {
			t.Errorf("%s mapped to a refusal: %v", code, err)
		}
	}
}

func TestSizes(t *testing.T) {
	if len(Sizes) != 11 {
		t.Fatalf("sizes = %d", len(Sizes))
	}
	if s, ok := SizeByCode("s"); !ok || s.Diameter != "6 in" {
		t.Fatalf("size s = %+v", s)
	}
	if _, ok := SizeByCode("S"); ok {
		t.Fatal("size codes are case-sensitive")
	}
}
