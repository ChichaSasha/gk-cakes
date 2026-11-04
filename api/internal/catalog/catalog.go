// Package catalog serves the cake catalog (contracts r6.5 K01, K07): cakes
// (listings), purchasable variants (one flavour in one size) and their
// immutable recipe versions, through the database gateway (migration
// 0009_catalog.sql). The database authorizes and versions every change; this
// package validates input, builds the K00 command identity and maps gateway
// refusals to typed errors.
package catalog

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Size is a GK-002 cake size. Servings and price belong to each recipe
// version (entered by the chef); the size names what is baked.
type Size struct {
	Code     string `json:"code"`
	Label    string `json:"label"`
	Diameter string `json:"diameter"`
}

// Sizes lists the sizes in display order (docs/creator/GK-002, base price
// list 2026-10-05: single tiers and tiered "from" sizes).
var Sizes = []Size{
	{"mini", "Mini", "5 in"},
	{"s", "S", "6 in"},
	{"m", "M", "7 in"},
	{"l", "L", "8 in"},
	{"xl", "XL", "10 in"},
	{"xxl", "XXL", "12 in"},
	{"tiers_4_7", "2 tiers", "4 + 7 in"},
	{"tiers_6_8", "2 tiers", "6 + 8 in"},
	{"tiers_8_10", "2 tiers", "8 + 10 in"},
	{"tiers_4_6_8", "3 tiers", "4 + 6 + 8 in"},
	{"tiers_6_9_11", "3 tiers", "6 + 9 + 11 in"},
}

// SizeByCode returns the size with code, or false.
func SizeByCode(code string) (Size, bool) {
	for _, s := range Sizes {
		if s.Code == code {
			return s, true
		}
	}
	return Size{}, false
}

// Stock modes of a variant.
const (
	MadeToOrder = "made_to_order"
	ReadyStock  = "ready_stock"
)

// AllergenCodes are the 14 allergens of UK FIC Annex II. Labels are the exact
// text the chef approved (for example "CEREALS CONTAINING GLUTEN (WHEAT)").
var AllergenCodes = []string{
	"celery", "cereals_containing_gluten", "crustaceans", "eggs", "fish", "lupin", "milk",
	"molluscs", "mustard", "tree_nuts", "peanuts", "sesame", "soybeans", "sulphites",
}

// Units of a recipe line (K07). Counts are whole.
var Units = []string{"g", "ml", "count"}

// Currency of every price (K00: GBP pence).
const Currency = "GBP"

// Limits of the public and staff pages (K01).
const (
	DefaultPageSize = 20
	MaxPageSize     = 50
)

// Errors returned by the store.
var (
	// ErrForbidden: the actor lacks the capability (GK403).
	ErrForbidden = errors.New("catalog: forbidden")
	// ErrNotFound: the object does not exist or is not visible (GK404).
	ErrNotFound = errors.New("catalog: not found")
	// ErrBadCursor: the page cursor is not one this API issued.
	ErrBadCursor = errors.New("catalog: bad cursor")
)

// ConflictError is a GK409 refusal: stale_version, key_reused,
// state_conflict or duplicate.
type ConflictError struct{ Code string }

func (e *ConflictError) Error() string { return "catalog: conflict: " + e.Code }

// InvalidError is input the API or the database refused (GK422). Field names
// the offending input when known; Message is safe to show.
type InvalidError struct {
	Field   string
	Message string
}

func (e *InvalidError) Error() string { return "catalog: invalid " + e.Field + ": " + e.Message }

func invalid(field, format string, args ...any) error {
	return &InvalidError{Field: field, Message: fmt.Sprintf(format, args...)}
}

var (
	uuidPattern     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	keyPattern      = regexp.MustCompile(`^[\x21-\x7e]{1,128}$`)
	quantityPattern = regexp.MustCompile(`^[0-9]{1,12}(\.[0-9]{1,2})?$`)
)

// ValidUUID reports whether s is a lowercase canonical UUID.
func ValidUUID(s string) bool { return uuidPattern.MatchString(s) }

// ValidKey reports whether s is a usable Idempotency-Key: 1 to 128 visible
// ASCII characters.
func ValidKey(s string) bool { return keyPattern.MatchString(s) }

// text checks a trimmed single-line or multi-line text field of 1..max
// characters (min 0 allows empty).
func text(field, s string, min, max int, multiline bool) error {
	if !utf8.ValidString(s) {
		return invalid(field, "must be valid text")
	}
	if s != strings.TrimSpace(s) {
		return invalid(field, "must not start or end with spaces")
	}
	n := utf8.RuneCountInString(s)
	if n < min || n > max {
		if min > 0 {
			return invalid(field, "must be %d to %d characters", min, max)
		}
		return invalid(field, "must be at most %d characters", max)
	}
	for _, r := range s {
		if r == '\n' && multiline {
			continue
		}
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return invalid(field, "must not contain control characters")
		}
	}
	return nil
}

func oneOf(field, s string, allowed []string) error {
	for _, a := range allowed {
		if s == a {
			return nil
		}
	}
	return invalid(field, "must be one of %s", strings.Join(allowed, ", "))
}

func uuidField(field, s string) error {
	if !ValidUUID(s) {
		return invalid(field, "must be an ID")
	}
	return nil
}

func expectedVersion(v int) error {
	if v < 1 {
		return invalid("expected_version", "is required")
	}
	return nil
}
