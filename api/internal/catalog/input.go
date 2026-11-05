package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"regexp"
	"strings"
)

// Command names, as stored in the command receipt (K00 command identity).
const (
	CmdFlavourCreate       = "catalog.flavour_create"
	CmdNoticeCreate        = "catalog.notice_create"
	CmdCakeCreate          = "catalog.cake_create"
	CmdCakeUpdate          = "catalog.cake_update"
	CmdCakeSetState        = "catalog.cake_set_state"
	CmdVariantCreate       = "catalog.variant_create"
	CmdVariantArchive      = "catalog.variant_archive"
	CmdRecipeVersionCreate = "catalog.recipe_version_create"
	CmdColourCreate        = "catalog.colour_create"
	CmdColourUpdate        = "catalog.colour_update"
)

// BodyHash is the SHA-256 of a command's canonical input: its name, the
// path resource and the decoded body re-encoded (struct fields in declared
// order, so the same input always hashes the same).
func BodyHash(command, resource string, body any) string {
	b, err := json.Marshal(struct {
		Command  string `json:"command"`
		Resource string `json:"resource"`
		Body     any    `json:"body"`
	}{command, resource, body})
	if err != nil {
		panic("catalog: command body is not encodable: " + err.Error()) // inputs are plain structs
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Optional is a PATCH field: Set when present in the body, Null when it was
// an explicit null.
type Optional[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// UnmarshalJSON records presence and null.
func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	var zero T
	o.Set, o.Null, o.Value = true, false, zero
	if string(b) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.Value)
}

// MarshalJSON encodes the canonical form used for hashing.
func (o Optional[T]) MarshalJSON() ([]byte, error) {
	switch {
	case !o.Set:
		return []byte(`{"set":false}`), nil
	case o.Null:
		return []byte(`{"set":true,"null":true}`), nil
	}
	v, err := json.Marshal(o.Value)
	if err != nil {
		return nil, err
	}
	return append(append([]byte(`{"set":true,"value":`), v...), '}'), nil
}

// FlavourInput creates a flavour.
type FlavourInput struct {
	Name string `json:"name"`
}

// Validate checks the input.
func (in FlavourInput) Validate() error { return text("name", in.Name, 1, 80, false) }

// NoticeInput creates a new shared-kitchen allergen notice version.
type NoticeInput struct {
	Body string `json:"body"`
}

// Validate checks the input.
func (in NoticeInput) Validate() error { return text("body", in.Body, 1, 2000, true) }

// CakeInput creates a cake listing (a draft). ColourID is optional (absent
// or null: no colour). It is omitted from the canonical body when nil, so a
// body without a colour hashes as it did before colours existed.
type CakeInput struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	ColourID    *string `json:"colour_id,omitempty"`
}

// NullableFields names the body fields that may be an explicit null.
func (CakeInput) NullableFields() []string { return []string{"colour_id"} }

// Validate checks the input.
func (in CakeInput) Validate() error {
	if err := text("name", in.Name, 1, 120, false); err != nil {
		return err
	}
	if err := text("description", in.Description, 0, 4000, true); err != nil {
		return err
	}
	if in.ColourID != nil {
		return uuidField("colour_id", *in.ColourID)
	}
	return nil
}

// CakePatch changes a cake's name, description and/or colour. ColourID null
// clears the colour; absent leaves it unchanged (omitted from the canonical
// body, so a patch without it hashes as before colours existed).
type CakePatch struct {
	ExpectedVersion int              `json:"expected_version"`
	Name            Optional[string] `json:"name"`
	Description     Optional[string] `json:"description"`
	ColourID        Optional[string] `json:"colour_id,omitzero"`
}

// NullableFields names the body fields that may be an explicit null.
func (CakePatch) NullableFields() []string { return []string{"colour_id"} }

// Validate checks the input. Only colour_id is nullable.
func (in CakePatch) Validate() error {
	if err := expectedVersion(in.ExpectedVersion); err != nil {
		return err
	}
	if !in.Name.Set && !in.Description.Set && !in.ColourID.Set {
		return invalid("name", "name, description or colour_id is required")
	}
	if in.Name.Null {
		return invalid("name", "cannot be null")
	}
	if in.Description.Null {
		return invalid("description", "cannot be null")
	}
	if in.Name.Set {
		if err := text("name", in.Name.Value, 1, 120, false); err != nil {
			return err
		}
	}
	if in.Description.Set {
		if err := text("description", in.Description.Value, 0, 4000, true); err != nil {
			return err
		}
	}
	if in.ColourID.Set && !in.ColourID.Null {
		return uuidField("colour_id", in.ColourID.Value)
	}
	return nil
}

