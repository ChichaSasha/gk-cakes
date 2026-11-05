package checkout

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/jackc/pgx/v5/pgconn"
)

// Production, late-payment and delivery-failure commands (K05, K04 Q2;
// migration 0022).
const (
	CmdStartProduction = "order.start_production"
	CmdLatePayment     = "order.late_payment"
	CmdDeliveryFailure = "refund.delivery_failure"
)

// ErrOrdersFrozen means a restore freeze refuses order changes for now (503,
// retryable).
var ErrOrdersFrozen = errors.New("checkout: a restore is in progress")

// ProductionInput starts production at the version the staff member saw.
type ProductionInput struct {
	ExpectedVersion int `json:"expected_version"`
}

// Validate checks the input.
func (in ProductionInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	return nil
}

// Started is the answer to a production start: the command result and the
// order's current lifecycle.
type Started struct {
	catalog.Result
	Lifecycle string
}

// StartProduction moves a confirmed order to in_production (chef or
// fulfillment). Replayed is true for the same key again and when production
// had already started (the timer or another request: one transition per
// order); Lifecycle is then the order's current one.
func (s *Store) StartProduction(ctx context.Context, subject, key, id string, in ProductionInput) (Started, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) {
		return Started{}, catalog.ErrNotFound
	}
	var r Started
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed, lifecycle FROM business.order_production_start(
		$1::uuid, $2::uuid, $3, $4, $5, $6)`,
		subject, id, key, catalog.BodyHash(CmdStartProduction, id, in), in.ExpectedVersion, s.now().UTC()).Scan(&r.ID, &r.Replayed, &r.Lifecycle)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "GK503" {
		return Started{}, ErrOrdersFrozen
	}
	if err != nil {
		return Started{}, gatewayError("production start", err)
	}
	return r, nil
}

// Agreement is the customer's agreement to a late payment being honoured
// (K04 Q2: silence never counts).
type Agreement struct {
	Channel     string `json:"channel"`
	AgreedAt    string `json:"agreed_at"`
	EvidenceRef string `json:"evidence_ref"`
}

// Agreement channels.
var agreementChannels = map[string]bool{"email": true, "phone": true, "instagram": true, "whatsapp": true,
	"in_person": true, "other": true}

// LatePaymentInput is the chef's decision on a paid_awaiting_agreement
// order: honour (with the customer's agreement) or full_return.
type LatePaymentInput struct {
	ExpectedVersion   int        `json:"expected_version"`
	Decision          string     `json:"decision"`
	CustomerAgreement *Agreement `json:"customer_agreement,omitempty"`
}

// Validate checks the input (the database checks agreed_at against the
// receipt instant).
func (in LatePaymentInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	switch in.Decision {
	case "honour":
		a := in.CustomerAgreement
		if a == nil {
			return invalid("customer_agreement", "is required to honour a late payment")
		}
		if !agreementChannels[a.Channel] {
			return invalid("customer_agreement.channel", "must be email, phone, instagram, whatsapp, in_person or other")
		}
		if _, err := time.Parse(time.RFC3339, a.AgreedAt); err != nil || len(a.AgreedAt) > 40 {
			return invalid("customer_agreement.agreed_at", "must be an RFC3339 instant with an offset")
		}
		r := a.EvidenceRef
		if !utf8.ValidString(r) || r != strings.TrimSpace(r) || utf8.RuneCountInString(r) < 1 || utf8.RuneCountInString(r) > 200 {
			return invalid("customer_agreement.evidence_ref", "must be 1 to 200 characters without surrounding spaces")
		}
		for _, c := range r {
			if unicode.IsControl(c) || c == '\u2028' || c == '\u2029' {
				return invalid("customer_agreement.evidence_ref", "must be one line without control characters")
			}
		}
	case "full_return":
		if in.CustomerAgreement != nil {
			return invalid("customer_agreement", "is only for honour")
		}
	default:
		return invalid("decision", "must be honour or full_return")
	}
	return nil
}

// LatePayment records the chef's late-payment decision and enqueues its
// protected commit (K11): 202, poll the order.
func (s *Store) LatePayment(ctx context.Context, subject, key, id string, in LatePaymentInput) (catalog.Result, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	if !envPattern.MatchString(s.Env) {
		return catalog.Result{}, errors.New("checkout: the recovery journal environment is not configured")
	}
	var channel, evidence *string
	var agreed *time.Time
	if a := in.CustomerAgreement; a != nil {
		t, err := time.Parse(time.RFC3339, a.AgreedAt)
		if err != nil {
			return catalog.Result{}, invalid("customer_agreement.agreed_at", "must be an RFC3339 instant with an offset")
		}
		channel, evidence, agreed = &a.Channel, &a.EvidenceRef, &t
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.order_late_payment_request(
		$1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		subject, id, key, catalog.BodyHash(CmdLatePayment, id, in), in.ExpectedVersion, in.Decision, channel, agreed, evidence,
		s.Env, s.now().UTC()).Scan(&r.ID, &r.Replayed)
	if err != nil {
		return catalog.Result{}, gatewayError("late payment", err)
	}
	return r, nil
}

// RecordDeliveryFailure records a GK-caused failed delivery on the order's
// succeeded delivery collection (chef): a 100% floor on it and the goal
// raised to that floor plus what the other collections already returned or
// reserved (K05). Interim: no HTTP route; delivery collections are not
// created by checkout yet.
func (s *Store) RecordDeliveryFailure(ctx context.Context, subject, key, orderID, collectionID string) (catalog.Result, error) {
	if !catalog.ValidUUID(orderID) || !catalog.ValidUUID(collectionID) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	body := struct {
		CollectionID string `json:"collection_id"`
	}{collectionID}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.refund_delivery_failure_record(
		$1::uuid, $2, $3, $4::uuid, $5::uuid)`,
		subject, key, catalog.BodyHash(CmdDeliveryFailure, orderID, body), orderID, collectionID).Scan(&r.ID, &r.Replayed)
	if err != nil {
		return catalog.Result{}, gatewayError("delivery failure", err)
	}
	return r, nil
}

