package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"unicode/utf8"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
)

// Catalog serves the cake catalog (catalog.Store).
type Catalog interface {
	PublicPage(ctx context.Context, cursor string, limit int) (catalog.Page, error)
	PublicCake(ctx context.Context, id string) (catalog.Cake, error)
	StaffPage(ctx context.Context, subject, cursor string, limit int) (catalog.Page, error)
	StaffCake(ctx context.Context, subject, id string) (catalog.Cake, error)
	RecipeVersion(ctx context.Context, subject, id string) (catalog.RecipeVersion, error)
	Flavours(ctx context.Context, subject string) ([]catalog.Flavour, error)
	CurrentNotice(ctx context.Context, subject string) (catalog.Notice, error)
	CreateFlavour(ctx context.Context, subject, key string, in catalog.FlavourInput) (catalog.Result, error)
	CreateNotice(ctx context.Context, subject, key string, in catalog.NoticeInput) (catalog.Result, error)
	CreateCake(ctx context.Context, subject, key string, in catalog.CakeInput) (catalog.Result, error)
	UpdateCake(ctx context.Context, subject, key, cakeID string, in catalog.CakePatch) (catalog.Result, error)
	SetCakeState(ctx context.Context, subject, key, cakeID string, in catalog.CakeStateInput) (catalog.Result, error)
	CreateVariant(ctx context.Context, subject, key, cakeID string, in catalog.VariantInput) (catalog.Result, error)
	ArchiveVariant(ctx context.Context, subject, key, variantID string, in catalog.VersionInput) (catalog.Result, error)
	CreateRecipeVersion(ctx context.Context, subject, key string, in catalog.RecipeVersionInput) (catalog.Result, error)
}

// maxCommandBytes bounds a catalog command body.
const maxCommandBytes = 64 << 10

// catalogPublicRoutes registers the customer catalog reads (no sign-in).
func catalogPublicRoutes(mux *http.ServeMux, cat Catalog, logger *slog.Logger) {
	mux.HandleFunc("GET /api/v1/cakes", func(w http.ResponseWriter, r *http.Request) {
		limit, ok := pageLimit(w, r)
		if !ok {
			return
		}
		page, err := cat.PublicPage(r.Context(), r.URL.Query().Get("cursor"), limit)
		if err != nil {
			catalogError(w, r, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
	mux.HandleFunc("GET /api/v1/cakes/{id}", func(w http.ResponseWriter, r *http.Request) {
		cake, err := cat.PublicCake(r.Context(), r.PathValue("id"))
		if err != nil {
			catalogError(w, r, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, cake)
	})
}

// catalogStaffRoutes registers the chef catalog. Reads are open to the chef
// and technical support (bounded read access, K00); every change needs the
// chef capability. The database checks the capability again.
func catalogStaffRoutes(mux *http.ServeMux, cat Catalog, id Identity, logger *slog.Logger) {
	readers := []string{identity.Chef, identity.TechnicalSupport}
	chef := []string{identity.Chef}

	mux.HandleFunc("GET /api/v1/admin/cakes", staff(id, logger, readers, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		limit, ok := pageLimit(w, r)
		if !ok {
			return
		}
		page, err := cat.StaffPage(r.Context(), a.Subject, r.URL.Query().Get("cursor"), limit)
		respond(w, r, logger, http.StatusOK, page, err)
	}))
	mux.HandleFunc("GET /api/v1/admin/cakes/{id}", staff(id, logger, readers, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		cake, err := cat.StaffCake(r.Context(), a.Subject, r.PathValue("id"))
		respond(w, r, logger, http.StatusOK, cake, err)
	}))
	mux.HandleFunc("GET /api/v1/admin/recipes/versions/{id}", staff(id, logger, readers, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		v, err := cat.RecipeVersion(r.Context(), a.Subject, r.PathValue("id"))
		respond(w, r, logger, http.StatusOK, v, err)
	}))
	mux.HandleFunc("GET /api/v1/admin/flavours", staff(id, logger, readers, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		list, err := cat.Flavours(r.Context(), a.Subject)
		respond(w, r, logger, http.StatusOK, map[string]any{"flavours": list}, err)
	}))
	mux.HandleFunc("GET /api/v1/admin/allergen-notices/current", staff(id, logger, readers, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		n, err := cat.CurrentNotice(r.Context(), a.Subject)
		respond(w, r, logger, http.StatusOK, n, err)
	}))
	mux.HandleFunc("GET /api/v1/admin/sizes", staff(id, logger, readers, func(w http.ResponseWriter, _ *http.Request, _ identity.Actor) {
		writeJSON(w, http.StatusOK, map[string]any{"sizes": catalog.Sizes, "allergen_codes": catalog.AllergenCodes, "units": catalog.Units})
	}))

	mux.HandleFunc("POST /api/v1/admin/flavours", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in catalog.FlavourInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := cat.CreateFlavour(r.Context(), a.Subject, key, in)
		respondCommand(w, r, logger, http.StatusCreated, res, err, func() (any, error) {
			return catalog.Flavour{ID: res.ID, Name: in.Name}, nil
		})
	}))
	mux.HandleFunc("POST /api/v1/admin/allergen-notices", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in catalog.NoticeInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := cat.CreateNotice(r.Context(), a.Subject, key, in)
		respondCommand(w, r, logger, http.StatusCreated, res, err, func() (any, error) {
			return catalog.Notice{ID: res.ID, Text: in.Body}, nil
		})
	}))
	mux.HandleFunc("POST /api/v1/admin/cakes", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in catalog.CakeInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := cat.CreateCake(r.Context(), a.Subject, key, in)
		respondCommand(w, r, logger, http.StatusCreated, res, err, func() (any, error) {
			return cat.StaffCake(r.Context(), a.Subject, res.ID)
		})
	}))
	mux.HandleFunc("PATCH /api/v1/admin/cakes/{id}", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in catalog.CakePatch
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := cat.UpdateCake(r.Context(), a.Subject, key, r.PathValue("id"), in)
		respondCommand(w, r, logger, http.StatusOK, res, err, func() (any, error) {
			return cat.StaffCake(r.Context(), a.Subject, res.ID)
		})
	}))
	mux.HandleFunc("POST /api/v1/admin/cakes/{id}/state", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in catalog.CakeStateInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := cat.SetCakeState(r.Context(), a.Subject, key, r.PathValue("id"), in)
		respondCommand(w, r, logger, http.StatusOK, res, err, func() (any, error) {
			return cat.StaffCake(r.Context(), a.Subject, res.ID)
		})
	}))
	mux.HandleFunc("POST /api/v1/admin/cakes/{id}/variants", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in catalog.VariantInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		cakeID := r.PathValue("id")
		res, err := cat.CreateVariant(r.Context(), a.Subject, key, cakeID, in)
		respondCommand(w, r, logger, http.StatusCreated, res, err, func() (any, error) {
			cake, err := cat.StaffCake(r.Context(), a.Subject, cakeID)
			return map[string]any{"variant_id": res.ID, "cake": cake}, err
		})
	}))
	mux.HandleFunc("POST /api/v1/admin/variants/{id}/archive", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in catalog.VersionInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := cat.ArchiveVariant(r.Context(), a.Subject, key, r.PathValue("id"), in)
		respondCommand(w, r, logger, http.StatusOK, res, err, func() (any, error) {
			return map[string]any{"variant_id": res.ID, "archived": true}, nil
		})
	}))
	mux.HandleFunc("POST /api/v1/admin/recipes/versions", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in catalog.RecipeVersionInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := cat.CreateRecipeVersion(r.Context(), a.Subject, key, in)
		respondCommand(w, r, logger, http.StatusCreated, res, err, func() (any, error) {
			return cat.RecipeVersion(r.Context(), a.Subject, res.ID)
		})
	}))
}