// Colour rules (approved GK-016 r01c sketch, K01 amendment 2026-11-05).
const (
	MaxColourLabel    = 40
	MaxColourPosition = 10000
)

// colourCodePattern: a slug of 1 to 40 characters; "all" is the shop
// filter's "All colours" value and is refused separately.
var colourCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// ValidColourCode reports whether s can be a colour code.
func ValidColourCode(s string) bool { return colourCodePattern.MatchString(s) && s != "all" }

func colourLabel(s string) error {
	if err := text("label", s, 1, MaxColourLabel, false); err != nil {
		return err
	}
	// The shop also trims U+FEFF (JavaScript's trim), which Go does not.
	if strings.Trim(s, "\ufeff") != s {
		return invalid("label", "must not start or end with spaces")
	}
	return nil
}

func colourPosition(p int) error {
	if p < 0 || p > MaxColourPosition {
		return invalid("position", "must be a whole number from 0 to %d", MaxColourPosition)
	}
	return nil
}

// ColourInput creates a colour. Position is required (0 is a valid
// position, so an absent one is refused, not read as 0).
type ColourInput struct {
	Code     string `json:"code"`
	Label    string `json:"label"`
	Position *int   `json:"position"`
}

// Validate checks the input.
func (in ColourInput) Validate() error {
	if !ValidColourCode(in.Code) {
		return invalid("code", "must be 1 to 40 lowercase letters, digits or hyphens, starting with a letter or digit, and not \"all\"")
	}
	if err := colourLabel(in.Label); err != nil {
		return err
	}
	if in.Position == nil {
		return invalid("position", "is required")
	}
	return colourPosition(*in.Position)
}

// ColourPatch changes a colour's label or position, or archives (true) or
// restores (false) it. The code never changes. No field is nullable.
type ColourPatch struct {
	ExpectedVersion int              `json:"expected_version"`
	Label           Optional[string] `json:"label"`
	Position        Optional[int]    `json:"position"`
	Archived        Optional[bool]   `json:"archived"`
}

// Validate checks the input.
func (in ColourPatch) Validate() error {
	if err := expectedVersion(in.ExpectedVersion); err != nil {
		return err
	}
	if !in.Label.Set && !in.Position.Set && !in.Archived.Set {
		return invalid("label", "label, position or archived is required")
	}
	switch {
	case in.Label.Null:
		return invalid("label", "cannot be null")
	case in.Position.Null:
		return invalid("position", "cannot be null")
	case in.Archived.Null:
		return invalid("archived", "cannot be null")
	}
	if in.Label.Set {
		if err := colourLabel(in.Label.Value); err != nil {
			return err
		}
	}
	if in.Position.Set {
		return colourPosition(in.Position.Value)
	}
	return nil
}

// Cake states a chef can set.
const (
	StateActive   = "active"
	StateArchived = "archived"
)

// CakeStateInput publishes, archives or restores a cake.
type CakeStateInput struct {
	ExpectedVersion int    `json:"expected_version"`
	State           string `json:"state"`
}

// Validate checks the input.
func (in CakeStateInput) Validate() error {
	if err := expectedVersion(in.ExpectedVersion); err != nil {
		return err
	}
	return oneOf("state", in.State, []string{StateActive, StateArchived})
}

// VariantInput adds a variant to a cake.
type VariantInput struct {
	FlavourID string `json:"flavour_id"`
	SizeCode  string `json:"size_code"`
	StockMode string `json:"stock_mode"`
}

// Validate checks the input.
func (in VariantInput) Validate() error {
	if err := uuidField("flavour_id", in.FlavourID); err != nil {
		return err
	}
	if _, ok := SizeByCode(in.SizeCode); !ok {
		codes := make([]string, len(Sizes))
		for i, s := range Sizes {
			codes[i] = s.Code
		}
		return invalid("size_code", "must be one of %s", strings.Join(codes, ", "))
	}
	return oneOf("stock_mode", in.StockMode, []string{MadeToOrder, ReadyStock})
}

// VersionInput carries an expected version only (archive).
type VersionInput struct {
	ExpectedVersion int `json:"expected_version"`
}

// Validate checks the input.
func (in VersionInput) Validate() error { return expectedVersion(in.ExpectedVersion) }

