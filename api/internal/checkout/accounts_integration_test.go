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
	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
)

// Customer accounts (GK-016 r02b, migration 0033).

const customerC = "77ac712e-4e38-48bb-a56d-fa7809ccc13f"

// resolve signs a subject in as the API does (identity_resolve_account).
func (h *harness) resolve(t *testing.T, subject string) identity.Actor {
	t.Helper()
	a, err := (&identity.Directory{DB: h.api}).Resolve(h.ctx, identity.Claims{Subject: subject})
	if err != nil {
		t.Fatalf("resolve %s: %v", subject, err)
	}
	return a
}

// signedInOrder places a 1000p made-to-order order for subject.
func (h *harness) signedInOrder(t *testing.T, f fixture, pol, subject, key string) string {
	t.Helper()
	in := checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot, FulfillmentMode: schedule.Collection,
		Lines: []checkout.QuoteLine{{VariantID: f.v1000, Quantity: 1}}}
	q := must[catalog.Result](t)(h.co.CreateQuote(h.ctx, subject, nil, "q-"+key, in)).ID
	return must[catalog.Result](t)(h.checkout(subject, nil, "k-"+key, q, pol)).ID
}

// guestOrder places a 1000p guest order and returns it with its capability
// digest.
func (h *harness) guestOrder(t *testing.T, f fixture, pol, key string) (string, []byte) {
	t.Helper()
	q, d := h.guestQuote(t, "q-"+key, checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot,
		FulfillmentMode: schedule.Collection, Lines: []checkout.QuoteLine{{VariantID: f.v1000, Quantity: 1}}})
	return must[catalog.Result](t)(h.checkout("", d, "k-"+key, q, pol)).ID, d
}

type summary struct {
	ID          string
	Version     int
	Lifecycle   string
	TotalMinor  int64  `json:"total_minor"`
	AddedBy     string `json:"added_by"`
	Fulfillment any    `json:"fulfillment_state"`
	Payment     *struct {
		State           string
		PaymentPageOpen bool `json:"payment_page_open"`
	}
	Refund *struct{ State string }
	Items  []struct {
		CakeName string `json:"cake_name"`
		Quantity int
	}
}

func (h *harness) myOrders(t *testing.T, subject, cursor string, limit int) ([]summary, string, string) {
	t.Helper()
	p, err := h.co.MyOrders(h.ctx, subject, cursor, limit)
	if err != nil {
		t.Fatalf("my orders: %v", err)
	}
	var rows []summary
	if err := json.Unmarshal(p.Orders, &rows); err != nil {
		t.Fatal(err)
	}
	return rows, p.NextCursor, string(p.Orders)
}

