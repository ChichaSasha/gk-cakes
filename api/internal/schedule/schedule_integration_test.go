//go:build integration

package schedule_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/httpapi"
	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	chef     = "22ac712e-4e38-48bb-a56d-fa7809ccc13f"
	fulfil   = "44ac712e-4e38-48bb-a56d-fa7809ccc13f"
	customer = "55ac712e-4e38-48bb-a56d-fa7809ccc13f"
)

// Owner schedule fixture (labelled test data, not a seed): BO-GK-001
// work_week, pickup_hours, pickup_slot (60 minutes, end within hours;
// Saturday starts 12:30-14:00 per K06) and daily_limit (7), gk019-owner-r7.
// Thursday, Friday and Sunday starts are illustrative chef-entered starts.
func ownerSchedule(revision int, bookableUntil string) schedule.VersionInput {
	half := func(from, to string) []string {
		var out []string
		t, _ := time.Parse("15:04", from)
		end, _ := time.Parse("15:04", to)
		for ; !t.After(end); t = t.Add(30 * time.Minute) {
			out = append(out, t.Format("15:04"))
		}
		return out
	}
	return schedule.VersionInput{
		ExpectedRevision: ptr(revision), StandardLimit: ptr(7), BookableUntil: bookableUntil,
		Weekdays: []schedule.WeekdayInput{
			{Weekday: "thursday", Opens: "11:00", Closes: "17:00", Starts: half("11:00", "16:00")},
			{Weekday: "friday", Opens: "10:00", Closes: "17:00", Starts: half("10:00", "16:00")},
			{Weekday: "saturday", Opens: "12:30", Closes: "15:00", Starts: []string{"12:30", "13:00", "13:30", "14:00"}},
			{Weekday: "sunday", Opens: "11:00", Closes: "12:00", Starts: []string{"11:00"}},
		},
		ClosedDates: []string{"2026-12-25"},
	}
}

