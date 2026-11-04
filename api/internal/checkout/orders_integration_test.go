//go:build integration

package checkout_test

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
)

// GK-003 refund policy fixture (labelled test data): the owner decision ID
// and the decisions.json revision this session read.
func (h *harness) policy(t *testing.T, key string) string {
	t.Helper()
	in := checkout.PolicyInput{SourceDecisionID: "BO-GK-003.refund_policy", SourceRevisionSHA256: strings.Repeat("ab", 32),
		Summary:     "Cancel more than 120 hours before handover: full refund. Later: 50% of the amount paid.",
		EffectiveAt: time.Now().UTC().Add(-time.Second).Format(time.RFC3339)}
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
	return must[catalog.Result](t)(h.co.RecordPolicy(h.ctx, chef, key, in)).ID
}

type orderJSON struct {
	ID                   string
	Lifecycle            string
	PaymentRequirement   string    `json:"payment_requirement"`
	TotalMinor           int64     `json:"total_minor"`
	FullRefundDeadlineAt time.Time `json:"full_refund_deadline_at"`
	Slot                 struct {
		StartsAt time.Time `json:"starts_at"`
	}
	Items []struct {
		NetPaidMinor int64   `json:"net_paid_minor"`
		LotID        *string `json:"lot_id"`
	}
}

func (h *harness) guestQuote(t *testing.T, key string, in checkout.QuoteInput) (string, []byte) {
	t.Helper()
	_, d := proof(t)
	return must[catalog.Result](t)(h.co.CreateQuote(h.ctx, "", d, key, in)).ID, d
}

func contact() checkout.Contact {
	return checkout.Contact{FullName: "Test Customer", Email: "test.customer@example.com"}
}

func (h *harness) checkout(subject string, d []byte, key, quote, policy string) (catalog.Result, error) {
	return h.co.Checkout(h.ctx, subject, d, key, checkout.CheckoutInput{QuoteID: quote, QuoteVersion: 1,
		ConsentedPolicyVersion: policy, Contact: contact()})
}

