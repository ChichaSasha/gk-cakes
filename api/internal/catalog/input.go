package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
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
	o.Set = true
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

// CakeInput creates a cake listing (a draft).
type CakeInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Validate checks the input.
func (in CakeInput) Validate() error {
	if err := text("name", in.Name, 1, 120, false); err != nil {
		return err
	}
	return text("description", in.Description, 0, 4000, true)
}

// CakePatch changes a cake's name and/or description.
type CakePatch struct {
	ExpectedVersion int              `json:"expected_version"`
	Name            Optional[string] `json:"name"`
	Description     Optional[string] `json:"description"`
}

// Validate checks the input. Neither field is nullable.
func (in CakePatch) Validate() error {
	if err := expectedVersion(in.ExpectedVersion); err != nil {
		return err
	}
	if !in.Name.Set && !in.Description.Set {
		return invalid("name", "name or description is required")
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
		return text("description", in.Description.Value, 0, 4000, true)
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
// the version (schema recipe_version.source_hash).
func (in RecipeVersionInput) SourceHash() string {
	content := in
	content.ExpectedVersion = 0
	return BodyHash("catalog.recipe_version", in.VariantID, content)
}