type harness struct {
	ctx   context.Context
	db    *dbtest.DB
	api   *pgxpool.Pool
	admin *pgx.Conn
	store *schedule.Store
	cat   *catalog.Store
	now   time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	db := dbtest.NewMigrated(t, "")
	pool, err := pgxpool.New(ctx, db.LoginURL(t, "gk_api"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	h := &harness{ctx: ctx, db: db, api: pool, admin: db.AdminConn(t), cat: &catalog.Store{DB: pool},
		now: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)} // a Thursday, GMT
	h.store = &schedule.Store{DB: pool, Now: func() time.Time { return h.now }}
	h.owner(t, "SELECT business.staff_grant($1::uuid, 'chef', 'test')", chef)
	return h
}

// owner runs SQL as the owner role, as checkout's gateway functions will.
func (h *harness) owner(t *testing.T, sql string, args ...any) error {
	t.Helper()
	conn := h.db.MigratorConn(t)
	tx, err := conn.Begin(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(h.ctx)
	if _, err := tx.Exec(h.ctx, "SET LOCAL ROLE gk_owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(h.ctx, sql, args...); err != nil {
		return err
	}
	return tx.Commit(h.ctx)
}

func (h *harness) mustOwner(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := h.owner(t, sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func day(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }

func pgMessageAny(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Message
	}
	return ""
}

func pgMessage(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "GK422" {
		return pgErr.Message
	}
	return ""
}

// rawPublish calls the gateway directly, bypassing the API's validation.
func (h *harness) rawPublish(expected int, key, windows string) error {
	var id string
	var replayed bool
	return h.api.QueryRow(h.ctx, `SELECT result_id::text, replayed FROM business.schedule_version_create(
		$1::uuid, $2, $3, $4, 7, '2027-01-05', $5::json, '{}')`, chef, key, strings.Repeat("a", 64), expected, windows).Scan(&id, &replayed)
}

func (h *harness) publish(t *testing.T, key string, in schedule.VersionInput) {
	t.Helper()
	if err := in.Validate(); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if _, err := h.store.PublishVersion(h.ctx, chef, key, in); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

func (h *harness) days(t *testing.T, from, to string, q int) map[string]schedule.Day {
	t.Helper()
	a, err := h.store.Availability(h.ctx, day(from), day(to), q)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]schedule.Day{}
	for _, d := range a.Days {
		out[d.Date] = d
	}
	return out
}

func conflict(err error) string {
	var c *catalog.ConflictError
	if errors.As(err, &c) {
		return c.Code
	}
	return ""
}

func TestScheduleVersionsAndPublicDays(t *testing.T) {
	h := newHarness(t)

	// Nothing is bookable before the chef publishes a schedule.
	if d := h.days(t, "2026-11-13", "2026-11-13", 1)["2026-11-13"]; d.Status != "outside_horizon" || d.RemainingPlaces != nil {
		t.Fatalf("before any schedule: %+v", d)
	}
	h.publish(t, "v1", ownerSchedule(0, "2027-01-05"))

	// A stale revision, and appointments that would end after closing
	// (Saturday 14:30-15:30, K06 case S05) or start before opening.
	if _, err := h.store.PublishVersion(h.ctx, chef, "v-stale", ownerSchedule(0, "2027-01-05")); conflict(err) != "stale_version" {
		t.Fatalf("stale publish: %v", err)
	}
	for name, c := range map[string]struct{ weekday, opens, closes, start, msg string }{
		"ends-after-closing": {"saturday", "12:30", "15:00", "14:30", "starts_invalid"}, // K06 case S05
		"before-opening":     {"saturday", "12:30", "15:00", "12:00", "starts_invalid"},
		"past-midnight":      {"sunday", "00:00", "00:30", "00:00", "starts_invalid"},
		"clock-change-hour":  {"sunday", "00:00", "03:00", "01:30", "starts_invalid"},
		"touches-01":         {"sunday", "00:00", "03:00", "00:15", "starts_invalid"},
	} {
		bad := ownerSchedule(1, "2027-01-05")
		bad.Weekdays = []schedule.WeekdayInput{{Weekday: c.weekday, Opens: c.opens, Closes: c.closes, Starts: []string{c.start}}}
		if err := bad.Validate(); err == nil {
			t.Errorf("%s: API validation passed", name)
		}
		raw := `[{"weekday":` + map[string]string{"saturday": "6", "sunday": "7"}[c.weekday] + `,"opens":"` + c.opens +
			`","closes":"` + c.closes + `","starts":["` + c.start + `"]}]`
		if err := h.rawPublish(1, "k-"+name, raw); pgMessage(err) != c.msg {
			t.Errorf("%s at the gateway: %v, want GK422 %s", name, err, c.msg)
		}
	}
	// Malformed windows are refused as input, not as constraint errors.
	for name, raw := range map[string]string{
		"null-opens":    `[{"weekday":6,"opens":null,"closes":"15:00","starts":[]}]`,
		"missing-close": `[{"weekday":6,"opens":"12:30","starts":[]}]`,
		"number-start":  `[{"weekday":6,"opens":"12:30","closes":"15:00","starts":[1230]}]`,
		"weekday-8":     `[{"weekday":8,"opens":"12:30","closes":"15:00","starts":[]}]`,
		"not-object":    `[6]`,
	} {
		if err := h.rawPublish(1, "k-"+name, raw); dbtest.SQLState(err) != "GK422" {
			t.Errorf("%s: %v, want GK422", name, err)
		}
	}

	// now = Thu 5 Nov 10:00 GMT; now + 192 h = Fri 13 Nov 10:00 GMT.
	d := h.days(t, "2026-11-09", "2026-11-15", 1)
	want := map[string]string{
		"2026-11-09": "closed",    // Monday
		"2026-11-12": "lead_time", // Thursday: last start 16:00 is before Fri 10:00
		"2026-11-13": "open",      // Friday 10:00 is exactly 192 h away: equality meets the minimum
		"2026-11-14": "open",
		"2026-11-15": "open",
	}
	for date, status := range want {
		if d[date].Status != status {
			t.Errorf("%s: %+v, want %s", date, d[date], status)
		}
	}
	if r := d["2026-11-13"].RemainingPlaces; r == nil || *r != 7 {
		t.Fatalf("remaining on an empty day = %v, want 7", r)
	}
	if d := h.days(t, "2026-12-25", "2026-12-25", 1)["2026-12-25"]; d.Status != "closed" {
		t.Fatalf("closed date: %+v", d)
	}
	if d := h.days(t, "2027-01-07", "2027-01-07", 1)["2027-01-07"]; d.Status != "outside_horizon" {
		t.Fatalf("after bookable_until: %+v", d)
	}
	// Ranges are bounded to 31 dates.
	if _, err := h.store.Availability(h.ctx, day("2026-11-01"), day("2026-12-02"), 1); err == nil {
		t.Fatal("a 32-day range was accepted")
	}

	// Published versions are immutable.
	if _, err := h.admin.Exec(h.ctx, "UPDATE business.schedule_version SET standard_limit = 1"); dbtest.SQLState(err) != "GK409" {
		t.Fatalf("version update: %v", err)
	}
	cur, err := h.store.CurrentVersion(h.ctx, chef)
	if err != nil || cur.Revision != 1 || len(cur.Weekdays) != 4 || cur.Weekdays[2].Weekday != "saturday" ||
		strings.Join(cur.Weekdays[2].Starts, ",") != "12:30,13:00,13:30,14:00" || len(cur.ClosedDates) != 1 {
		t.Fatalf("current version = %+v, %v", cur, err)
	}
}

// T10: 691,199 seconds before the start fails, 691,200 passes.
func TestLeadTimeBoundaryAndSlots(t *testing.T) {
	h := newHarness(t)
	h.publish(t, "v1", ownerSchedule(0, "2027-06-30"))
	h.now = time.Date(2026, 11, 5, 10, 0, 1, 0, time.UTC)
	c, err := h.store.Choices(h.ctx, day("2026-11-13"), schedule.Collection, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Slots) != 13 || c.Slots[0].Availability != "unavailable" || *c.Slots[0].ReasonCode != "lead_time" ||
		c.Slots[1].Availability != "available" || c.Slots[1].ReasonCode != nil {
		t.Fatalf("Friday slots at now+1s = %+v", c.Slots[:2])
	}
	if got := c.Slots[0].StartsAt.Sub(h.now); got != 192*time.Hour-time.Second {
		t.Fatalf("first slot is %s away", got)
	}
	if !c.Slots[0].EndsAt.Equal(c.Slots[0].StartsAt.Add(time.Hour)) {
		t.Fatal("appointments last 60 minutes")
	}
	h.now = h.now.Add(-time.Second)
	if c, _ := h.store.Choices(h.ctx, day("2026-11-13"), schedule.Collection, 1); c.Slots[0].Availability != "available" {
		t.Fatalf("exactly 192 h: %+v", c.Slots[0])
	}
	// GK delivery times are requestable until the chef agrees feasibility.
	c, _ = h.store.Choices(h.ctx, day("2026-11-14"), schedule.GKDelivery, 1)
	if len(c.Slots) != 4 || c.Slots[0].Availability != "requestable" || *c.Slots[0].ReasonCode != "chef_confirms" {
		t.Fatalf("delivery slots = %+v", c.Slots)
	}
	// Slot IDs are stable for a version, date and start; a new version
	// changes them.
	again, _ := h.store.Choices(h.ctx, day("2026-11-14"), schedule.Collection, 1)
	if again.Slots[0].ID != c.Slots[0].ID {
		t.Fatal("slot ID depends on the mode or the call")
	}
	h.publish(t, "v2", ownerSchedule(1, "2027-06-30"))
	if v2, _ := h.store.Choices(h.ctx, day("2026-11-14"), schedule.Collection, 1); v2.Slots[0].ID == c.Slots[0].ID || *v2.ScheduleRevision != 2 {
		t.Fatal("a new schedule version kept the old slot IDs")
	}
	// Daylight saving: Sunday 28 March 2027 is the first BST day; 11:00
	// local is 10:00 UTC. The Sunday before is GMT.
	bst, _ := h.store.Choices(h.ctx, day("2027-03-28"), schedule.Collection, 1)
	gmt, _ := h.store.Choices(h.ctx, day("2027-03-21"), schedule.Collection, 1)
	if len(bst.Slots) != 1 || bst.Slots[0].StartsAt.UTC().Hour() != 10 || gmt.Slots[0].StartsAt.UTC().Hour() != 11 {
		t.Fatalf("DST: BST %+v, GMT %+v", bst.Slots, gmt.Slots)
	}
	// A closed day has no choices but a status; Monday has no starts.
	if c, _ := h.store.Choices(h.ctx, day("2026-11-16"), schedule.Collection, 1); c.DayStatus != "closed" || len(c.Slots) != 0 {
		t.Fatalf("Monday = %+v", c)
	}
}

// K04 soft capacity: admission counts committed standard cakes plus the
// quantity; holds are advisory; committing never refuses; lowering the limit
// keeps accepted orders; chef extras are outside the standard count.
func TestSoftDayCapacity(t *testing.T) {
	h := newHarness(t)
	h.publish(t, "v1", ownerSchedule(0, "2027-01-05"))
	const d = "2026-11-14"
	order := func(i int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012x", i) }
	admit := func(i, q int) error {
		return h.owner(t, "SELECT business.capacity_admit($1::uuid, $2::date, $3, now() + interval '15 minutes')", order(i), d, q)
	}
	commit := func(i int) { h.mustOwner(t, "SELECT business.capacity_commit($1::uuid, $2::date)", order(i), d) }

	for i := range 6 {
		if err := admit(i, 1); err != nil {
			t.Fatal(err)
		}
		commit(i)
	}
	// Committed 6: a 2-cake order is not admitted; 1 cake is.
	if err := admit(6, 2); dbtest.SQLState(err) != "GK409" {
		t.Fatalf("6 + 2 > 7: %v, want GK409 day_full", err)
	}
	// Two concurrent holds for the last place are both admitted (holds do
	// not own the last place), and both payments commit.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for k := range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); errs[k] = admit(7+k, 1) }()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("concurrent last-place holds: %v, %v", errs[0], errs[1])
	}
	pub := h.days(t, d, d, 1)[d]
	if pub.Status != "full" || *pub.RemainingPlaces != 0 {
		t.Fatalf("with 6 committed and 2 held: %+v", pub)
	}
	commit(7)
	commit(8)
	// Over the limit (8 of 7) both stay accepted; new admissions stop.
	if err := admit(9, 1); dbtest.SQLState(err) != "GK409" {
		t.Fatalf("admission at 8 of 7: %v", err)
	}
	// The chef lowers the limit 7 -> 5: all 8 stay committed.
	if _, err := h.store.SetDayLimit(h.ctx, chef, "lim", day(d), schedule.DayLimitInput{ExpectedVersion: ptr(1), StandardLimit: ptr(5)}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.SetDayLimit(h.ctx, chef, "lim2", day(d), schedule.DayLimitInput{ExpectedVersion: ptr(1), StandardLimit: ptr(6)}); conflict(err) != "stale_version" {
		t.Fatalf("stale limit: %v", err)
	}
	// A chef extra of 10 cakes is visible as workload, outside the count.
	h.mustOwner(t, `INSERT INTO business.capacity_allocation (id, order_ref, day, cake_quantity, classification, state, chef_evidence)
		VALUES (gen_random_uuid(), gen_random_uuid(), $1::date, 10, 'chef_extra', 'committed', 'Instagram order agreed by Elina')`, d)
	h.grant(t, fulfil, "fulfillment")
	days, err := h.store.StaffDays(h.ctx, fulfil, day(d), day(d))
	if err != nil || len(days) != 1 {
		t.Fatal(err)
	}
	if got := days[0]; got.StandardLimit != 5 || !got.LimitOverride || got.CommittedStandard != 8 || got.ChefExtra != 10 ||
		got.Workload != 18 || got.RemainingPlaces != 0 || got.OnlineClosed {
		t.Fatalf("staff day = %+v", got)
	}
	// Release frees a place only below the limit; release is idempotent.
	h.mustOwner(t, "SELECT business.capacity_release($1::uuid, $2::date)", order(8), d)
	h.mustOwner(t, "SELECT business.capacity_release($1::uuid, $2::date)", order(8), d)
	if err := h.owner(t, "SELECT business.capacity_commit($1::uuid, $2::date)", order(8), d); dbtest.SQLState(err) != "GK409" {
		t.Fatalf("commit after release: %v", err)
	}
	// Customers and fulfilment cannot change limits; gk_api cannot call
	// the engine directly.
	h.owner(t, "SELECT business.account_ensure($1::uuid, false)", customer)
	for _, s := range []string{fulfil, customer} {
		if _, err := h.store.SetDayLimit(h.ctx, s, "x", day(d), schedule.DayLimitInput{ExpectedVersion: ptr(2), StandardLimit: ptr(9)}); !errors.Is(err, catalog.ErrForbidden) {
			t.Errorf("%s sets a limit: %v", s, err)
		}
	}
	for _, sql := range []string{
		"SELECT business.capacity_admit(gen_random_uuid(), '2026-11-14', 1, now())",
		"SELECT business.capacity_commit(gen_random_uuid(), '2026-11-14')",
		"SELECT business.lot_reserve(gen_random_uuid(), 1, now())",
	} {
		if _, err := h.api.Exec(h.ctx, sql); dbtest.SQLState(err) != "42501" {
			t.Errorf("gk_api %s: %v, want 42501", sql, err)
		}
	}
}

func (h *harness) grant(t *testing.T, subject, capability string) {
	t.Helper()
	h.mustOwner(t, "SELECT business.staff_grant($1::uuid, $2, 'test')", subject, capability)
}

func (h *harness) readyVariant(t *testing.T) string {
	t.Helper()
	f, _ := h.cat.CreateFlavour(h.ctx, chef, "f", catalog.FlavourInput{Name: "Snickers"})
	n, _ := h.cat.CreateNotice(h.ctx, chef, "n", catalog.NoticeInput{Body: "Shared kitchen notice"})
	c, _ := h.cat.CreateCake(h.ctx, chef, "c", catalog.CakeInput{Name: "Chocolate swirl"})
	v, err := h.cat.CreateVariant(h.ctx, chef, "v", c.ID, catalog.VariantInput{FlavourID: f.ID, SizeCode: "s", StockMode: catalog.ReadyStock})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.cat.CreateRecipeVersion(h.ctx, chef, "r", catalog.RecipeVersionInput{VariantID: v.ID, ExpectedVersion: 1,
		PriceMinor: 9000, ServingsMin: 7, ServingsMax: 10, Coating: "Swiss meringue buttercream",
		Composition: []string{"Chocolate sponge"}, NoticeID: n.ID,
		Allergens: []catalog.Allergen{{Code: "peanuts", Label: "PEANUTS", Prominent: true}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.cat.SetCakeState(h.ctx, chef, "p", c.ID, catalog.CakeStateInput{ExpectedVersion: 1, State: catalog.StateActive}); err != nil {
		t.Fatal(err)
	}
	return v.ID
}

func ptr[T any](v T) *T { return &v }

// Finished cakes are exclusive physical stock: never oversold.
func TestFinishedCakeLots(t *testing.T) {
	h := newHarness(t)
	variant := h.readyVariant(t)
	expires := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	lot, err := h.store.CreateLot(h.ctx, chef, "l1", schedule.LotInput{VariantID: variant, Kind: schedule.LotReady, Quantity: 1,
		PriceMinor: ptr[int64](9000), ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	h.store.Now = time.Now
	if lots, _ := h.store.PublicLots(h.ctx); len(lots) != 0 {
		t.Fatalf("unreleased lot is public: %+v", lots)
	}
	if _, err := h.store.SetLotState(h.ctx, chef, "rel", lot.ID, schedule.LotStateInput{ExpectedVersion: 1, Action: "release"}); err != nil {
		t.Fatal(err)
	}
	lots, _ := h.store.PublicLots(h.ctx)
	if len(lots) != 1 || lots[0].Available != 1 || lots[0].PriceMinor != 9000 || lots[0].Produced != nil || lots[0].Version != 0 {
		t.Fatalf("public lots = %+v", lots)
	}
	// A reservation in progress holds the lot: a second one waits, then
	// finds no stock (stock_unavailable), never a constraint error.
	reserve := func(tx pgx.Tx) error {
		if _, err := tx.Exec(h.ctx, "SET LOCAL ROLE gk_owner"); err != nil {
			return err
		}
		_, err := tx.Exec(h.ctx, "SELECT business.lot_reserve($1::uuid, 1, now())", lot.ID)
		return err
	}
	first, err := h.db.MigratorConn(t).Begin(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := reserve(first); err != nil {
		t.Fatal(err)
	}
	second := make(chan error, 1)
	go func() {
		tx, err := h.db.MigratorConn(t).Begin(h.ctx)
		if err == nil {
			err = reserve(tx)
			_ = tx.Rollback(h.ctx)
		}
		second <- err
	}()
	select {
	case err := <-second:
		t.Fatalf("second reservation finished while the first held the lot: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := first.Commit(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-second; pgMessageAny(err) != "stock_unavailable" {
		t.Fatalf("second reservation: %v, want GK409 stock_unavailable", err)
	}
	if lots, _ := h.store.PublicLots(h.ctx); len(lots) != 0 {
		t.Fatalf("a fully reserved lot is public: %+v", lots)
	}
	h.mustOwner(t, "SELECT business.lot_settle($1::uuid, 1, true)", lot.ID)
	if err := h.owner(t, "SELECT business.lot_settle($1::uuid, 1, false)", lot.ID); dbtest.SQLState(err) != "GK409" {
		t.Fatalf("settle without a reservation: %v", err)
	}

	// Resale: base x 50%, half up; original handover kept.
	original := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	for base, price := range map[int64]int64{9000: 4500, 9001: 4501, 1: 1} {
		r, err := h.store.CreateLot(h.ctx, chef, fmt.Sprintf("resale-%d", base), schedule.LotInput{
			VariantID: variant, Kind: schedule.LotResale, Quantity: 1, ResaleBaseMinor: ptr(base),
			OriginalHandoverAt: original.Format(time.RFC3339), ExpiresAt: expires})
		if err != nil {
			t.Fatal(err)
		}
		staffLots, _ := h.store.StaffLots(h.ctx, chef, true)
		for _, l := range staffLots {
			if l.ID == r.ID && (l.PriceMinor != price || l.OriginalHandoverAt == nil || !l.OriginalHandoverAt.Equal(original)) {
				t.Errorf("resale base %d: %+v, want price %d", base, l, price)
			}
		}
	}
	// A resale lot cannot carry its own price.
	if _, err := h.api.Exec(h.ctx, `SELECT * FROM business.lot_create($1::uuid, 'bad', $2, $3::uuid, 'resale', 1, 100, 9000, now(), now() + interval '1 day')`,
		chef, strings.Repeat("d", 64), variant); dbtest.SQLState(err) != "GK422" {
		t.Fatalf("resale with a price: %v", err)
	}
	if _, err := h.admin.Exec(h.ctx, "UPDATE business.finished_cake_lot SET reserved_quantity = 5"); err == nil {
		t.Fatal("reserved beyond produced was stored")
	}
}

// stubIdentity authenticates "chef" (package identity tests real tokens).
type stubIdentity struct{}

func (stubIdentity) Authenticate(_ context.Context, token string) (identity.Actor, error) {
	if token != "chef" {
		return identity.Actor{}, identity.ErrUnauthenticated
	}
	return identity.Actor{AccountID: "acc", Subject: chef, Capabilities: []string{identity.Chef}}, nil
}
func (stubIdentity) CheckSession(context.Context, string, identity.Actor) error { return nil }
func (stubIdentity) ListStaff(context.Context, identity.Actor) ([]identity.StaffMember, error) {
	return nil, nil
}

func TestScheduleOverHTTP(t *testing.T) {
	h := newHarness(t)
	srv := httptest.NewServer(httpapi.NewHandler(httpapi.Options{DB: h.api, Logger: slog.New(slog.DiscardHandler),
		Identity: stubIdentity{}, Schedule: h.store}))
	t.Cleanup(srv.Close)
	call := func(method, path, key string, body any, want int) map[string]any {
		t.Helper()
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, srv.URL+path, rd)
		if key != "" {
			req.Header.Set("Authorization", "Bearer chef")
			req.Header.Set("Idempotency-Key", key)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if resp.StatusCode != want {
			t.Fatalf("%s %s = %d %v, want %d", method, path, resp.StatusCode, out, want)
		}
		return out
	}
	v := call("POST", "/api/v1/admin/schedule/versions", "v1", ownerSchedule(0, "2027-01-05"), 201)
	if v["revision"].(float64) != 1 {
		t.Fatalf("published = %v", v)
	}
	a := call("GET", "/api/v1/availability?from=2026-11-12&to=2026-11-15", "", nil, 200)
	if a["timezone"] != "Europe/London" || len(a["days"].([]any)) != 4 {
		t.Fatalf("availability = %v", a)
	}
	c := call("GET", "/api/v1/schedule?date=2026-11-14&fulfillment_mode=collection&purpose=new_purchase", "", nil, 200)
	slots := c["slots"].([]any)
	if len(slots) != 4 || slots[0].(map[string]any)["availability"] != "available" {
		t.Fatalf("schedule = %v", c)
	}
	call("GET", "/api/v1/schedule?date=2026-11-14&fulfillment_mode=collection&purpose=pickup_move", "", nil, 422)
	call("GET", "/api/v1/schedule?date=2026-11-14&fulfillment_mode=post", "", nil, 422)
	call("GET", "/api/v1/availability?from=2026-11-01&to=2026-12-02", "", nil, 422)
	call("GET", "/api/v1/availability?from=2026-13-01&to=2026-12-02", "", nil, 422)
	limit := call("POST", "/api/v1/admin/days/2026-11-14/limit", "l1", map[string]any{"expected_version": 0, "standard_limit": 3}, 200)
	if limit["standard_limit"].(float64) != 3 || limit["limit_override"] != true {
		t.Fatalf("limit = %v", limit)
	}
	// Lot commands answer with that lot even when others exist.
	variant := h.readyVariant(t)
	expires := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	var ids []string
	for i := range 3 {
		l := call("POST", "/api/v1/admin/lots", fmt.Sprintf("lot-%d", i), map[string]any{"variant_id": variant, "kind": "ready",
			"quantity": 1, "price_minor": 8000 + i, "expires_at": expires}, 201)
		if l["price_minor"].(float64) != float64(8000+i) {
			t.Fatalf("lot %d answered with %v", i, l)
		}
		ids = append(ids, l["id"].(string))
	}
	rel := call("POST", "/api/v1/admin/lots/"+ids[1]+"/state", "rel", map[string]any{"expected_version": 1, "action": "release"}, 200)
	if rel["id"] != ids[1] || rel["released_at"] == nil {
		t.Fatalf("release answered with %v", rel)
	}
	ready := call("GET", "/api/v1/ready-cakes", "", nil, 200)["cakes"].([]any)
	if len(ready) != 1 || ready[0].(map[string]any)["id"] != ids[1] {
		t.Fatalf("ready cakes = %v", ready)
	}
}

func TestCapacityEdges(t *testing.T) {
	h := newHarness(t)
	h.publish(t, "v1", ownerSchedule(0, "2027-01-05"))
	const d = "2026-11-14" // Saturday, last start 14:00 GMT
	order := func(i int) string { return fmt.Sprintf("00000000-0000-4000-9000-%012x", i) }

	// Day-level lead time: the last start exactly 192 h away keeps the day
	// open; one second later it closes.
	last := time.Date(2026, 11, 14, 14, 0, 0, 0, time.UTC)
	h.now = last.Add(-192 * time.Hour)
	if got := h.days(t, d, d, 1)[d].Status; got != "open" {
		t.Fatalf("last start exactly 192 h away: %s", got)
	}
	h.now = h.now.Add(time.Second)
	if got := h.days(t, d, d, 1)[d].Status; got != "lead_time" {
		t.Fatalf("last start 192 h - 1 s away: %s", got)
	}
	h.now = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)

	// Chef extras neither reduce public places nor block admission.
	h.mustOwner(t, "SELECT business.capacity_day_lock($1::date)", d)
	h.mustOwner(t, `INSERT INTO business.capacity_allocation (id, order_ref, day, cake_quantity, classification, state, chef_evidence)
		VALUES (gen_random_uuid(), gen_random_uuid(), $1::date, 10, 'chef_extra', 'committed', 'Instagram order agreed by Elina')`, d)
	if r := *h.days(t, d, d, 1)[d].RemainingPlaces; r != 7 {
		t.Fatalf("remaining with 10 chef extras = %d, want 7", r)
	}
	for i := range 6 {
		h.mustOwner(t, "SELECT business.capacity_admit($1::uuid, $2::date, 1, now() + interval '15 minutes')", order(i), d)
		h.mustOwner(t, "SELECT business.capacity_commit($1::uuid, $2::date)", order(i), d)
	}
	// One place left: a 2-cake purchase sees every slot full; 1 cake sees
	// them available.
	two, _ := h.store.Choices(h.ctx, day(d), schedule.Collection, 2)
	one, _ := h.store.Choices(h.ctx, day(d), schedule.Collection, 1)
	if two.DayStatus != "full" || two.Slots[0].Availability != "unavailable" || *two.Slots[0].ReasonCode != "full" ||
		one.Slots[0].Availability != "available" {
		t.Fatalf("slots with 1 place left: q2 %+v / q1 %+v", two.Slots[0], one.Slots[0])
	}

	// The day lock serializes admission with a commit in progress: with 6
	// committed and order 6 held, committing it (uncommitted transaction)
	// makes a concurrent admission wait, then refuse.
	h.mustOwner(t, "SELECT business.capacity_admit($1::uuid, $2::date, 1, now() + interval '15 minutes')", order(6), d)
	tx, err := h.db.MigratorConn(t).Begin(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(h.ctx, "SET LOCAL ROLE gk_owner; SELECT business.capacity_commit('"+order(6)+"'::uuid, '"+d+"'::date)"); err != nil {
		t.Fatal(err)
	}
	admitted := make(chan error, 1)
	go func() {
		admitted <- h.owner(t, "SELECT business.capacity_admit($1::uuid, $2::date, 1, now() + interval '15 minutes')", order(7), d)
	}()
	select {
	case err := <-admitted:
		t.Fatalf("admission finished during an uncommitted commit: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-admitted; pgMessageAny(err) != "day_full" {
		t.Fatalf("admission after the 7th commit: %v, want day_full", err)
	}

	// Clearing an override brings back the schedule default.
	days, _ := h.store.StaffDays(h.ctx, chef, day(d), day(d))
	try := func(key string, in schedule.DayLimitInput) error {
		_, err := h.store.SetDayLimit(h.ctx, chef, key, day(d), in)
		return err
	}
	if err := try("lim3", schedule.DayLimitInput{ExpectedVersion: ptr(days[0].Version), StandardLimit: ptr(3)}); err != nil {
		t.Fatal(err)
	}
	if err := try("clear", schedule.DayLimitInput{ExpectedVersion: ptr(days[0].Version + 1), UseScheduleDefault: true}); err != nil {
		t.Fatal(err)
	}
	if days, _ := h.store.StaffDays(h.ctx, chef, day(d), day(d)); days[0].StandardLimit != 7 || days[0].LimitOverride {
		t.Fatalf("after clearing: %+v", days[0])
	}

	// Two first limits on a new day race: the first holds its transaction
	// open; the second waits, then gets a stale version (409), never a fault.
	limitTx := func(tx pgx.Tx, key string, limit int) error {
		var id string
		var replayed bool
		return tx.QueryRow(h.ctx, `SELECT result_id::text, replayed FROM business.capacity_day_limit_set(
			$1::uuid, $2, $3, '2026-11-21', 0, $4)`, chef, key, strings.Repeat("e", 64), limit).Scan(&id, &replayed)
	}
	apiA, apiB := h.db.LoginConn(t, "gk_api"), h.db.LoginConn(t, "gk_api")
	txA, err := apiA.Begin(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := limitTx(txA, "race-a", 4); err != nil {
		t.Fatal(err)
	}
	raced := make(chan error, 1)
	go func() {
		txB, err := apiB.Begin(h.ctx)
		if err == nil {
			err = limitTx(txB, "race-b", 5)
			_ = txB.Rollback(h.ctx)
		}
		raced <- err
	}()
	select {
	case err := <-raced:
		t.Fatalf("second first-limit finished while the first was open: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := txA.Commit(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-raced; pgMessageAny(err) != "stale_version" {
		t.Fatalf("racing first limit: %v, want GK409 stale_version", err)
	}

	// A replayed publish returns the version it published, not a newer one.
	h.publish(t, "v2", ownerSchedule(1, "2027-01-05"))
	res, err := h.store.PublishVersion(h.ctx, chef, "v1", ownerSchedule(0, "2027-01-05"))
	if err != nil || !res.Replayed {
		t.Fatalf("replay: %+v, %v", res, err)
	}
	if v, err := h.store.VersionByID(h.ctx, chef, res.ID); err != nil || v.Revision != 1 {
		t.Fatalf("replayed version = %+v, %v; want revision 1", v, err)
	}
}

func TestLotVisibilityAndRules(t *testing.T) {
	h := newHarness(t)
	variant := h.readyVariant(t)
	h.store.Now = time.Now
	expires := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	newLot := func(key string) string {
		r, err := h.store.CreateLot(h.ctx, chef, key, schedule.LotInput{VariantID: variant, Kind: schedule.LotReady, Quantity: 2,
			PriceMinor: ptr[int64](8000), ExpiresAt: expires})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.store.SetLotState(h.ctx, chef, key+"-rel", r.ID, schedule.LotStateInput{ExpectedVersion: 1, Action: "release"}); err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	public := func() map[string]bool {
		lots, err := h.store.PublicLots(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]bool{}
		for _, l := range lots {
			m[l.ID] = true
		}
		return m
	}
	reserve := func(id string) string {
		return pgMessageAny(h.owner(t, "SELECT business.lot_reserve($1::uuid, 1, now())", id))
	}
	unreleased, err := h.store.CreateLot(h.ctx, chef, "u", schedule.LotInput{VariantID: variant, Kind: schedule.LotReady, Quantity: 1,
		PriceMinor: ptr[int64](8000), ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	withdrawn, expired := newLot("w"), newLot("e")
	if _, err := h.store.SetLotState(h.ctx, chef, "w-wd", withdrawn, schedule.LotStateInput{ExpectedVersion: 2, Action: "withdraw"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.admin.Exec(h.ctx, "UPDATE business.finished_cake_lot SET expires_at = now() - interval '1 minute' WHERE id = $1", expired); err != nil {
		t.Fatal(err)
	}
	if p := public(); p[unreleased.ID] || p[withdrawn] || p[expired] {
		t.Fatalf("hidden lots are public: %v", p)
	}
	for name, id := range map[string]string{"unreleased": unreleased.ID, "withdrawn": withdrawn, "expired": expired} {
		if got := reserve(id); got != "stock_unavailable" {
			t.Errorf("reserve %s: %q, want stock_unavailable", name, got)
		}
	}
	// A resale cake whose original handover has passed is neither listed
	// nor reservable; one handed over after it expires is refused.
	resale, err := h.store.CreateLot(h.ctx, chef, "rs", schedule.LotInput{VariantID: variant, Kind: schedule.LotResale, Quantity: 1,
		ResaleBaseMinor: ptr[int64](9000), OriginalHandoverAt: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339), ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	h.store.SetLotState(h.ctx, chef, "rs-rel", resale.ID, schedule.LotStateInput{ExpectedVersion: 1, Action: "release"})
	if !public()[resale.ID] {
		t.Fatal("released resale lot is not public")
	}
	if _, err := h.admin.Exec(h.ctx, "UPDATE business.finished_cake_lot SET original_handover_at = now() - interval '1 minute' WHERE id = $1", resale.ID); err != nil {
		t.Fatal(err)
	}
	if public()[resale.ID] || reserve(resale.ID) != "stock_unavailable" {
		t.Fatal("a resale cake past its original handover is still offered")
	}
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if _, err := h.store.CreateLot(h.ctx, chef, "past", schedule.LotInput{VariantID: variant, Kind: schedule.LotResale, Quantity: 1,
		ResaleBaseMinor: ptr[int64](9000), OriginalHandoverAt: past, ExpiresAt: expires}); err == nil {
		t.Fatal("resale with a past original handover was accepted")
	}
	late := time.Now().Add(96 * time.Hour).UTC().Format(time.RFC3339)
	if _, err := h.store.CreateLot(h.ctx, chef, "late", schedule.LotInput{VariantID: variant, Kind: schedule.LotResale, Quantity: 1,
		ResaleBaseMinor: ptr[int64](9000), OriginalHandoverAt: late, ExpiresAt: expires}); err == nil {
		t.Fatal("resale handed over after it expires was accepted")
	}
	// A ready lot needs a ready-stock variant.
	f, _ := h.cat.CreateFlavour(h.ctx, chef, "f2", catalog.FlavourInput{Name: "Vanilla Raspberry"})
	c, _ := h.cat.CreateCake(h.ctx, chef, "c2", catalog.CakeInput{Name: "Pink piping"})
	mto, _ := h.cat.CreateVariant(h.ctx, chef, "v2", c.ID, catalog.VariantInput{FlavourID: f.ID, SizeCode: "s", StockMode: catalog.MadeToOrder})
	n, _ := h.cat.CreateNotice(h.ctx, chef, "n2", catalog.NoticeInput{Body: "Notice"})
	if _, err := h.cat.CreateRecipeVersion(h.ctx, chef, "r2", catalog.RecipeVersionInput{VariantID: mto.ID, ExpectedVersion: 1,
		PriceMinor: 9000, ServingsMin: 7, ServingsMax: 10, Coating: "Swiss meringue buttercream", Composition: []string{"Vanilla sponge"},
		NoticeID: n.ID, Allergens: []catalog.Allergen{{Code: "eggs", Label: "EGGS"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.CreateLot(h.ctx, chef, "mto", schedule.LotInput{VariantID: mto.ID, Kind: schedule.LotReady, Quantity: 1,
		PriceMinor: ptr[int64](9000), ExpiresAt: expires}); conflict(err) != "state_conflict" {
		t.Fatalf("ready lot of a made-to-order variant: %v", err)
	}
	// A lot of an unpublished (archived) cake is hidden and not reservable.
	open := newLot("o")
	cakes, _ := h.cat.StaffPage(h.ctx, chef, "", 50)
	for _, ck := range cakes.Cakes {
		if ck.Name == "Chocolate swirl" {
			if _, err := h.cat.SetCakeState(h.ctx, chef, "arch", ck.ID, catalog.CakeStateInput{ExpectedVersion: ck.Version, State: catalog.StateArchived}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if public()[open] || reserve(open) != "stock_unavailable" {
		t.Fatal("a lot of an archived cake is still offered")
	}
}
