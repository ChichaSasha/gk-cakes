package checkout

import (
	"context"
	"encoding/json"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
)

// Command names of checkout and policies.
const (
	CmdCheckout     = "checkout.create"
	CmdPolicyRecord = "policy.record"
)

// PollAfterSeconds is the K00 async polling hint.
const PollAfterSeconds = 3

// Contact is who the order is for (K02, GK-003 order_contacts): the
// customer's full name always, a different payer's name only when another
// person pays, and an email for service notices.
type Contact struct {
	FullName  string `json:"full_name"`
	Email     string `json:"email"`
	PayerName string `json:"payer_name,omitempty"`
}

// CheckoutInput turns a quote into an order awaiting payment.
type CheckoutInput struct {
	QuoteID                string  `json:"quote_id"`
	QuoteVersion           int     `json:"quote_version"`
	ConsentedPolicyVersion string  `json:"consented_policy_version"`
	Contact                Contact `json:"contact"`
	GuestProof             string  `json:"guest_proof,omitempty"`
}

var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

func name(field, s string, max int) error {
	if !utf8.ValidString(s) || s != strings.TrimSpace(s) || utf8.RuneCountInString(s) < 1 || utf8.RuneCountInString(s) > max {
		return invalid(field, "must be 1 to %d characters without surrounding spaces", max)
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return invalid(field, "must not contain control characters")
		}
	}
	return nil
}

// Validate checks the input.
func (in CheckoutInput) Validate() error {
	if !catalog.ValidUUID(in.QuoteID) {
		return invalid("quote_id", "must be a quote ID")
	}
	if in.QuoteVersion != 1 {
		return invalid("quote_version", "must be the quote's version")
	}
	if !catalog.ValidUUID(in.ConsentedPolicyVersion) {
		return invalid("consented_policy_version", "must be the current policy version ID")
	}
	if err := name("contact.full_name", in.Contact.FullName, 120); err != nil {
		return err
	}
	if in.Contact.PayerName != "" {
		if err := name("contact.payer_name", in.Contact.PayerName, 120); err != nil {
			return err
		}
	}
	e := in.Contact.Email
	if len(e) < 3 || len(e) > 254 || !emailPattern.MatchString(e) {
		return invalid("contact.email", "must be an email address")
	}
	if a, err := mail.ParseAddress(e); err != nil || a.Address != e || a.Name != "" {
		return invalid("contact.email", "must be an email address")
	}
	return nil
}

func (in CheckoutInput) commandBody() CheckoutInput {
	in.GuestProof = ""
	return in
}

// Placed is the 202 answer to a checkout (K00 async): the order exists and
// payment is pending; it is never an accepted order.
type Placed struct {
	OrderID          string `json:"order_id"`
	State            string `json:"state"`
	PollAfterSeconds int    `json:"poll_after_seconds"`
}

// Checkout creates the order of a quote. subject is the signed-in
// customer's verified subject, or "" for a guest with digest.
func (s *Store) Checkout(ctx context.Context, subject string, digest []byte, key string, in CheckoutInput) (catalog.Result, error) {
	var owner *string
	if subject != "" {
		owner, digest = &subject, nil
	}
	var payer *string
	if in.Contact.PayerName != "" {
		payer = &in.Contact.PayerName
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.checkout_create(
		$1::uuid, $2, $3, $4, $5::uuid, $6, $7::uuid, $8, $9, $10, $11)`,
		owner, digest, key, catalog.BodyHash(CmdCheckout, in.QuoteID, in.commandBody()), in.QuoteID, in.QuoteVersion,
		in.ConsentedPolicyVersion, in.Contact.FullName, payer, in.Contact.Email, s.now().UTC()).Scan(&r.ID, &r.Replayed)
	if err != nil {
		return catalog.Result{}, gatewayError("checkout", err)
	}
	return r, nil
}

// Order is a stored order as JSON (the gateway builds it).
type Order = json.RawMessage

// GetOrder returns an order for its owner or an active guest capability
// with read access, or ErrNotFound.
func (s *Store) GetOrder(ctx context.Context, id, subject string, digest []byte) (Order, error) {
	if !catalog.ValidUUID(id) || (subject == "" && digest == nil) {
		return nil, catalog.ErrNotFound
	}
	var owner *string
	if subject != "" {
		owner, digest = &subject, nil
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT business.order_get($1::uuid, $2::uuid, $3)`, id, owner, digest).Scan(&raw); err != nil {
		return nil, gatewayError("order", err)
	}
	if raw == nil {
		return nil, catalog.ErrNotFound
	}
	return Order(s.withDeliveryFailureClock(s.withDeliveryChoiceClock(s.withDeliveryClock(s.withAcceptable(raw))))), nil
}

