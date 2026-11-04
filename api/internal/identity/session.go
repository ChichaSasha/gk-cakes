package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// SessionChecker asks the provider whether a token's session still exists.
// Signing out ends the session at the provider but cannot revoke an issued
// token, so a valid token alone does not prove a current session. Supabase's
// GET /user answers 403 session_not_found for an ended session (observed on
// the sandbox, 2026-11-04).
type SessionChecker struct {
	// Issuer as for Verifier; the check calls Issuer + "/user".
	Issuer string
	// PublishableKey is the provider's public client key (apikey header).
	PublishableKey string
	HTTP           *http.Client
}

// endedCodes are the provider error codes (Supabase error_code) that mean
// the session or user is gone. Any other refusal, such as a wrong publishable
// key, is a configuration or provider fault and fails closed as
// ErrUnavailable rather than signing the user out.
var endedCodes = map[string]bool{
	"session_not_found": true, "session_expired": true, "user_not_found": true,
	"user_banned": true, "bad_jwt": true, "no_authorization": true,
}

// Check returns nil when the provider confirms the session of token for
// subject, ErrSessionEnded when the provider says the session or user is
// gone, and ErrUnavailable otherwise (the caller fails closed). Errors carry
// the provider status and code, never the token.
func (s *SessionChecker) Check(ctx context.Context, token, subject string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Issuer+"/user", nil)
	if err != nil {
		return fmt.Errorf("%w: bad issuer URL", ErrUnavailable)
	}
	req.Header.Set("apikey", s.PublishableKey)
	req.Header.Set("Authorization", "Bearer "+token)
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: fetchTimeout, CheckRedirect: NoRedirects}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: session check: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var answer struct {
		ID        string `json:"id"`
		ErrorCode string `json:"error_code"`
	}
	readable := json.Unmarshal(body, &answer) == nil
	switch {
	case resp.StatusCode == http.StatusOK && readable && answer.ID == subject:
		return nil
	case resp.StatusCode == http.StatusOK:
		return fmt.Errorf("%w: session check answered 200 without this user", ErrUnavailable)
	case (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && endedCodes[answer.ErrorCode]:
		return fmt.Errorf("%w: provider status %d %s", ErrSessionEnded, resp.StatusCode, answer.ErrorCode)
	default:
		code := answer.ErrorCode
		if !codePattern.MatchString(code) {
			code = "-"
		}
		return fmt.Errorf("%w: session check status %d code %s", ErrUnavailable, resp.StatusCode, code)
	}
}

var codePattern = regexp.MustCompile(`^[a-z_]{1,64}$`)

// Capabilities a staff member can hold (K00). Future roles grant nothing.
const (
	Chef             = "chef"
	Fulfillment      = "fulfillment"
	TechnicalSupport = "technical_support"
)

// ErrDisabled: the account exists but is disabled; it may do nothing.
var ErrDisabled = errors.New("identity: account disabled")

// Actor is a verified subject resolved to its local account.
type Actor struct {
	AccountID    string
	Subject      string
	SessionID    string
	ExpiresAt    time.Time
	Capabilities []string // active staff capabilities, in name order
}

// Has reports whether the actor holds an active capability.
func (a Actor) Has(capability string) bool { return slices.Contains(a.Capabilities, capability) }

// Staff reports whether the actor holds any active staff capability.
func (a Actor) Staff() bool { return len(a.Capabilities) > 0 }

// Querier is satisfied by *pgxpool.Pool and *pgx.Conn.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Directory resolves accounts through the database gateway (gk_api login).
type Directory struct{ DB Querier }

// Resolve returns the account of a verified subject, creating it on first
// use; a disabled account is ErrDisabled.
func (d *Directory) Resolve(ctx context.Context, c Claims) (Actor, error) {
	a := Actor{Subject: c.Subject, SessionID: c.SessionID, ExpiresAt: c.ExpiresAt}
	var disabled bool
	err := d.DB.QueryRow(ctx, `SELECT account_id::text, account_disabled, account_capabilities
		FROM business.identity_resolve($1::uuid)`, c.Subject).Scan(&a.AccountID, &disabled, &a.Capabilities)
	switch {
	case err != nil:
		return Actor{}, fmt.Errorf("identity: resolve account: %w", err)
	case disabled:
		return Actor{}, ErrDisabled
	}
	return a, nil
}

// StaffMember is one active capability grant.
type StaffMember struct {
	AccountID  string    `json:"account_id"`
	Capability string    `json:"capability"`
	GrantedAt  time.Time `json:"granted_at"`
}

// ListStaff returns the active staff for a chef actor (checked again by the
// database).
func (d *Directory) ListStaff(ctx context.Context, actor Actor) ([]StaffMember, error) {
	rows, err := d.DB.Query(ctx, `SELECT staff_account_id::text, staff_capability, staff_granted_at
		FROM business.identity_staff_list($1::uuid)`, actor.Subject)
	if err != nil {
		return nil, fmt.Errorf("identity: list staff: %w", err)
	}
	staff, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (StaffMember, error) {
		var m StaffMember
		return m, r.Scan(&m.AccountID, &m.Capability, &m.GrantedAt)
	})
	if err != nil {
		return nil, fmt.Errorf("identity: list staff: %w", err)
	}
	return staff, nil
}

// Service authenticates bearer tokens: verify, then resolve the account.
type Service struct {
	Verifier  *Verifier
	Sessions  *SessionChecker
	Directory *Directory
}

// Authenticate verifies token and resolves its actor.
func (s *Service) Authenticate(ctx context.Context, token string) (Actor, error) {
	c, err := s.Verifier.Verify(ctx, token)
	if err != nil {
		return Actor{}, err
	}
	return s.Directory.Resolve(ctx, c)
}

// CheckSession confirms with the provider that the actor's session is
// current; required before staff operations and sensitive mutations.
func (s *Service) CheckSession(ctx context.Context, token string, a Actor) error {
	return s.Sessions.Check(ctx, token, a.Subject)
}

// ListStaff lists active staff for a chef.
func (s *Service) ListStaff(ctx context.Context, a Actor) ([]StaffMember, error) {
	return s.Directory.ListStaff(ctx, a)
}