// ProductionFreezeRecheck is how long a production timer waits while a
// restore freezes order changes; ProductionWait while a cancellation or
// late-payment decision on the order is unresolved.
const (
	ProductionFreezeRecheck = 5 * time.Minute
	ProductionWait          = 5 * time.Minute
)

// Productions is the worker's job handler for scheduled production starts
// (K05; migration 0022): at payment or free confirmation + 48 hours a
// still-confirmed order moves to in_production, effective at that instant;
// a manual start first makes it a no-op (one transition per order). A
// restore freeze defers it before anything else. Jobs of other kinds return
// jobs.ErrNoHandler.
type Productions struct {
	DB     Querier // gk_worker pool
	Logger *slog.Logger
}

// Handle runs the leased production timer.
func (p *Productions) Handle(ctx context.Context, job jobs.Job) error {
	var (
		status string
		order  *string
	)
	err := p.DB.QueryRow(ctx, `SELECT status, order_id::text FROM business.production_timer_run($1::uuid, $2::uuid, $3)`,
		job.ID, job.Worker, job.Generation).Scan(&status, &order)
	if err != nil {
		return leaseErr(err)
	}
	if status == "not_production" {
		return jobs.ErrNoHandler
	}
	logger := p.Logger
	if logger == nil {
		logger = slog.Default()
	}
	log := logger.With("job_id", job.ID, "generation", job.Generation, "order_id", deref(order))
	switch status {
	case "frozen":
		return jobs.Defer(ProductionFreezeRecheck, recovery.ErrFrozen)
	case "waiting":
		return jobs.Defer(ProductionWait, errors.New("checkout: production waits for an unresolved order decision"))
	case "started":
		log.Info("production started on schedule")
		return nil
	case "done":
		log.Info("production timer ended: already started or no longer confirmed")
		return nil
	}
	return fmt.Errorf("checkout: unexpected production timer status %q", status)
}