func summaryIDs(rows []summary) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func (h *harness) auditDetails(t *testing.T, action string) []string {
	t.Helper()
	rows, err := h.admin.Query(h.ctx, `SELECT coalesce(detail::text, '') FROM business.audit_event WHERE action = $1 ORDER BY occurred_at, id`, action)
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

func orderLifecycle(t *testing.T, raw []byte) (string, int) {
	t.Helper()
	var o struct {
		Lifecycle string
		Version   int
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	return o.Lifecycle, o.Version
}

// Each account sees only its own orders, newest placed first, in pages;
// the summary has order_get's visible state and no contact details; a
// guest order is in no account; a disabled account reads nothing.
func TestMyOrdersAreOwnerOnlyAndPaged(t *testing.T) {
	h := newHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.resolve(t, customerA)
	h.resolve(t, customerB)
	var mine []string
	for i, k := range []string{"a1", "a2", "a3"} {
		h.now = h.now.Add(time.Duration(i) * time.Minute)
		mine = append([]string{h.signedInOrder(t, f, pol, customerA, k)}, mine...)
	}
	theirs := h.signedInOrder(t, f, pol, customerB, "b1")
	h.guestOrder(t, f, pol, "g1")

	first, cursor, raw := h.myOrders(t, customerA, "", 2)
	if !slices.Equal(summaryIDs(first), mine[:2]) || cursor == "" {
		t.Fatalf("first page %v %q, want %v and a cursor", summaryIDs(first), cursor, mine[:2])
	}
	for _, forbidden := range []string{"contact_email", "customer_full_name", "example.com", "checkout_url", "policy_version_id"} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("summary shows %s: %s", forbidden, raw)
		}
	}
	second, next, _ := h.myOrders(t, customerA, cursor, 2)
	if !slices.Equal(summaryIDs(second), mine[2:]) || next != "" {
		t.Fatalf("second page %v %q, want %v and no cursor", summaryIDs(second), next, mine[2:])
	}
	for _, s := range append(first, second...) {
		raw, err := h.co.GetOrder(h.ctx, s.ID, customerA, nil)
		if err != nil {
			t.Fatal(err)
		}
		lifecycle, version := orderLifecycle(t, raw)
		if s.Lifecycle != lifecycle || s.Version != version || s.TotalMinor != 1000 || s.AddedBy != "checkout" ||
			len(s.Items) != 1 || s.Items[0].CakeName != "Pink piping" || s.Items[0].Quantity != 1 {
			t.Errorf("summary %+v, order lifecycle %s version %d", s, lifecycle, version)
		}
	}
	if rows, _, _ := h.myOrders(t, customerB, "", 0); !slices.Equal(summaryIDs(rows), []string{theirs}) {
		t.Fatalf("customer B sees %v, want only %s", summaryIDs(rows), theirs)
	}
	h.resolve(t, customerC)
	if rows, _, _ := h.myOrders(t, customerC, "", 0); len(rows) != 0 {
		t.Fatalf("a new account sees %v", summaryIDs(rows))
	}
	if _, err := h.co.MyOrders(h.ctx, customerA, "bm90LWEtY3Vyc29y", 0); !errors.Is(err, catalog.ErrBadCursor) {
		t.Fatalf("bad cursor: %v", err)
	}
	if _, err := h.co.MyOrders(h.ctx, customerA, "", 51); invalidField(err) == "" {
		t.Fatalf("limit 51: %v", err)
	}
	h.owner(t, "SELECT business.account_set_disabled($1::uuid, true, 'test')", customerB)
	if _, err := h.co.MyOrders(h.ctx, customerB, "", 0); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("disabled account: %v", err)
	}
}

