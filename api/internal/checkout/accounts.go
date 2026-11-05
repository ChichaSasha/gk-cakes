package checkout

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/jackc/pgx/v5/pgconn"
)

// Customer accounts (GK-016 r02b, migration 0033; contracts r6.5 K00
// claims, K02 /me/orders and profile, K09 retention and deletion). The
// database authorizes every read and command; this file validates input and
// maps refusals.

// Command names of the account commands (K00 command receipts).
const (
	CmdProfileUpdate = "account.profile_update"
	CmdClaimSubmit   = "account.claim_submit"
	CmdClaimWithdraw = "account.claim_withdraw"
	CmdClaimDecide   = "order_claim.decide"
	CmdOwnerAssign   = "order.owner_assign"
	CmdAccountDelete = "account.delete"
)

// Page sizes of the account lists.
const (
	DefaultAccountPage = 20
	MaxAccountPage     = 50
)

var (
	// ErrReauthenticate: the command needs a sign-in within the last 10
	// minutes (account deletion; 401 with a step-up challenge).
	ErrReauthenticate = errors.New("checkout: a fresh sign-in is required")
	// ErrAccountDeleted: the account was deleted; only its deletion replays.
	ErrAccountDeleted = errors.New("checkout: account deleted")
)

// accountError maps the account gateway's refusals: the step-up and
// deleted-account refusals, a restore freeze (order changes), then the
// common codes.
func accountError(op string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "GK403" && pgErr.Message == "reauthentication_required":
			return ErrReauthenticate
		case pgErr.Code == "GK403" && pgErr.Message == "account_deleted":
			return ErrAccountDeleted
		case pgErr.Code == "GK503":
			return ErrOrdersFrozen
		}
	}
	return gatewayError(op, err)
}

func (s *Store) accountCommand(ctx context.Context, name, sql string, args ...any) (catalog.Result, error) {
	var r catalog.Result
	if err := s.DB.QueryRow(ctx, sql, args...).Scan(&r.ID, &r.Replayed); err != nil {
		return catalog.Result{}, accountError(name, err)
	}
	return r, nil
}

func (s *Store) accountJSON(ctx context.Context, op, sql string, args ...any) (json.RawMessage, error) {
	var raw []byte
	if err := s.DB.QueryRow(ctx, sql, args...).Scan(&raw); err != nil {
		return nil, accountError(op, err)
	}
	return json.RawMessage(raw), nil
}

// ------------------------------------------------------------------ cursors

