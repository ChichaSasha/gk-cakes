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
	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
	"github.com/jackc/pgx/v5"
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
		ExpectedRevision: revision, StandardLimit: 7, BookableUntil: bookableUntil,
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
	db := dbtest.New(t)
	if _, err := migrate.Run(ctx, db.MigratorConn(t), migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
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
	for name, starts := range map[string][]string{"ends after closing": {"14:30"}, "before opening": {"12:00"}} {
		bad := ownerSchedule(1, "2027-01-05")
		bad.Weekdays[2].Starts = starts
		if err := bad.Validate(); err == nil {
			t.Errorf("%s: API validation passed", name)
		}
		raw := `[{"weekday":6,"opens":"12:30","closes":"15:00","starts":["` + starts[0] + `"]}]`
		var id string
		var replayed bool
		err := h.api.QueryRow(h.ctx, `SELECT result_id::text, replayed FROM business.schedule_version_create(
			$1::uuid, $2, $3, 1, 7, '2027-01-05', $4::json, '{}')`, chef, "k-"+name, strings.Repeat("a", 64), raw).Scan(&id, &replayed)
		if dbtest.SQLState(err) != "GK422" {
			t.Errorf("%s at the gateway: %v, want GK422", name, err)
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
	if _, err := h.store.SetDayLimit(h.ctx, chef, "lim", day(d), schedule.DayLimitInput{ExpectedVersion: 1, StandardLimit: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.SetDayLimit(h.ctx, chef, "lim2", day(d), schedule.DayLimitInput{ExpectedVersion: 1, StandardLimit: 6}); conflict(err) != "stale_version" {
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
	if pub := h.days(t, d, d, 1)[d]; *pub.RemainingPlaces != 0 {
		t.Fatalf("public remaining = %d, want 0 (extras are not shown)", *pub.RemainingPlaces)
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
		if _, err := h.store.SetDayLimit(h.ctx, s, "x", day(d), schedule.DayLimitInput{ExpectedVersion: 2, StandardLimit: 9}); !errors.Is(err, catalog.ErrForbidden) {
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
	// Two concurrent reservations of the last cake: exactly one wins.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for k := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[k] = h.owner(t, "SELECT business.lot_reserve($1::uuid, 1, now())", lot.ID)
		}()
	}
	wg.Wait()
	won := 0
	for _, err := range errs {
		switch dbtest.SQLState(err) {
		case "":
			won++
		case "GK409":
		default:
			t.Fatalf("reserve: %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("%d reservations of one cake succeeded", won)
	}
	if lots, _ := h.store.PublicLots(h.ctx); len(lots) != 0 {
		t.Fatalf("a fully reserved lot is public: %+v", lots)
	}
	h.mustOwner(t, "SELECT business.lot_settle($1::uuid, 1, true)", lot.ID)
	if err := h.owner(t, "SELECT business.lot_settle($1::uuid, 1, false)", lot.ID); dbtest.SQLState(err) != "GK409" {
		t.Fatalf("settle without a reservation: %v", err)
	}

	// Resale: base x 50%, half up; original handover kept.
	original := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
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
	// A resale lot cannot carry its own price; withdrawn lots are hidden.
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
	call("GET", "/api/v1/ready-cakes", "", nil, 200)
}
