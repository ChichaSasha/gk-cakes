package checkout

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/jackc/pgx/v5/pgconn"
)

// Appointment changes (K06; migration 0031): the customer's same-day pickup
// move and the chef's date/time amendment with the customer's agreement,
// both K11 order commands.
const (
	CmdPickupMove        = "order.pickup_move"
	CmdAppointmentChange = "order.appointment_change"
)

// PickupMoveInput moves the order's appointment to another published start
// of the same day at the version the customer saw.
type PickupMoveInput struct {
	TargetSlotID    string `json:"target_slot_id"`
	ExpectedVersion int    `json:"expected_version"`
}

// Validate checks the input.
func (in PickupMoveInput) Validate() error {
	if !catalog.ValidUUID(in.TargetSlotID) {
		return invalid("target_slot_id", "must be a slot ID from the move choices")
	}
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	return nil
}

// AppointmentInput is the chef's amendment of an order's date and time,
// agreed with the customer, with a feasibility note (shelf life,
// preparation).
type AppointmentInput struct {
	ExpectedVersion   int        `json:"expected_version"`
	TargetDate        string     `json:"target_date"`
	TargetSlotID      string     `json:"target_slot_id"`
	CustomerAgreement *Agreement `json:"customer_agreement"`
	FeasibilityNote   string     `json:"feasibility_note"`
}

// Validate checks the input (the database checks agreed_at against the
// placement and the receipt instant).
func (in AppointmentInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	if d, err := time.Parse(time.DateOnly, in.TargetDate); err != nil || d.Format(time.DateOnly) != in.TargetDate {
		return invalid("target_date", "must be a date (YYYY-MM-DD)")
	}
	if !catalog.ValidUUID(in.TargetSlotID) {
		return invalid("target_slot_id", "must be a slot ID of the target date")
	}
	if in.CustomerAgreement == nil {
		return invalid("customer_agreement", "is required: the customer agrees a date change first")
	}
	if err := in.CustomerAgreement.validate(); err != nil {
		return err
	}
	return singleLine("feasibility_note", in.FeasibilityNote, 500)
}

func (a Agreement) validate() error {
	if !agreementChannels[a.Channel] {
		return invalid("customer_agreement.channel", "must be email, phone, instagram, whatsapp, in_person or other")
	}
	if _, err := time.Parse(time.RFC3339, a.AgreedAt); err != nil || len(a.AgreedAt) > 40 {
		return invalid("customer_agreement.agreed_at", "must be an RFC3339 instant with an offset")
	}
	return singleLine("customer_agreement.evidence_ref", a.EvidenceRef, 200)
}

func singleLine(field, s string, n int) error {
	if !utf8.ValidString(s) || s != strings.TrimSpace(s) || utf8.RuneCountInString(s) < 1 || utf8.RuneCountInString(s) > n {
		return invalid(field, "must be 1 to %d characters without surrounding spaces", n)
	}
	for _, c := range s {
		if unicode.IsControl(c) || c == '\u2028' || c == '\u2029' {
			return invalid(field, "must be one line without control characters")
		}
	}
	return nil
}

// Moved is the answer to an appointment change: the command (202, poll the
// order) or Unchanged when the order already has that appointment (no
// command; the result is the order).
type Moved struct {
	catalog.Result
	Unchanged bool
}

// MovePickup records the customer's same-day move of order id (owner, or a
// guest capability with "move_pickup") and enqueues its protected commit.
func (s *Store) MovePickup(ctx context.Context, id, subject string, digest []byte, key string, in PickupMoveInput) (Moved, error) {
	if !catalog.ValidUUID(id) || (subject == "" && digest == nil) {
		return Moved{}, catalog.ErrNotFound
	}
	if !envPattern.MatchString(s.Env) {
		return Moved{}, errors.New("checkout: the recovery journal environment is not configured")
	}
	var owner *string
	if subject != "" {
		owner, digest = &subject, nil
	}
	var m Moved
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed, unchanged FROM business.order_pickup_move_request(
		$1::uuid, $2, $3::uuid, $4, $5, $6, $7::uuid, $8, $9)`,
		owner, digest, id, key, catalog.BodyHash(CmdPickupMove, id, in), in.ExpectedVersion, in.TargetSlotID, s.Env,
		s.now().UTC()).Scan(&m.ID, &m.Replayed, &m.Unchanged)
	return m, appointmentError("pickup move", err)
}

// ChangeAppointment records the chef's agreed date/time amendment of order
// id and enqueues its protected commit.
func (s *Store) ChangeAppointment(ctx context.Context, subject, key, id string, in AppointmentInput) (Moved, error) {
	id = strings.ToLower(id)
	if !catalog.ValidUUID(id) {
		return Moved{}, catalog.ErrNotFound
	}
	if !envPattern.MatchString(s.Env) {
		return Moved{}, errors.New("checkout: the recovery journal environment is not configured")
	}
	a := in.CustomerAgreement
	if a == nil {
		return Moved{}, invalid("customer_agreement", "is required: the customer agrees a date change first")
	}
	agreed, err := time.Parse(time.RFC3339, a.AgreedAt)
	if err != nil {
		return Moved{}, invalid("customer_agreement.agreed_at", "must be an RFC3339 instant with an offset")
	}
	var m Moved
	err = s.DB.QueryRow(ctx, `SELECT result_id::text, replayed, unchanged FROM business.order_appointment_change_request(
		$1::uuid, $2::uuid, $3, $4, $5, $6::date, $7::uuid, $8, $9, $10, $11, $12, $13)`,
		subject, id, key, catalog.BodyHash(CmdAppointmentChange, id, in), in.ExpectedVersion, in.TargetDate,
		in.TargetSlotID, a.Channel, agreed, a.EvidenceRef, in.FeasibilityNote, s.Env, s.now().UTC(),
	).Scan(&m.ID, &m.Replayed, &m.Unchanged)
	return m, appointmentError("appointment change", err)
}

func appointmentError(op string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "GK503" {
		return ErrOrdersFrozen
	}
	return gatewayError(op, err)
}
