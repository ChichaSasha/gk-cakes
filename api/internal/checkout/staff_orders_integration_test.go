//go:build integration

package checkout_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
)

// Staff order reads (migration 0029; K00 capabilities, K09 access, K11
// visible lifecycle, K05 refund state).

type staffRow = map[string]any

func (h *payHarness) staffList(t *testing.T, subject string, f checkout.StaffOrderFilter) ([]staffRow, string) {
	t.Helper()
	page, err := h.co.StaffOrders(h.ctx, subject, f)
	if err != nil {
		t.Fatalf("staff list by %s: %v", subject, err)
	}
	var rows []staffRow
	if err := json.Unmarshal(page.Items, &rows); err != nil {
		t.Fatal(err)
	}
	return rows, page.NextCursor
}

func (h *payHarness) staffGet(t *testing.T, subject, order string) staffRow {
	t.Helper()
	raw, err := h.co.StaffOrder(h.ctx, subject, order)
	if err != nil {
		t.Fatalf("staff order by %s: %v", subject, err)
	}
	var v staffRow
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func ids(rows []staffRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r["id"].(string))
	}
	return out
}

func rowOf(t *testing.T, rows []staffRow, id string) staffRow {
	t.Helper()
	for _, r := range rows {
		if r["id"] == id {
			return r
		}
	}
	t.Fatalf("order %s not listed in %v", id, ids(rows))
	return nil
}

func keys(m staffRow) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func flags(v staffRow) []string {
	var out []string
	for _, f := range v["attention"].([]any) {
		out = append(out, f.(string))
	}
	return out
}

// noneOf fails when any object in v has one of the keys (or any money
// key, *_minor, when money is set), or any string carries the customer's
// email.
func noneOf(t *testing.T, label string, v any, money bool, forbidden ...string) {
	t.Helper()
	switch x := v.(type) {
	case map[string]any:
		for k, sub := range x {
			if slices.Contains(forbidden, k) || (money && strings.HasSuffix(k, "_minor")) {
				t.Errorf("%s: %q is visible", label, k)
			}
			noneOf(t, label+"."+k, sub, money, forbidden...)
		}
	case []any:
		for _, sub := range x {
			noneOf(t, label, sub, money, forbidden...)
		}
	case string:
		if strings.Contains(x, "example.com") {
			t.Errorf("%s: contact email visible", label)
		}
	}
}

var notForSupport = []string{"contact_email", "different_payer_name", "currency", "rate", "policy_rate", "provider_session_id",
	"payment_intent_id", "provider_refund_id", "resolution_note", "checkout_url", "payment_state"}

// placeOn places a guest order for one 1000p made-to-order cake on date's
// slot (index into the day's starts) at the harness clock.
func (h *payHarness) placeOn(t *testing.T, f fixture, pol, key, date string, slot int) (string, []byte) {
	t.Helper()
	c, err := h.sch.Choices(h.ctx, day(date), schedule.Collection, 1)
	if err != nil || len(c.Slots) <= slot {
		t.Fatalf("slots of %s: %+v %v", date, c, err)
	}
	quote, d := h.guestQuote(t, "q-"+key, checkout.QuoteInput{Date: date, SlotID: c.Slots[slot].ID,
		FulfillmentMode: schedule.Collection, Lines: []checkout.QuoteLine{{VariantID: f.v1000, Quantity: 1}}})
	return must[catalog.Result](t)(h.checkout("", d, "k-"+key, quote, pol)).ID, d
}