// Allergen is one "contains" declaration of a recipe version.
type Allergen struct {
	Code      string `json:"code"`
	Label     string `json:"label"`
	Prominent bool   `json:"prominent"`
}

// Line is one planned recipe quantity (K07): an exact decimal string with at
// most two decimals; counts are whole.
type Line struct {
	Component string `json:"component"`
	Quantity  string `json:"quantity"`
	Unit      string `json:"unit"`
}

// RecipeVersionInput creates the next immutable recipe version of a variant
// and makes it current.
type RecipeVersionInput struct {
	VariantID       string     `json:"variant_id"`
	ExpectedVersion int        `json:"expected_version"`
	PriceMinor      int64      `json:"price_minor"`
	ServingsMin     int        `json:"servings_min"`
	ServingsMax     int        `json:"servings_max"`
	Coating         string     `json:"coating"`
	Composition     []string   `json:"composition"`
	NoticeID        string     `json:"allergen_notice_id"`
	Allergens       []Allergen `json:"allergens"`
	Lines           []Line     `json:"lines"`
}

// MaxPriceMinor bounds a single cake price (£100,000).
const MaxPriceMinor = 10_000_000

// Validate checks the input.
func (in RecipeVersionInput) Validate() error {
	if err := uuidField("variant_id", in.VariantID); err != nil {
		return err
	}
	if err := expectedVersion(in.ExpectedVersion); err != nil {
		return err
	}
	if in.PriceMinor < 1 || in.PriceMinor > MaxPriceMinor {
		return invalid("price_minor", "must be a whole number of pence from 1 to %d", MaxPriceMinor)
	}
	if in.ServingsMin < 1 || in.ServingsMin > 500 {
		return invalid("servings_min", "must be 1 to 500")
	}
	if in.ServingsMax < in.ServingsMin || in.ServingsMax > 500 {
		return invalid("servings_max", "must be from servings_min to 500")
	}
	if err := text("coating", in.Coating, 1, 200, false); err != nil {
		return err
	}
	if len(in.Composition) < 1 || len(in.Composition) > 20 {
		return invalid("composition", "must list 1 to 20 components")
	}
	for _, c := range in.Composition {
		if err := text("composition", c, 1, 120, false); err != nil {
			return err
		}
	}
	if err := uuidField("allergen_notice_id", in.NoticeID); err != nil {
		return err
	}
	// Missing allergen disclosure disables buying (K01): a version always
	// declares what it contains.
	if len(in.Allergens) < 1 || len(in.Allergens) > len(AllergenCodes) {
		return invalid("allergens", "must declare 1 to %d allergens", len(AllergenCodes))
	}
	seen := map[string]bool{}
	for _, a := range in.Allergens {
		if err := oneOf("allergens", a.Code, AllergenCodes); err != nil {
			return err
		}
		if seen[a.Code] {
			return invalid("allergens", "must not repeat %s", a.Code)
		}
		seen[a.Code] = true
		if err := text("allergens", a.Label, 1, 80, false); err != nil {
			return err
		}
	}
	if len(in.Lines) > 100 {
		return invalid("lines", "must have at most 100 lines")
	}
	for _, l := range in.Lines {
		if err := text("lines", l.Component, 1, 120, false); err != nil {
			return err
		}
		if err := oneOf("lines", l.Unit, Units); err != nil {
			return err
		}
		if !quantityPattern.MatchString(l.Quantity) {
			return invalid("lines", "quantity must be a decimal with at most 2 decimals")
		}
		q, ok := new(big.Rat).SetString(l.Quantity)
		if !ok || q.Sign() <= 0 {
			return invalid("lines", "quantity must be more than 0")
		}
		if l.Unit == "count" && !q.IsInt() {
			return invalid("lines", "a count must be a whole number")
		}
	}
	return nil
}

// SourceHash is the SHA-256 of the version's canonical content, stored with
// the version (schema recipe_version.source_hash). Omitted and empty lists
// hash the same.
func (in RecipeVersionInput) SourceHash() string {
	content := in.normalized()
	content.ExpectedVersion = 0
	return BodyHash("catalog.recipe_version", in.VariantID, content)
}

func (in RecipeVersionInput) normalized() RecipeVersionInput {
	if in.Lines == nil {
		in.Lines = []Line{}
	}
	if in.Composition == nil {
		in.Composition = []string{}
	}
	if in.Allergens == nil {
		in.Allergens = []Allergen{}
	}
	return in
}
