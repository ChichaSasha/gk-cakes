//go:build integration

package checkout_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	chef      = "22ac712e-4e38-48bb-a56d-fa7809ccc13f"
	customerA = "55ac712e-4e38-48bb-a56d-fa7809ccc13f"
	customerB = "66ac712e-4e38-48bb-a56d-fa7809ccc13f"
)

type harness struct {
	ctx   context.Context
	db    *dbtest.DB
	api   *pgxpool.Pool
	admin *pgx.Conn
	co    *checkout.Store
	cat   *catalog.Store
	sch   *schedule.Store
	now   time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessAt(t, "")
}

// newHarnessAt migrates up to and including version target (all when "").
func newHarnessAt(t *testing.T, target string) *harness {
	t.Helper()
	ctx := context.Background()
	db := dbtest.New(t)
	if _, err := migrate.RunTo(ctx, db.MigratorConn(t), migrations.FS, target); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, db.LoginURL(t, "gk_api"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	h := &harness{ctx: ctx, db: db, api: pool, admin: db.AdminConn(t), cat: &catalog.Store{DB: pool},
		now: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)}
	clock := func() time.Time { return h.now }
	h.co = &checkout.Store{DB: pool, Now: clock}
	h.sch = &schedule.Store{DB: pool, Now: clock}
	h.owner(t, "SELECT business.staff_grant($1::uuid, 'chef', 'test')", chef)
	return h
}

func pgMessage(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Message
	}
	return ""
}

