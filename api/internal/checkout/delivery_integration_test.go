//go:build integration

package checkout_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
)

// GK delivery quote, separate delivery payment and free-delivery waiver
// (K03, K04, K05, K06; GK-017 r02b state 15, Q9; migration 0035).

// The appointment of every delivery order here: Saturday 14 November 12:30
// London (GMT), so pay-by is Thursday 12 November 12:30Z.
var deliveryPayBy = time.Date(2026, 11, 12, 12, 30, 0, 0, time.UTC)

type deliveryBlock struct {
	State   string
	Payable *bool
	Quote   *struct {
		ID          string
		AmountMinor int64 `json:"amount_minor"`
		Currency    string
		QuotedAt    time.Time `json:"quoted_at"`
		PayBy       time.Time `json:"pay_by"`
	}
	PaidMinor      *int64 `json:"paid_minor"`
	UnmatchedMinor *int64 `json:"unmatched_minor"`
	Waived         *struct{ At time.Time }
	Payment        *struct {
		ID          string
		State       string
		AmountMinor int64   `json:"amount_minor"`
		CheckoutURL *string `json:"checkout_url"`
	}
}

type deliveryOrderView struct {
	Version              int
	Lifecycle            string
	PaidAt               *time.Time `json:"paid_at"`
	FullRefundDeadlineAt time.Time  `json:"full_refund_deadline_at"`
	Payment              *struct{ ID, State string }
	Delivery             *deliveryBlock
	Refund               *refundView
}

func (h *payHarness) deliveryView(t *testing.T, order string, d []byte) (deliveryOrderView, string) {
	t.Helper()
	raw, err := h.co.GetOrder(h.ctx, order, "", d)
	if err != nil {
		t.Fatal(err)
	}
	var v deliveryOrderView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v, string(raw)
}

// deliveryOrder places a 10000p GK delivery order (with discount code code,
// if any), pays it through the fake Stripe and returns it confirmed with
// its guest digest and cake collection.
func (h *payHarness) deliveryOrder(t *testing.T, f fixture, pol, key, code string) (string, []byte, string) {
	t.Helper()
	res := must[catalog.Result](t)
	fl := res(h.cat.CreateFlavour(h.ctx, chef, "f-"+key, catalog.FlavourInput{Name: "Lemon " + key})).ID
	n := res(h.cat.CreateNotice(h.ctx, chef, "n-"+key, catalog.NoticeInput{Body: "Kitchen notice " + key})).ID
	v := res(h.cat.CreateVariant(h.ctx, chef, "v-"+key, f.cake, catalog.VariantInput{FlavourID: fl, SizeCode: "xl", StockMode: catalog.MadeToOrder})).ID
	res(h.cat.CreateRecipeVersion(h.ctx, chef, "r-"+key, catalog.RecipeVersionInput{VariantID: v, ExpectedVersion: 1,
		PriceMinor: 10000, ServingsMin: 20, ServingsMax: 25, Coating: "Swiss meringue buttercream",
		Composition: []string{"Lemon sponge"}, NoticeID: n, Allergens: []catalog.Allergen{{Code: "eggs", Label: "EGGS"}}}))
	quote, d := h.guestQuote(t, "q-"+key, checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot,
		FulfillmentMode: schedule.GKDelivery, Lines: []checkout.QuoteLine{{VariantID: v, Quantity: 1}}, DiscountCode: code})
	order := must[catalog.Result](t)(h.checkout("", d, "k-"+key, quote, pol)).ID
	coll, session := h.started(t, order, d)
	h.stripe.pay(session, time.Date(2026, 11, 5, 10, 9, 0, 0, time.UTC))
	h.deliver(t, "evt_paid_"+key, "checkout.session.completed", "checkout.session", session, coll, order)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("cake payment: %v", errs)
	}
	if v, raw := h.deliveryView(t, order, d); v.Lifecycle != "confirmed" {
		t.Fatalf("not confirmed: %s", raw)
	}
	return order, d, coll
}

func (h *payHarness) quoteDelivery(key, order string, version int, amount int64) (catalog.Result, error) {
	return h.co.QuoteDelivery(h.ctx, chef, key, order, checkout.DeliveryQuoteInput{ExpectedVersion: version, AmountMinor: amount})
}

func (h *payHarness) payDelivery(order string, d []byte, key string, version int, quote string) (catalog.Result, error) {
	return h.co.StartDeliveryPayment(h.ctx, order, "", d, key, checkout.DeliveryPaymentInput{ExpectedVersion: version, QuoteID: quote})
}

// deliveryStarted pays the current quote and runs its dispatch: the
// collection and its session.
func (h *payHarness) deliveryStarted(t *testing.T, order string, d []byte, key string) (string, string) {
	t.Helper()
	v, raw := h.deliveryView(t, order, d)
	if v.Delivery == nil || v.Delivery.Quote == nil {
		t.Fatalf("no quote: %s", raw)
	}
	res, err := h.payDelivery(order, d, key, v.Version, v.Delivery.Quote.ID)
	if err != nil {
		t.Fatalf("delivery payment: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("dispatch: %v", errs)
	}
	c := h.collection(t, res.ID)
	if c.State != "pending" || c.SessionID == "" {
		t.Fatalf("after dispatch %+v", c)
	}
	return res.ID, c.SessionID
}

// deliveryPaid completes the delivery collection's session and its webhook.
func (h *payHarness) deliveryPaid(t *testing.T, order, coll, session, evt string) {
	t.Helper()
	h.stripe.pay(session, time.Date(2026, 11, 5, 11, 0, 0, 0, time.UTC))
	h.deliver(t, evt, "checkout.session.completed", "checkout.session", session, coll, order)
	if errs := h.drain(t); len(errs) != 0 || h.collection(t, coll).State != "succeeded" {
		t.Fatalf("delivery paid: %v %+v", errs, h.collection(t, coll))
	}
}

func (h *payHarness) deliveryOutcome(t *testing.T, coll string) string {
	t.Helper()
	var outcome string
	var reason *string
	if err := h.admin.QueryRow(h.ctx, "SELECT outcome, reason FROM business.delivery_payment_outcome WHERE collection_id = $1::uuid",
		coll).Scan(&outcome, &reason); err != nil {
		t.Fatalf("outcome of %s: %v", coll, err)
	}
	if reason != nil {
		return outcome + "/" + *reason
	}
	return outcome
}

// latePaid records the money of a cancelled collection's session arriving
// after all, as a reconcile that read Stripe before the decision would
// (real funds; migration 0017 records them as paid).
func (h *payHarness) latePaid(t *testing.T, coll, session string) {
	t.Helper()
	h.stripe.pay(session, time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC))
	h.holdProduction(t)
	h.owner(t, "SELECT business.payment_schedule_reconcile($1::uuid, now())", coll)
	h.workers++
	worker := fmt.Sprintf("00000000-0000-4000-8000-%012d", h.workers)
	var job string
	var gen int64
	if err := h.worker.QueryRow(h.ctx, `SELECT job_id::text, job_generation FROM business.job_claim($1::uuid, interval '60 seconds')`,
		worker).Scan(&job, &gen); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := h.worker.QueryRow(h.ctx, `SELECT collection_state FROM business.payment_reconcile_record(
		$1::uuid, $2::uuid, $3, $4::uuid, 'paid', $5, 'complete', 'paid', $6, $7::json, $8, NULL)`,
		job, worker, gen, coll, session, "pi_fake_"+session, `{"session_id":"`+session+`"}`,
		time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)).Scan(&state); err != nil || state != "succeeded" {
		t.Fatalf("late paid %s: %v", state, err)
	}
	if _, err := h.worker.Exec(h.ctx, "SELECT business.job_complete($1::uuid, $2::uuid, $3)", job, worker, gen); err != nil {
		t.Fatal(err)
	}
}

