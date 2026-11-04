// Package checkout serves discount codes and storefront quotes (contracts
// r6.5 K02, K03, K04) through the database gateway (migration
// 0011_checkout_quotes.sql). The database prices and authorizes; this
// package validates input, derives the guest proof digest and maps refusals
// to the catalog error types the HTTP layer answers.
package checkout

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Command names.
const (
	CmdCodeCreate  = "discount.code_create"
	CmdCodeRevise  = "discount.code_revise"
	CmdCodeArchive = "discount.code_archive"
	CmdQuoteCreate = "checkout.quote_create"
)

// Code kinds (GK-007 discount_types).
const (
	KindPercentage   = "percentage"
	KindFreeDelivery = "free_delivery"
)

// Limits of a basket (K03 storefront quotes).
const (
	MaxLines = 20
	MaxCakes = 20
)

// Querier is satisfied by *pgxpool.Pool and *pgx.Conn.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Store serves codes and quotes (gk_api login).
type Store struct {
	DB  Querier
	Now func() time.Time
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

var codePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{2,31}$`)

func invalid(field, format string, args ...any) error {
	return &catalog.InvalidError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// NormalizeCode is a code as the database stores it: trimmed, upper case.
func NormalizeCode(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// GuestProofDigest decodes a guest proof (256 random bits, unpadded
// base64url: 43 characters) and returns its SHA-256 digest; only the digest
// is stored (K02).
func GuestProofDigest(proof string) ([]byte, error) {
	if len(proof) != 43 {
		return nil, invalid("guest_proof", "must be 32 random bytes in unpadded base64url")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(proof)
	if err != nil || len(raw) != 32 {
		return nil, invalid("guest_proof", "must be 32 random bytes in unpadded base64url")
	}
	sum := sha256.Sum256(raw)
	return sum[:], nil
}

// CodeTerms are the terms of a code version.
type CodeTerms struct {
	Kind             string `json:"kind"`
	BasisPoints      *int   `json:"basis_points,omitempty"`
	ExpiresAt        string `json:"expires_at,omitempty"`
	TotalLimit       *int   `json:"total_limit,omitempty"`
	PerCustomerLimit *int   `json:"per_customer_limit,omitempty"`
}

func (t CodeTerms) validate() error {
	switch t.Kind {
	case KindPercentage:
		if t.BasisPoints == nil || *t.BasisPoints < 1 || *t.BasisPoints > 10000 {
			return invalid("basis_points", "a percentage code needs 1 to 10000 basis points (0.01%% to 100%%)")
		}
	case KindFreeDelivery:
		if t.BasisPoints != nil {
			return invalid("basis_points", "a free-delivery code has no basis points")
		}
	default:
		return invalid("kind", "must be percentage or free_delivery")
	}
	if t.ExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339, t.ExpiresAt); err != nil || len(t.ExpiresAt) > 40 {
			return invalid("expires_at", "must be an RFC3339 instant with an offset")
		}
	}
	if t.TotalLimit != nil && (*t.TotalLimit < 1 || *t.TotalLimit > 1_000_000) {
		return invalid("total_limit", "must be 1 to 1000000")
	}
	if t.PerCustomerLimit != nil && (*t.PerCustomerLimit < 1 || *t.PerCustomerLimit > 1000) {
		return invalid("per_customer_limit", "must be 1 to 1000")
	}
	return nil
}

func (t CodeTerms) expires() *time.Time {
	if t.ExpiresAt == "" {
		return nil
	}
	e, _ := time.Parse(time.RFC3339, t.ExpiresAt)
	return &e
}

// CodeInput creates a discount code.
type CodeInput struct {
	Code string `json:"code"`
	CodeTerms
}

// Validate checks the input.
func (in CodeInput) Validate() error {
	if !codePattern.MatchString(NormalizeCode(in.Code)) {
		return invalid("code", "must be 3 to 32 letters, digits, - or _, starting with a letter or digit")
	}
	return in.validate()
}

// CodeRevision publishes new terms for a code.
type CodeRevision struct {
	ExpectedVersion int `json:"expected_version"`
	CodeTerms
}

// Validate checks the input.
func (in CodeRevision) Validate() error {
	if in.ExpectedVersion < 1 || in.ExpectedVersion > catalog.MaxVersion {
		return invalid("expected_version", "is required")
	}
	return in.validate()
}

// QuoteLine is one basket line: a made-to-order variant or a ready-made
// cake lot.
type QuoteLine struct {
	VariantID string `json:"variant_id,omitempty"`
	LotID     string `json:"lot_id,omitempty"`
	Quantity  int    `json:"quantity"`
}

// QuoteInput asks for a quote. GuestProof is required unless signed in; it
// is identity, not part of the command body.
type QuoteInput struct {
	Date            string      `json:"date"`
	SlotID          string      `json:"slot_id"`
	FulfillmentMode string      `json:"fulfillment_mode"`
	Lines           []QuoteLine `json:"lines"`
	DiscountCode    string      `json:"discount_code,omitempty"`
	GuestProof      string      `json:"guest_proof,omitempty"`
}

// Validate checks the input.
func (in QuoteInput) Validate() error {
	if _, err := schedule.ParseDate("date", in.Date); err != nil {
		return err
	}
	if !catalog.ValidUUID(in.SlotID) {
		return invalid("slot_id", "must be a slot ID from the schedule")
	}
	switch in.FulfillmentMode {
	case schedule.Collection, schedule.CustomerCourier, schedule.GKDelivery:
	default:
		return invalid("fulfillment_mode", "must be collection, customer_courier or gk_delivery")
	}
	if len(in.Lines) < 1 || len(in.Lines) > MaxLines {
		return invalid("lines", "must have 1 to %d lines", MaxLines)
	}
	total := 0
	seen := map[string]bool{}
	for _, l := range in.Lines {
		if (l.VariantID == "") == (l.LotID == "") {
			return invalid("lines", "each line needs either variant_id or lot_id")
		}
		id := l.VariantID + l.LotID
		if !catalog.ValidUUID(id) {
			return invalid("lines", "variant_id and lot_id must be IDs")
		}
		if seen[id] {
			return invalid("lines", "one line per variant or ready cake")
		}
		seen[id] = true
		if l.Quantity < 1 || l.Quantity > MaxCakes {
			return invalid("lines", "quantity must be 1 to %d", MaxCakes)
		}
		total += l.Quantity
	}
	if total > MaxCakes {
		return invalid("lines", "at most %d cakes an order", MaxCakes)
	}
	if in.DiscountCode != "" && !codePattern.MatchString(NormalizeCode(in.DiscountCode)) {
		return invalid("discount_code", "is not a valid code")
	}
	return nil
}

// commandBody is what the quote's body hash covers: everything but the
// guest proof.
func (in QuoteInput) commandBody() QuoteInput {
	in.GuestProof = ""
	in.DiscountCode = NormalizeCode(in.DiscountCode)
	return in
}

// Quote is a stored quote as JSON (the gateway builds it).
type Quote = json.RawMessage

// CreateQuote prices a basket. subject is the signed-in customer's verified
// subject, or "" for a guest with digest.
func (s *Store) CreateQuote(ctx context.Context, subject string, digest []byte, key string, in QuoteInput) (catalog.Result, error) {
	type line struct {
		VariantID *string `json:"variant_id,omitempty"`
		LotID     *string `json:"lot_id,omitempty"`
		Quantity  int     `json:"quantity"`
	}
	lines := make([]line, len(in.Lines))
	for i, l := range in.Lines {
		lines[i].Quantity = l.Quantity
		if l.VariantID != "" {
			lines[i].VariantID = &in.Lines[i].VariantID
		} else {
			lines[i].LotID = &in.Lines[i].LotID
		}
	}
	raw, err := json.Marshal(lines)
	if err != nil {
		return catalog.Result{}, err
	}
	var owner *string
	if subject != "" {
		owner, digest = &subject, nil
	}
	var code *string
	if in.DiscountCode != "" {
		c := NormalizeCode(in.DiscountCode)
		code = &c
	}
	var r catalog.Result
	err = s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.quote_create(
		$1::uuid, $2, $3, $4, $5::date, $6::uuid, $7, $8::json, $9, $10)`,
		owner, digest, key, catalog.BodyHash(CmdQuoteCreate, "", in.commandBody()), in.Date, in.SlotID,
		in.FulfillmentMode, string(raw), code, s.now().UTC()).Scan(&r.ID, &r.Replayed)
	if err != nil {
		return catalog.Result{}, gatewayError("quote", err)
	}
	return r, nil
}