// ownerErr is owner without failing the test.
func (h *harness) ownerErr(t *testing.T, sql string, args ...any) error {
	t.Helper()
	tx, err := h.db.MigratorConn(t).Begin(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(h.ctx)
	if _, err := tx.Exec(h.ctx, "SET LOCAL ROLE gk_owner"); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(h.ctx, sql, args...)
	return err
}

func (h *harness) owner(t *testing.T, sql string, args ...any) {
	t.Helper()
	tx, err := h.db.MigratorConn(t).Begin(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(h.ctx)
	if _, err := tx.Exec(h.ctx, "SET LOCAL ROLE gk_owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(h.ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	if err := tx.Commit(h.ctx); err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T { return &v }

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

// fixture: an active cake with made-to-order variants priced 1001p and
// 1000p (AC1 money fixture), a ready-stock variant with a released lot of
// 2 at 8500p, and a schedule with Saturday 12:30-15:00 starts, limit 7.
type fixture struct {
	v1001, v1000, readyLot, cake string
	slot                         string
}

func (h *harness) setup(t *testing.T) fixture {
	t.Helper()
	res := must[catalog.Result](t)
	var f fixture
	fl := res(h.cat.CreateFlavour(h.ctx, chef, "f", catalog.FlavourInput{Name: "Vanilla Raspberry"})).ID
	n := res(h.cat.CreateNotice(h.ctx, chef, "n", catalog.NoticeInput{Body: "Shared kitchen notice"})).ID
	f.cake = res(h.cat.CreateCake(h.ctx, chef, "c", catalog.CakeInput{Name: "Pink piping"})).ID
	variant := func(key, size, mode string, price int64) string {
		v := res(h.cat.CreateVariant(h.ctx, chef, key, f.cake, catalog.VariantInput{FlavourID: fl, SizeCode: size, StockMode: mode})).ID
		res(h.cat.CreateRecipeVersion(h.ctx, chef, key+"-r", catalog.RecipeVersionInput{VariantID: v, ExpectedVersion: 1,
			PriceMinor: price, ServingsMin: 7, ServingsMax: 10, Coating: "Swiss meringue buttercream",
			Composition: []string{"Vanilla sponge"}, NoticeID: n, Allergens: []catalog.Allergen{{Code: "eggs", Label: "EGGS"}}}))
		return v
	}
	f.v1001 = variant("a", "s", catalog.MadeToOrder, 1001)
	f.v1000 = variant("b", "m", catalog.MadeToOrder, 1000)
	ready := variant("r", "l", catalog.ReadyStock, 9000)
	res(h.cat.SetCakeState(h.ctx, chef, "pub", f.cake, catalog.CakeStateInput{ExpectedVersion: 1, State: catalog.StateActive}))
	f.readyLot = res(h.sch.CreateLot(h.ctx, chef, "lot", schedule.LotInput{VariantID: ready, Kind: schedule.LotReady, Quantity: 2,
		PriceMinor: ptr[int64](8500), ExpiresAt: time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)})).ID
	res(h.sch.SetLotState(h.ctx, chef, "rel", f.readyLot, schedule.LotStateInput{ExpectedVersion: 1, Action: "release"}))
	res(h.sch.PublishVersion(h.ctx, chef, "sched", schedule.VersionInput{ExpectedRevision: ptr(0), StandardLimit: ptr(7),
		BookableUntil: "2027-01-31", Weekdays: []schedule.WeekdayInput{
			{Weekday: "saturday", Opens: "12:30", Closes: "15:00", Starts: []string{"12:30", "13:00", "13:30", "14:00"}}}}))
	c, err := h.sch.Choices(h.ctx, day("2026-11-14"), schedule.Collection, 1)
	if err != nil || len(c.Slots) != 4 {
		t.Fatalf("slots: %+v %v", c, err)
	}
	f.slot = c.Slots[0].ID
	return f
}

func day(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }

func proof(t *testing.T) (string, []byte) {
	t.Helper()
	var b [32]byte
	_, _ = rand.Read(b[:])
	p := base64.RawURLEncoding.EncodeToString(b[:])
	d, err := checkout.GuestProofDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	return p, d
}

type quoteJSON struct {
	ID              string
	Version         int
	Currency        string
	SentAt          time.Time `json:"sent_at"`
	ExpiresAt       time.Time `json:"expires_at"`
	SubtotalMinor   int64     `json:"subtotal_minor"`
	DiscountMinor   int64     `json:"discount_minor"`
	TotalMinor      int64     `json:"total_minor"`
	FulfillmentMode string    `json:"fulfillment_mode"`
	Discount        *struct{ Code, Kind string }
	Lines           []struct {
		LineNo         int   `json:"line_no"`
		UnitPriceMinor int64 `json:"unit_price_minor"`
		DiscountMinor  int64 `json:"discount_minor"`
		NetMinor       int64 `json:"net_minor"`
		Quantity       int
	}
}

func (h *harness) quote(t *testing.T, subject string, digest []byte, key string, in checkout.QuoteInput) (quoteJSON, error) {
	t.Helper()
	if err := in.Validate(); err != nil {
		t.Fatalf("input: %v", err)
	}
	r, err := h.co.CreateQuote(h.ctx, subject, digest, key, in)
	if err != nil {
		return quoteJSON{}, err
	}
	raw, err := h.co.GetQuote(h.ctx, r.ID, subject, digest)
	if err != nil {
		t.Fatal(err)
	}
	var q quoteJSON
	if err := json.Unmarshal(raw, &q); err != nil {
		t.Fatal(err)
	}
	q.ID = r.ID
	return q, nil
}

func (h *harness) code(t *testing.T, key string, in checkout.CodeInput) string {
	t.Helper()
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
	return must[catalog.Result](t)(h.co.CreateCode(h.ctx, chef, key, in)).ID
}

func conflict(err error) string {
	var c *catalog.ConflictError
	if errors.As(err, &c) {
		return c.Code
	}
	return ""
}

func invalidField(err error) string {
	var inv *catalog.InvalidError
	if errors.As(err, &inv) {
		return inv.Field + ": " + inv.Message
	}
	return ""
}

// AC1: 1001p + 1000p at 10% is 200p, allocated 100p + 100p (floor 100/99,
// the leftover penny to the larger remainder), and the totals add up.
func TestPercentageCodeMoney(t *testing.T) {
	h := newHarness(t)
	f := h.setup(t)
	h.code(t, "c10", checkout.CodeInput{Code: "spring10", CodeTerms: checkout.CodeTerms{Kind: checkout.KindPercentage, BasisPoints: ptr(1000)}})
	_, digest := proof(t)
	q, err := h.quote(t, "", digest, "q1", checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot, FulfillmentMode: schedule.Collection,
		Lines: []checkout.QuoteLine{{VariantID: f.v1001, Quantity: 1}, {VariantID: f.v1000, Quantity: 1}}, DiscountCode: " Spring10 "})
	if err != nil {
		t.Fatal(err)
	}
	if q.SubtotalMinor != 2001 || q.DiscountMinor != 200 || q.TotalMinor != 1801 || q.Currency != "GBP" ||
		q.Lines[0].DiscountMinor != 100 || q.Lines[1].DiscountMinor != 100 || q.Lines[0].NetMinor != 901 || q.Lines[1].NetMinor != 900 ||
		q.Discount == nil || q.Discount.Code != "SPRING10" || q.Version != 1 {
		t.Fatalf("quote = %+v", q)
	}
	// Commands answer with their own code among several.
	other := h.code(t, "c20", checkout.CodeInput{Code: "OTHER20", CodeTerms: checkout.CodeTerms{Kind: checkout.KindPercentage, BasisPoints: ptr(2000)}})
	if c, err := h.co.CodeByID(h.ctx, chef, other); err != nil || c.Code != "OTHER20" || *c.BasisPoints != 2000 {
		t.Fatalf("code by ID = %+v, %v", c, err)
	}
	if !q.ExpiresAt.Equal(q.SentAt.Add(24*time.Hour)) || !q.SentAt.Equal(h.now) {
		t.Fatalf("validity %s - %s", q.SentAt, q.ExpiresAt)
	}
	// The same key and body return the same quote; another body is refused.
	in := checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot, FulfillmentMode: schedule.Collection,
		Lines: []checkout.QuoteLine{{VariantID: f.v1001, Quantity: 1}, {VariantID: f.v1000, Quantity: 1}}, DiscountCode: "SPRING10"}
	if again, err := h.co.CreateQuote(h.ctx, "", digest, "q1", in); err != nil || again.ID != q.ID || !again.Replayed {
		t.Fatalf("replay = %+v, %v", again, err)
	}
	in.Lines[0].Quantity = 2
	if _, err := h.co.CreateQuote(h.ctx, "", digest, "q1", in); conflict(err) != "key_reused" {
		t.Fatalf("changed body: %v", err)
	}
	// Quotes are immutable.
	if _, err := h.admin.Exec(h.ctx, "UPDATE business.checkout_quote SET total_minor = 1"); dbtest.SQLState(err) != "GK409" {
		t.Fatalf("quote update: %v", err)
	}
}

// The allocation always sums to D = round_half_up(subtotal x bps / 10000),
// every share is within one penny of its exact proportion, and the K03 and
// GK-005 fixtures hold (1005p at 10% = 101p; 10100p at 15% = 1515p).
func TestDiscountAllocationProperties(t *testing.T) {
	h := newHarness(t)
	alloc := func(amounts []int64, bps int) []int64 {
		var out []int64
		if err := h.admin.QueryRow(h.ctx, "SELECT business.discount_allocate($1::bigint[], $2)", amounts, bps).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	for _, c := range []struct {
		amounts []int64
		bps     int
		want    []int64
	}{
		{[]int64{1001, 1000}, 1000, []int64{100, 100}},
		{[]int64{1005}, 1000, []int64{101}},
		{[]int64{10100}, 1500, []int64{1515}},
		{[]int64{5000}, 1000, []int64{500}},
		{[]int64{1, 1, 1}, 5000, []int64{1, 1, 0}}, // D = 2 (1.5 half up); ties by position
		{[]int64{9000}, 10000, []int64{9000}},
	} {
		if got := alloc(c.amounts, c.bps); fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("allocate(%v, %d) = %v, want %v", c.amounts, c.bps, got, c.want)
		}
	}
	rng := mrand.New(mrand.NewPCG(1, 2))
	for range 300 {
		n := 1 + rng.IntN(20)
		amounts := make([]int64, n)
		var sum int64
		for i := range amounts {
			amounts[i] = 1 + rng.Int64N(200_000_000)
			sum += amounts[i]
		}
		bps := 1 + rng.IntN(10000)
		got := alloc(amounts, bps)
		d := (sum*int64(bps) + 5000) / 10000
		var total int64
		for i, a := range got {
			total += a
			exact := float64(d) * float64(amounts[i]) / float64(sum)
			if float64(a) < exact-1 || float64(a) > exact+1 || a < 0 || a > amounts[i] {
				t.Fatalf("share %d of %v at %d bps = %d, exact %.2f", i, amounts, bps, a, exact)
			}
		}
		if total != d {
			t.Fatalf("allocation of %v at %d bps sums to %d, want %d", amounts, bps, total, d)
		}
	}
	// gk_api cannot call the owner-internal helper.
	var out []int64
	if err := h.api.QueryRow(h.ctx, "SELECT business.discount_allocate('{1}'::bigint[], 1)").Scan(&out); dbtest.SQLState(err) != "42501" {
		t.Fatalf("gk_api discount_allocate: %v", err)
	}
}

func TestQuoteRules(t *testing.T) {
	h := newHarness(t)
	f := h.setup(t)
	_, digest := proof(t)
	base := func() checkout.QuoteInput {
		return checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot, FulfillmentMode: schedule.Collection,
			Lines: []checkout.QuoteLine{{VariantID: f.v1001, Quantity: 1}}}
	}
	key := 0
	// Each guest quote needs a fresh proof (K02); d only marks a guest.
	try := func(subject string, d []byte, in checkout.QuoteInput) error {
		key++
		if d != nil {
			_, d = proof(t)
		}
		_, err := h.quote(t, subject, d, fmt.Sprintf("k%d", key), in)
		return err
	}
	fresh := func() []byte { _, d := proof(t); return d }

	// Codes: free delivery only with GK delivery; a per-customer code needs
	// sign-in; expired and archived codes are unknown.
	h.code(t, "fd", checkout.CodeInput{Code: "FREEDELIVERY", CodeTerms: checkout.CodeTerms{Kind: checkout.KindFreeDelivery}})
	h.code(t, "pc", checkout.CodeInput{Code: "ONCE15", CodeTerms: checkout.CodeTerms{Kind: checkout.KindPercentage, BasisPoints: ptr(1500), PerCustomerLimit: ptr(1)}})
	soon := h.code(t, "ex", checkout.CodeInput{Code: "SOON", CodeTerms: checkout.CodeTerms{Kind: checkout.KindPercentage, BasisPoints: ptr(500)}})
	// A later version that expired an hour before the quote clock (written
	// directly: the API only accepts future expiries).
	h.owner(t, `INSERT INTO business.discount_code_version (id, code_id, revision, kind, basis_points, expires_at, created_by)
		SELECT gen_random_uuid(), $1::uuid, 2, 'percentage', 500, '2026-11-05T09:00:00Z', created_by
		FROM business.discount_code WHERE id = $1::uuid`, soon)
	old := h.code(t, "ar", checkout.CodeInput{Code: "OLD", CodeTerms: checkout.CodeTerms{Kind: checkout.KindPercentage, BasisPoints: ptr(500)}})
	must[catalog.Result](t)(h.co.ArchiveCode(h.ctx, chef, "arch", old, catalog.VersionInput{ExpectedVersion: 1}))

	in := base()
	in.DiscountCode = "freedelivery"
	if err := try("", digest, in); !strings.HasPrefix(invalidField(err), "discount_code: applies to GK delivery") {
		t.Fatalf("free delivery with collection: %v", err)
	}
	in.FulfillmentMode = schedule.GKDelivery
	q, err := h.quote(t, "", fresh(), "fd-ok", in)
	if err != nil || q.DiscountMinor != 0 || q.TotalMinor != 1001 || q.Discount == nil || q.Discount.Kind != "free_delivery" {
		t.Fatalf("free delivery with GK delivery: %+v %v", q, err)
	}
	in = base()
	in.DiscountCode = "ONCE15"
	if err := try("", digest, in); !strings.Contains(invalidField(err), "sign in") {
		t.Fatalf("per-customer code as guest: %v", err)
	}
	if err := try(customerA, nil, in); err != nil {
		t.Fatalf("per-customer code signed in: %v", err)
	}
	in.DiscountCode = "OLD"
	if err := try(customerA, nil, in); !strings.HasPrefix(invalidField(err), "discount_code") {
		t.Fatalf("archived code: %v", err)
	}
	in.DiscountCode = "SOON"
	if err := try(customerA, nil, in); invalidField(err) != "discount_code: is not a current code" {
		t.Fatalf("expired code: %v", err)
	}

	// The appointment must be offered now: 192 h lead from now.
	h.now = time.Date(2026, 11, 6, 13, 0, 0, 0, time.UTC) // Sat 14 Nov 12:30 GMT is less than 192 h away
	if err := try("", digest, base()); conflict(err) != "slot_unavailable" {
		t.Fatalf("slot inside the lead time: %v", err)
	}
	h.now = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	in = base()
	in.SlotID = "00000000-0000-4000-8000-000000000000"
	if err := try("", digest, in); conflict(err) != "slot_unavailable" {
		t.Fatalf("unknown slot: %v", err)
	}

	// A full day: no made-to-order cakes, but ready-made cakes are quotable.
	h.owner(t, "SELECT business.capacity_day_lock('2026-11-14')")
	h.owner(t, "UPDATE business.handover_day SET standard_limit = 0 WHERE day = '2026-11-14'")
	if err := try("", digest, base()); conflict(err) != "slot_unavailable" {
		t.Fatalf("made-to-order on a full day: %v", err)
	}
	ready := base()
	ready.Lines = []checkout.QuoteLine{{LotID: f.readyLot, Quantity: 2}}
	q, err = h.quote(t, "", fresh(), "ready", ready)
	if err != nil || q.TotalMinor != 17000 || q.Lines[0].UnitPriceMinor != 8500 {
		t.Fatalf("ready cakes on a full day: %+v %v", q, err)
	}
	ready.Lines[0].Quantity = 3
	if err := try("", digest, ready); conflict(err) != "item_unavailable" {
		t.Fatalf("more ready cakes than in stock: %v", err)
	}
	h.owner(t, "UPDATE business.handover_day SET standard_limit = NULL WHERE day = '2026-11-14'")

	// An unpublished cake is not quotable.
	cake := must[catalog.Cake](t)(h.cat.StaffCake(h.ctx, chef, f.cake))
	must[catalog.Result](t)(h.cat.SetCakeState(h.ctx, chef, "unpub", f.cake, catalog.CakeStateInput{ExpectedVersion: cake.Version, State: catalog.StateArchived}))
	if err := try("", digest, base()); conflict(err) != "item_unavailable" {
		t.Fatalf("archived cake: %v", err)
	}
}