// The profile: optional fields with strict validation, expected_version,
// replay by key, null clears; audited with field names only.
func TestProfileUpdates(t *testing.T) {
	h := newHarness(t)
	h.resolve(t, customerA)
	type profile struct {
		FullName         *string `json:"full_name"`
		Phone, Instagram *string
		Version          int
	}
	read := func() profile {
		t.Helper()
		raw, err := h.co.Profile(h.ctx, customerA)
		if err != nil {
			t.Fatal(err)
		}
		var p profile
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if p := read(); p.Version != 1 || p.FullName != nil || p.Phone != nil || p.Instagram != nil {
		t.Fatalf("new profile %+v", p)
	}
	set := func(s string) catalog.Optional[string] { return catalog.Optional[string]{Set: true, Value: s} }
	in := checkout.ProfilePatch{ExpectedVersion: 1, FullName: set("Anna Example"), Phone: set("+44 7700 900123")}
	res, err := h.co.UpdateProfile(h.ctx, customerA, "p1", in)
	if err != nil || res.Replayed {
		t.Fatalf("update: %+v %v", res, err)
	}
	if p := read(); p.Version != 2 || *p.FullName != "Anna Example" || *p.Phone != "+44 7700 900123" || p.Instagram != nil {
		t.Fatalf("after update %+v", p)
	}
	if res, err := h.co.UpdateProfile(h.ctx, customerA, "p1", in); err != nil || !res.Replayed {
		t.Fatalf("replay: %+v %v", res, err)
	}
	changed := in
	changed.Phone = set("07700 900123")
	if _, err := h.co.UpdateProfile(h.ctx, customerA, "p1", changed); conflict(err) != "key_reused" {
		t.Fatalf("changed body, same key: %v", err)
	}
	if _, err := h.co.UpdateProfile(h.ctx, customerA, "p2", changed); conflict(err) != "stale_version" {
		t.Fatalf("stale: %v", err)
	}
	for name, bad := range map[string]checkout.ProfilePatch{
		"phone":            {ExpectedVersion: 2, Phone: set("call me")},
		"phone digits":     {ExpectedVersion: 2, Phone: set("+44 12")},
		"instagram":        {ExpectedVersion: 2, Instagram: set("@anna")},
		"full_name":        {ExpectedVersion: 2, FullName: set(" Anna")},
		"profile":          {ExpectedVersion: 2},
		"expected_version": {Instagram: set("anna.cakes")},
	} {
		if got := invalidField(bad.Validate()); !strings.HasPrefix(got, strings.Fields(name)[0]+":") {
			t.Errorf("%s: %q", name, got)
		}
	}
	clear := checkout.ProfilePatch{ExpectedVersion: 2, Phone: catalog.Optional[string]{Set: true, Null: true}, Instagram: set("anna.cakes_1")}
	if _, err := h.co.UpdateProfile(h.ctx, customerA, "p3", clear); err != nil {
		t.Fatal(err)
	}
	if p := read(); p.Version != 3 || p.Phone != nil || *p.Instagram != "anna.cakes_1" || *p.FullName != "Anna Example" {
		t.Fatalf("after clearing the phone %+v", p)
	}
	details := h.auditDetails(t, "account.profile_updated")
	if len(details) != 2 || details[0] != `{"fields" : ["full_name","phone"]}` || details[1] != `{"fields" : ["phone","instagram"]}` {
		t.Fatalf("audit %v", details)
	}
	// The database refuses what the API would (a bypassing caller).
	if _, err := h.api.Exec(h.ctx, `SELECT * FROM business.account_profile_update($1::uuid, 'p4', $2, 3, false, NULL, true, 'x', false, NULL)`,
		customerA, strings.Repeat("a", 64)); pgMessage(err) != "profile_invalid" {
		t.Fatalf("invalid phone in the database: %v", err)
	}
}

type claimView struct {
	ID              string
	Version         int
	State           string
	Path            string
	Channel         string
	Reference       *string
	Note            *string
	OrderID         *string `json:"order_id"`
	ApproximateDate *string `json:"approximate_date"`
}

func (h *harness) claim(t *testing.T, subject, id string) (claimView, string) {
	t.Helper()
	raw, err := h.co.MyClaim(h.ctx, subject, id)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	var c claimView
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c, string(raw)
}

func (h *harness) staffClaims(t *testing.T, state string) []map[string]any {
	t.Helper()
	p, err := h.co.StaffClaims(h.ctx, chef, state, "", 0)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(p.Claims, &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func (h *harness) orderVersion(t *testing.T, order string) int {
	t.Helper()
	var v int
	if err := h.admin.QueryRow(h.ctx, "SELECT version FROM business.orders WHERE id = $1::uuid", order).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// Path c: reference + channel + roughly when + note, pending until the chef
// decides; approving links the order and revokes its guest capability;
// declining reveals nothing; only the chef sees and decides the queue.
func TestOlderOrderClaimsWaitForTheChef(t *testing.T) {
	h := newHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.owner(t, "SELECT business.staff_grant($1::uuid, 'fulfillment', 'test')", fulfilment)
	h.owner(t, "SELECT business.staff_grant($1::uuid, 'technical_support', 'test')", support)
	h.resolve(t, customerA)
	h.resolve(t, customerB)
	order, digest := h.guestOrder(t, f, pol, "g1")

	in := checkout.ClaimInput{Channel: "instagram", Reference: "IG-0613", ApproximateDate: "2026-06", Note: "Pink cake for my mum.\nCollected in June."}
	for field, bad := range map[string]checkout.ClaimInput{
		"note":             {Channel: "instagram", Reference: "IG-0613"},
		"reference":        {Channel: "offline", Note: "x"},
		"channel":          {Channel: "email", Reference: "IG-0613", Note: "x"},
		"approximate_date": {Channel: "offline", Reference: "IG-0613", Note: "x", ApproximateDate: "2026-02-30"},
	} {
		if _, err := h.co.SubmitClaim(h.ctx, customerA, nil, "bad", bad); !strings.HasPrefix(invalidField(err), field+":") {
			t.Errorf("%s: %v", field, err)
		}
	}
	res, err := h.co.SubmitClaim(h.ctx, customerA, nil, "c1", in)
	if err != nil || res.Replayed {
		t.Fatalf("submit: %+v %v", res, err)
	}
	if again, err := h.co.SubmitClaim(h.ctx, customerA, nil, "c1", in); err != nil || !again.Replayed || again.ID != res.ID {
		t.Fatalf("replay: %+v %v", again, err)
	}
	c, raw := h.claim(t, customerA, res.ID)
	if c.State != "pending" || c.Path != "evidence" || c.OrderID != nil || *c.Reference != "IG-0613" || *c.ApproximateDate != "2026-06" {
		t.Fatalf("pending claim %s", raw)
	}
	if _, err := h.co.MyClaim(h.ctx, customerB, res.ID); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("another customer reads the claim: %v", err)
	}
	for _, s := range []string{fulfilment, support, customerA} {
		if _, err := h.co.StaffClaims(h.ctx, s, "", "", 0); !errors.Is(err, catalog.ErrForbidden) {
			t.Errorf("%s reads the queue: %v", s, err)
		}
		if _, err := h.co.DecideClaim(h.ctx, s, "d", res.ID, checkout.ClaimDecisionInput{ExpectedVersion: 1, Decision: "decline"}); !errors.Is(err, catalog.ErrForbidden) {
			t.Errorf("%s decides: %v", s, err)
		}
	}
	queue := h.staffClaims(t, "pending")
	if len(queue) != 1 || queue[0]["id"] != res.ID || queue[0]["account"].(map[string]any)["id"] == nil || queue[0]["order_id"] != nil {
		t.Fatalf("queue %v", queue)
	}

	approve := checkout.ClaimDecisionInput{ExpectedVersion: 2, Decision: "approve", OrderID: order, Note: "Matched IG DM of 13 June"}
	if _, err := h.co.DecideClaim(h.ctx, chef, "d1", res.ID, approve); conflict(err) != "stale_version" {
		t.Fatalf("stale decision: %v", err)
	}
	approve.ExpectedVersion = 1
	if d, err := h.co.DecideClaim(h.ctx, chef, "d1", res.ID, approve); err != nil || d.Replayed {
		t.Fatalf("approve: %+v %v", d, err)
	}
	if d, err := h.co.DecideClaim(h.ctx, chef, "d1", res.ID, approve); err != nil || !d.Replayed {
		t.Fatalf("approve replay: %+v %v", d, err)
	}
	c, raw = h.claim(t, customerA, res.ID)
	if c.State != "approved" || c.OrderID == nil || *c.OrderID != order || strings.Contains(raw, "Matched") {
		t.Fatalf("approved claim %s", raw)
	}
	rows, _, _ := h.myOrders(t, customerA, "", 0)
	if len(rows) != 1 || rows[0].ID != order || rows[0].AddedBy != "claim" {
		t.Fatalf("account orders after approval %+v", rows)
	}
	if _, err := h.co.GetOrder(h.ctx, order, "", digest); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("guest capability still reads after approval: %v", err)
	}
	if _, err := h.co.GetOrder(h.ctx, order, customerA, nil); err != nil {
		t.Fatalf("owner reads: %v", err)
	}
	if got := h.auditDetails(t, "order.owner_linked"); len(got) != 1 || !strings.Contains(got[0], `"source" : "claim"`) ||
		!strings.Contains(got[0], `"guest_capabilities_revoked" : 1`) {
		t.Fatalf("association audit %v", got)
	}
	if n := h.count(t, `SELECT count(*) FROM business.order_owner_event WHERE order_id = $1::uuid AND kind = 'linked'
		AND source = 'claim' AND claim_id = $2::uuid`, order, res.ID); n != 1 {
		t.Fatalf("association events %d", n)
	}
	if dbtest.SQLState(h.ownerErr(t, "UPDATE business.order_owner_event SET occurred_at = now()")) != "GK409" {
		t.Fatal("association history is mutable")
	}

	// B asks for the same order: approving it for B is refused; declining
	// tells B nothing.
	other := must[catalog.Result](t)(h.co.SubmitClaim(h.ctx, customerB, nil, "c2", checkout.ClaimInput{Channel: "offline",
		Reference: "Cash order", Note: "Collected last week"}))
	if _, err := h.co.DecideClaim(h.ctx, chef, "d2", other.ID, checkout.ClaimDecisionInput{ExpectedVersion: 1, Decision: "approve", OrderID: order}); conflict(err) != "order_owned" {
		t.Fatalf("approve an order of another account: %v", err)
	}
	if _, err := h.co.DecideClaim(h.ctx, chef, "d3", other.ID, checkout.ClaimDecisionInput{ExpectedVersion: 1, Decision: "decline", OrderID: order}); invalidField(err) != "order_id: must be omitted when declining" {
		t.Fatalf("decline with an order: %v", err)
	}
	if _, err := h.co.DecideClaim(h.ctx, chef, "d3", other.ID, checkout.ClaimDecisionInput{ExpectedVersion: 1, Decision: "decline", Note: "No such order in IG"}); err != nil {
		t.Fatal(err)
	}
	c, raw = h.claim(t, customerB, other.ID)
	if c.State != "declined" || c.OrderID != nil || strings.Contains(raw, "No such") || strings.Contains(raw, order) {
		t.Fatalf("declined claim %s", raw)
	}
	if rows, _, _ := h.myOrders(t, customerB, "", 0); len(rows) != 0 {
		t.Fatalf("declined claim linked %v", summaryIDs(rows))
	}
	if q := h.staffClaims(t, "declined"); len(q) != 1 || q[0]["decision_note"] != "No such order in IG" {
		t.Fatalf("declined queue %v", q)
	}
	// Withdrawal and the limit of 5 pending claims.
	var pending []string
	for _, k := range []string{"w1", "w2", "w3", "w4", "w5"} {
		pending = append(pending, must[catalog.Result](t)(h.co.SubmitClaim(h.ctx, customerB, nil, k,
			checkout.ClaimInput{Channel: "offline", Reference: "Ref " + k, Note: "Note"})).ID)
	}
	if _, err := h.co.SubmitClaim(h.ctx, customerB, nil, "w6", checkout.ClaimInput{Channel: "offline", Reference: "Ref", Note: "Note"}); conflict(err) != "claim_limit" {
		t.Fatalf("sixth pending claim: %v", err)
	}
	if _, err := h.co.WithdrawClaim(h.ctx, customerA, "x", pending[0], catalog.VersionInput{ExpectedVersion: 1}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("withdraw another's claim: %v", err)
	}
	if _, err := h.co.WithdrawClaim(h.ctx, customerB, "x", pending[0], catalog.VersionInput{ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if c, _ := h.claim(t, customerB, pending[0]); c.State != "withdrawn" || c.Version != 2 {
		t.Fatalf("withdrawn %+v", c)
	}
	if _, err := h.co.DecideClaim(h.ctx, chef, "d4", pending[0], checkout.ClaimDecisionInput{ExpectedVersion: 2, Decision: "decline"}); conflict(err) != "state_conflict" {
		t.Fatalf("decide a withdrawn claim: %v", err)
	}
	if _, err := h.co.SubmitClaim(h.ctx, customerB, nil, "w6", checkout.ClaimInput{Channel: "offline", Reference: "Ref", Note: "Note"}); err != nil {
		t.Fatalf("after a withdrawal: %v", err)
	}
	var list []claimView
	raw2, _ := h.co.MyClaims(h.ctx, customerB)
	if err := json.Unmarshal(raw2, &list); err != nil || len(list) != 7 {
		t.Fatalf("B's claims %s %v", raw2, err)
	}
	for _, d := range append(h.auditDetails(t, "order_claim.submitted"), h.auditDetails(t, "order_claim.declined")...) {
		if strings.Contains(d, "IG-0613") || strings.Contains(d, "mum") || strings.Contains(d, "No such") {
			t.Errorf("personal text in audit: %s", d)
		}
	}
}

// Path a: the customer holds the order's guest capability in this browser
// (the emailed link does not exist yet); the claim still waits for the
// chef, who may approve only that order; approval revokes the capability.
func TestClaimFromTheOrderCapability(t *testing.T) {
	h := newHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.resolve(t, customerA)
	order, digest := h.guestOrder(t, f, pol, "g1")
	another, _ := h.guestOrder(t, f, pol, "g2")
	_, unknown := proof(t)

	link := checkout.ClaimInput{Channel: "website_email_link"}
	if _, err := h.co.SubmitClaim(h.ctx, customerA, unknown, "u", link); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("unknown capability: %v", err)
	}
	if _, err := h.co.SubmitClaim(h.ctx, customerA, digest, "r", checkout.ClaimInput{Channel: "website_email_link", Reference: "x"}); invalidField(err) == "" {
		t.Fatalf("reference with a capability: %v", err)
	}
	res := must[catalog.Result](t)(h.co.SubmitClaim(h.ctx, customerA, digest, "l1", link))
	c, raw := h.claim(t, customerA, res.ID)
	if c.State != "pending" || c.Path != "email_link" || c.OrderID != nil || strings.Contains(raw, order) {
		t.Fatalf("pending link claim %s", raw)
	}
	if _, err := h.co.SubmitClaim(h.ctx, customerA, digest, "l2", link); conflict(err) != "claim_pending" {
		t.Fatalf("second link claim: %v", err)
	}
	// The guest link keeps working while the claim waits.
	if _, err := h.co.GetOrder(h.ctx, order, "", digest); err != nil {
		t.Fatalf("guest read while pending: %v", err)
	}
	if _, err := h.co.DecideClaim(h.ctx, chef, "d1", res.ID, checkout.ClaimDecisionInput{ExpectedVersion: 1, Decision: "approve", OrderID: another}); !strings.HasPrefix(invalidField(err), "order_id:") {
		t.Fatalf("approve another order: %v", err)
	}
	must[catalog.Result](t)(h.co.DecideClaim(h.ctx, chef, "d2", res.ID, checkout.ClaimDecisionInput{ExpectedVersion: 1, Decision: "approve", OrderID: order}))
	if _, err := h.co.GetOrder(h.ctx, order, "", digest); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("guest read after approval: %v", err)
	}
	if _, err := h.co.SubmitClaim(h.ctx, customerA, digest, "l3", link); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("claim with a revoked capability: %v", err)
	}
	// A capability issued later for an order already in the account.
	_, fresh := proof(t)
	h.owner(t, `INSERT INTO business.order_guest_capability (id, order_id, capability_digest, allowed_commands)
		VALUES (gen_random_uuid(), $1::uuid, $2, ARRAY['read'])`, order, fresh)
	if _, err := h.co.SubmitClaim(h.ctx, customerA, fresh, "l4", link); conflict(err) != "already_yours" {
		t.Fatalf("claim of an order already in the account: %v", err)
	}
}

// The chef adds an order to an account directly, with evidence; guest
// capabilities are revoked; only the chef, only an unowned order.
func TestChefAddsAnOrderToAnAccount(t *testing.T) {
	h := newHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.owner(t, "SELECT business.staff_grant($1::uuid, 'fulfillment', 'test')", fulfilment)
	a := h.resolve(t, customerA)
	h.resolve(t, customerB)
	order, digest := h.guestOrder(t, f, pol, "g1")
	v := h.orderVersion(t, order)
	in := checkout.OwnerAssignInput{ExpectedVersion: v, AccountSubject: customerA, Evidence: "Customer asked by email on 4 Nov"}
	if _, err := h.co.AssignOwner(h.ctx, fulfilment, "a", order, in); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("fulfilment: %v", err)
	}
	stale := in
	stale.ExpectedVersion = v + 1
	if _, err := h.co.AssignOwner(h.ctx, chef, "a0", order, stale); conflict(err) != "stale_version" {
		t.Fatalf("stale: %v", err)
	}
	unknownAccount := in
	unknownAccount.AccountSubject = "88ac712e-4e38-48bb-a56d-fa7809ccc13f"
	if _, err := h.co.AssignOwner(h.ctx, chef, "a1", order, unknownAccount); !strings.HasPrefix(invalidField(err), "account_id:") {
		t.Fatalf("unknown account: %v", err)
	}
	if res, err := h.co.AssignOwner(h.ctx, chef, "a2", order, in); err != nil || res.Replayed || res.ID != order {
		t.Fatalf("assign: %+v %v", res, err)
	}
	if res, err := h.co.AssignOwner(h.ctx, chef, "a2", order, in); err != nil || !res.Replayed {
		t.Fatalf("assign replay: %+v %v", res, err)
	}
	if _, err := h.co.GetOrder(h.ctx, order, "", digest); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("guest read after the chef added it: %v", err)
	}
	rows, _, _ := h.myOrders(t, customerA, "", 0)
	if len(rows) != 1 || rows[0].AddedBy != "chef" || rows[0].Version != v+1 {
		t.Fatalf("account orders %+v", rows)
	}
	byID := in
	byID.ExpectedVersion, byID.AccountSubject, byID.AccountID = v+1, "", a.AccountID
	if _, err := h.co.AssignOwner(h.ctx, chef, "a3", order, byID); conflict(err) != "duplicate" {
		t.Fatalf("again to the same account: %v", err)
	}
	toB := byID
	toB.AccountID, toB.AccountSubject = "", customerB
	if _, err := h.co.AssignOwner(h.ctx, chef, "a4", order, toB); conflict(err) != "order_owned" {
		t.Fatalf("to another account: %v", err)
	}
	if got := h.auditDetails(t, "order.owner_linked"); len(got) != 1 || strings.Contains(got[0], "email") {
		t.Fatalf("audit %v", got)
	}
	var evidence string
	if err := h.admin.QueryRow(h.ctx, "SELECT evidence FROM business.order_owner_event WHERE order_id = $1::uuid", order).Scan(&evidence); err != nil || evidence != in.Evidence {
		t.Fatalf("evidence %q %v", evidence, err)
	}
	// A restore freeze refuses order changes.
	other, _ := h.guestOrder(t, f, pol, "g2")
	h.owner(t, "UPDATE business.recovery_freeze SET frozen = true, reason = 'test'")
	if _, err := h.co.AssignOwner(h.ctx, chef, "a5", other, checkout.OwnerAssignInput{ExpectedVersion: 1, AccountSubject: customerB,
		Evidence: "x"}); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("frozen: %v", err)
	}
}
