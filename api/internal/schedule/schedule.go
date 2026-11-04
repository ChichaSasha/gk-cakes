// Package schedule serves handover schedules, soft day capacity and finished
// cake lots (contracts r6.5 K01, K04, K06) through the database gateway
// (migration 0010_schedule_capacity.sql). The database authorizes, versions
// and counts; this package validates input and maps refusals to the catalog
// error types the HTTP layer already answers.
package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Timezone of every local date and clock time (K00).
const Timezone = "Europe/London"

// MaxRangeDays bounds a date range read (K01: at most 31 local dates).
const MaxRangeDays = 31

// Fulfilment modes (BO-GK-001.fulfillment_modes).
const (
	Collection      = "collection"
	CustomerCourier = "customer_courier"
	GKDelivery      = "gk_delivery"
)

// Modes lists the fulfilment modes.
var Modes = []string{Collection, CustomerCourier, GKDelivery}

// Weekdays are the wire names of ISO weekdays 1 (Monday) to 7 (Sunday).
var Weekdays = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

// Command names (K00 command identity).
const (
	CmdVersionCreate = "schedule.version_create"
	CmdDayLimitSet   = "capacity.day_limit_set"
	CmdLotCreate     = "lot.create"
	CmdLotSetState   = "lot.set_state"
)