func (h *payHarness) staffAudits(t *testing.T, action string) []string {
	t.Helper()
	rows, err := h.admin.Query(h.ctx, `SELECT subject_kind || ' ' || detail::text FROM business.audit_event
		WHERE action = $1 ORDER BY occurred_at, id`, action)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

var november = checkout.StaffOrderFilter{From: day("2026-11-01"), To: day("2026-11-30")}

// Each view sees what K00/K09 allow: the chef everything (never the
// customer's payment link), technical support no money, contact or provider
// references, fulfillment only orders that were ever confirmed, without
// money, contact or attention. The chef detail holds every order_get field
// with the same values. Every read is audited without personal data.
func TestStaffOrderViewsAreRedactedByRole(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	paid, paidColl, _ := h.paidCollection(t, f, pol, "paid")
	open, openD := h.tenThousandOrder(t, f, pol, "open")
	openColl, _ := h.started(t, open, openD) // pending, with a checkout URL

	// The list.
	chefRows, next := h.staffList(t, chef, november)
	if next != "" || len(chefRows) != 2 {
		t.Fatalf("chef list %v %q", ids(chefRows), next)
	}
	p := rowOf(t, chefRows, paid)
	if p["contact_email"] != "test.customer@example.com" || p["customer_full_name"] != "Test Customer" || p["total_minor"] != 10000.0 ||
		p["currency"] != "GBP" || p["payment_state"] != "succeeded" || p["paid_at"] == nil || p["lifecycle"] != "confirmed" ||
		p["raw_lifecycle"] != "confirmed" || p["fulfillment_mode"] != "collection" || p["date"] != "2026-11-14" ||
		p["needs_attention"] != false || len(flags(p)) != 0 || p["refund"] != nil || p["version"] == nil {
		t.Fatalf("chef row %v", p)
	}
	if prod := p["production"].(map[string]any); prod["state"] != "scheduled" || prod["scheduled_at"] != "2026-11-07T10:09:00+00:00" {
		t.Fatalf("production %v", prod)
	}
	items := p["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["cake_name"] != "Pink piping" || items[0].(map[string]any)["quantity"] != 1.0 {
		t.Fatalf("items %v", items)
	}
	noneOf(t, "chef row items", items, true)
	if o := rowOf(t, chefRows, open); o["payment_state"] != "pending" || o["lifecycle"] != "awaiting_payment" ||
		o["production"].(map[string]any)["state"] != "not_scheduled" {
		t.Fatalf("open row %v", o)
	}
	supportRows, _ := h.staffList(t, support, november)
	if len(supportRows) != 2 {
		t.Fatalf("support list %v", ids(supportRows))
	}
	for _, r := range supportRows {
		noneOf(t, "support row", r, true, append(notForSupport, "refund", "paid_at", "free_confirmed_at", "payment_requirement")...)
		if r["customer_full_name"] != "Test Customer" || r["attention"] == nil || r["needs_attention"] != false {
			t.Errorf("support row %v", r)
		}
	}
	fulfilRows, _ := h.staffList(t, fulfilment, november)
	wantFulfil := []string{"cancel_reason", "customer_full_name", "date", "fulfillment_mode", "fulfillment_state", "id", "items",
		"lifecycle", "placed_at", "production", "raw_lifecycle", "slot", "version"}
	if len(fulfilRows) != 1 || fulfilRows[0]["id"] != paid || !slices.Equal(keys(fulfilRows[0]), wantFulfil) {
		t.Fatalf("fulfillment list %v", fulfilRows)
	}
	noneOf(t, "fulfillment row", fulfilRows[0], true, notForSupport...)
	if _, err := h.co.StaffOrders(h.ctx, fulfilment, checkout.StaffOrderFilter{From: november.From, To: november.To,
		NeedsAttention: ptr(true)}); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("fulfillment needs_attention filter: %v", err)
	}
	for _, who := range []string{customerA, "00000000-0000-4000-8000-00000000dead"} {
		if _, err := h.co.StaffOrders(h.ctx, who, november); !errors.Is(err, catalog.ErrForbidden) {
			t.Fatalf("non-staff list: %v", err)
		}
		if _, err := h.co.StaffOrder(h.ctx, who, paid); !errors.Is(err, catalog.ErrForbidden) {
			t.Fatalf("non-staff detail: %v", err)
		}
	}

	// The detail. The chef's holds every customer field with its value,
	// except the payment link.
	for _, o := range []struct {
		id   string
		d    []byte
		coll string
	}{{paid, h.digestOf(t, paid), paidColl}, {open, openD, openColl}} {
		raw, err := h.co.GetOrder(h.ctx, o.id, "", o.d)
		if err != nil {
			t.Fatal(err)
		}
		var customer staffRow
		if err := json.Unmarshal(raw, &customer); err != nil {
			t.Fatal(err)
		}
		v := h.staffGet(t, chef, o.id)
		for k, want := range customer {
			got, _ := json.Marshal(v[k])
			w, _ := json.Marshal(want)
			if k == "payment" {
				pay := want.(map[string]any)
				if o.id == open && pay["checkout_url"] == nil {
					t.Fatalf("the open order has no payment link to hide: %s", raw)
				}
				delete(pay, "checkout_url")
				w, _ = json.Marshal(pay)
			}
			if string(got) != string(w) {
				t.Errorf("%s %s: staff %s, customer %s", o.id, k, got, w)
			}
		}
		colls := v["collections"].([]any)
		c := colls[len(colls)-1].(map[string]any)
		if c["id"] != o.coll || !strings.HasPrefix(c["provider_session_id"].(string), "cs_") || c["amount_minor"] != 10000.0 {
			t.Fatalf("chef collections %v", colls)
		}
		for _, k := range []string{"raw_lifecycle", "recovery_state", "attention", "needs_attention", "production", "refund_goal",
			"refund_operations", "refund_observations", "commands", "audit"} {
			if _, ok := v[k]; !ok {
				t.Errorf("chef detail lacks %s", k)
			}
		}
	}
	v := h.staffGet(t, chef, paid)
	if v["contact_email"] != "test.customer@example.com" || v["refund_goal"] != nil || v["recovery_state"] != "secured" {
		t.Fatalf("chef detail %v", v)
	}
	cmds := v["commands"].([]any)
	if len(cmds) != 1 || cmds[0].(map[string]any)["kind"] != "paid_confirm" || cmds[0].(map[string]any)["state"] != "secured" ||
		cmds[0].(map[string]any)["outcome_status"] != "confirmed" || cmds[0].(map[string]any)["requested_by"] != "system" {
		t.Fatalf("commands %v", cmds)
	}
	noneOf(t, "commands", cmds, false, "note", "evidence_ref", "payload")
	audit := v["audit"].([]any)
	if len(audit) == 0 {
		t.Fatal("no audit summary")
	}
	for _, a := range audit {
		if k := keys(a.(map[string]any)); !slices.Equal(k, []string{"action", "actor_kind", "occurred_at"}) {
			t.Fatalf("audit entry %v", a)
		}
	}
	sv := h.staffGet(t, support, open)
	noneOf(t, "support detail", sv, true, notForSupport...)
	if sv["customer_full_name"] != "Test Customer" || sv["payment"].(map[string]any)["state"] != "pending" ||
		len(sv["collections"].([]any)) != 1 || sv["commands"] == nil || sv["audit"] == nil {
		t.Fatalf("support detail %v", sv)
	}
	fv := h.staffGet(t, fulfilment, paid)
	wantFulfil = []string{"appointment_change", "appointment_history", "cancel_reason", "cancelled_at", "customer_full_name", "date",
		"fulfillment", "fulfillment_history", "fulfillment_mode", "id", "items", "lifecycle", "placed_at", "production", "raw_lifecycle",
		"slot", "version"}
	if !slices.Equal(keys(fv), wantFulfil) || fv["version"] != v["version"] {
		t.Fatalf("fulfillment detail %v", fv)
	}
	noneOf(t, "fulfillment detail", fv, true, notForSupport...)
	if _, err := h.co.StaffOrder(h.ctx, fulfilment, open); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("fulfillment, unconfirmed order: %v", err)
	}
	for _, missing := range []string{"00000000-0000-4000-8000-000000000404", "not-a-uuid"} {
		if _, err := h.co.StaffOrder(h.ctx, chef, missing); !errors.Is(err, catalog.ErrNotFound) {
			t.Fatalf("unknown order: %v", err)
		}
	}

	// Audits: one per read, never personal data; detail reads stay out of
	// the order's audit summary.
	listed := h.staffAudits(t, "order.staff_listed")
	if len(listed) != 3 || !strings.Contains(listed[0], `"view" : "chef"`) || !strings.Contains(listed[1], `"view" : "technical_support"`) ||
		!strings.Contains(listed[2], `"view" : "fulfillment"`) || !strings.Contains(listed[0], `"items" : 2`) ||
		!strings.HasPrefix(listed[0], "account ") {
		t.Fatalf("list audits %q", listed)
	}
	viewed := h.staffAudits(t, "order.staff_viewed")
	if len(viewed) != 5 || !strings.HasPrefix(viewed[0], `order {"view" : "chef"}`) { // chef x3, support, fulfillment; 404s are not reads
		t.Fatalf("detail audits %q", viewed)
	}
	for _, a := range append(listed, viewed...) {
		if strings.Contains(a, "Test Customer") || strings.Contains(a, "example.com") {
			t.Fatalf("personal data in an audit: %s", a)
		}
	}
	for _, a := range h.staffGet(t, chef, paid)["audit"].([]any) {
		if a.(map[string]any)["action"] == "order.staff_viewed" {
			t.Fatal("read events in the audit summary")
		}
	}
	// Only the two gateways are reachable.
	if _, err := h.api.Exec(h.ctx, "SELECT business.order_view($1::uuid)", paid); dbtest.SQLState(err) != "42501" {
		t.Fatalf("order_view from gk_api: %v", err)
	}
	h.noPersonalDataInLogs(t)
}