// Policy is a customer policy version.
type Policy struct {
	ID          string    `json:"id"`
	Summary     string    `json:"summary"`
	EffectiveAt time.Time `json:"effective_at"`
	Source      string    `json:"source_decision_id"`
}

// CurrentPolicy returns the policy customers consent to now, or ErrNotFound.
func (s *Store) CurrentPolicy(ctx context.Context) (Policy, error) {
	rows, err := s.DB.Query(ctx, `SELECT policy_id::text, policy_summary, policy_effective_at, policy_source
		FROM business.policy_current($1)`, s.now().UTC())
	if err != nil {
		return Policy{}, gatewayError("policy", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Policy{}, gatewayError("policy", err)
		}
		return Policy{}, catalog.ErrNotFound
	}
	var p Policy
	if err := rows.Scan(&p.ID, &p.Summary, &p.EffectiveAt, &p.Source); err != nil {
		return Policy{}, gatewayError("policy", err)
	}
	return p, nil
}

// PolicyInput records a policy version.
type PolicyInput struct {
	SourceDecisionID     string `json:"source_decision_id"`
	SourceRevisionSHA256 string `json:"source_revision_sha256"`
	Summary              string `json:"summary"`
	EffectiveAt          string `json:"effective_at"`
	ApproverEvidence     string `json:"approver_evidence"`
}

var (
	decisionPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,120}$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Validate checks the input.
func (in PolicyInput) Validate() error {
	if !decisionPattern.MatchString(in.SourceDecisionID) {
		return invalid("source_decision_id", "must be the owner decision ID")
	}
	if !sha256Pattern.MatchString(in.SourceRevisionSHA256) {
		return invalid("source_revision_sha256", "must be 64 lowercase hex characters")
	}
	if !utf8.ValidString(in.Summary) || in.Summary != strings.TrimSpace(in.Summary) || in.Summary == "" ||
		utf8.RuneCountInString(in.Summary) > 4000 {
		return invalid("summary", "must be 1 to 4000 characters")
	}
	for _, r := range in.Summary {
		if r != '\n' && (unicode.IsControl(r) || r == '\u2028' || r == '\u2029') {
			return invalid("summary", "must not contain control characters")
		}
	}
	if _, err := time.Parse(time.RFC3339, in.EffectiveAt); err != nil || len(in.EffectiveAt) > 40 {
		return invalid("effective_at", "must be an RFC3339 instant with an offset")
	}
	return name("approver_evidence", in.ApproverEvidence, 500)
}

// RecordPolicy records a policy version.
func (s *Store) RecordPolicy(ctx context.Context, subject, key string, in PolicyInput) (catalog.Result, error) {
	effective, _ := time.Parse(time.RFC3339, in.EffectiveAt)
	return s.command(ctx, CmdPolicyRecord, `SELECT result_id::text, replayed FROM business.policy_record(
		$1::uuid, $2, $3, $4, $5, $6, $7, $8)`,
		subject, key, catalog.BodyHash(CmdPolicyRecord, "", in), in.SourceDecisionID, in.SourceRevisionSHA256,
		in.Summary, effective, in.ApproverEvidence)
}