// Querier is satisfied by *pgxpool.Pool and *pgx.Conn.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Store reads and changes schedules, capacity and lots (gk_api login).
type Store struct {
	DB  Querier
	Now func() time.Time // defaults to time.Now
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

var (
	clockPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	datePattern  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
)

func invalid(field, format string, args ...any) error {
	return &catalog.InvalidError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// ParseDate parses a strict YYYY-MM-DD local date.
func ParseDate(field, s string) (time.Time, error) {
	if !datePattern.MatchString(s) {
		return time.Time{}, invalid(field, "must be a date YYYY-MM-DD")
	}
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, invalid(field, "must be a date YYYY-MM-DD")
	}
	return d, nil
}

// CheckRange validates an inclusive range of at most MaxRangeDays dates.
func CheckRange(from, to time.Time) error {
	if to.Before(from) {
		return invalid("to", "must not be before from")
	}
	if to.Sub(from) > (MaxRangeDays-1)*24*time.Hour {
		return invalid("to", "the range must be at most %d days", MaxRangeDays)
	}
	return nil
}

func clockMinutes(s string) int {
	var h, m int
	_, _ = fmt.Sscanf(s, "%d:%d", &h, &m)
	return h*60 + m
}

// WeekdayInput is one opening weekday of a schedule version.
type WeekdayInput struct {
	Weekday string   `json:"weekday"`
	Opens   string   `json:"opens"`
	Closes  string   `json:"closes"`
	Starts  []string `json:"starts"`
}

// VersionInput publishes the next schedule version.
type VersionInput struct {
	ExpectedRevision int            `json:"expected_revision"`
	StandardLimit    int            `json:"standard_limit"`
	BookableUntil    string         `json:"bookable_until"`
	Weekdays         []WeekdayInput `json:"weekdays"`
	ClosedDates      []string       `json:"closed_dates"`
}

// AppointmentMinutes is the appointment length (BO-GK-001.pickup_slot).
const AppointmentMinutes = 60

// Validate checks the input as the database does.
func (in VersionInput) Validate() error {
	if in.ExpectedRevision < 0 || in.ExpectedRevision > catalog.MaxVersion {
		return invalid("expected_revision", "must be the current revision (0 when there is none)")
	}
	if in.StandardLimit < 0 || in.StandardLimit > 100 {
		return invalid("standard_limit", "must be 0 to 100")
	}
	if _, err := ParseDate("bookable_until", in.BookableUntil); err != nil {
		return err
	}
	if len(in.Weekdays) > 7 {
		return invalid("weekdays", "must list each weekday at most once")
	}
	seen := map[string]bool{}
	for _, w := range in.Weekdays {
		if !slices.Contains(Weekdays, w.Weekday) {
			return invalid("weekdays", "weekday must be one of monday to sunday")
		}
		if seen[w.Weekday] {
			return invalid("weekdays", "must list each weekday at most once")
		}
		seen[w.Weekday] = true
		if !clockPattern.MatchString(w.Opens) || !clockPattern.MatchString(w.Closes) {
			return invalid("weekdays", "opens and closes must be HH:MM")
		}
		opens, closes := clockMinutes(w.Opens), clockMinutes(w.Closes)
		if opens >= closes {
			return invalid("weekdays", "%s must open before it closes", w.Weekday)
		}
		if len(w.Starts) > 48 {
			return invalid("weekdays", "at most 48 starts a day")
		}
		starts := map[string]bool{}
		for _, st := range w.Starts {
			if !clockPattern.MatchString(st) {
				return invalid("weekdays", "starts must be HH:MM")
			}
			m := clockMinutes(st)
			// The 60-minute appointment must end within the hours.
			if m < opens || m+AppointmentMinutes > closes {
				return invalid("weekdays", "%s %s: the 60-minute appointment must start and end within %s-%s",
					w.Weekday, st, w.Opens, w.Closes)
			}
			if starts[st] {
				return invalid("weekdays", "%s lists %s twice", w.Weekday, st)
			}
			starts[st] = true
		}
	}
	if len(in.ClosedDates) > 400 {
		return invalid("closed_dates", "at most 400 dates")
	}
	dates := map[string]bool{}
	for _, d := range in.ClosedDates {
		if _, err := ParseDate("closed_dates", d); err != nil {
			return err
		}
		if dates[d] {
			return invalid("closed_dates", "lists %s twice", d)
		}
		dates[d] = true
	}
	return nil
}

// DayLimitInput sets a day's standard limit.
type DayLimitInput struct {
	ExpectedVersion int `json:"expected_version"`
	StandardLimit   int `json:"standard_limit"`
}

// Validate checks the input. expected_version is 0 for a day without an
// override yet.
func (in DayLimitInput) Validate() error {
	if in.ExpectedVersion < 0 || in.ExpectedVersion > catalog.MaxVersion {
		return invalid("expected_version", "must be the day's version (0 when it has none)")
	}
	if in.StandardLimit < 0 || in.StandardLimit > 100 {
		return invalid("standard_limit", "must be 0 to 100")
	}
	return nil
}

// Lot kinds.
const (
	LotReady  = "ready"
	LotResale = "resale"
)

// LotInput records a finished cake lot of a variant's current recipe
// version. Ready lots need price_minor; resale lots resale_base_minor and
// original_handover_at (the price is the base x 50%, half up).
type LotInput struct {
	VariantID          string `json:"variant_id"`
	Kind               string `json:"kind"`
	Quantity           int    `json:"quantity"`
	PriceMinor         *int64 `json:"price_minor,omitempty"`
	ResaleBaseMinor    *int64 `json:"resale_base_minor,omitempty"`
	OriginalHandoverAt string `json:"original_handover_at,omitempty"`
	ExpiresAt          string `json:"expires_at"`
}

// Validate checks the input.
func (in LotInput) Validate() error {
	if !catalog.ValidUUID(in.VariantID) {
		return invalid("variant_id", "must be an ID")
	}
	if in.Quantity < 1 || in.Quantity > 100 {
		return invalid("quantity", "must be 1 to 100")
	}
	switch in.Kind {
	case LotReady:
		if in.PriceMinor == nil || *in.PriceMinor < 1 || *in.PriceMinor > catalog.MaxPriceMinor {
			return invalid("price_minor", "a ready lot needs a price from 1 to %d pence", catalog.MaxPriceMinor)
		}
		if in.ResaleBaseMinor != nil || in.OriginalHandoverAt != "" {
			return invalid("kind", "a ready lot has no resale base or original handover")
		}
	case LotResale:
		if in.PriceMinor != nil {
			return invalid("price_minor", "a resale price is the base amount x 50%%; give resale_base_minor")
		}
		if in.ResaleBaseMinor == nil || *in.ResaleBaseMinor < 1 || *in.ResaleBaseMinor > catalog.MaxPriceMinor {
			return invalid("resale_base_minor", "must be 1 to %d pence", catalog.MaxPriceMinor)
		}
		if _, err := parseInstant("original_handover_at", in.OriginalHandoverAt); err != nil {
			return err
		}
	default:
		return invalid("kind", "must be ready or resale")
	}
	_, err := parseInstant("expires_at", in.ExpiresAt)
	return err
}

func parseInstant(field, s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil || len(s) > 40 {
		return time.Time{}, invalid(field, "must be an RFC3339 instant with an offset")
	}
	return t, nil
}

// LotStateInput releases or withdraws a lot.
type LotStateInput struct {
	ExpectedVersion int    `json:"expected_version"`
	Action          string `json:"action"`
}

// Validate checks the input.
func (in LotStateInput) Validate() error {
	if in.ExpectedVersion < 1 || in.ExpectedVersion > catalog.MaxVersion {
		return invalid("expected_version", "is required")
	}
	if in.Action != "release" && in.Action != "withdraw" {
		return invalid("action", "must be release or withdraw")
	}
	return nil
}

// ------------------------------------------------------------------ reads

// Day is one date of the public availability read.
type Day struct {
	Date            string `json:"date"`
	Status          string `json:"status"`
	RemainingPlaces *int   `json:"remaining_places"`
}

// Availability is the public availability of a date range.
type Availability struct {
	Timezone         string    `json:"timezone"`
	AsOf             time.Time `json:"as_of"`
	ScheduleRevision *int      `json:"schedule_revision"`
	Days             []Day     `json:"days"`
}

// Availability returns the public status and remaining standard places of
// each date in [from, to] for a purchase of quantity cakes.
func (s *Store) Availability(ctx context.Context, from, to time.Time, quantity int) (Availability, error) {
	now := s.now().UTC()
	rows, err := s.DB.Query(ctx, `SELECT day, day_status, remaining, schedule_revision
		FROM business.schedule_public_days($1::date, $2::date, $3, $4)`, from, to, now, quantity)
	if err != nil {
		return Availability{}, gatewayError("availability", err)
	}
	a := Availability{Timezone: Timezone, AsOf: now, Days: []Day{}}
	defer rows.Close()
	for rows.Next() {
		var d Day
		var day time.Time
		var rev *int
		if err := rows.Scan(&day, &d.Status, &d.RemainingPlaces, &rev); err != nil {
			return Availability{}, gatewayError("availability", err)
		}
		d.Date = day.Format(time.DateOnly)
		a.ScheduleRevision = rev
		a.Days = append(a.Days, d)
	}
	if err := rows.Err(); err != nil {
		return Availability{}, gatewayError("availability", err)
	}
	return a, nil
}

// Slot is one appointment choice.
type Slot struct {
	ID           string    `json:"id"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
	Availability string    `json:"availability"`
	ReasonCode   *string   `json:"reason_code"`
}

// Choices is the public schedule projection of one date (K06).
type Choices struct {
	ScheduleRevision *int      `json:"schedule_revision"`
	AsOf             time.Time `json:"as_of"`
	Timezone         string    `json:"timezone"`
	Date             string    `json:"date"`
	FulfillmentMode  string    `json:"fulfillment_mode"`
	DayStatus        string    `json:"day_status"`
	Slots            []Slot    `json:"slots"`
}

// Choices returns the appointment choices of a date for a new purchase.
// Browsing creates no hold.
func (s *Store) Choices(ctx context.Context, day time.Time, mode string, quantity int) (Choices, error) {
	now := s.now().UTC()
	c := Choices{AsOf: now, Timezone: Timezone, Date: day.Format(time.DateOnly), FulfillmentMode: mode, Slots: []Slot{}}
	if err := s.DB.QueryRow(ctx, `SELECT day_status, schedule_revision
		FROM business.schedule_public_days($1::date, $1::date, $2, $3)`, day, now, quantity).Scan(&c.DayStatus, &c.ScheduleRevision); err != nil {
		return Choices{}, gatewayError("schedule", err)
	}
	rows, err := s.DB.Query(ctx, `SELECT slot_id::text, starts_at, ends_at, availability, reason_code
		FROM business.schedule_public_slots($1::date, $2, $3, $4)`, day, mode, now, quantity)
	if err != nil {
		return Choices{}, gatewayError("schedule", err)
	}
	slots, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Slot, error) {
		var sl Slot
		return sl, r.Scan(&sl.ID, &sl.StartsAt, &sl.EndsAt, &sl.Availability, &sl.ReasonCode)
	})
	if err != nil {
		return Choices{}, gatewayError("schedule", err)
	}
	if slots != nil {
		c.Slots = slots
	}
	return c, nil
}

// Lot is a finished cake lot. Staff fields are omitted publicly.
type Lot struct {
	ID                 string       `json:"id"`
	Kind               string       `json:"kind"`
	CakeID             string       `json:"cake_id"`
	CakeName           string       `json:"cake_name"`
	VariantID          string       `json:"variant_id"`
	RecipeVersionID    string       `json:"recipe_version_id"`
	Flavour            string       `json:"flavour"`
	Size               catalog.Size `json:"size"`
	PriceMinor         int64        `json:"price_minor"`
	Currency           string       `json:"currency"`
	Available          int          `json:"available"`
	ExpiresAt          time.Time    `json:"expires_at"`
	OriginalHandoverAt *time.Time   `json:"original_handover_at,omitempty"`
	ResaleBaseMinor    *int64       `json:"resale_base_minor,omitempty"`
	Produced           *int         `json:"produced,omitempty"`
	Reserved           *int         `json:"reserved,omitempty"`
	Sold               *int         `json:"sold,omitempty"`
	ReleasedAt         *time.Time   `json:"released_at,omitempty"`
	WithdrawnAt        *time.Time   `json:"withdrawn_at,omitempty"`
	Version            int          `json:"version,omitempty"`
	CreatedAt          *time.Time   `json:"created_at,omitempty"`
}

// PublicLots returns released, unexpired finished cakes with stock left.
func (s *Store) PublicLots(ctx context.Context) ([]Lot, error) {
	rows, err := s.DB.Query(ctx, `SELECT lot_id::text, lot_kind, cake_id::text, cake_name, variant_id::text, flavour,
		size_code, price_minor, available, expires_at, original_handover_at, recipe_version_id::text
		FROM business.lot_public_list($1)`, s.now().UTC())
	if err != nil {
		return nil, gatewayError("ready cakes", err)
	}
	lots, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Lot, error) {
		var l Lot
		var size string
		err := r.Scan(&l.ID, &l.Kind, &l.CakeID, &l.CakeName, &l.VariantID, &l.Flavour, &size, &l.PriceMinor,
			&l.Available, &l.ExpiresAt, &l.OriginalHandoverAt, &l.RecipeVersionID)
		l.Size, _ = catalog.SizeByCode(size)
		l.Currency = catalog.Currency
		return l, err
	})
	if err != nil {
		return nil, gatewayError("ready cakes", err)
	}
	if lots == nil {
		lots = []Lot{}
	}
	return lots, nil
}

// StaffLots lists lots for staff (withdrawn ones too when asked).
func (s *Store) StaffLots(ctx context.Context, subject string, includeInactive bool) ([]Lot, error) {
	rows, err := s.DB.Query(ctx, `SELECT lot_id::text, lot_kind, cake_id::text, cake_name, variant_id::text, flavour,
		size_code, price_minor, resale_base_minor, produced, reserved, sold, expires_at, original_handover_at,
		released_at, withdrawn_at, lot_version, recipe_version_id::text, created_at
		FROM business.lot_staff_list($1::uuid, $2)`, subject, includeInactive)
	if err != nil {
		return nil, gatewayError("lots", err)
	}
	lots, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Lot, error) {
		var l Lot
		var size string
		var produced, reserved, sold int
		var created time.Time
		err := r.Scan(&l.ID, &l.Kind, &l.CakeID, &l.CakeName, &l.VariantID, &l.Flavour, &size, &l.PriceMinor,
			&l.ResaleBaseMinor, &produced, &reserved, &sold, &l.ExpiresAt, &l.OriginalHandoverAt, &l.ReleasedAt,
			&l.WithdrawnAt, &l.Version, &l.RecipeVersionID, &created)
		l.Size, _ = catalog.SizeByCode(size)
		l.Currency = catalog.Currency
		l.Produced, l.Reserved, l.Sold, l.CreatedAt = &produced, &reserved, &sold, &created
		l.Available = produced - reserved - sold
		return l, err
	})
	if err != nil {
		return nil, gatewayError("lots", err)
	}
	if lots == nil {
		lots = []Lot{}
	}
	return lots, nil
}

// Version is a published schedule version.
type Version struct {
	Revision      int             `json:"revision"`
	StandardLimit int             `json:"standard_limit"`
	BookableUntil *string         `json:"bookable_until"`
	CreatedAt     time.Time       `json:"created_at"`
	Weekdays      []VersionWindow `json:"weekdays"`
	ClosedDates   []string        `json:"closed_dates"`
}

// VersionWindow is one weekday of a published version.
type VersionWindow struct {
	Weekday string   `json:"weekday"`
	Opens   string   `json:"opens"`
	Closes  string   `json:"closes"`
	Starts  []string `json:"starts"`
}

// CurrentVersion returns the current schedule version, or ErrNotFound.
func (s *Store) CurrentVersion(ctx context.Context, subject string) (Version, error) {
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT business.schedule_staff_current($1::uuid)`, subject).Scan(&raw); err != nil {
		return Version{}, gatewayError("schedule", err)
	}
	if raw == nil {
		return Version{}, catalog.ErrNotFound
	}
	var v struct {
		Version
		Weekdays []struct {
			Weekday int      `json:"weekday"`
			Opens   string   `json:"opens"`
			Closes  string   `json:"closes"`
			Starts  []string `json:"starts"`
		} `json:"weekdays"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return Version{}, fmt.Errorf("schedule: current version: %w", err)
	}
	out := v.Version
	out.Weekdays = []VersionWindow{}
	for _, w := range v.Weekdays {
		if w.Weekday < 1 || w.Weekday > 7 {
			return Version{}, fmt.Errorf("schedule: current version: weekday %d", w.Weekday)
		}
		out.Weekdays = append(out.Weekdays, VersionWindow{Weekday: Weekdays[w.Weekday-1], Opens: w.Opens, Closes: w.Closes, Starts: w.Starts})
	}
	return out, nil
}

// StaffDay is one date of the chef and fulfilment day view.
type StaffDay struct {
	Date              string `json:"date"`
	State             string `json:"state"`
	StandardLimit     int    `json:"standard_limit"`
	LimitOverride     bool   `json:"limit_override"`
	Version           int    `json:"version"`
	CommittedStandard int64  `json:"committed_standard"`
	TentativeStandard int64  `json:"tentative_standard"`
	ChefExtra         int64  `json:"chef_extra"`
	Workload          int64  `json:"workload"`
	RemainingPlaces   int    `json:"remaining_places"`
	OnlineClosed      bool   `json:"online_closed"`
}

// StaffDays returns the day view of [from, to].
func (s *Store) StaffDays(ctx context.Context, subject string, from, to time.Time) ([]StaffDay, error) {
	rows, err := s.DB.Query(ctx, `SELECT day, day_state, effective_limit, limit_override, day_version,
		committed_standard, tentative_standard, chef_extra, workload, remaining, online_closed
		FROM business.schedule_staff_days($1::uuid, $2::date, $3::date, $4)`, subject, from, to, s.now().UTC())
	if err != nil {
		return nil, gatewayError("days", err)
	}
	days, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (StaffDay, error) {
		var d StaffDay
		var day time.Time
		err := r.Scan(&day, &d.State, &d.StandardLimit, &d.LimitOverride, &d.Version, &d.CommittedStandard,
			&d.TentativeStandard, &d.ChefExtra, &d.Workload, &d.RemainingPlaces, &d.OnlineClosed)
		d.Date = day.Format(time.DateOnly)
		return d, err
	})
	if err != nil {
		return nil, gatewayError("days", err)
	}
	if days == nil {
		days = []StaffDay{}
	}
	return days, nil
}

// -------------------------------------------------------------- commands

// PublishVersion publishes the next schedule version.
func (s *Store) PublishVersion(ctx context.Context, subject, key string, in VersionInput) (catalog.Result, error) {
	type window struct {
		Weekday int      `json:"weekday"`
		Opens   string   `json:"opens"`
		Closes  string   `json:"closes"`
		Starts  []string `json:"starts"`
	}
	windows := []window{}
	for _, w := range in.Weekdays {
		starts := w.Starts
		if starts == nil {
			starts = []string{}
		}
		windows = append(windows, window{Weekday: slices.Index(Weekdays, w.Weekday) + 1, Opens: w.Opens, Closes: w.Closes, Starts: starts})
	}
	raw, err := json.Marshal(windows)
	if err != nil {
		return catalog.Result{}, err
	}
	closed := in.ClosedDates
	if closed == nil {
		closed = []string{}
	}
	return s.command(ctx, CmdVersionCreate, `SELECT result_id::text, replayed FROM business.schedule_version_create(
		$1::uuid, $2, $3, $4, $5, $6::date, $7::json, $8::date[])`,
		subject, key, catalog.BodyHash(CmdVersionCreate, "", in), in.ExpectedRevision, in.StandardLimit,
		in.BookableUntil, string(raw), closed)
}

// SetDayLimit sets a day's standard limit.
func (s *Store) SetDayLimit(ctx context.Context, subject, key string, day time.Time, in DayLimitInput) (catalog.Result, error) {
	date := day.Format(time.DateOnly)
	return s.command(ctx, CmdDayLimitSet, `SELECT result_id::text, replayed FROM business.capacity_day_limit_set(
		$1::uuid, $2, $3, $4::date, $5, $6)`,
		subject, key, catalog.BodyHash(CmdDayLimitSet, date, in), date, in.ExpectedVersion, in.StandardLimit)
}

// CreateLot records a finished cake lot.
func (s *Store) CreateLot(ctx context.Context, subject, key string, in LotInput) (catalog.Result, error) {
	expires, _ := parseInstant("expires_at", in.ExpiresAt)
	var original *time.Time
	if in.OriginalHandoverAt != "" {
		t, _ := parseInstant("original_handover_at", in.OriginalHandoverAt)
		original = &t
	}
	return s.command(ctx, CmdLotCreate, `SELECT result_id::text, replayed FROM business.lot_create(
		$1::uuid, $2, $3, $4::uuid, $5, $6, $7, $8, $9, $10)`,
		subject, key, catalog.BodyHash(CmdLotCreate, in.VariantID, in), in.VariantID, in.Kind, in.Quantity,
		in.PriceMinor, in.ResaleBaseMinor, original, expires)
}

// SetLotState releases or withdraws a lot.
func (s *Store) SetLotState(ctx context.Context, subject, key, lotID string, in LotStateInput) (catalog.Result, error) {
	if !catalog.ValidUUID(lotID) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	return s.command(ctx, CmdLotSetState, `SELECT result_id::text, replayed FROM business.lot_set_state(
		$1::uuid, $2, $3, $4::uuid, $5, $6)`,
		subject, key, catalog.BodyHash(CmdLotSetState, lotID, in), lotID, in.ExpectedVersion, in.Action)
}

func (s *Store) command(ctx context.Context, name, sql string, args ...any) (catalog.Result, error) {
	var r catalog.Result
	if err := s.DB.QueryRow(ctx, sql, args...).Scan(&r.ID, &r.Replayed); err != nil {
		return catalog.Result{}, gatewayError(name, err)
	}
	return r, nil
}

var invalidFields = map[string]string{
	"range_invalid": "to", "quantity_invalid": "quantity", "mode_invalid": "fulfillment_mode",
	"windows_invalid": "weekdays", "starts_invalid": "weekdays", "closed_dates_invalid": "closed_dates",
	"standard_limit_invalid": "standard_limit", "day_invalid": "date", "lot_invalid": "kind",
	"expires_invalid": "expires_at", "action_invalid": "action", "expected_version_required": "expected_version",
	"idempotency_key_invalid": "Idempotency-Key",
}

var invalidMessages = map[string]string{
	"expires_invalid": "must be in the future",
}

var conflictCodes = map[string]bool{"stale_version": true, "key_reused": true, "state_conflict": true, "duplicate": true}

func gatewayError(op string, err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return fmt.Errorf("schedule: %s: %w", op, err)
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
	return fmt.Errorf("schedule: %s: %w", op, err)
}