// A quote is readable only by its owner or its guest proof holder.
func TestQuoteAccess(t *testing.T) {
	h := newHarness(t)
	f := h.setup(t)
	_, digest := proof(t)
	_, other := proof(t)
	in := checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot, FulfillmentMode: schedule.Collection,
		Lines: []checkout.QuoteLine{{VariantID: f.v1000, Quantity: 1}}}
	guest := must[catalog.Result](t)(h.co.CreateQuote(h.ctx, "", digest, "g", in))
	mine := must[catalog.Result](t)(h.co.CreateQuote(h.ctx, customerA, nil, "a", in))
	for name, c := range map[string]struct {
		id, subject string
		digest      []byte
		ok          bool
	}{
		"guest with proof":      {guest.ID, "", digest, true},
		"guest, other proof":    {guest.ID, "", other, false},
		"guest quote, customer": {guest.ID, customerB, nil, false},
		"owner":                 {mine.ID, customerA, nil, true},
		"other customer":        {mine.ID, customerB, nil, false},
		"owner quote, a proof":  {mine.ID, "", digest, false},
	} {
		_, err := h.co.GetQuote(h.ctx, c.id, c.subject, c.digest)
		if c.ok != (err == nil) || (!c.ok && !errors.Is(err, catalog.ErrNotFound)) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The same key in another scope is another quote.
	if r := must[catalog.Result](t)(h.co.CreateQuote(h.ctx, "", other, "g", in)); r.ID == guest.ID || r.Replayed {
		t.Fatal("a guest's key leaked into another guest's scope")
	}
}