// makeReady starts production (chef) and marks the order ready
// (fulfilment) at its current version.
func (h *payHarness) makeReady(t *testing.T, order string, d []byte, key string) int {
	t.Helper()
	v, _ := h.deliveryView(t, order, d)
	if _, err := h.co.StartProduction(h.ctx, chef, "p-"+key, order, checkout.ProductionInput{ExpectedVersion: v.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.fulfil(fulfilment, "r-"+key, order, checkout.ActionReady, v.Version+1); err != nil {
		t.Fatalf("ready: %v", err)
	}
	return v.Version + 2
}

// compactJSON is raw without insignificant spaces (the API may re-encode a
// representation).
func compactJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var b bytes.Buffer
	if len(raw) == 0 {
		return ""
	}
	if err := json.Compact(&b, raw); err != nil {
		t.Fatalf("compact %s: %v", raw, err)
	}
	return b.String()
}

func staffDelivery(t *testing.T, m map[string]json.RawMessage) (map[string]json.RawMessage, string) {
	t.Helper()
	var d map[string]json.RawMessage
	if err := json.Unmarshal(m["delivery"], &d); err != nil {
		t.Fatalf("delivery %s: %v", m["delivery"], err)
	}
	return d, string(m["delivery"])
}

// Quote rules: GK delivery only, on a confirmed cake; from £100 to the
// money bound; versioned and idempotent; chef only; a new quote replaces an
// unpaid one; refused at or after pay-by and during a restore freeze
// (replays still answer); the note stays out of the audit and the
// customer's view.
func TestDeliveryQuoteRules(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)

	// A collection order has no delivery block and takes no quote.
	coll, _, _ := h.paidCollection(t, f, pol, "col")
	cd := h.digestOf(t, coll)
	if v, raw := h.deliveryView(t, coll, cd); v.Delivery != nil || !strings.Contains(raw, `"delivery" : null`) {
		t.Fatalf("collection order delivery: %s", raw)
	}
	cv, _ := h.deliveryView(t, coll, cd)
	if _, err := h.quoteDelivery("c0", coll, cv.Version, 12000); conflict(err) != "state_conflict" {
		t.Fatalf("collection order quote: %v", err)
	}

	// A GK delivery order still awaiting payment takes no quote either.
	res := must[catalog.Result](t)
	quote, ud := h.guestQuote(t, "q-unpaid", checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot,
		FulfillmentMode: schedule.GKDelivery, Lines: []checkout.QuoteLine{{VariantID: f.v1001, Quantity: 1}}})
	unpaid := res(h.checkout("", ud, "k-unpaid", quote, pol)).ID
	if v, raw := h.deliveryView(t, unpaid, ud); v.Delivery == nil || v.Delivery.State != "awaiting_quote" || v.Delivery.Payable == nil ||
		*v.Delivery.Payable {
		t.Fatalf("unpaid delivery order: %s", raw)
	}
	if _, err := h.quoteDelivery("u0", unpaid, 1, 12000); conflict(err) != "state_conflict" {
		t.Fatalf("unpaid order quote: %v", err)
	}

	order, d, _ := h.deliveryOrder(t, f, pol, "dq", "")
	v, raw := h.deliveryView(t, order, d)
	if v.Delivery == nil || v.Delivery.State != "awaiting_quote" || v.Delivery.Quote != nil || *v.Delivery.Payable {
		t.Fatalf("awaiting quote: %s", raw)
	}
	version := v.Version
	for name, who := range map[string]string{"fulfilment": fulfilment, "support": support, "customer": customerA} {
		if _, err := h.co.QuoteDelivery(h.ctx, who, "x", order, checkout.DeliveryQuoteInput{ExpectedVersion: version, AmountMinor: 12000}); !errors.Is(err, catalog.ErrForbidden) {
			t.Errorf("quote by %s: %v", name, err)
		}
	}
	// The database bounds the amount too (the store passes it through).
	for _, amount := range []int64{9999, 100000000, 0} {
		if _, err := h.quoteDelivery("bad", order, version, amount); !strings.HasPrefix(invalidField(err), "amount_minor") {
			t.Errorf("amount %d: %v", amount, err)
		}
	}
	if _, err := h.quoteDelivery("stale", order, version+1, 12000); conflict(err) != "stale_version" {
		t.Fatalf("stale: %v", err)
	}
	note := "Route via the A13, measured 8.2 km"
	q1, err := h.co.QuoteDelivery(h.ctx, chef, "q1", order, checkout.DeliveryQuoteInput{ExpectedVersion: version, AmountMinor: 12000, Note: &note})
	if err != nil || q1.Replayed {
		t.Fatalf("quote: %+v %v", q1, err)
	}
	if again, err := h.co.QuoteDelivery(h.ctx, chef, "q1", order, checkout.DeliveryQuoteInput{ExpectedVersion: version, AmountMinor: 12000, Note: &note}); err != nil ||
		!again.Replayed || again.ID != q1.ID {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, err := h.quoteDelivery("q1", order, version, 13000); conflict(err) != "key_reused" {
		t.Fatalf("key reused: %v", err)
	}
	v, raw = h.deliveryView(t, order, d)
	if v.Version != version+1 || v.Delivery.State != "quoted" || !*v.Delivery.Payable || v.Delivery.Quote.ID != q1.ID ||
		v.Delivery.Quote.AmountMinor != 12000 || v.Delivery.Quote.Currency != "GBP" || !v.Delivery.Quote.PayBy.Equal(deliveryPayBy) ||
		!v.Delivery.Quote.QuotedAt.Equal(h.now) || v.Delivery.Payment != nil || v.Delivery.PaidMinor != nil {
		t.Fatalf("quoted: %s", raw)
	}
	if strings.Contains(raw, "A13") {
		t.Fatalf("the chef's note reached the customer: %s", raw)
	}
	// A new quote replaces the unpaid one (immutable history).
	q2, err := h.quoteDelivery("q2", order, version+1, 15000)
	if err != nil {
		t.Fatal(err)
	}
	if v, raw := h.deliveryView(t, order, d); v.Version != version+2 || v.Delivery.Quote.ID != q2.ID || v.Delivery.Quote.AmountMinor != 15000 {
		t.Fatalf("replaced: %s", raw)
	}
	if n := h.count(t, "SELECT count(*) FROM business.delivery_quote WHERE order_id = $1::uuid", order); n != 2 {
		t.Fatalf("%d quotes, want both kept", n)
	}
	audits := h.auditDetails(t, "delivery.quoted")
	if len(audits) != 2 || !strings.Contains(audits[1], `"replaced_quote_id" : "`+q1.ID) || strings.Contains(strings.Join(audits, ""), "A13") {
		t.Fatalf("audit %v", audits)
	}

	// Staff views: the chef sees the note and amounts; technical support
	// neither; fulfilment the state only.
	chefView := h.staffJSON(t, chef, order)
	if hist := compactJSON(t, chefView["delivery_history"]); !strings.Contains(hist, "A13") || !strings.Contains(hist, `"amount_minor":15000`) ||
		!strings.Contains(hist, `"current":true`) {
		t.Fatalf("chef history %s", hist)
	}
	sup := h.staffJSON(t, support, order)
	if hist, del := string(sup["delivery_history"]), string(sup["delivery"]); strings.Contains(hist, "A13") || strings.Contains(hist, "amount") ||
		strings.Contains(del, "amount") || strings.Contains(del, "GBP") || !strings.Contains(del, `"pay_by"`) {
		t.Fatalf("support delivery %s history %s", del, hist)
	}
	ful := h.staffJSON(t, fulfilment, order)
	if del, _ := staffDelivery(t, ful); len(del) != 1 || string(del["state"]) != `"quoted"` || ful["delivery_history"] != nil {
		t.Fatalf("fulfilment delivery %s", ful["delivery"])
	}

	// A restore freeze refuses a new quote; the same key still replays.
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if _, err := h.quoteDelivery("q3", order, version+2, 16000); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("frozen quote: %v", err)
	}
	if again, err := h.quoteDelivery("q2", order, version+1, 15000); err != nil || !again.Replayed || again.ID != q2.ID {
		t.Fatalf("frozen replay: %+v %v", again, err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")

	// Pay-by (original handover - 48 h): quoting at it is refused; the
	// order page says pay_by_passed at the API's clock.
	h.now = deliveryPayBy.Add(-time.Second)
	if v, raw := h.deliveryView(t, order, d); v.Delivery.State != "quoted" || !*v.Delivery.Payable {
		t.Fatalf("a second before pay-by: %s", raw)
	}
	// Fulfilment's state follows the API's clock too (whatever the real
	// date), and the pay-by it is decided from is never shown.
	fulfilmentState := func() string {
		t.Helper()
		del, raw := staffDelivery(t, h.staffJSON(t, fulfilment, order))
		if len(del) != 1 {
			t.Fatalf("fulfilment delivery %s", raw)
		}
		return string(del["state"])
	}
	if got := fulfilmentState(); got != `"quoted"` {
		t.Fatalf("fulfilment a second before pay-by: %s", got)
	}
	h.now = deliveryPayBy
	if _, err := h.quoteDelivery("q4", order, version+2, 16000); conflict(err) != "delivery_pay_by_passed" {
		t.Fatalf("quote at pay-by: %v", err)
	}
	if v, raw := h.deliveryView(t, order, d); v.Delivery.State != "pay_by_passed" || *v.Delivery.Payable {
		t.Fatalf("at pay-by: %s", raw)
	}
	if got := fulfilmentState(); got != `"pay_by_passed"` {
		t.Fatalf("fulfilment at pay-by: %s", got)
	}
	if _, err := h.payDelivery(order, d, "late", version+2, q2.ID); conflict(err) != "delivery_pay_by_passed" {
		t.Fatalf("payment at pay-by: %v", err)
	}
	if n := h.count(t, "SELECT count(*) FROM business.payment_collection WHERE order_id = $1::uuid AND kind = 'delivery'", order); n != 0 {
		t.Fatalf("%d delivery collections after refusals", n)
	}
	if del, raw := staffDelivery(t, h.staffJSON(t, chef, order)); string(del["state"]) != `"pay_by_passed"` || string(del["payable"]) != "false" {
		t.Fatalf("chef at pay-by: %s", raw)
	}
}

// The customer pays the current quote through the payment worker; the
// webhook and reconcile settle it as paid: the cake's lifecycle, anchors and
// refund deadline are untouched; the order can then be dispatched.
func TestDeliveryPaymentIsPaidAndDispatched(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d, cake := h.deliveryOrder(t, f, pol, "dp", "")
	before, _ := h.deliveryView(t, order, d)
	_, anchor := h.productionJob(t, order)
	if _, err := h.payDelivery(order, d, "early", before.Version, "cccccccc-1111-4111-8111-11111111111c"); conflict(err) != "quote_not_current" {
		t.Fatalf("pay before any quote: %v", err)
	}
	q, err := h.quoteDelivery("q1", order, before.Version, 12000)
	if err != nil {
		t.Fatal(err)
	}
	version := before.Version + 1
	_, otherDigest := proof(t)
	for name, c := range map[string]struct {
		subject string
		digest  []byte
		version int
		quote   string
		check   func(error) bool
	}{
		"another guest":    {"", otherDigest, version, q.ID, func(err error) bool { return errors.Is(err, catalog.ErrNotFound) }},
		"another customer": {customerB, nil, version, q.ID, func(err error) bool { return errors.Is(err, catalog.ErrNotFound) }},
		"stale version":    {"", d, version - 1, q.ID, func(err error) bool { return conflict(err) == "stale_version" }},
		"another quote":    {"", d, version, "cccccccc-1111-4111-8111-11111111111c", func(err error) bool { return conflict(err) == "quote_not_current" }},
	} {
		_, err := h.co.StartDeliveryPayment(h.ctx, order, c.subject, c.digest, "x-"+strings.ReplaceAll(name, " ", "-"), checkout.DeliveryPaymentInput{ExpectedVersion: c.version, QuoteID: c.quote})
		if !c.check(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A restore freeze refuses a new delivery collection.
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	if _, err := h.payDelivery(order, d, "dpay", version, q.ID); !errors.Is(err, checkout.ErrRecoveryFrozen) {
		t.Fatalf("frozen: %v", err)
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	res, err := h.payDelivery(order, d, "dpay", version, q.ID)
	if err != nil || res.Replayed {
		t.Fatalf("delivery payment: %+v %v", res, err)
	}
	if again, err := h.payDelivery(order, d, "dpay", version, q.ID); err != nil || !again.Replayed || again.ID != res.ID {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, err := h.payDelivery(order, d, "dpay-2", version, q.ID); conflict(err) != "payment_in_progress" {
		t.Fatalf("second payment: %v", err)
	}
	if _, err := h.quoteDelivery("q2", order, version, 13000); conflict(err) != "payment_in_progress" {
		t.Fatalf("requote while paying: %v", err)
	}
	if _, err := h.co.WaiveDelivery(h.ctx, chef, "w", order, checkout.DeliveryWaiverInput{ExpectedVersion: version, DistanceM: 5000,
		DistanceSource: "map"}); conflict(err) != "waiver_not_eligible" {
		t.Fatalf("waiver without a free-delivery code: %v", err)
	}
	if v, raw := h.deliveryView(t, order, d); v.Delivery.State != "payment_checking" || *v.Delivery.Payable ||
		v.Delivery.Payment == nil || v.Delivery.Payment.ID != res.ID || v.Delivery.Payment.State != "created" ||
		v.Payment == nil || v.Payment.ID != cake {
		t.Fatalf("checking: %s", raw)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("dispatch: %v", errs)
	}
	c := h.collection(t, res.ID)
	if c.State != "pending" {
		t.Fatalf("collection %+v", c)
	}
	h.stripe.mu.Lock()
	form := h.stripe.forms[res.ID]
	h.stripe.mu.Unlock()
	if !strings.Contains(form, "unit_amount%5D=12000") || !strings.Contains(form, "currency%5D=gbp") ||
		!strings.Contains(form, "metadata%5Bcollection_id%5D="+res.ID) || strings.Contains(form, "example.com") ||
		strings.Contains(form, "Test Customer") {
		t.Fatalf("Stripe request %s", form)
	}
	v, raw := h.deliveryView(t, order, d)
	if v.Delivery.State != "payment_checking" || v.Delivery.Payment.State != "pending" || v.Delivery.Payment.CheckoutURL == nil ||
		v.Delivery.Payment.AmountMinor != 12000 {
		t.Fatalf("pending: %s", raw)
	}
	// The chef's view never carries the customer's payment link.
	if del, raw := staffDelivery(t, h.staffJSON(t, chef, order)); strings.Contains(raw, "checkout.stripe.test") || del["payment"] == nil {
		t.Fatalf("chef delivery %s", raw)
	}

	h.deliveryPaid(t, order, res.ID, c.SessionID, "evt_delivery_paid")
	if got := h.deliveryOutcome(t, res.ID); got != "paid" {
		t.Fatalf("outcome %s", got)
	}
	after, raw := h.deliveryView(t, order, d)
	if after.Delivery.State != "paid" || after.Delivery.PaidMinor == nil || *after.Delivery.PaidMinor != 12000 || *after.Delivery.Payable ||
		after.Delivery.Payment.CheckoutURL != nil {
		t.Fatalf("paid: %s", raw)
	}
	// K03: the delivery payment never restarts the cake's preparation or
	// deadline and changes no lifecycle; no order command was recorded.
	if after.Lifecycle != before.Lifecycle || after.Version != version || !after.PaidAt.Equal(*before.PaidAt) ||
		!after.FullRefundDeadlineAt.Equal(before.FullRefundDeadlineAt) || after.Payment.ID != cake {
		t.Fatalf("cake changed: before %+v after %s", before, raw)
	}
	if _, at := h.productionJob(t, order); !at.Equal(anchor) {
		t.Fatalf("production anchor %s -> %s", anchor, at)
	}
	if n := h.count(t, "SELECT count(*) FROM business.order_command WHERE collection_id = $1::uuid", res.ID); n != 0 {
		t.Fatalf("%d commands for the delivery collection", n)
	}
	if n := h.count(t, "SELECT count(*) FROM business.order_command WHERE order_id = $1::uuid", order); n != 1 {
		t.Fatalf("%d commands, want only the cake's paid confirmation", n)
	}
	if audits := h.auditDetails(t, "delivery.paid"); len(audits) != 1 || !strings.Contains(audits[0], res.ID) {
		t.Fatalf("audit %v", audits)
	}
	// Paid: no new quote, no second payment.
	if _, err := h.quoteDelivery("q3", order, version, 13000); conflict(err) != "state_conflict" {
		t.Fatalf("requote after payment: %v", err)
	}
	if _, err := h.payDelivery(order, d, "dpay-3", version, q.ID); conflict(err) != "state_conflict" {
		t.Fatalf("pay again: %v", err)
	}

	// K06: the paid delivery lets the order out for delivery.
	ready := h.makeReady(t, order, d, "dp")
	if r, err := h.fulfil(fulfilment, "d1", order, checkout.ActionDispatch, ready); err != nil || r.FulfillmentState != "out_for_delivery" {
		t.Fatalf("dispatch: %+v %v", r, err)
	}
	if v, raw := h.deliveryView(t, order, d); v.Delivery.State != "paid" || *v.Delivery.Payable {
		t.Fatalf("out for delivery: %s", raw)
	}
	h.noPersonalDataInLogs(t)
}

// Failure and expiry make the quote payable again; a payment started before
// pay-by that succeeds after it is honoured (K04); a success after the quote
// was replaced, or a second success for one quote, is kept as real money,
// does not pay the current quote and asks the chef (attention).
func TestDeliveryPaymentFailureExpiryAndLateSuccess(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, d, _ := h.deliveryOrder(t, f, pol, "dx", "")
	v, _ := h.deliveryView(t, order, d)
	q1, err := h.quoteDelivery("q1", order, v.Version, 12000)
	if err != nil {
		t.Fatal(err)
	}
	version := v.Version + 1

	// A definitive Stripe refusal: failed, payable again.
	h.stripe.status, h.stripe.errType = 400, "invalid_request_error"
	rejected, err := h.payDelivery(order, d, "rej", version, q1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(attention(errs)) != 1 || h.collection(t, rejected.ID).State != "failed" {
		t.Fatalf("rejected: %v %+v", errs, h.collection(t, rejected.ID))
	}
	h.stripe.status, h.stripe.errType = 0, ""
	if v, raw := h.deliveryView(t, order, d); v.Delivery.State != "quoted" || !*v.Delivery.Payable || v.Delivery.Payment.State != "failed" {
		t.Fatalf("after failure: %s", raw)
	}

	// An expired session: cancelled, payable again.
	c1, s1 := h.deliveryStarted(t, order, d, "c1")
	h.stripe.session(s1).status = "expired"
	h.deliver(t, "evt_c1_expired", "checkout.session.expired", "checkout.session", s1, c1, order)
	if errs := h.drain(t); len(errs) != 0 || h.collection(t, c1).State != "cancelled" {
		t.Fatalf("expired: %v %+v", errs, h.collection(t, c1))
	}
	if v, raw := h.deliveryView(t, order, d); v.Delivery.State != "quoted" || !*v.Delivery.Payable {
		t.Fatalf("after expiry: %s", raw)
	}

	// The chef replaces the quote; the expired session's money then arrives
	// after all (real funds): kept, not the current quote's, attention.
	q2, err := h.quoteDelivery("q2", order, version, 14000)
	if err != nil {
		t.Fatal(err)
	}
	version++
	h.latePaid(t, c1, s1)
	if got := h.deliveryOutcome(t, c1); got != "unmatched/quote_replaced" {
		t.Fatalf("outcome %s", got)
	}
	if v, raw := h.deliveryView(t, order, d); v.Delivery.State != "quoted" || v.Delivery.Quote.ID != q2.ID || v.Delivery.PaidMinor != nil ||
		v.Version != version {
		t.Fatalf("unmatched: %s", raw)
	}
	flags := string(h.staffJSON(t, chef, order)["attention"])
	if !strings.Contains(flags, "delivery_payment_unmatched") {
		t.Fatalf("attention %s", flags)
	}
	if hist := compactJSON(t, h.staffJSON(t, chef, order)["delivery_history"]); !strings.Contains(hist, `"reason":"quote_replaced"`) {
		t.Fatalf("history %s", hist)
	}
	ready := h.makeReady(t, order, d, "dx")
	if _, err := h.fulfil(fulfilment, "d0", order, checkout.ActionDispatch, ready); conflict(err) != "delivery_not_paid" {
		t.Fatalf("dispatch on an unmatched payment: %v", err)
	}
	version = ready

	// The current quote is paid by a payment started before pay-by that
	// succeeds after it: honoured.
	c2, s2 := h.deliveryStarted(t, order, d, "c2")
	h.now = deliveryPayBy.Add(time.Hour)
	if _, err := h.payDelivery(order, d, "late", version, q2.ID); conflict(err) != "payment_in_progress" {
		t.Fatalf("pay while checking after pay-by: %v", err)
	}
	if v, raw := h.deliveryView(t, order, d); v.Delivery.State != "payment_checking" {
		t.Fatalf("checking after pay-by: %s", raw)
	}
	h.deliveryPaid(t, order, c2, s2, "evt_c2_paid")
	if got := h.deliveryOutcome(t, c2); got != "paid" {
		t.Fatalf("late but underway: %s", got)
	}
	if v, raw := h.deliveryView(t, order, d); v.Delivery.State != "paid" || *v.Delivery.PaidMinor != 14000 {
		t.Fatalf("paid after pay-by: %s", raw)
	}
	if _, err := h.fulfil(fulfilment, "d1", order, checkout.ActionDispatch, version); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	// The stray payment stays visible until it is returned.
	if flags := string(h.staffJSON(t, chef, order)["attention"]); !strings.Contains(flags, "delivery_payment_unmatched") {
		t.Fatalf("attention after the current payment %s", flags)
	}
	if audits := h.auditDetails(t, "delivery.payment_unmatched"); len(audits) != 1 || !strings.Contains(audits[0], "quote_replaced") {
		t.Fatalf("audit %v", audits)
	}
	// The chef returns the stray payment with an agreed exception on that
	// collection; once returned, the flag clears.
	h.withRefunds()
	v, _ = h.deliveryView(t, order, d)
	if _, err := h.co.RecordRefund(h.ctx, chef, "stray", order, refund(v.Version, 12000, "Paid an old delivery quote",
		alloc(c1, 12000))); err != nil {
		t.Fatalf("exception refund: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("refund:\n%s", reasons(errs))
	}
	if ops := h.operations(t, c1); len(ops) != 1 || ops[0].State != "succeeded" {
		t.Fatalf("stray refund %+v", ops)
	}
	if flags := string(h.staffJSON(t, chef, order)["attention"]); strings.Contains(flags, "delivery_payment_unmatched") {
		t.Fatalf("attention after the return %s", flags)
	}
}

// One quote paid twice (an expired session that succeeds after a second
// payment did): the second success is unmatched (already_paid); a
// succeeded payment with a recorded mismatch is not "paid" for dispatch.
func TestDeliveryDoublePaymentIsNotUsedTwice(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, _ := h.deliveryOrder(t, f, pol, "dd", "")
	v, _ := h.deliveryView(t, order, d)
	if _, err := h.quoteDelivery("q1", order, v.Version, 12000); err != nil {
		t.Fatal(err)
	}
	c1, s1 := h.deliveryStarted(t, order, d, "c1")
	h.stripe.session(s1).status = "expired"
	h.deliver(t, "evt_c1_expired", "checkout.session.expired", "checkout.session", s1, c1, order)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatal(errs)
	}
	c2, s2 := h.deliveryStarted(t, order, d, "c2")
	h.deliveryPaid(t, order, c2, s2, "evt_c2_paid")
	h.latePaid(t, c1, s1)
	if got := h.deliveryOutcome(t, c1); got != "unmatched/already_paid" {
		t.Fatalf("second success %s", got)
	}
	if v, raw := h.deliveryView(t, order, d); v.Delivery.State != "paid" || *v.Delivery.PaidMinor != 12000 {
		t.Fatalf("paid once: %s", raw)
	}
	// A later contradiction on the paying collection: back to checking.
	reason := "charge was disputed"
	h.recordDirect(t, c2, s2, "mismatch", `{"session_id":"`+s2+`","charge_disputed":true}`, nil, &reason)
	if v, raw := h.deliveryView(t, order, d); v.Delivery.State != "payment_checking" || v.Delivery.PaidMinor != nil {
		t.Fatalf("under reconciliation: %s", raw)
	}
}

// Free-delivery waiver: only with the order's free-delivery code and a
// current unpaid quote; records the measured distance, its source and the
// chef's approval; the quote is waived (nothing to pay, dispatch allowed);
// the code is never used twice.
func TestDeliveryWaiver(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	code := h.code(t, "fd", checkout.CodeInput{Code: "FREEDEL", CodeTerms: checkout.CodeTerms{Kind: checkout.KindFreeDelivery}})
	order, d, _ := h.deliveryOrder(t, f, pol, "dw", "FREEDEL")
	v, _ := h.deliveryView(t, order, d)
	in := checkout.DeliveryWaiverInput{ExpectedVersion: v.Version, DistanceM: 8200, DistanceSource: "Google Maps driving route, SE1 2AA"}
	if _, err := h.co.WaiveDelivery(h.ctx, chef, "w0", order, in); conflict(err) != "state_conflict" {
		t.Fatalf("waiver before a quote: %v", err)
	}
	q, err := h.quoteDelivery("q1", order, v.Version, 12000)
	if err != nil {
		t.Fatal(err)
	}
	in.ExpectedVersion = v.Version + 1
	for name, who := range map[string]string{"fulfilment": fulfilment, "support": support} {
		if _, err := h.co.WaiveDelivery(h.ctx, who, "x", order, in); !errors.Is(err, catalog.ErrForbidden) {
			t.Errorf("waiver by %s: %v", name, err)
		}
	}
	for name, bad := range map[string]checkout.DeliveryWaiverInput{
		"zero distance": {ExpectedVersion: in.ExpectedVersion, DistanceM: 0, DistanceSource: "map"},
		"padded source": {ExpectedVersion: in.ExpectedVersion, DistanceM: 10, DistanceSource: " map"},
	} {
		if _, err := h.co.WaiveDelivery(h.ctx, chef, "bad-"+name, order, bad); invalidField(err) == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := h.co.WaiveDelivery(h.ctx, chef, "stale", order, checkout.DeliveryWaiverInput{ExpectedVersion: v.Version,
		DistanceM: 8200, DistanceSource: "map"}); conflict(err) != "stale_version" {
		t.Fatalf("stale: %v", err)
	}
	// Not while a payment of the quote is in flight.
	coll, err := h.payDelivery(order, d, "dpay", in.ExpectedVersion, q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.co.WaiveDelivery(h.ctx, chef, "inflight", order, in); conflict(err) != "payment_in_progress" {
		t.Fatalf("waiver while paying: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatal(errs)
	}
	s := h.collection(t, coll.ID).SessionID
	h.stripe.session(s).status = "expired"
	h.deliver(t, "evt_expired", "checkout.session.expired", "checkout.session", s, coll.ID, order)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatal(errs)
	}

	w, err := h.co.WaiveDelivery(h.ctx, chef, "w1", order, in)
	if err != nil || w.Replayed {
		t.Fatalf("waiver: %+v %v", w, err)
	}
	if again, err := h.co.WaiveDelivery(h.ctx, chef, "w1", order, in); err != nil || !again.Replayed || again.ID != w.ID {
		t.Fatalf("replay: %+v %v", again, err)
	}
	after, raw := h.deliveryView(t, order, d)
	if after.Version != in.ExpectedVersion+1 || after.Delivery.State != "waived" || after.Delivery.Waived == nil ||
		!after.Delivery.Waived.At.Equal(h.now) || *after.Delivery.Payable || after.Delivery.Quote.ID != q.ID {
		t.Fatalf("waived: %s", raw)
	}
	var dist int
	var source string
	var approved bool
	if err := h.admin.QueryRow(h.ctx, `SELECT distance_m, distance_source, eligibility_approved FROM business.delivery_waiver
		WHERE id = $1::uuid`, w.ID).Scan(&dist, &source, &approved); err != nil || dist != 8200 || source != in.DistanceSource || !approved {
		t.Fatalf("waiver row %d %q %v %v", dist, source, approved, err)
	}
	if n := h.count(t, "SELECT count(*) FROM business.code_application WHERE code_id = $1::uuid", code); n != 1 {
		t.Fatalf("%d code uses, want the order's one", n)
	}
	if n := h.count(t, "SELECT count(*) FROM business.code_application WHERE code_id = $1::uuid AND state = 'consumed'", code); n != 1 {
		t.Fatal("the code use is not consumed once")
	}
	audits := h.auditDetails(t, "delivery.waived")
	if len(audits) != 1 || strings.Contains(audits[0], "SE1") || strings.Contains(audits[0], "distance") {
		t.Fatalf("audit %v", audits)
	}
	if hist := compactJSON(t, h.staffJSON(t, support, order)["delivery_history"]); strings.Contains(hist, "SE1") || !strings.Contains(hist, `"distance_m":8200`) {
		t.Fatalf("support history %s", hist)
	}
	if _, err := h.co.WaiveDelivery(h.ctx, chef, "w2", order, checkout.DeliveryWaiverInput{ExpectedVersion: after.Version,
		DistanceM: 8000, DistanceSource: "map"}); conflict(err) != "state_conflict" {
		t.Fatalf("second waiver: %v", err)
	}
	if _, err := h.payDelivery(order, d, "dpay-2", after.Version, q.ID); conflict(err) != "state_conflict" {
		t.Fatalf("pay a waived quote: %v", err)
	}
	ready := h.makeReady(t, order, d, "dw")
	if r, err := h.fulfil(fulfilment, "d1", order, checkout.ActionDispatch, ready); err != nil || r.FulfillmentState != "out_for_delivery" {
		t.Fatalf("dispatch on a waiver: %+v %v", r, err)
	}
	// Dispatched: no new quote or waiver.
	if _, err := h.quoteDelivery("q2", order, ready+1, 12000); conflict(err) != "state_conflict" {
		t.Fatalf("quote after dispatch: %v", err)
	}
}

// K05: a paid delivery collection stays a chef-only refund share. The chef's
// cancellation (100%) returns the cake automatically; the delivery share is a
// visible obligation the chef pays separately.
func TestDeliveryRefundStaysChefOnly(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, cake := h.deliveryOrder(t, f, pol, "dr", "")
	v, _ := h.deliveryView(t, order, d)
	if _, err := h.quoteDelivery("q1", order, v.Version, 12000); err != nil {
		t.Fatal(err)
	}
	delivery, s := h.deliveryStarted(t, order, d, "c1")
	h.deliveryPaid(t, order, delivery, s, "evt_delivery_paid")
	v, _ = h.deliveryView(t, order, d)
	h.withRefunds()
	if _, err := h.co.ChefCancel(h.ctx, chef, "cancel", order, checkout.ChefCancelInput{ExpectedVersion: v.Version, Reason: "Oven failure"}); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("cancel:\n%s", reasons(errs))
	}
	v, raw := h.deliveryView(t, order, d)
	if v.Lifecycle != "cancelled" || v.Refund == nil || v.Refund.TargetMinor != 22000 || v.Refund.SucceededMinor != 10000 ||
		v.Refund.DeliveryPending != 12000 || v.Refund.State != "partly_returned" {
		t.Fatalf("cancelled %s", raw)
	}
	if creates := h.stripe.refundCreates(); len(creates) != 1 || h.stripe.refundForm(creates[0]).Get("amount") != "10000" ||
		len(h.operations(t, delivery)) != 0 || len(h.operations(t, cake)) != 1 {
		t.Fatalf("refunds %v", creates)
	}
	if v.Delivery.State != "paid" || *v.Delivery.Payable {
		t.Fatalf("delivery after cancellation %s", raw)
	}
	// The chef's separate delivery action returns it.
	if _, err := h.co.PayDeliveryRefund(h.ctx, chef, "dref", order, checkout.DeliveryRefundInput{ExpectedVersion: v.Version,
		CollectionID: delivery}); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("delivery refund:\n%s", reasons(errs))
	}
	if v, raw := h.deliveryView(t, order, d); v.Refund.State != "returned" || v.Refund.SucceededMinor != 22000 {
		t.Fatalf("returned %s", raw)
	}
}

// A delivery payment that succeeds after the order was cancelled is kept
// (order_not_open), never marked paid, and, the order already having a
// refund goal (the chef's 100% cancellation), added to that goal and
// returned in full (K05 cumulative goal).
func TestDeliveryPaymentAfterCancellationIsUnmatched(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, cake := h.deliveryOrder(t, f, pol, "dc", "")
	v, _ := h.deliveryView(t, order, d)
	if _, err := h.quoteDelivery("q1", order, v.Version, 12000); err != nil {
		t.Fatal(err)
	}
	coll, s := h.deliveryStarted(t, order, d, "c1")
	v, _ = h.deliveryView(t, order, d)
	h.withRefunds()
	if _, err := h.co.ChefCancel(h.ctx, chef, "cancel", order, checkout.ChefCancelInput{ExpectedVersion: v.Version, Reason: "Oven failure"}); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("cancel:\n%s", reasons(errs))
	}
	if _, err := h.payDelivery(order, d, "after", v.Version+1, v.Delivery.Quote.ID); conflict(err) != "payment_in_progress" {
		t.Fatalf("pay on a cancelled order: %v", err)
	}
	h.deliveryPaid(t, order, coll, s, "evt_after_cancel")
	if got := h.deliveryOutcome(t, coll); got != "unmatched/order_not_open" {
		t.Fatalf("outcome %s", got)
	}
	if v, raw := h.deliveryView(t, order, d); v.Lifecycle != "cancelled" || v.Delivery.State == "paid" || *v.Delivery.Payable {
		t.Fatalf("cancelled %s", raw)
	}
	if c, s := refundAmounts(t, h, cake), refundAmounts(t, h, coll); len(c) != 1 || c[0] != 10000 || len(s) != 1 || s[0] != 12000 {
		t.Fatalf("refunds cake %v stray %v", c, s)
	}
	if v, raw := h.deliveryView(t, order, d); v.Refund.TargetMinor != 22000 || v.Refund.SucceededMinor != 22000 ||
		v.Refund.State != "returned" || v.Delivery.UnmatchedMinor != nil {
		t.Fatalf("returned %s", raw)
	}
	if flags := string(h.staffJSON(t, chef, order)["attention"]); strings.Contains(flags, "delivery_payment_unmatched") {
		t.Fatalf("attention %s", flags)
	}
	// A cancelled order takes no new quote.
	v, _ = h.deliveryView(t, order, d)
	if _, err := h.quoteDelivery("q2", order, v.Version, 12000); conflict(err) != "state_conflict" {
		t.Fatalf("quote on a cancelled order: %v", err)
	}
}

// strayOrder is a paid 10000p GK delivery order whose first delivery quote
// (12000p) was paid only after its session expired and the chef had
// replaced it with a 14000p quote: the 12000p is unmatched (quote_replaced).
// It returns the order, its digest, the cake and stray collections and the
// current quote.
func (h *payHarness) strayOrder(t *testing.T, f fixture, pol, key string) (string, []byte, string, string, string) {
	t.Helper()
	order, d, cake := h.deliveryOrder(t, f, pol, key, "")
	v, _ := h.deliveryView(t, order, d)
	if _, err := h.quoteDelivery("q1", order, v.Version, 12000); err != nil {
		t.Fatal(err)
	}
	stray, s := h.deliveryStarted(t, order, d, "c1")
	h.stripe.session(s).status = "expired"
	h.deliver(t, "evt_"+key+"_expired", "checkout.session.expired", "checkout.session", s, stray, order)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatal(errs)
	}
	q2, err := h.quoteDelivery("q2", order, v.Version+1, 14000)
	if err != nil {
		t.Fatal(err)
	}
	h.latePaid(t, stray, s)
	if got := h.deliveryOutcome(t, stray); got != "unmatched/quote_replaced" {
		t.Fatalf("stray outcome %s", got)
	}
	if v, raw := h.deliveryView(t, order, d); v.Delivery.UnmatchedMinor == nil || *v.Delivery.UnmatchedMinor != 12000 ||
		v.Delivery.State != "quoted" {
		t.Fatalf("stray view %s", raw)
	}
	return order, d, cake, stray, q2.ID
}

// previewStray is the cancellation preview's unmatched_minor: the stray
// delivery payment it returns in full beside amount_minor.
func (h *payHarness) previewStray(t *testing.T, order string, d []byte) int64 {
	t.Helper()
	raw, err := h.co.PreviewCancellation(h.ctx, order, "", d)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		UnmatchedMinor *int64 `json:"unmatched_minor"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.UnmatchedMinor == nil {
		t.Fatalf("preview %s: %v", raw, err)
	}
	return *p.UnmatchedMinor
}

// cancellationStray is the cancellation block's unmatched_minor (nil when
// the outcome states none) and the order JSON.
func (h *payHarness) cancellationStray(t *testing.T, order string, d []byte) (*int64, string) {
	t.Helper()
	raw, err := h.co.GetOrder(h.ctx, order, "", d)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Cancellation map[string]json.RawMessage
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	m, ok := v.Cancellation["unmatched_minor"]
	if !ok {
		return nil, string(raw)
	}
	var n int64
	if err := json.Unmarshal(m, &n); err != nil {
		t.Fatal(err)
	}
	return &n, string(raw)
}

// cancelNotice renders the cancelled notice from the order's secured
// cancellation outcome, as the notice machinery does when it is secured.
func (h *payHarness) cancelNotice(t *testing.T, order string) string {
	t.Helper()
	tx, err := h.db.MigratorConn(t).Begin(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(h.ctx)
	if _, err := tx.Exec(h.ctx, "SET LOCAL ROLE gk_owner"); err != nil {
		t.Fatal(err)
	}
	var text string
	if err := tx.QueryRow(h.ctx, `SELECT r.body_text FROM business.orders o
		JOIN business.order_command c ON c.order_id = o.id AND c.command = 'cancel'
		JOIN business.recovery_operation ro ON ro.id = c.id AND ro.state = 'secured',
		business.order_notice_render('cancelled', o, ro.outcome -> 'evidence', 'https://gk.example') r
		WHERE o.id = $1::uuid`, order).Scan(&text); err != nil {
		t.Fatalf("render: %v", err)
	}
	return text
}

// returnStray is the chef's documented return of a stray payment: an agreed
// exception of its full amount on that collection, dispatched.
func (h *payHarness) returnStray(t *testing.T, order string, d []byte, stray string) {
	t.Helper()
	v, _ := h.deliveryView(t, order, d)
	if _, err := h.co.RecordRefund(h.ctx, chef, "stray-"+stray[:8], order, refund(v.Version, 12000, "Paid an old delivery quote",
		alloc(stray, 12000))); err != nil {
		t.Fatalf("return the stray: %v", err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("stray refund:\n%s", reasons(errs))
	}
	if got := refundAmounts(t, h, stray); len(got) != 1 || got[0] != 12000 {
		t.Fatalf("stray refund %v", got)
	}
	if v, raw := h.deliveryView(t, order, d); v.Delivery.UnmatchedMinor != nil {
		t.Fatalf("returned stray still shown: %s", raw)
	}
}

// refundAmounts is the collection's refund operations' amounts, oldest
// first; each must have succeeded.
func refundAmounts(t *testing.T, h *payHarness, coll string) []int64 {
	t.Helper()
	rows, err := h.admin.Query(h.ctx, `SELECT state, amount_minor FROM business.refund_operation WHERE collection_id = $1::uuid
		ORDER BY created_at, id`, coll)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var state string
		var amount int64
		if err := rows.Scan(&state, &amount); err != nil {
			t.Fatal(err)
		}
		if state != "succeeded" {
			t.Fatalf("refund of %s is %s", coll, state)
		}
		out = append(out, amount)
	}
	return out
}

// K05 policy basis: an unmatched delivery payment is not what the order
// cost. A percentage (50% after the deadline, no-show) is of the cake (and a
// matched delivery) only; the stray is returned in full, once, whether the
// chef returned it first or not; its return never reduces the cake's share.
func TestStrayDeliveryPaymentStaysOutOfThePolicyBasis(t *testing.T) {
	t.Run("returned, then a late cancellation", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		h.withRefunds()
		order, d, cake, stray, _ := h.strayOrder(t, f, pol, "sr")
		h.returnStray(t, order, d, stray)
		h.now = cancelDeadline
		v, _ := h.cancelView(t, order, d)
		p := h.preview(t, order, d)
		if p.Rate != 50 || p.PaidMinor != 10000 || p.AmountMinor != 5000 || len(p.Allocations) != 1 ||
			p.Allocations[0].CollectionID != cake || p.Allocations[0].AmountMinor != 5000 || p.DeliveryPending != 0 {
			t.Fatalf("preview %+v", p)
		}
		if got := h.previewStray(t, order, d); got != 0 {
			t.Fatalf("preview stray %d after its return", got)
		}
		if _, err := h.cancel(order, d, "c", v.Version, p); err != nil {
			t.Fatal(err)
		}
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("cancel:\n%s", reasons(errs))
		}
		if n, raw := h.cancellationStray(t, order, d); n != nil {
			t.Fatalf("returned stray shown again: %s", raw)
		}
		if text := h.cancelNotice(t, order); strings.Contains(text, "delivery payment") || !strings.Contains(text, "£50") {
			t.Fatalf("notice %s", text)
		}
		if got := refundAmounts(t, h, cake); len(got) != 1 || got[0] != 5000 {
			t.Fatalf("cake refunds %v", got)
		}
		if got := refundAmounts(t, h, stray); len(got) != 1 {
			t.Fatalf("stray refunded again: %v", got)
		}
		cv, raw := h.cancelView(t, order, d)
		if cv.Lifecycle != "cancelled" || *cv.Cancellation.Rate != 50 || *cv.Cancellation.AmountMinor != 5000 ||
			cv.Refund.TargetMinor != 17000 || cv.Refund.SucceededMinor != 17000 || cv.Refund.State != "returned" {
			t.Fatalf("cancelled %s", raw)
		}
		if flags := string(h.staffJSON(t, chef, order)["attention"]); strings.Contains(flags, "delivery") || strings.Contains(flags, "refund") {
			t.Fatalf("attention %s", flags)
		}
	})
	t.Run("not returned, then a late cancellation", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		h.withRefunds()
		h.staffGrants(t)
		order, d, cake, stray, _ := h.strayOrder(t, f, pol, "sn")
		// The chef sees the stray amount; technical support only that a
		// delivery is quoted (no money); fulfilment the state.
		if del, raw := staffDelivery(t, h.staffJSON(t, chef, order)); string(del["unmatched_minor"]) != "12000" {
			t.Fatalf("chef delivery %s", raw)
		}
		if _, raw := staffDelivery(t, h.staffJSON(t, support, order)); strings.Contains(raw, "unmatched") || strings.Contains(raw, "amount") {
			t.Fatalf("support delivery %s", raw)
		}
		h.now = cancelDeadline
		v, _ := h.cancelView(t, order, d)
		p := h.preview(t, order, d)
		// The amount is the policy amount (50% of the cake); the allocations
		// also return the stray in full (its implicit floor).
		allocs := map[string]int64{}
		for _, a := range p.Allocations {
			allocs[a.CollectionID] = a.AmountMinor
		}
		if p.Rate != 50 || p.PaidMinor != 10000 || p.AmountMinor != 5000 || len(allocs) != 2 || allocs[cake] != 5000 ||
			allocs[stray] != 12000 || p.DeliveryPending != 0 {
			t.Fatalf("preview %+v", p)
		}
		// What the customer is told matches what is returned: the policy
		// amount and, beside it, the stray returned in full.
		if got := h.previewStray(t, order, d); got != 12000 {
			t.Fatalf("preview stray %d", got)
		}
		if _, err := h.cancel(order, d, "c", v.Version, p); err != nil {
			t.Fatal(err)
		}
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("cancel:\n%s", reasons(errs))
		}
		if n, raw := h.cancellationStray(t, order, d); n == nil || *n != 12000 || !strings.Contains(raw, `"amount_minor":5000,"unmatched_minor":12000`) {
			t.Fatalf("cancellation %s", raw)
		}
		// (Rendered from the outcome evidence alone, as when the cancellation
		// is secured before the refunds return.)
		if text := h.cancelNotice(t, order); !strings.Contains(text, "Refund requested: £50,") ||
			!strings.Contains(text, "delivery payment of £120") || !strings.Contains(text, "£170 is being returned to you in total") {
			t.Fatalf("notice %s", text)
		}
		if sup := string(h.staffJSON(t, support, order)["cancellation"]); strings.Contains(sup, "unmatched") || strings.Contains(sup, "amount") {
			t.Fatalf("support cancellation %s", sup)
		}
		if c, s := refundAmounts(t, h, cake), refundAmounts(t, h, stray); len(c) != 1 || c[0] != 5000 || len(s) != 1 || s[0] != 12000 {
			t.Fatalf("refunds cake %v stray %v", c, s)
		}
		cv, raw := h.cancelView(t, order, d)
		if cv.Refund.TargetMinor != 17000 || cv.Refund.SucceededMinor != 17000 || cv.Refund.State != "returned" || cv.Refund.DeliveryPending != 0 {
			t.Fatalf("cancelled %s", raw)
		}
		if v, raw := h.deliveryView(t, order, d); v.Delivery.UnmatchedMinor != nil {
			t.Fatalf("stray still shown: %s", raw)
		}
		if flags := string(h.staffJSON(t, chef, order)["attention"]); strings.Contains(flags, "delivery_payment_unmatched") {
			t.Fatalf("attention %s", flags)
		}
		// No double return: the chef can no longer return the stray.
		cv, _ = h.cancelView(t, order, d)
		if _, err := h.co.RecordRefund(h.ctx, chef, "again", order, refund(cv.Version, 12000, "Again", alloc(stray, 12000))); conflict(err) != "refund_limit" {
			t.Fatalf("second return of the stray: %v", err)
		}
	})
	t.Run("before the deadline", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		h.withRefunds()
		order, d, cake, stray, _ := h.strayOrder(t, f, pol, "sb")
		v, _ := h.cancelView(t, order, d)
		p := h.preview(t, order, d)
		if p.Rate != 100 || p.PaidMinor != 10000 || p.AmountMinor != 10000 {
			t.Fatalf("preview %+v", p)
		}
		if _, err := h.cancel(order, d, "c", v.Version, p); err != nil {
			t.Fatal(err)
		}
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("cancel:\n%s", reasons(errs))
		}
		if c, s := refundAmounts(t, h, cake), refundAmounts(t, h, stray); len(c) != 1 || c[0] != 10000 || len(s) != 1 || s[0] != 12000 {
			t.Fatalf("refunds cake %v stray %v", c, s)
		}
		if cv, raw := h.cancelView(t, order, d); cv.Refund.TargetMinor != 22000 || cv.Refund.State != "returned" {
			t.Fatalf("cancelled %s", raw)
		}
	})
	t.Run("returned, then a no-show", func(t *testing.T) {
		h := newPayHarness(t)
		f := h.setup(t)
		pol := h.policy(t, "p1")
		h.staffGrants(t)
		h.withRefunds()
		order, d, cake, stray, _ := h.strayOrder(t, f, pol, "so")
		h.returnStray(t, order, d, stray)
		// The current 14000p quote is paid, the order sent out and nobody is
		// there: 50% of what the order cost (cake 10000p + delivery 14000p).
		paid, s := h.deliveryStarted(t, order, d, "c2")
		h.deliveryPaid(t, order, paid, s, "evt_so_paid")
		ready := h.makeReady(t, order, d, "so")
		if _, err := h.fulfil(fulfilment, "d1", order, checkout.ActionDispatch, ready); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		h.now = at("12:30")
		if _, err := h.fulfil(chef, "ns", order, checkout.ActionNoShow, ready+1); err != nil {
			t.Fatalf("no-show: %v", err)
		}
		if errs := h.drain(t); len(errs) != 0 {
			t.Fatalf("no-show:\n%s", reasons(errs))
		}
		if c := refundAmounts(t, h, cake); len(c) != 1 || c[0] != 5000 {
			t.Fatalf("cake refunds %v", c)
		}
		if got := h.operations(t, paid); len(got) != 0 {
			t.Fatalf("delivery refunded automatically: %+v", got)
		}
		v, raw := h.deliveryView(t, order, d)
		if v.Lifecycle != "completed" || v.Refund.TargetMinor != 24000 || v.Refund.SucceededMinor != 17000 ||
			v.Refund.DeliveryPending != 7000 {
			t.Fatalf("no-show %s", raw)
		}
	})
}

// A delivery collection whose Stripe call is never admitted (no key on the
// worker) is in flight only for its first 15 minutes: then it is flagged
// delivery_payment_stuck, holds no money (the delivery is payable again and
// account deletion is not blocked) and the next delivery command closes it
// as never dispatched, so no Stripe call is ever made for it. The quote may
// be the Stripe maximum (99999999p), and the line item names the delivery.
func TestStuckDeliveryCollectionIsClosed(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, _ := h.deliveryOrder(t, f, pol, "dk", "")
	v, _ := h.deliveryView(t, order, d)
	q1, err := h.quoteDelivery("q1", order, v.Version, 12000)
	if err != nil {
		t.Fatal(err)
	}
	version := v.Version + 1
	keyless := *h.pay
	keyless.Provider = nil
	h.handler = jobs.Chain(h.cmds.Handle, keyless.Handle, h.reviews.Handle)
	stuck, err := h.payDelivery(order, d, "p1", version, q1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 1 {
		t.Fatalf("without a key: %v", errs)
	}
	block := func() string {
		var b *string
		if err := h.admin.QueryRow(h.ctx, "SELECT business.account_order_block(o) FROM business.orders o WHERE o.id = $1::uuid",
			order).Scan(&b); err != nil {
			t.Fatal(err)
		}
		if b == nil {
			return ""
		}
		return *b
	}
	flags := func() string { return string(h.staffJSON(t, chef, order)["attention"]) }
	if v, raw := h.deliveryView(t, order, d); v.Delivery.State != "payment_checking" || *v.Delivery.Payable {
		t.Fatalf("young collection: %s", raw)
	}
	if _, err := h.payDelivery(order, d, "p2", version, q1.ID); conflict(err) != "payment_in_progress" {
		t.Fatalf("pay again while young: %v", err)
	}
	if b, fl := block(), flags(); b != "payment_being_checked" || strings.Contains(fl, "delivery_payment_stuck") {
		t.Fatalf("young: block %q flags %s", b, fl)
	}

	h.age(t, stuck.ID)
	if v, raw := h.deliveryView(t, order, d); v.Delivery.State != "quoted" || !*v.Delivery.Payable {
		t.Fatalf("stuck collection: %s", raw)
	}
	if b, fl := block(), flags(); b != "" || !strings.Contains(fl, "delivery_payment_stuck") {
		t.Fatalf("stuck: block %q flags %s", b, fl)
	}
	// The chef's new quote closes it; the order can be quoted and paid
	// again.
	q2, err := h.quoteDelivery("q2", order, version, checkout.MaxDeliveryMinor)
	if err != nil {
		t.Fatalf("re-quote over a stuck collection: %v", err)
	}
	if n := h.count(t, `SELECT count(*) FROM business.payment_collection WHERE id = $1::uuid AND state = 'cancelled'
		AND cancel_reason = 'never_dispatched' AND dispatch_started_at IS NULL`, stuck.ID); n != 1 {
		t.Fatal("the stuck collection was not closed as never dispatched")
	}
	if fl := flags(); strings.Contains(fl, "delivery_payment_stuck") {
		t.Fatalf("flag after the close %s", fl)
	}
	if got := h.auditDetails(t, "payment.cancelled"); len(got) != 1 || !strings.Contains(got[0], stuck.ID) ||
		!strings.Contains(got[0], "never_dispatched") {
		t.Fatalf("audit %v", got)
	}
	h.handler = jobs.Chain(h.cmds.Handle, h.pay.Handle, h.reviews.Handle)
	paid, err := h.payDelivery(order, d, "p3", version+1, q2.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("dispatch: %v", errs)
	}
	h.stripe.mu.Lock()
	creates, form := append([]string(nil), h.stripe.creates...), h.stripe.forms[paid.ID]
	h.stripe.mu.Unlock()
	// One create for the new collection; none ever for the closed one (the
	// cake's own create is the first).
	if len(creates) != 2 || creates[1] != paid.ID {
		t.Fatalf("creates %v", creates)
	}
	if c := h.collection(t, stuck.ID); c.State != "cancelled" {
		t.Fatalf("closed collection %+v", c)
	}
	if !strings.Contains(form, "unit_amount%5D=99999999") || !strings.Contains(form, "GK+Cakes+delivery") ||
		strings.Contains(form, "GK+Cakes+order") {
		t.Fatalf("delivery request %s", form)
	}
	h.stripe.mu.Lock()
	cakeForm := h.stripe.forms[creates[0]]
	h.stripe.mu.Unlock()
	if !strings.Contains(cakeForm, "GK+Cakes+order") {
		t.Fatalf("cake request %s", cakeForm)
	}
}

// K05 cumulative goal: a stray that arrives after a goal was recorded is
// added on top of it, never taken from the cake's share. The reviewer's
// probe: a late cancellation (goal 5000p) with the cake refund pending at
// Stripe; the in-flight delivery payment succeeds (unmatched,
// order_not_open); then the cake refund fails. The end state returns cake
// 5000p + stray 12000p.
func TestStrayAfterAGoalIsAddedOnTop(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, cake := h.deliveryOrder(t, f, pol, "sg", "")
	v, _ := h.deliveryView(t, order, d)
	if _, err := h.quoteDelivery("q1", order, v.Version, 12000); err != nil {
		t.Fatal(err)
	}
	stray, s := h.deliveryStarted(t, order, d, "c1")
	h.withRefunds()
	h.stripe.mu.Lock()
	h.stripe.refunds.newStatus = "pending"
	h.stripe.mu.Unlock()
	h.now = cancelDeadline
	v, _ = h.deliveryView(t, order, d)
	p := h.preview(t, order, d)
	if p.Rate != 50 || p.AmountMinor != 5000 {
		t.Fatalf("preview %+v", p)
	}
	if _, err := h.cancel(order, d, "c", v.Version, p); err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("cancel:\n%s", reasons(errs))
	}
	if ops := h.operations(t, cake); len(ops) != 1 || ops[0].State != "pending" {
		t.Fatalf("cake refund %+v", ops)
	}
	// The delivery money arrives: added on top of the goal and reserved.
	h.stripe.mu.Lock()
	h.stripe.refunds.newStatus = ""
	h.stripe.mu.Unlock()
	h.deliveryPaid(t, order, stray, s, "evt_sg_paid")
	if got := h.deliveryOutcome(t, stray); got != "unmatched/order_not_open" {
		t.Fatalf("outcome %s", got)
	}
	if got := refundAmounts(t, h, stray); len(got) != 1 || got[0] != 12000 {
		t.Fatalf("stray refunds %v", got)
	}
	if cv, raw := h.cancelView(t, order, d); cv.Refund.TargetMinor != 17000 {
		t.Fatalf("raised goal %s", raw)
	}
	// The cake refund fails: under the raised goal its remainder is
	// reserved once (K05), on the cake.
	h.stripe.setRefund("re_fake1", "failed", "expired_or_canceled_card")
	h.refundEvent(t, "evt_re_failed", "refund.failed", "re_fake1")
	errs := h.drain(t)
	for _, err := range errs {
		if got := attention([]error{err}); len(got) != 1 || !strings.Contains(got[0], "refund failed") {
			t.Fatalf("after the failure:\n%s", reasons(errs))
		}
	}
	rows, err := h.admin.Query(h.ctx, `SELECT state, amount_minor FROM business.refund_operation WHERE collection_id = $1::uuid
		ORDER BY created_at, id`, cake)
	if err != nil {
		t.Fatal(err)
	}
	var cakeOps []string
	for rows.Next() {
		var state string
		var amount int64
		if err := rows.Scan(&state, &amount); err != nil {
			t.Fatal(err)
		}
		cakeOps = append(cakeOps, fmt.Sprintf("%s %d", state, amount))
	}
	rows.Close()
	if strings.Join(cakeOps, ", ") != "failed 5000, succeeded 5000" {
		t.Fatalf("cake refunds %v", cakeOps)
	}
	if got := refundAmounts(t, h, stray); len(got) != 1 || got[0] != 12000 {
		t.Fatalf("stray refunded again: %v", got)
	}
	cv, raw := h.cancelView(t, order, d)
	if cv.Refund.TargetMinor != 17000 || cv.Refund.SucceededMinor != 17000 || cv.Refund.State != "returned" {
		t.Fatalf("end state %s", raw)
	}
	if dv, raw := h.deliveryView(t, order, d); dv.Delivery.UnmatchedMinor != nil {
		t.Fatalf("stray still shown: %s", raw)
	}
}