var limitPattern = regexp.MustCompile(`^[0-9]{1,3}$`)

// pageLimit reads ?limit= (1 to 50, default 20).
func pageLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return catalog.DefaultPageSize, true
	}
	n, err := strconv.Atoi(raw)
	if !limitPattern.MatchString(raw) || err != nil || n < 1 || n > catalog.MaxPageSize {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "limit must be 1 to 50", false)
		return 0, false
	}
	return n, true
}

// validator is a pointer to a command input.
type validator interface{ Validate() error }

// readCommand checks the Idempotency-Key header and decodes a strict JSON
// body into in: one object, no unknown or repeated fields, at most
// maxCommandBytes. It writes the error response and returns false when the
// command cannot proceed.
func readCommand(w http.ResponseWriter, r *http.Request, in validator) (string, bool) {
	key := r.Header.Get("Idempotency-Key")
	if !catalog.ValidKey(key) {
		writeError(w, r, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED",
			"An Idempotency-Key header of 1 to 128 visible characters is required", false)
		return "", false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCommandBytes))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		writeError(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "Payload too large", false)
		return "", false
	case err != nil:
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Unreadable request body", false)
		return "", false
	}
	if !utf8.Valid(body) {
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "The request body must be UTF-8", false)
		return "", false
	}
	if err := decodeStrict(body, in); err != nil {
		var null *nullError
		if errors.As(err, &null) {
			// K00: explicit null clears only named nullable fields; no
			// catalog command has one.
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", null.field+" cannot be null", false)
			return "", false
		}
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "The request body must be one JSON object with known fields", false)
		return "", false
	}
	if err := in.Validate(); err != nil {
		var inv *catalog.InvalidError
		if errors.As(err, &inv) {
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", inv.Field+" "+inv.Message, false)
			return "", false
		}
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "The request is not valid", false)
		return "", false
	}
	return key, true
}