// Handover-day range, visible lifecycle and attention filters; keyset
// pages by slot start, placement and ID stay stable when orders are added
// before the cursor or change state between pages.
func TestStaffOrderListFiltersAndPages(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	base := h.now
	at := func(m int) { h.now = base.Add(time.Duration(m) * time.Minute) }
	// Placed between the policy (an hour before base) and the fake payment
	// time (base + 9 minutes).
	at(-50)
	a, _ := h.placeOn(t, f, pol, "a", "2026-11-14", 1) // 13:00
	at(-40)
	b, _ := h.placeOn(t, f, pol, "b", "2026-11-14", 0) // 12:30
	at(-30)
	c, cd := h.placeOn(t, f, pol, "c", "2026-11-14", 0) // 12:30, later
	at(-20)
	d, _ := h.placeOn(t, f, pol, "d", "2026-11-21", 0)
	at(-10)
	e, _ := h.placeOn(t, f, pol, "e", "2026-11-28", 0)

	window := checkout.StaffOrderFilter{From: day("2026-11-14"), To: day("2026-11-21"), Limit: 2}
	rows, next := h.staffList(t, chef, window)
	if !slices.Equal(ids(rows), []string{b, c}) || next == "" {
		t.Fatalf("page 1 %v %q", ids(rows), next)
	}
	// Between pages: an order placed earlier sorts before the cursor, and c
	// is paid (its lifecycle changes); page 2 is unaffected.
	at(-45)
	early, _ := h.placeOn(t, f, pol, "early", "2026-11-14", 0)
	h.payNow(t, c, cd, "c")
	window.Cursor = next
	rows, next = h.staffList(t, chef, window)
	if !slices.Equal(ids(rows), []string{a, d}) || next != "" {
		t.Fatalf("page 2 %v %q", ids(rows), next)
	}
	all, _ := h.staffList(t, support, checkout.StaffOrderFilter{From: day("2026-11-01"), To: day("2026-11-30"), Limit: 100})
	if !slices.Equal(ids(all), []string{early, b, c, a, d, e}) {
		t.Fatalf("all %v", ids(all))
	}
	if one, _ := h.staffList(t, chef, checkout.StaffOrderFilter{From: day("2026-11-28"), To: day("2026-11-28")}); !slices.Equal(ids(one), []string{e}) {
		t.Fatalf("one day %v", ids(one))
	}

	// Visible lifecycles.
	conf, _ := h.staffList(t, chef, checkout.StaffOrderFilter{From: november.From, To: november.To, Lifecycles: []string{"confirmed"}})
	if !slices.Equal(ids(conf), []string{c}) {
		t.Fatalf("confirmed %v", ids(conf))
	}
	unpaid, _ := h.staffList(t, chef, checkout.StaffOrderFilter{From: november.From, To: november.To,
		Lifecycles: []string{"awaiting_payment", "cancelled"}})
	if !slices.Equal(ids(unpaid), []string{early, b, a, d, e}) {
		t.Fatalf("unpaid %v", ids(unpaid))
	}
	h.owner(t, "UPDATE business.orders SET recovery_state = 'confirmation_pending' WHERE id = $1::uuid", b)
	pending, _ := h.staffList(t, chef, checkout.StaffOrderFilter{From: november.From, To: november.To,
		Lifecycles: []string{"confirmation_pending"}})
	if !slices.Equal(ids(pending), []string{b}) || pending[0]["raw_lifecycle"] != "awaiting_payment" {
		t.Fatalf("confirmation pending %v", pending)
	}

	// Attention.
	coll := h.collectionOf(t, c)
	h.owner(t, "UPDATE business.payment_collection SET mismatch_reason = 'test contradiction' WHERE id = $1::uuid", coll)
	att, _ := h.staffList(t, support, checkout.StaffOrderFilter{From: november.From, To: november.To, NeedsAttention: ptr(true)})
	if !slices.Equal(ids(att), []string{c}) || !slices.Equal(flags(att[0]), []string{"payment_mismatch"}) || att[0]["needs_attention"] != true {
		t.Fatalf("needs attention %v", att)
	}
	calm, _ := h.staffList(t, chef, checkout.StaffOrderFilter{From: november.From, To: november.To, NeedsAttention: ptr(false)})
	if !slices.Equal(ids(calm), []string{early, b, a, d, e}) {
		t.Fatalf("no attention %v", ids(calm))
	}

	// Limits: the API refuses before the database, and the gateway itself
	// refuses too.
	for name, bad := range map[string]checkout.StaffOrderFilter{
		"94 days":   {From: day("2026-11-01"), To: day("2027-02-02")},
		"reversed":  {From: day("2026-11-02"), To: day("2026-11-01")},
		"limit":     {From: day("2026-11-01"), To: day("2026-11-01"), Limit: 101},
		"lifecycle": {From: day("2026-11-01"), To: day("2026-11-01"), Lifecycles: []string{"paid"}},
	} {
		var inv *catalog.InvalidError
		if _, err := h.co.StaffOrders(h.ctx, chef, bad); !errors.As(err, &inv) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := h.co.StaffOrders(h.ctx, chef, checkout.StaffOrderFilter{From: day("2026-11-01"), To: day("2027-02-01")}); err != nil {
		t.Fatalf("93 days: %v", err)
	}
	if _, err := h.co.StaffOrders(h.ctx, chef, checkout.StaffOrderFilter{From: november.From, To: november.To, Cursor: "bad"}); !errors.Is(err, catalog.ErrBadCursor) {
		t.Fatalf("bad cursor: %v", err)
	}
	for name, sql := range map[string]string{
		"94 days":   "SELECT business.order_staff_list($1::uuid, '2026-11-01', '2027-02-02', NULL, NULL, NULL, NULL, NULL, 10)",
		"no from":   "SELECT business.order_staff_list($1::uuid, NULL, '2026-11-02', NULL, NULL, NULL, NULL, NULL, 10)",
		"limit":     "SELECT business.order_staff_list($1::uuid, '2026-11-01', '2026-11-02', NULL, NULL, NULL, NULL, NULL, 101)",
		"lifecycle": "SELECT business.order_staff_list($1::uuid, '2026-11-01', '2026-11-02', ARRAY['paid'], NULL, NULL, NULL, NULL, 10)",
		"cursor":    "SELECT business.order_staff_list($1::uuid, '2026-11-01', '2026-11-02', NULL, NULL, now(), NULL, NULL, 10)",
	} {
		if _, err := h.api.Exec(h.ctx, sql, chef); dbtest.SQLState(err) != "GK422" {
			t.Errorf("gateway %s: %v", name, err)
		}
	}
}

func (h *payHarness) collectionOf(t *testing.T, order string) string {
	t.Helper()
	var id string
	if err := h.admin.QueryRow(h.ctx, `SELECT id::text FROM business.payment_collection WHERE order_id = $1::uuid
		ORDER BY created_at DESC LIMIT 1`, order).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// The visible lifecycle (K11 pending states) and each attention flag.
func TestStaffOrderLifecycleAndAttentionFlags(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	state := func(order string) (string, string, []string) {
		t.Helper()
		v := h.staffGet(t, support, order)
		return v["lifecycle"].(string), v["raw_lifecycle"].(string), flags(v)
	}
	want := func(order, shown, raw string, fl ...string) {
		t.Helper()
		gotShown, gotRaw, gotFlags := state(order)
		if gotShown != shown || gotRaw != raw || !slices.Equal(gotFlags, fl) {
			t.Fatalf("%s: %s/%s %v, want %s/%s %v", order, gotShown, gotRaw, gotFlags, shown, raw, fl)
		}
		row := rowOf(t, func() []staffRow { r, _ := h.staffList(t, chef, november); return r }(), order)
		if row["lifecycle"] != shown || row["raw_lifecycle"] != raw || !slices.Equal(flags(row), fl) {
			t.Fatalf("%s list row %v", order, row)
		}
	}

	// confirmation_pending and cancellation_pending as customers see them.
	o, od := h.tenThousandOrder(t, f, pol, "pend")
	want(o, "awaiting_payment", "awaiting_payment")
	h.owner(t, "UPDATE business.orders SET recovery_state = 'confirmation_pending' WHERE id = $1::uuid", o)
	want(o, "confirmation_pending", "awaiting_payment")
	h.owner(t, "UPDATE business.orders SET recovery_state = 'cancellation_pending', lifecycle = 'cancelled', cancel_reason = 'chef_cancelled', cancelled_at = now() WHERE id = $1::uuid", o)
	want(o, "cancellation_pending", "cancelled")
	if v := h.staffGet(t, chef, o); v["cancel_reason"] != nil {
		t.Fatalf("cancel reason before the cancellation is secured: %v", v)
	}
	h.owner(t, "UPDATE business.orders SET recovery_state = 'secured' WHERE id = $1::uuid", o)
	want(o, "cancelled", "cancelled")
	if v := h.staffGet(t, fulfilment, func() string { p, _, _ := h.paidCollection(t, f, pol, "x"); return p }()); v["lifecycle"] != "confirmed" {
		t.Fatalf("fulfillment %v", v)
	}
	_ = od

	// payment_unknown, payment_mismatch.
	u, ud := h.tenThousandOrder(t, f, pol, "unk")
	uc, _ := h.started(t, u, ud)
	h.owner(t, "UPDATE business.payment_collection SET state = 'unknown' WHERE id = $1::uuid", uc)
	want(u, "awaiting_payment", "awaiting_payment", "payment_unknown")
	h.owner(t, "UPDATE business.payment_collection SET mismatch_reason = 'test contradiction' WHERE id = $1::uuid", uc)
	want(u, "awaiting_payment", "awaiting_payment", "payment_unknown", "payment_mismatch")

	// paid_awaiting_agreement (K04 Q2): no production for fulfillment.
	late, _ := h.lateOrder(t, f, pol, "late")
	want(late, "paid_awaiting_agreement", "paid_awaiting_agreement", "paid_awaiting_agreement")
	if _, err := h.co.StaffOrder(h.ctx, fulfilment, late); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("fulfillment, awaiting agreement: %v", err)
	}
}

// The detail of a late capture with an unknown refund and a quarantined
// dashboard refund: the goal, the operation, the observation and the
// commands; amounts and provider references for the chef only.
func TestStaffOrderDetailRefunds(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.staffGrants(t)
	order, _, coll := h.lateCapture(t, f, pol, "fix")
	r := h.unknownPastWindow(t, coll)
	h.stripe.dashboardRefund(r.PI, 5000, "succeeded")
	h.due(t)
	h.drain(t) // the complete list: the dashboard refund quarantined
	v := h.staffGet(t, chef, order)
	if v["lifecycle"] != "cancelled" || v["cancel_reason"] != "payment_not_completed" ||
		!slices.Equal(flags(v), []string{"refund_needs_attention", "refund_quarantine"}) || v["needs_attention"] != true {
		t.Fatalf("chef detail %v %v", v["lifecycle"], v["attention"])
	}
	goal := v["refund_goal"].(map[string]any)
	if goal["target_minor"] != 10000.0 || goal["reason"] == nil || goal["remaining_minor"] != 0.0 || goal["version"] == nil {
		t.Fatalf("goal %v", goal)
	}
	ops := v["refund_operations"].([]any)
	op := ops[0].(map[string]any)
	if len(ops) != 1 || op["id"] != r.ID || op["state"] != "unknown" || op["amount_minor"] != 10000.0 || op["channel"] != "provider" ||
		op["window_ended"] != true || op["collection_id"] != coll || op["record_state"] == nil {
		t.Fatalf("operations %v", ops)
	}
	obs := v["refund_observations"].([]any)
	ob := obs[0].(map[string]any)
	if len(obs) != 1 || ob["quarantined"] != true || ob["amount_minor"] != 5000.0 || ob["status"] != "succeeded" ||
		!strings.HasPrefix(ob["provider_refund_id"].(string), "re_") || ob["quarantine_reason"] == nil || ob["resolved_at"] != nil {
		t.Fatalf("observations %v", obs)
	}
	if rf := v["refund"].(map[string]any); rf["state"] != "needs_attention" {
		t.Fatalf("refund %v", rf)
	}
	var kinds []string
	for _, c := range v["commands"].([]any) {
		cm := c.(map[string]any)
		kinds = append(kinds, cm["kind"].(string)+":"+cm["outcome_status"].(string))
	}
	if !slices.Equal(kinds, []string{"paid_confirm:refused"}) {
		t.Fatalf("commands %v", kinds)
	}
	row := rowOf(t, func() []staffRow { r, _ := h.staffList(t, chef, november); return r }(), order)
	if rf := row["refund"].(map[string]any); rf["state"] != "needs_attention" || rf["target_minor"] != 10000.0 || rf["delivery_pending_minor"] != 0.0 {
		t.Fatalf("list refund %v", rf)
	}
	sv := h.staffGet(t, support, order)
	noneOf(t, "support detail", sv, true, notForSupport...)
	sops, sobs := sv["refund_operations"].([]any), sv["refund_observations"].([]any)
	if len(sops) != 1 || sops[0].(map[string]any)["state"] != "unknown" || len(sobs) != 1 || sobs[0].(map[string]any)["quarantined"] != true ||
		sv["refund_goal"].(map[string]any)["reason"] == nil || sv["refund"].(map[string]any)["state"] != "needs_attention" {
		t.Fatalf("support detail %v %v", sops, sobs)
	}
	if _, err := h.co.StaffOrder(h.ctx, fulfilment, order); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("fulfillment, never confirmed: %v", err)
	}
}