func encodeKeyset(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeKeyset(s string) (*time.Time, *string, error) {
	if s == "" {
		return nil, nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) > 120 {
		return nil, nil, catalog.ErrBadCursor
	}
	at, id, ok := strings.Cut(string(raw), "|")
	t, err := time.Parse(time.RFC3339Nano, at)
	if !ok || err != nil || !catalog.ValidUUID(id) {
		return nil, nil, catalog.ErrBadCursor
	}
	return &t, &id, nil
}

func pageLimit(limit int) (int, error) {
	switch {
	case limit == 0:
		return DefaultAccountPage, nil
	case limit < 1 || limit > MaxAccountPage:
		return 0, invalid("limit", "must be 1 to %d", MaxAccountPage)
	}
	return limit, nil
}

// page decodes a gateway page {items, next{<at>, id}} into items and the
// next cursor.
func page(raw []byte, atField string) (json.RawMessage, string, error) {
	var out struct {
		Items json.RawMessage            `json:"items"`
		Next  map[string]json.RawMessage `json:"next"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, "", err
	}
	if out.Next == nil {
		return out.Items, "", nil
	}
	var at time.Time
	var id string
	if err := json.Unmarshal(out.Next[atField], &at); err != nil {
		return nil, "", err
	}
	if err := json.Unmarshal(out.Next["id"], &id); err != nil {
		return nil, "", err
	}
	return out.Items, encodeKeyset(at, id), nil
}

// ---------------------------------------------------------------- my orders

// MyOrderPage is one page of the signed-in customer's orders (K02
// owner-only summaries); NextCursor is empty on the last page.
type MyOrderPage struct {
	Orders     json.RawMessage `json:"orders"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// MyOrders lists the account's orders, newest placed first.
func (s *Store) MyOrders(ctx context.Context, subject, cursor string, limit int) (MyOrderPage, error) {
	n, err := pageLimit(limit)
	if err != nil {
		return MyOrderPage{}, err
	}
	at, id, err := decodeKeyset(cursor)
	if err != nil {
		return MyOrderPage{}, err
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT business.account_order_page($1::uuid, $2, $3::uuid, $4)::text`,
		subject, at, id, n).Scan(&raw); err != nil {
		return MyOrderPage{}, accountError("my orders", err)
	}
	items, next, err := page(raw, "placed_at")
	if err != nil {
		return MyOrderPage{}, err
	}
	return MyOrderPage{Orders: items, NextCursor: next}, nil
}

// ------------------------------------------------------------------ profile

// Profile returns the account's optional contact details and their version.
func (s *Store) Profile(ctx context.Context, subject string) (json.RawMessage, error) {
	return s.accountJSON(ctx, "profile", `SELECT business.account_profile_get($1::uuid)::text`, subject)
}

// ProfilePatch changes the optional profile fields (GK-016 r02b answer 10:
// name, phone, Instagram name, all optional). An omitted field stays; null
// clears it.
type ProfilePatch struct {
	ExpectedVersion int                      `json:"expected_version"`
	FullName        catalog.Optional[string] `json:"full_name"`
	Phone           catalog.Optional[string] `json:"phone"`
	Instagram       catalog.Optional[string] `json:"instagram"`
}

// NullableFields: every profile field may be cleared.
func (ProfilePatch) NullableFields() []string { return []string{"full_name", "phone", "instagram"} }

var (
	phonePattern     = regexp.MustCompile(`^\+?[0-9][0-9 ()-]{5,22}[0-9]$`)
	instagramPattern = regexp.MustCompile(`^[A-Za-z0-9._]{1,30}$`)
)

// ValidPhone reports whether s looks like a phone number: optional +,
// digits, spaces, hyphens and brackets, 7 to 15 digits.
func ValidPhone(s string) bool {
	if !phonePattern.MatchString(s) {
		return false
	}
	digits := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits >= 7 && digits <= 15
}

// Validate checks the patch.
func (p ProfilePatch) Validate() error {
	if p.ExpectedVersion < 1 {
		return invalid("expected_version", "is required")
	}
	if !p.FullName.Set && !p.Phone.Set && !p.Instagram.Set {
		return invalid("profile", "must change at least one of full_name, phone and instagram")
	}
	if p.FullName.Set && !p.FullName.Null {
		if err := name("full_name", p.FullName.Value, 120); err != nil {
			return err
		}
	}
	if p.Phone.Set && !p.Phone.Null && !ValidPhone(p.Phone.Value) {
		return invalid("phone", "must be a phone number like 07700 900123 or +44 7700 900123")
	}
	if p.Instagram.Set && !p.Instagram.Null && !instagramPattern.MatchString(p.Instagram.Value) {
		return invalid("instagram", "must be an Instagram name of up to 30 letters, numbers, full stops and underscores, without @")
	}
	return nil
}

func patchField(o catalog.Optional[string]) (bool, *string) {
	if !o.Set || o.Null {
		return o.Set, nil
	}
	v := o.Value
	return true, &v
}

// UpdateProfile changes the profile (expected_version is the profile's).
func (s *Store) UpdateProfile(ctx context.Context, subject, key string, in ProfilePatch) (catalog.Result, error) {
	if err := in.Validate(); err != nil {
		return catalog.Result{}, err
	}
	setName, fullName := patchField(in.FullName)
	setPhone, phone := patchField(in.Phone)
	setInstagram, instagram := patchField(in.Instagram)
	return s.accountCommand(ctx, CmdProfileUpdate, `SELECT result_id::text, replayed FROM business.account_profile_update(
		$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		subject, key, catalog.BodyHash(CmdProfileUpdate, "", in), in.ExpectedVersion,
		setName, fullName, setPhone, phone, setInstagram, instagram)
}

// ------------------------------------------------------------------- claims

// Claim channels: how the customer ordered.
var ClaimChannels = map[string]bool{"instagram": true, "offline": true, "website_email_link": true}

// ClaimInput asks the chef to add an older order to the account (K00).
// Without an order capability: reference, channel, an optional approximate
// date (YYYY-MM or YYYY-MM-DD) and a note are required (path c). With the
// order's capability (path a): channel website_email_link and an optional
// note only.
type ClaimInput struct {
	Channel         string `json:"channel"`
	Reference       string `json:"reference,omitempty"`
	ApproximateDate string `json:"approximate_date,omitempty"`
	Note            string `json:"note,omitempty"`
}

var approximateDate = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])(-[0-9]{2})?$`)

// claimNote checks a note of 1 to 500 characters: line breaks allowed, no
// other control characters, no surrounding spaces.
func claimNote(field, s string) error {
	if !utf8.ValidString(s) || s != strings.TrimSpace(s) || utf8.RuneCountInString(s) < 1 || utf8.RuneCountInString(s) > 500 {
		return invalid(field, "must be 1 to 500 characters without surrounding spaces")
	}
	for _, r := range s {
		if (unicode.IsControl(r) && r != '\n') || r == '\u2028' || r == '\u2029' {
			return invalid(field, "must not contain control characters other than line breaks")
		}
	}
	return nil
}

// Validate checks the fields that are present (which are required depends
// on the path; see Store.SubmitClaim).
func (in ClaimInput) Validate() error {
	if !ClaimChannels[in.Channel] {
		return invalid("channel", "must be instagram, offline or website_email_link")
	}
	if in.Reference != "" {
		if err := name("reference", in.Reference, 64); err != nil {
			return err
		}
	}
	if in.ApproximateDate != "" {
		layout := "2006-01"
		if len(in.ApproximateDate) == 10 {
			layout = time.DateOnly
		}
		if _, err := time.Parse(layout, in.ApproximateDate); err != nil || !approximateDate.MatchString(in.ApproximateDate) {
			return invalid("approximate_date", "must be a month (YYYY-MM) or a date (YYYY-MM-DD)")
		}
	}
	if in.Note != "" {
		if err := claimNote("note", in.Note); err != nil {
			return err
		}
	}
	return nil
}

// SubmitClaim records a pending claim; guestDigest is the order capability
// held in this browser (path a), nil for path c.
func (s *Store) SubmitClaim(ctx context.Context, subject string, guestDigest []byte, key string, in ClaimInput) (catalog.Result, error) {
	if err := in.Validate(); err != nil {
		return catalog.Result{}, err
	}
	if guestDigest == nil {
		switch {
		case in.Reference == "":
			return catalog.Result{}, invalid("reference", "is required: the order reference from the chef")
		case in.Note == "":
			return catalog.Result{}, invalid("note", "is required: a short note for the chef")
		}
	} else if in.Channel != "website_email_link" || in.Reference != "" || in.ApproximateDate != "" {
		return catalog.Result{}, invalid("channel", "must be website_email_link, without reference or approximate_date, when you add an order from its link")
	}
	hashed := struct {
		Input      ClaimInput `json:"input"`
		Capability string     `json:"capability"`
	}{in, hex.EncodeToString(guestDigest)}
	return s.accountCommand(ctx, CmdClaimSubmit, `SELECT result_id::text, replayed FROM business.order_claim_submit(
		$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9)`,
		subject, key, catalog.BodyHash(CmdClaimSubmit, "", hashed), in.Channel, optional(in.Reference),
		optional(in.ApproximateDate), optional(in.Note), guestDigest, s.now().UTC())
}

// WithdrawClaim withdraws one of the customer's pending claims.
func (s *Store) WithdrawClaim(ctx context.Context, subject, key, id string, in catalog.VersionInput) (catalog.Result, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	if in.ExpectedVersion < 1 {
		return catalog.Result{}, invalid("expected_version", "is required")
	}
	return s.accountCommand(ctx, CmdClaimWithdraw, `SELECT result_id::text, replayed FROM business.order_claim_withdraw(
		$1::uuid, $2, $3, $4::uuid, $5, $6)`,
		subject, key, catalog.BodyHash(CmdClaimWithdraw, id, in), id, in.ExpectedVersion, s.now().UTC())
}

// MyClaims lists the customer's claims, newest first.
func (s *Store) MyClaims(ctx context.Context, subject string) (json.RawMessage, error) {
	return s.accountJSON(ctx, "claims", `SELECT business.account_claim_list($1::uuid)::text`, subject)
}

// MyClaim reads one of the customer's claims.
func (s *Store) MyClaim(ctx context.Context, subject, id string) (json.RawMessage, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) {
		return nil, catalog.ErrNotFound
	}
	return s.accountJSON(ctx, "claim", `SELECT business.account_claim_get($1::uuid, $2::uuid)::text`, subject, id)
}

// ClaimStates are the states the chef's queue filters on.
var ClaimStates = map[string]bool{"pending": true, "approved": true, "declined": true, "withdrawn": true}

// ClaimPage is one page of the chef's claim queue.
type ClaimPage struct {
	Claims     json.RawMessage `json:"claims"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// StaffClaims reads the chef's claim queue (state default pending, oldest
// first).
func (s *Store) StaffClaims(ctx context.Context, subject, state, cursor string, limit int) (ClaimPage, error) {
	if state == "" {
		state = "pending"
	}
	if !ClaimStates[state] {
		return ClaimPage{}, invalid("state", "must be pending, approved, declined or withdrawn")
	}
	n, err := pageLimit(limit)
	if err != nil {
		return ClaimPage{}, err
	}
	at, id, err := decodeKeyset(cursor)
	if err != nil {
		return ClaimPage{}, err
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT business.order_claim_staff_list($1::uuid, $2, $3, $4::uuid, $5)::text`,
		subject, state, at, id, n).Scan(&raw); err != nil {
		return ClaimPage{}, accountError("claim queue", err)
	}
	items, next, err := page(raw, "received_at")
	if err != nil {
		return ClaimPage{}, err
	}
	return ClaimPage{Claims: items, NextCursor: next}, nil
}

// ClaimDecisionInput is the chef's decision on a claim: approve with the
// order to add, or decline (no order). Note is internal (never shown to the
// customer).
type ClaimDecisionInput struct {
	ExpectedVersion int    `json:"expected_version"`
	Decision        string `json:"decision"`
	OrderID         string `json:"order_id,omitempty"`
	Note            string `json:"note,omitempty"`
}

// Validate checks the decision.
func (in ClaimDecisionInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "is required")
	}
	switch in.Decision {
	case "approve":
		if !catalog.ValidUUID(in.OrderID) {
			return invalid("order_id", "is required to approve: the order to add")
		}
	case "decline":
		if in.OrderID != "" {
			return invalid("order_id", "must be omitted when declining")
		}
	default:
		return invalid("decision", "must be approve or decline")
	}
	if in.Note != "" {
		if err := name("note", in.Note, 500); err != nil {
			return err
		}
	}
	return nil
}

// DecideClaim records the chef's decision.
func (s *Store) DecideClaim(ctx context.Context, subject, key, id string, in ClaimDecisionInput) (catalog.Result, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	if err := in.Validate(); err != nil {
		return catalog.Result{}, err
	}
	return s.accountCommand(ctx, CmdClaimDecide, `SELECT result_id::text, replayed FROM business.order_claim_decide(
		$1::uuid, $2, $3, $4::uuid, $5, $6, $7::uuid, $8, $9)`,
		subject, key, catalog.BodyHash(CmdClaimDecide, id, in), id, in.ExpectedVersion, in.Decision,
		optional(in.OrderID), optional(in.Note), s.now().UTC())
}

// OwnerAssignInput adds an unowned order to an account at the customer's
// request (chef): account_id or account_subject (exactly one) and an
// evidence note (one line, no card details).
type OwnerAssignInput struct {
	ExpectedVersion int    `json:"expected_version"`
	AccountID       string `json:"account_id,omitempty"`
	AccountSubject  string `json:"account_subject,omitempty"`
	Evidence        string `json:"evidence"`
}

// Validate checks the input.
func (in OwnerAssignInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "is required")
	}
	if (in.AccountID == "") == (in.AccountSubject == "") {
		return invalid("account_id", "give exactly one of account_id and account_subject")
	}
	if in.AccountID != "" && !catalog.ValidUUID(in.AccountID) {
		return invalid("account_id", "must be an account ID")
	}
	if in.AccountSubject != "" && !catalog.ValidUUID(in.AccountSubject) {
		return invalid("account_subject", "must be a sign-in subject ID")
	}
	return name("evidence", in.Evidence, 500)
}

// AssignOwner links the order to the account.
func (s *Store) AssignOwner(ctx context.Context, subject, key, orderID string, in OwnerAssignInput) (catalog.Result, error) {
	orderID = strings.ToLower(orderID)
	if !catalog.ValidUUID(orderID) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	if err := in.Validate(); err != nil {
		return catalog.Result{}, err
	}
	return s.accountCommand(ctx, CmdOwnerAssign, `SELECT result_id::text, replayed FROM business.order_owner_assign(
		$1::uuid, $2, $3, $4::uuid, $5, $6::uuid, $7::uuid, $8, $9)`,
		subject, key, catalog.BodyHash(CmdOwnerAssign, orderID, in), orderID, in.ExpectedVersion,
		optional(in.AccountID), optional(in.AccountSubject), in.Evidence, s.now().UTC())
}

// ----------------------------------------------------------------- deletion

// PreviewDeletion returns what deleting the account would do now: orders
// that block it (unsettled money), confirmed upcoming orders (warning),
// counts, and the preview_id/preview_hash the deletion must present within
// 15 minutes.
func (s *Store) PreviewDeletion(ctx context.Context, subject string) (json.RawMessage, error) {
	return s.accountJSON(ctx, "deletion preview", `SELECT business.account_deletion_preview_create($1::uuid, $2)::text`,
		subject, s.now().UTC())
}

// DeletionInput confirms a deletion preview.
type DeletionInput struct {
	PreviewID   string `json:"preview_id"`
	PreviewHash string `json:"preview_hash"`
}

// Validate checks the input.
func (in DeletionInput) Validate() error {
	if !catalog.ValidUUID(in.PreviewID) || !hashPattern.MatchString(in.PreviewHash) {
		return invalid("preview_id", "must be a deletion preview you requested, with its preview_hash")
	}
	return nil
}

// Deleted is the outcome of a deletion (the same on a replay).
type Deleted struct {
	AccountID      string    `json:"account_id"`
	DeletedAt      time.Time `json:"deleted_at"`
	OrdersUnlinked int       `json:"orders_unlinked"`
	Replayed       bool      `json:"-"`
}

// DeleteAccount deletes the account. authenticatedAt is the token's sign-in
// instant; it must be within 10 minutes of the receipt (ErrReauthenticate).
func (s *Store) DeleteAccount(ctx context.Context, subject string, authenticatedAt time.Time, key string, in DeletionInput) (Deleted, error) {
	if err := in.Validate(); err != nil {
		return Deleted{}, err
	}
	var auth *time.Time
	if !authenticatedAt.IsZero() {
		t := authenticatedAt.UTC()
		auth = &t
	}
	var d Deleted
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed, deleted_at, orders_unlinked FROM business.account_delete(
		$1::uuid, $2, $3, $4::uuid, $5, $6, $7)`,
		subject, key, catalog.BodyHash(CmdAccountDelete, "", in), in.PreviewID, in.PreviewHash, auth, s.now().UTC()).
		Scan(&d.AccountID, &d.Replayed, &d.DeletedAt, &d.OrdersUnlinked)
	if err != nil {
		return Deleted{}, accountError("account deletion", err)
	}
	return d, nil
}