func TestQuoteSafetyRules(t *testing.T) {
	h := newHarness(t)
	f := h.setup(t)
	base := checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot, FulfillmentMode: schedule.Collection,
		Lines: []checkout.QuoteLine{{VariantID: f.v1000, Quantity: 1}}}
	direct := func(p_now string, code any, lines string, digest []byte) error {
		var id string
		var replayed bool
		return h.api.QueryRow(h.ctx, `SELECT result_id::text, replayed FROM business.quote_create(NULL, $1, 'direct', $2,
			'2026-11-14', $3::uuid, 'collection', $4::json, $5, $6::timestamptz)`,
			digest, strings.Repeat("a", 64), f.slot, lines, code, p_now).Scan(&id, &replayed)
	}

	// A guest proof belongs to one quote: a replay is fine, a new key is not.
	_, digest := proof(t)
	first := must[catalog.Result](t)(h.co.CreateQuote(h.ctx, "", digest, "one", base))
	if again := must[catalog.Result](t)(h.co.CreateQuote(h.ctx, "", digest, "one", base)); again.ID != first.ID || !again.Replayed {
		t.Fatalf("replay with the same proof: %+v", again)
	}
	if _, err := h.co.CreateQuote(h.ctx, "", digest, "two", base); !strings.HasPrefix(invalidField(err), "guest_proof") {
		t.Fatalf("reused proof: %v", err)
	}

	// A ready cake must be within its safe expiry at the end of the
	// appointment (Sat 14 Nov 13:30 GMT for the first slot).
	shortLot := must[catalog.Result](t)(h.sch.CreateLot(h.ctx, chef, "short", schedule.LotInput{VariantID: h.readyVariantOf(t, f),
		Kind: schedule.LotReady, Quantity: 1, PriceMinor: ptr[int64](5000), ExpiresAt: time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)})).ID
	must[catalog.Result](t)(h.sch.SetLotState(h.ctx, chef, "short-rel", shortLot, schedule.LotStateInput{ExpectedVersion: 1, Action: "release"}))
	h.owner(t, "UPDATE business.finished_cake_lot SET expires_at = '2026-11-14T13:29:59Z' WHERE id = $1", shortLot)
	in := base
	in.Lines = []checkout.QuoteLine{{LotID: shortLot, Quantity: 1}}
	_, d := proof(t)
	if _, err := h.co.CreateQuote(h.ctx, "", d, "short", in); conflict(err) != "item_unavailable" {
		t.Fatalf("lot expiring before the appointment ends: %v", err)
	}
	h.owner(t, "UPDATE business.finished_cake_lot SET expires_at = '2026-11-14T13:30:00Z' WHERE id = $1", shortLot)
	_, d = proof(t)
	if _, err := h.co.CreateQuote(h.ctx, "", d, "short", in); err != nil {
		t.Fatalf("lot good through the end of the appointment: %v", err)
	}

	// Resale lots are not quotable yet.
	h.owner(t, `UPDATE business.finished_cake_lot SET kind = 'resale', resale_base_minor = 10000, price_minor = 5000,
		original_handover_at = '2026-11-14T12:30:00Z' WHERE id = $1`, shortLot)
	_, d = proof(t)
	if _, err := h.co.CreateQuote(h.ctx, "", d, "resale", in); conflict(err) != "item_unavailable" {
		t.Fatalf("resale lot: %v", err)
	}

	// The gateway caps a basket at 20 cakes even when the API is bypassed,
	// also for ready cakes, which need no day places.
	readyV := h.readyVariantOf(t, f)
	lot := func(key string, q int) string {
		id := must[catalog.Result](t)(h.sch.CreateLot(h.ctx, chef, key, schedule.LotInput{VariantID: readyV, Kind: schedule.LotReady,
			Quantity: q, PriceMinor: ptr[int64](5000), ExpiresAt: time.Now().Add(60 * 24 * time.Hour).UTC().Format(time.RFC3339)})).ID
		must[catalog.Result](t)(h.sch.SetLotState(h.ctx, chef, key+"-rel", id, schedule.LotStateInput{ExpectedVersion: 1, Action: "release"}))
		return id
	}
	l11, l10 := lot("l11", 11), lot("l10", 10)
	_, d = proof(t)
	lines := `[{"lot_id":"` + l11 + `","quantity":11},{"lot_id":"` + l10 + `","quantity":10}]`
	if err := direct("2026-11-05T10:00:00Z", nil, lines, d); pgMessage(err) != "lines_invalid" {
		t.Fatalf("21 ready cakes: %v, want GK422 lines_invalid", err)
	}

	// Code expiry: refused at the expiry instant, accepted a second before.
	h.code(t, "edge", checkout.CodeInput{Code: "EDGE", CodeTerms: checkout.CodeTerms{Kind: checkout.KindPercentage, BasisPoints: ptr(1000)}})
	h.owner(t, `INSERT INTO business.discount_code_version (id, code_id, revision, kind, basis_points, expires_at, created_by)
		SELECT gen_random_uuid(), id, 2, 'percentage', 1000, '2026-11-05T10:00:00Z', created_by FROM business.discount_code WHERE code = 'EDGE'`)
	one := `[{"variant_id":"` + f.v1000 + `","quantity":1}]`
	_, d = proof(t)
	if err := direct("2026-11-05T10:00:00Z", "EDGE", one, d); dbtest.SQLState(err) != "GK422" {
		t.Fatalf("code at its expiry: %v", err)
	}
	_, d = proof(t)
	if err := direct("2026-11-05T09:59:59Z", "EDGE", one, d); err != nil {
		t.Fatalf("code a second before expiry: %v", err)
	}

	// A disabled owner can neither read nor create quotes.
	mine := must[catalog.Result](t)(h.co.CreateQuote(h.ctx, customerA, nil, "mine", base))
	h.owner(t, "SELECT business.account_set_disabled($1::uuid, true, 'test')", customerA)
	if _, err := h.co.GetQuote(h.ctx, mine.ID, customerA, nil); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("disabled owner reads: %v", err)
	}
	if _, err := h.co.CreateQuote(h.ctx, customerA, nil, "mine-2", base); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("disabled owner creates: %v", err)
	}

	// Retention: only the owner-only purge removes quotes, and only those
	// expired more than 30 days ago.
	if _, err := h.admin.Exec(h.ctx, "DELETE FROM business.checkout_quote"); dbtest.SQLState(err) != "GK409" {
		t.Fatalf("plain delete: %v", err)
	}
	var before int
	if err := h.admin.QueryRow(h.ctx, "SELECT count(*) FROM business.checkout_quote").Scan(&before); err != nil {
		t.Fatal(err)
	}
	// An old quote (sent 40 days ago) with one line.
	h.owner(t, `INSERT INTO business.checkout_quote (id, guest_proof_digest, idempotency_key, body_sha256, handover_day, slot_id,
		slot_starts_at, slot_ends_at, schedule_revision, fulfillment_mode, subtotal_minor, discount_minor, total_minor, sent_at, expires_at)
		SELECT 'aaaaaaaa-0000-4000-8000-000000000001', sha256('old'::bytea), 'old', body_sha256, handover_day, slot_id, slot_starts_at,
		       slot_ends_at, schedule_revision, fulfillment_mode, subtotal_minor, discount_minor, total_minor,
		       now() - interval '40 days', now() - interval '39 days'
		FROM business.checkout_quote WHERE id = $1`, first.ID)
	h.owner(t, `INSERT INTO business.checkout_quote_line (quote_id, line_no, variant_id, recipe_version_id, quantity, unit_price_minor,
		discount_minor, net_minor) SELECT 'aaaaaaaa-0000-4000-8000-000000000001', 1, variant_id, recipe_version_id, quantity,
		unit_price_minor, discount_minor, net_minor FROM business.checkout_quote_line WHERE quote_id = $1`, first.ID)
	// A quote that expired 10 days ago is kept; a cutoff newer than 30 days
	// is refused.
	h.owner(t, `INSERT INTO business.checkout_quote (id, guest_proof_digest, idempotency_key, body_sha256, handover_day, slot_id,
		slot_starts_at, slot_ends_at, schedule_revision, fulfillment_mode, subtotal_minor, discount_minor, total_minor, sent_at, expires_at)
		SELECT 'aaaaaaaa-0000-4000-8000-000000000002', sha256('recent'::bytea), 'recent', body_sha256, handover_day, slot_id,
		       slot_starts_at, slot_ends_at, schedule_revision, fulfillment_mode, subtotal_minor, discount_minor, total_minor,
		       now() - interval '11 days', now() - interval '10 days'
		FROM business.checkout_quote WHERE id = $1`, first.ID)
	before++
	if err := h.ownerErr(t, "SELECT business.quote_purge_expired(now() - interval '29 days')"); dbtest.SQLState(err) != "22023" {
		t.Fatalf("purge cutoff 29 days ago: %v, want 22023", err)
	}
	h.owner(t, "SELECT business.quote_purge_expired(now() - interval '31 days')")
	var n, oldLines int
	if err := h.admin.QueryRow(h.ctx, `SELECT (SELECT count(*) FROM business.checkout_quote),
		(SELECT count(*) FROM business.checkout_quote_line WHERE quote_id = 'aaaaaaaa-0000-4000-8000-000000000001')`).Scan(&n, &oldLines); err != nil ||
		n != before || oldLines != 0 {
		t.Fatalf("after purge: %d quotes (want %d), %d old lines (want 0), %v", n, before, oldLines, err)
	}
	if _, err := h.api.Exec(h.ctx, "SELECT business.quote_purge_expired(now() - interval '31 days')"); dbtest.SQLState(err) != "42501" {
		t.Fatalf("gk_api purge: %v", err)
	}
}

// readyVariantOf returns the fixture's ready-stock variant.
func (h *harness) readyVariantOf(t *testing.T, f fixture) string {
	t.Helper()
	var id string
	if err := h.admin.QueryRow(h.ctx, "SELECT variant_id::text FROM business.finished_cake_lot WHERE id = $1", f.readyLot).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