// decodeStrict decodes exactly one JSON object with no unknown fields, no
// repeated keys at any depth, no null value and nothing after it. Keys must
// be lower_snake_case ASCII (K00): encoding/json matches field names without
// regard to case, so "NAME" would otherwise reach the "name" field.
func decodeStrict(body []byte, v any) error {
	if err := noDuplicateKeys(body); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing data after the JSON object")
	}
	return nil
}

// nullError: the body has an explicit null at field.
type nullError struct{ field string }

func (e *nullError) Error() string { return "null value for " + e.field }

var wireKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// noDuplicateKeys rejects a JSON document that is not an object, repeats a
// key in any object, uses a key that is not lower_snake_case or holds a null.
func noDuplicateKeys(body []byte) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	first, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := first.(json.Delim); !ok || d != '{' {
		return errors.New("not a JSON object")
	}
	return walkObject(dec, 1, "")
}

const maxJSONDepth = 16

func walkObject(dec *json.Decoder, depth int, _ string) error {
	if depth > maxJSONDepth {
		return errors.New("JSON nested too deeply")
	}
	seen := map[string]bool{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		k, ok := t.(string)
		if !ok {
			return errors.New("object key is not a string")
		}
		if !wireKey.MatchString(k) {
			return errors.New("key " + strconv.Quote(k) + " is not lower_snake_case")
		}
		if seen[k] {
			return errors.New("repeated key " + strconv.Quote(k))
		}
		seen[k] = true
		if err := walkValue(dec, depth, k); err != nil {
			return err
		}
	}
	_, err := dec.Token() // '}'
	return err
}

func walkValue(dec *json.Decoder, depth int, field string) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	if t == nil {
		return &nullError{field: field}
	}
	d, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch d {
	case '{':
		return walkObject(dec, depth+1, field)
	case '[':
		if depth+1 > maxJSONDepth {
			return errors.New("JSON nested too deeply")
		}
		for dec.More() {
			if err := walkValue(dec, depth+1, field); err != nil {
				return err
			}
		}
		_, err := dec.Token() // ']'
		return err
	}
	return errors.New("unexpected JSON delimiter")
}

func respond(w http.ResponseWriter, r *http.Request, logger *slog.Logger, status int, v any, err error) {
	if err != nil {
		catalogError(w, r, logger, err)
		return
	}
	writeJSON(w, status, v)
}

// respondCommand answers a command: the representation from read on
// success (a replay answers with the same status and marks itself), or the
// refusal.
func respondCommand(w http.ResponseWriter, r *http.Request, logger *slog.Logger, status int, res catalog.Result,
	err error, read func() (any, error)) {
	if err != nil {
		catalogError(w, r, logger, err)
		return
	}
	v, err := read()
	if err != nil {
		catalogError(w, r, logger, err)
		return
	}
	if res.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(w, status, v)
}

var conflictMessages = map[string]string{
	"stale_version":  "This was changed by someone else. Reload and try again.",
	"key_reused":     "This Idempotency-Key was already used for a different request",
	"state_conflict": "This change isn't possible in the current state",
	"duplicate":      "This already exists",
	// Quotes: what the customer chose changed since it was shown; choose again.
	"item_unavailable": "A cake in your basket is no longer available as chosen. Please check your basket.",
	"slot_unavailable": "That time is no longer available. Please choose another time.",
	// Checkout: the reviewed quote can no longer be accepted as it is.
	"price_changed":  "Something in your quote changed. Please review the new quote.",
	"quote_expired":  "This quote has expired. Please review a new quote.",
	"policy_changed": "Our terms changed since you reviewed them. Please review them again.",
	"code_used_up":   "This code has been used up.",
}

// catalogError maps catalog errors to K00 responses without SQL detail.
func catalogError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var conflict *catalog.ConflictError
	var inv *catalog.InvalidError
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Not found", false)
	case errors.Is(err, catalog.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "You don't have access to this", false)
	case errors.Is(err, catalog.ErrBadCursor):
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "The page cursor is not valid", false)
	case errors.As(err, &conflict):
		msg, ok := conflictMessages[conflict.Code]
		if !ok {
			msg = conflictMessages["state_conflict"]
		}
		code := "CONFLICT"
		switch conflict.Code {
		case "stale_version":
			code = "STALE_VERSION"
		case "key_reused":
			code = "KEY_REUSED"
		case "duplicate":
			code = "DUPLICATE"
		case "item_unavailable":
			code = "ITEM_UNAVAILABLE"
		case "slot_unavailable":
			code = "SLOT_UNAVAILABLE"
		case "price_changed":
			code = "PRICE_CHANGED"
		case "quote_expired":
			code = "QUOTE_EXPIRED"
		case "policy_changed":
			code = "POLICY_CHANGED"
		case "code_used_up":
			code = "CODE_USED_UP"
		}
		writeError(w, r, http.StatusConflict, code, msg, false)
	case errors.As(err, &inv):
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", inv.Field+" "+inv.Message, false)
	default:
		logger.Warn("catalog request failed", "request_id", requestID(r.Context()), "error", err.Error())
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Service temporarily unavailable", true)
	}
}