func (h *harness) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := h.admin.QueryRow(h.ctx, sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCheckoutCreatesOneOrderWithHolds(t *testing.T) {
	h := newHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.code(t, "c10", checkout.CodeInput{Code: "SPRING10", CodeTerms: checkout.CodeTerms{Kind: checkout.KindPercentage, BasisPoints: ptr(1000)}})
	in := checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot, FulfillmentMode: schedule.Collection, DiscountCode: "SPRING10",
		Lines: []checkout.QuoteLine{{VariantID: f.v1001, Quantity: 1}, {VariantID: f.v1000, Quantity: 1}, {LotID: f.readyLot, Quantity: 1}}}
	quote, d := h.guestQuote(t, "q", in)

	// A repeated checkout of the same quote, even concurrent, makes one order.
	results := make([]catalog.Result, 4)
	errs := make([]error, 4)
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); results[i], errs[i] = h.checkout("", d, "k", quote, pol) }()
	}
	wg.Wait()
	created := 0
	for i := range 4 {
		if errs[i] != nil || results[i].ID != results[0].ID {
			t.Fatalf("checkout %d: %+v %v", i, results[i], errs[i])
		}
		if !results[i].Replayed {
			created++
		}
	}
	if created != 1 || h.count(t, "SELECT count(*) FROM business.orders") != 1 {
		t.Fatalf("%d orders created", created)
	}
	order := results[0].ID
	// Another body for the same quote is refused.
	if _, err := h.co.Checkout(h.ctx, "", d, "k2", checkout.CheckoutInput{QuoteID: quote, QuoteVersion: 1,
		ConsentedPolicyVersion: pol, Contact: checkout.Contact{FullName: "Someone Else", Email: "x@example.com"}}); conflict(err) != "key_reused" {
		t.Fatalf("other body: %v", err)
	}

	raw, err := h.co.GetOrder(h.ctx, order, "", d)
	if err != nil {
		t.Fatal(err)
	}
	var o orderJSON
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	// 1001 + 1000 + 8500 = 10501; 10% = 1050.1 -> 1050; total 9451.
	if o.Lifecycle != "awaiting_payment" || o.PaymentRequirement != "full" || o.TotalMinor != 9451 || len(o.Items) != 3 ||
		!o.FullRefundDeadlineAt.Equal(o.Slot.StartsAt.Add(-120*time.Hour)) {
		t.Fatalf("order = %+v", o)
	}
	var net int64
	for _, it := range o.Items {
		net += it.NetPaidMinor
	}
	if net != 9451 {
		t.Fatalf("items net %d, want 9451", net)
	}
	// Holds: 2 made-to-order cakes tentative for 15 minutes; 1 ready cake
	// reserved; the code use reserved; a guest capability.
	if n := h.count(t, `SELECT cake_quantity FROM business.capacity_allocation WHERE order_ref = $1 AND state = 'tentative'
		AND expires_at = $2`, order, h.now.Add(15*time.Minute)); n != 2 {
		t.Fatalf("tentative cakes = %d", n)
	}
	if n := h.count(t, "SELECT reserved_quantity FROM business.finished_cake_lot WHERE id = $1", f.readyLot); n != 1 {
		t.Fatalf("lot reserved = %d", n)
	}
	if n := h.count(t, "SELECT count(*) FROM business.code_application WHERE order_id = $1 AND state = 'reserved' AND discount_minor = 1050", order); n != 1 {
		t.Fatal("code use not reserved")
	}
	// Access: the creating proof reads; another proof or a customer does not.
	_, other := proof(t)
	if _, err := h.co.GetOrder(h.ctx, order, "", other); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("other proof: %v", err)
	}
	if _, err := h.co.GetOrder(h.ctx, order, customerA, nil); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("customer reads a guest order: %v", err)
	}
	// Snapshots are immutable.
	for _, sql := range []string{
		"UPDATE business.orders SET total_minor = 1",
		"UPDATE business.orders SET full_refund_deadline_at = now()",
		"UPDATE business.order_item SET net_paid_minor = 1",
		"DELETE FROM business.orders",
	} {
		if _, err := h.admin.Exec(h.ctx, sql); dbtest.SQLState(err) != "GK409" {
			t.Errorf("%s: %v", sql, err)
		}
	}
}