// GetQuote returns a quote for its owner or guest proof holder, or
// ErrNotFound.
func (s *Store) GetQuote(ctx context.Context, id, subject string, digest []byte) (Quote, error) {
	if !catalog.ValidUUID(id) || (subject == "" && digest == nil) {
		return nil, catalog.ErrNotFound
	}
	var owner *string
	if subject != "" {
		owner = &subject
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT business.quote_get($1::uuid, $2::uuid, $3)`, id, owner, digest).Scan(&raw); err != nil {
		return nil, gatewayError("quote", err)
	}
	if raw == nil {
		return nil, catalog.ErrNotFound
	}
	return Quote(raw), nil
}

// Code is a discount code with its current terms.
type Code struct {
	ID               string     `json:"id"`
	Code             string     `json:"code"`
	Version          int        `json:"version"`
	ArchivedAt       *time.Time `json:"archived_at"`
	VersionID        string     `json:"version_id"`
	Kind             string     `json:"kind"`
	BasisPoints      *int       `json:"basis_points"`
	ExpiresAt        *time.Time `json:"expires_at"`
	TotalLimit       *int       `json:"total_limit"`
	PerCustomerLimit *int       `json:"per_customer_limit"`
	TermsSince       time.Time  `json:"terms_since"`
}

// Codes lists discount codes for staff.
func (s *Store) Codes(ctx context.Context, subject string) ([]Code, error) {
	rows, err := s.DB.Query(ctx, `SELECT code_id::text, code, code_version, archived_at, version_id::text, kind,
		basis_points, expires_at, total_limit, per_customer_limit, version_created_at
		FROM business.discount_staff_list($1::uuid, NULL)`, subject)
	if err != nil {
		return nil, gatewayError("codes", err)
	}
	codes, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Code, error) {
		var c Code
		return c, r.Scan(&c.ID, &c.Code, &c.Version, &c.ArchivedAt, &c.VersionID, &c.Kind, &c.BasisPoints,
			&c.ExpiresAt, &c.TotalLimit, &c.PerCustomerLimit, &c.TermsSince)
	})
	if err != nil {
		return nil, gatewayError("codes", err)
	}
	if codes == nil {
		codes = []Code{}
	}
	return codes, nil
}

// CodeByID returns one code, or ErrNotFound.
func (s *Store) CodeByID(ctx context.Context, subject, id string) (Code, error) {
	if !catalog.ValidUUID(id) {
		return Code{}, catalog.ErrNotFound
	}
	rows, err := s.DB.Query(ctx, `SELECT code_id::text, code, code_version, archived_at, version_id::text, kind,
		basis_points, expires_at, total_limit, per_customer_limit, version_created_at
		FROM business.discount_staff_list($1::uuid, $2::uuid)`, subject, id)
	if err != nil {
		return Code{}, gatewayError("code", err)
	}
	codes, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Code, error) {
		var c Code
		return c, r.Scan(&c.ID, &c.Code, &c.Version, &c.ArchivedAt, &c.VersionID, &c.Kind, &c.BasisPoints,
			&c.ExpiresAt, &c.TotalLimit, &c.PerCustomerLimit, &c.TermsSince)
	})
	if err != nil {
		return Code{}, gatewayError("code", err)
	}
	if len(codes) != 1 {
		return Code{}, catalog.ErrNotFound
	}
	return codes[0], nil
}

// CreateCode creates a discount code.
func (s *Store) CreateCode(ctx context.Context, subject, key string, in CodeInput) (catalog.Result, error) {
	in.Code = NormalizeCode(in.Code)
	return s.command(ctx, CmdCodeCreate, `SELECT result_id::text, replayed FROM business.discount_code_create(
		$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9)`,
		subject, key, catalog.BodyHash(CmdCodeCreate, "", in), in.Code, in.Kind, in.BasisPoints, in.expires(),
		in.TotalLimit, in.PerCustomerLimit)
}

// ReviseCode publishes new terms for a code.
func (s *Store) ReviseCode(ctx context.Context, subject, key, codeID string, in CodeRevision) (catalog.Result, error) {
	if !catalog.ValidUUID(codeID) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	return s.command(ctx, CmdCodeRevise, `SELECT result_id::text, replayed FROM business.discount_code_revise(
		$1::uuid, $2, $3, $4::uuid, $5, $6, $7, $8, $9, $10)`,
		subject, key, catalog.BodyHash(CmdCodeRevise, codeID, in), codeID, in.ExpectedVersion, in.Kind, in.BasisPoints,
		in.expires(), in.TotalLimit, in.PerCustomerLimit)
}

// ArchiveCode stops a code from being used in new quotes.
func (s *Store) ArchiveCode(ctx context.Context, subject, key, codeID string, in catalog.VersionInput) (catalog.Result, error) {
	if !catalog.ValidUUID(codeID) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	return s.command(ctx, CmdCodeArchive, `SELECT result_id::text, replayed FROM business.discount_code_archive(
		$1::uuid, $2, $3, $4::uuid, $5)`,
		subject, key, catalog.BodyHash(CmdCodeArchive, codeID, in), codeID, in.ExpectedVersion)
}

func (s *Store) command(ctx context.Context, name, sql string, args ...any) (catalog.Result, error) {
	var r catalog.Result
	if err := s.DB.QueryRow(ctx, sql, args...).Scan(&r.ID, &r.Replayed); err != nil {
		return catalog.Result{}, gatewayError(name, err)
	}
	return r, nil
}

// QuoteConflictCodes are the GK409 refusals of a quote: the basket or the
// appointment changed since it was shown; the customer chooses again.
var QuoteConflictCodes = map[string]bool{"item_unavailable": true, "slot_unavailable": true}

var invalidFields = map[string]string{
	"proof_invalid": "guest_proof", "mode_invalid": "fulfillment_mode", "lines_invalid": "lines",
	"lines_duplicate": "lines", "code_unknown": "discount_code", "code_requires_sign_in": "discount_code",
	"code_not_applicable": "discount_code", "kind_invalid": "kind", "expires_invalid": "expires_at",
	"limit_invalid": "total_limit", "per_customer_limit_invalid": "per_customer_limit", "code_invalid": "code",
	"idempotency_key_invalid": "Idempotency-Key", "proof_reused": "guest_proof",
	"expected_version_required": "expected_version",
}

var invalidMessages = map[string]string{
	"code_unknown":          "is not a current code",
	"code_requires_sign_in": "needs you to sign in (it is limited per customer)",
	"code_not_applicable":   "applies to GK delivery only",
	"expires_invalid":       "must be in the future",
	"lines_duplicate":       "must not repeat a cake",
	"proof_reused":          "must be fresh for each new quote",
}

var conflictCodes = map[string]bool{"stale_version": true, "key_reused": true, "state_conflict": true,
	"duplicate": true, "item_unavailable": true, "slot_unavailable": true}

func gatewayError(op string, err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return fmt.Errorf("checkout: %s: %w", op, err)
	}
	switch pgErr.Code {
	case "GK403":
		return catalog.ErrForbidden
	case "GK404":
		return catalog.ErrNotFound
	case "GK409":
		if conflictCodes[pgErr.Message] {
			return &catalog.ConflictError{Code: pgErr.Message}
		}
		return &catalog.ConflictError{Code: "state_conflict"}
	case "GK422":
		field, ok := invalidFields[pgErr.Message]
		if !ok {
			field = "input"
		}
		msg, ok := invalidMessages[pgErr.Message]
		if !ok {
			msg = "is not valid"
		}
		return &catalog.InvalidError{Field: field, Message: msg}
	}
	return fmt.Errorf("checkout: %s: %w", op, err)
}