// AC2: one physical cake is allocated once, and a refused checkout leaves no
// partial holds; both admitted last-place day payments are accepted.
func TestCheckoutResourcesAndNoPartialHolds(t *testing.T) {
	h := newHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	both := []checkout.QuoteLine{{VariantID: f.v1000, Quantity: 1}, {LotID: f.readyLot, Quantity: 2}}
	q1, d1 := h.guestQuote(t, "q1", checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot, FulfillmentMode: schedule.Collection, Lines: both})
	q2, d2 := h.guestQuote(t, "q2", checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot, FulfillmentMode: schedule.Collection, Lines: both})
	if _, err := h.checkout("", d1, "k", q1, pol); err != nil {
		t.Fatal(err)
	}
	if _, err := h.checkout("", d2, "k", q2, pol); conflict(err) != "item_unavailable" {
		t.Fatalf("second checkout of the same 2 cakes: %v", err)
	}
	if h.count(t, "SELECT count(*) FROM business.orders") != 1 ||
		h.count(t, "SELECT count(*) FROM business.capacity_allocation") != 1 ||
		h.count(t, "SELECT reserved_quantity FROM business.finished_cake_lot WHERE id = $1", f.readyLot) != 2 {
		t.Fatal("a refused checkout left a partial hold")
	}

	// Last place: 6 committed (directly, as paid orders would be), then two
	// customers check out the last cake; both are admitted and both commit.
	day := "2026-11-21"
	sat := must[schedule.Choices](t)(h.sch.Choices(h.ctx, dayOf(day), schedule.Collection, 1))
	for i := range 6 {
		q, d := h.guestQuote(t, "fill"+string(rune('a'+i)), checkout.QuoteInput{Date: day, SlotID: sat.Slots[0].ID,
			FulfillmentMode: schedule.Collection, Lines: []checkout.QuoteLine{{VariantID: f.v1001, Quantity: 1}}})
		r := must[catalog.Result](t)(h.checkout("", d, "k", q, pol))
		h.owner(t, "SELECT business.capacity_commit($1::uuid, $2::date)", r.ID, day)
	}
	var last []catalog.Result
	var quotes [2]string
	var proofs [2][]byte
	for i := range 2 {
		quotes[i], proofs[i] = h.guestQuote(t, "last"+string(rune('a'+i)), checkout.QuoteInput{Date: day, SlotID: sat.Slots[0].ID,
			FulfillmentMode: schedule.Collection, Lines: []checkout.QuoteLine{{VariantID: f.v1001, Quantity: 1}}})
	}
	for i := range 2 {
		last = append(last, must[catalog.Result](t)(h.checkout("", proofs[i], "k", quotes[i], pol)))
	}
	// While the two holds exist (6 committed + 2 held of 7), a new quote is
	// still given: holds do not own the last place.
	held := checkout.QuoteInput{Date: day, SlotID: sat.Slots[0].ID, FulfillmentMode: schedule.Collection,
		Lines: []checkout.QuoteLine{{VariantID: f.v1001, Quantity: 1}}}
	_, dh := proof(t)
	if _, err := h.co.CreateQuote(h.ctx, "", dh, "held", held); err != nil {
		t.Fatalf("quote on a day full of holds: %v", err)
	}
	for _, r := range last {
		h.owner(t, "SELECT business.capacity_commit($1::uuid, $2::date)", r.ID, day)
	}
	if n := h.count(t, "SELECT sum(cake_quantity)::int FROM business.capacity_allocation WHERE day = $1 AND state = 'committed'", day); n != 8 {
		t.Fatalf("committed on the last-place day = %d, want 8 of 7", n)
	}
	// Committed beyond the limit: no new quote for made-to-order cakes.
	_, dh = proof(t)
	if _, err := h.co.CreateQuote(h.ctx, "", dh, "after", held); conflict(err) != "slot_unavailable" {
		t.Fatalf("quote on a committed-full day: %v", err)
	}
}

func dayOf(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }

func TestCheckoutRefusesChangedQuotes(t *testing.T) {
	h := newHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	line := []checkout.QuoteLine{{VariantID: f.v1001, Quantity: 1}}
	quote := func(key string, lines []checkout.QuoteLine, code string) (string, []byte) {
		return h.guestQuote(t, key, checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot, FulfillmentMode: schedule.Collection,
			Lines: lines, DiscountCode: code})
	}

	// Equality expires the quote (24 h from its sent_at).
	q, d := quote("exp", line, "")
	h.now = h.now.Add(24 * time.Hour)
	if _, err := h.checkout("", d, "k", q, pol); conflict(err) != "quote_expired" {
		t.Fatalf("at expiry: %v", err)
	}
	h.now = h.now.Add(-24 * time.Hour)

	// A new recipe version (price) after the quote.
	q, d = quote("price", line, "")
	cake := must[catalog.Cake](t)(h.cat.StaffCake(h.ctx, chef, f.cake))
	var v catalog.Variant
	for _, x := range cake.Variants {
		if x.ID == f.v1001 {
			v = x
		}
	}
	must[catalog.Result](t)(h.cat.CreateRecipeVersion(h.ctx, chef, "r2", catalog.RecipeVersionInput{VariantID: f.v1001,
		ExpectedVersion: v.Version, PriceMinor: 1100, ServingsMin: 7, ServingsMax: 10, Coating: "Swiss meringue buttercream",
		Composition: []string{"Vanilla sponge"}, NoticeID: v.Recipe.Notice.ID, Allergens: v.Recipe.Allergens}))
	if _, err := h.checkout("", d, "k", q, pol); conflict(err) != "price_changed" {
		t.Fatalf("after a new recipe version: %v", err)
	}

	// A revised code after the quote.
	code := h.code(t, "c", checkout.CodeInput{Code: "TEN", CodeTerms: checkout.CodeTerms{Kind: checkout.KindPercentage, BasisPoints: ptr(1000), TotalLimit: ptr(1)}})
	line2 := []checkout.QuoteLine{{VariantID: f.v1000, Quantity: 1}}
	q, d = quote("code", line2, "TEN")
	qb, db := quote("code-b", line2, "TEN")
	must[catalog.Result](t)(h.co.ReviseCode(h.ctx, chef, "rev", code, checkout.CodeRevision{ExpectedVersion: 1,
		CodeTerms: checkout.CodeTerms{Kind: checkout.KindPercentage, BasisPoints: ptr(1500), TotalLimit: ptr(1)}}))
	if _, err := h.checkout("", d, "k", q, pol); conflict(err) != "price_changed" {
		t.Fatalf("after a code revision: %v", err)
	}
	_ = qb
	_ = db

	// The code's total limit: the second use is refused.
	q1, d1 := quote("lim1", line2, "TEN")
	q2, d2 := quote("lim2", line2, "TEN")
	if _, err := h.checkout("", d1, "k", q1, pol); err != nil {
		t.Fatal(err)
	}
	if _, err := h.checkout("", d2, "k", q2, pol); conflict(err) != "code_used_up" {
		t.Fatalf("second use of a 1-use code: %v", err)
	}

	// A new policy after the quote's review.
	q, d = quote("pol", line2, "")
	h.policy(t, "p2")
	if _, err := h.checkout("", d, "k", q, pol); conflict(err) != "policy_changed" {
		t.Fatalf("old policy: %v", err)
	}

	// A wrong proof is a missing quote; a signed-in stranger too.
	q, _ = quote("proof", line2, "")
	_, wrong := proof(t)
	cur := must[checkout.Policy](t)(h.co.CurrentPolicy(h.ctx)).ID
	if _, err := h.checkout("", wrong, "k", q, cur); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("wrong proof: %v", err)
	}
	if _, err := h.checkout(customerB, nil, "k", q, cur); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("stranger: %v", err)
	}
	// A malformed email at the gateway is input, not a fault.
	q, d = quote("mail", line2, "")
	var id string
	var replayed bool
	err := h.api.QueryRow(h.ctx, `SELECT result_id::text, replayed FROM business.checkout_create(NULL, $1, 'k', $2, $3::uuid, 1,
		$4::uuid, 'Test Customer', NULL, 'not-an-email', $5)`, d, strings.Repeat("a", 64), q, cur, h.now).Scan(&id, &replayed)
	if dbtest.SQLState(err) != "GK422" {
		t.Fatalf("bad email at the gateway: %v", err)
	}
}

func TestSignedInCheckout(t *testing.T) {
	h := newHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.code(t, "pc", checkout.CodeInput{Code: "ONCE", CodeTerms: checkout.CodeTerms{Kind: checkout.KindPercentage, BasisPoints: ptr(500), PerCustomerLimit: ptr(1)}})
	in := checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot, FulfillmentMode: schedule.Collection,
		Lines: []checkout.QuoteLine{{VariantID: f.v1000, Quantity: 1}}, DiscountCode: "ONCE"}
	q1 := must[catalog.Result](t)(h.co.CreateQuote(h.ctx, customerA, nil, "a1", in)).ID
	q2 := must[catalog.Result](t)(h.co.CreateQuote(h.ctx, customerA, nil, "a2", in)).ID
	o := must[catalog.Result](t)(h.checkout(customerA, nil, "k", q1, pol))
	if _, err := h.checkout(customerA, nil, "k", q2, pol); conflict(err) != "code_used_up" {
		t.Fatalf("per-customer limit: %v", err)
	}
	if _, err := h.co.GetOrder(h.ctx, o.ID, customerA, nil); err != nil {
		t.Fatalf("owner reads: %v", err)
	}
	if _, err := h.co.GetOrder(h.ctx, o.ID, customerB, nil); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("another customer reads: %v", err)
	}
	if h.count(t, "SELECT count(*) FROM business.order_guest_capability") != 0 {
		t.Fatal("a signed-in order got a guest capability")
	}
}
