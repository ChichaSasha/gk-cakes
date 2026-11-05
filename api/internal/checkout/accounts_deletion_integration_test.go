//go:build integration

package checkout_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
)

// Account deletion (GK-016 r02b answers 2, 3, 11; K09).

type deletionPreview struct {
	PreviewID   string `json:"preview_id"`
	PreviewHash string `json:"preview_hash"`
	CanDelete   bool   `json:"can_delete"`
	Blocked     []struct {
		OrderID string `json:"order_id"`
		Reason  string
	}
	Upcoming []struct {
		OrderID   string `json:"order_id"`
		Date      string
		Lifecycle string
	}
	LinkedOrders      int       `json:"linked_orders"`
	PendingClaims     int       `json:"pending_claims"`
	StaffCapabilities []string  `json:"staff_capabilities"`
	ExpiresAt         time.Time `json:"expires_at"`
}

func (h *harness) deletionPreview(t *testing.T, subject string) deletionPreview {
	t.Helper()
	raw, err := h.co.PreviewDeletion(h.ctx, subject)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	var p deletionPreview
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func (h *harness) deleteAccount(subject string, auth time.Time, key string, p deletionPreview) (checkout.Deleted, error) {
	return h.co.DeleteAccount(h.ctx, subject, auth, key, checkout.DeletionInput{PreviewID: p.PreviewID, PreviewHash: p.PreviewHash})
}

// assign adds a guest order to the account of subject as the chef.
func (h *harness) assign(t *testing.T, order, subject, key string) {
	t.Helper()
	must[catalog.Result](t)(h.co.AssignOwner(h.ctx, chef, key, order, checkout.OwnerAssignInput{
		ExpectedVersion: h.orderVersion(t, order), AccountSubject: subject, Evidence: "Asked by Anna Example on Instagram"}))
}

// Unsettled money blocks deletion (a payment being checked, paid awaiting
// a date, a refund in progress), checked again after the locks.
func TestAccountDeletionIsBlockedWhileMoneyIsUnsettled(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	h.resolve(t, customerB)

	pending, pendingD := h.tenThousandOrder(t, f, pol, "q")
	h.started(t, pending, pendingD)
	late, _ := h.lateOrder(t, f, pol, "l")
	refunded, _, _ := h.paidCollection(t, f, pol, "r")
	d := h.digestOf(t, refunded)
	v, _ := h.cancelView(t, refunded, d)
	if _, err := h.cancel(refunded, d, "c-r", v.Version, h.preview(t, refunded, d)); err != nil {
		t.Fatal(err)
	}
	if errs := withoutRefundDispatch(h.drain(t)); len(errs) != 0 {
		t.Fatalf("cancel: %s", reasons(errs))
	}
	if v, raw := h.cancelView(t, refunded, d); v.Refund == nil || v.Refund.State == "returned" {
		t.Fatalf("refund should still be in progress: %s", raw)
	}
	for i, o := range []string{pending, late, refunded} {
		h.assign(t, o, customerB, "as"+string(rune('0'+i)))
	}
	p := h.deletionPreview(t, customerB)
	got := map[string]string{}
	for _, b := range p.Blocked {
		got[b.OrderID] = b.Reason
	}
	want := map[string]string{pending: "payment_being_checked", late: "paid_awaiting_agreement", refunded: "refund_in_progress"}
	if p.CanDelete || len(got) != 3 || got[pending] != want[pending] || got[late] != want[late] || got[refunded] != want[refunded] ||
		len(p.Upcoming) != 0 || p.LinkedOrders != 3 {
		t.Fatalf("preview %+v, want blocked %v", p, want)
	}
	if _, err := h.deleteAccount(customerB, h.now, "del", p); conflict(err) != "account_deletion_blocked" {
		t.Fatalf("delete: %v", err)
	}
	var deleted *time.Time
	if err := h.admin.QueryRow(h.ctx, "SELECT deleted_at FROM business.account WHERE provider_subject = $1::uuid", customerB).Scan(&deleted); err != nil || deleted != nil {
		t.Fatalf("account deleted_at %v %v", deleted, err)
	}
}

// A confirmed upcoming order warns but does not block; an active staff
// account is refused (operator path). Deletion needs a fresh sign-in and
// the unchanged preview; it unlinks the orders (kept), revokes guest
// capabilities, closes and erases claims,
// anonymises the profile, records a tombstone and a pending provider
// deletion, audits counts only, and replays by key.
func TestAccountDeletionEffects(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	a := h.resolve(t, customerA)
	h.resolve(t, customerB)
	upcoming, _, _ := h.paidCollection(t, f, pol, "p")
	h.assign(t, upcoming, customerA, "as1")
	later := h.digestOf(t, upcoming) // a capability issued after the link
	set := func(s string) catalog.Optional[string] { return catalog.Optional[string]{Set: true, Value: s} }
	must[catalog.Result](t)(h.co.UpdateProfile(h.ctx, customerA, "pr", checkout.ProfilePatch{ExpectedVersion: 1,
		FullName: set("Anna Example"), Phone: set("07700 900123"), Instagram: set("anna.example")}))
	claim := must[catalog.Result](t)(h.co.SubmitClaim(h.ctx, customerA, nil, "cl", checkout.ClaimInput{Channel: "instagram",
		Reference: "IG-0613", Note: "Anna's birthday cake"}))
	h.owner(t, "SELECT business.staff_grant($1::uuid, 'fulfillment', 'test')", customerA)

	p := h.deletionPreview(t, customerA)
	if p.CanDelete || len(p.Blocked) != 0 || len(p.Upcoming) != 1 || p.Upcoming[0].OrderID != upcoming ||
		p.Upcoming[0].Date != "2026-11-14" || p.Upcoming[0].Lifecycle != "confirmed" || p.LinkedOrders != 1 ||
		p.PendingClaims != 1 || !slices.Equal(p.StaffCapabilities, []string{"fulfillment"}) ||
		!p.ExpiresAt.Equal(h.now.Add(15*time.Minute)) || len(p.PreviewHash) != 64 {
		t.Fatalf("preview %+v", p)
	}
	// An active staff account is not deleted by its holder (operator path).
	if _, err := h.deleteAccount(customerA, h.now, "d-staff", p); conflict(err) != "staff_account" {
		t.Fatalf("staff self-deletion: %v", err)
	}
	h.owner(t, "SELECT business.staff_revoke($1::uuid, 'fulfillment', 'test: leaving')", customerA)
	if p = h.deletionPreview(t, customerA); !p.CanDelete || len(p.StaffCapabilities) != 0 {
		t.Fatalf("preview after the operator revoked the grant %+v", p)
	}

	// A fresh sign-in: within 10 minutes before the receipt.
	for name, auth := range map[string]time.Time{"none": {}, "11 minutes ago": h.now.Add(-11 * time.Minute),
		"in the future": h.now.Add(2 * time.Minute)} {
		if _, err := h.deleteAccount(customerA, auth, "d-"+strings.ReplaceAll(name, " ", "-"), p); !errors.Is(err, checkout.ErrReauthenticate) {
			t.Errorf("sign-in %s: %v", name, err)
		}
	}
	fresh := h.now.Add(-9 * time.Minute)
	wrong := p
	wrong.PreviewHash = strings.Repeat("0", 64)
	if _, err := h.deleteAccount(customerA, fresh, "d1", wrong); !strings.HasPrefix(invalidField(err), "preview_id:") {
		t.Fatalf("wrong hash: %v", err)
	}
	if _, err := h.deleteAccount(customerB, fresh, "d1", p); !strings.HasPrefix(invalidField(err), "preview_id:") {
		t.Fatalf("another account's preview: %v", err)
	}
	// The facts changed (another claim) or the preview expired: stale.
	second := must[catalog.Result](t)(h.co.SubmitClaim(h.ctx, customerA, nil, "cl2", checkout.ClaimInput{Channel: "offline",
		Reference: "Cash", Note: "Paid in cash"}))
	if _, err := h.deleteAccount(customerA, fresh, "d2", p); conflict(err) != "preview_stale" {
		t.Fatalf("changed facts: %v", err)
	}
	must[catalog.Result](t)(h.co.WithdrawClaim(h.ctx, customerA, "wd", second.ID, catalog.VersionInput{ExpectedVersion: 1}))
	p = h.deletionPreview(t, customerA)
	h.now = h.now.Add(15 * time.Minute)
	if _, err := h.deleteAccount(customerA, h.now, "d3", p); conflict(err) != "preview_stale" {
		t.Fatalf("expired preview: %v", err)
	}
	p = h.deletionPreview(t, customerA)
	h.owner(t, "UPDATE business.recovery_freeze SET frozen = true, reason = 'test'")
	if _, err := h.deleteAccount(customerA, h.now, "d4", p); !errors.Is(err, checkout.ErrOrdersFrozen) {
		t.Fatalf("frozen: %v", err)
	}
	h.owner(t, "UPDATE business.recovery_freeze SET frozen = false, reason = NULL")

	res, err := h.deleteAccount(customerA, h.now.Add(-time.Minute), "del", p)
	if err != nil || res.Replayed || res.AccountID != a.AccountID || res.OrdersUnlinked != 1 || !res.DeletedAt.Equal(h.now) {
		t.Fatalf("delete: %+v %v", res, err)
	}
	// A lost answer is read back with the same key and body, without a new
	// sign-in and during a freeze.
	h.now = h.now.Add(time.Hour)
	if again, err := h.deleteAccount(customerA, time.Time{}, "del", p); err != nil || !again.Replayed || !again.DeletedAt.Equal(res.DeletedAt) ||
		again.OrdersUnlinked != 1 {
		t.Fatalf("replay without a fresh sign-in: %+v %v", again, err)
	}
	h.owner(t, "UPDATE business.recovery_freeze SET frozen = true, reason = 'test'")
	if again, err := h.deleteAccount(customerA, time.Time{}, "del", p); err != nil || !again.Replayed {
		t.Fatalf("replay during a freeze: %+v %v", again, err)
	}
	h.owner(t, "UPDATE business.recovery_freeze SET frozen = false, reason = NULL")
	if _, err := h.deleteAccount(customerA, h.now, "del", deletionPreview{PreviewID: p.PreviewID, PreviewHash: strings.Repeat("b", 64)}); conflict(err) != "key_reused" {
		t.Fatalf("same key, other body: %v", err)
	}
	if _, err := h.deleteAccount(customerA, h.now, "del-2", p); !errors.Is(err, checkout.ErrAccountDeleted) {
		t.Fatalf("a new key after deletion: %v", err)
	}

	// The account: deleted, disabled, anonymous; signing in is refused.
	var name, phone, instagram *string
	var deletedAt, disabledAt *time.Time
	if err := h.admin.QueryRow(h.ctx, `SELECT profile_full_name, profile_phone, profile_instagram, deleted_at, disabled_at
		FROM business.account WHERE id = $1::uuid`, a.AccountID).Scan(&name, &phone, &instagram, &deletedAt, &disabledAt); err != nil {
		t.Fatal(err)
	}
	if name != nil || phone != nil || instagram != nil || deletedAt == nil || disabledAt == nil {
		t.Fatalf("account after deletion: %v %v %v %v %v", name, phone, instagram, deletedAt, disabledAt)
	}
	if actor, err := (&identity.Directory{DB: h.api}).Resolve(h.ctx, identity.Claims{Subject: customerA}); !errors.Is(err, identity.ErrDeleted) ||
		actor.AccountID != a.AccountID || len(actor.Capabilities) != 0 {
		t.Fatalf("resolve after deletion: %+v %v", actor, err)
	}
	if _, err := h.co.MyOrders(h.ctx, customerA, "", 0); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("my orders after deletion: %v", err)
	}
	if _, err := h.co.Profile(h.ctx, customerA); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("profile after deletion: %v", err)
	}
	// The order is kept, unlinked, with no working guest link.
	var owner *string
	var lifecycle, email string
	if err := h.admin.QueryRow(h.ctx, "SELECT owner_account_id::text, lifecycle, contact_email FROM business.orders WHERE id = $1::uuid",
		upcoming).Scan(&owner, &lifecycle, &email); err != nil || owner != nil || lifecycle != "confirmed" || email == "" {
		t.Fatalf("order after deletion: %v %s %q %v", owner, lifecycle, email, err)
	}
	if _, err := h.co.GetOrder(h.ctx, upcoming, "", later); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("guest capability after deletion: %v", err)
	}
	if _, err := h.co.GetOrder(h.ctx, upcoming, customerA, nil); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("former owner reads: %v", err)
	}
	checks := map[string]string{
		"tombstone": `SELECT count(*) FROM business.account_tombstone WHERE account_id = $1::uuid AND reason = 'customer_request'
			AND orders_unlinked = 1`,
		"pending provider deletion": `SELECT count(*) FROM business.account_provider_deletion WHERE account_id = $1::uuid AND completed_at IS NULL`,
		"no active staff grant":     `SELECT 1 - count(*) FROM business.staff_capability WHERE account_id = $1::uuid AND revoked_at IS NULL`,
		"claims erased": `SELECT count(*) FROM business.order_claim WHERE account_id = $1::uuid AND erased_at IS NOT NULL AND reference IS NULL
			AND note IS NULL AND state IN ('closed', 'withdrawn')`,
		"unlink event":    `SELECT count(*) FROM business.order_owner_event WHERE account_id = $1::uuid AND kind = 'unlinked' AND source = 'account_deleted'`,
		"evidence erased": `SELECT count(*) FROM business.order_owner_event WHERE account_id = $1::uuid AND kind = 'linked' AND evidence IS NULL`,
		"no active capability": `SELECT 1 - count(*) FROM business.order_guest_capability WHERE order_id = (SELECT order_id FROM
			business.order_owner_event WHERE account_id = $1::uuid LIMIT 1) AND revoked_at IS NULL`,
	}
	wantCounts := map[string]int{"tombstone": 1, "pending provider deletion": 1, "no active staff grant": 1, "claims erased": 2,
		"unlink event": 1, "evidence erased": 1, "no active capability": 1}
	for name, sql := range checks {
		if n := h.count(t, sql, a.AccountID); n != wantCounts[name] {
			t.Errorf("%s: %d, want %d", name, n, wantCounts[name])
		}
	}
	if c := h.count(t, `SELECT count(*) FROM business.order_claim WHERE id = $1::uuid AND state = 'closed'`, claim.ID); c != 1 {
		t.Errorf("pending claim not closed")
	}
	details := h.auditDetails(t, "account.deleted")
	if len(details) != 1 || details[0] != `{"orders_unlinked" : 1, "upcoming_orders" : 1, "guest_capabilities_revoked" : 1, "claims_erased" : 2}` {
		t.Fatalf("audit %v", details)
	}
	var everything string
	if err := h.admin.QueryRow(h.ctx, "SELECT coalesce(string_agg(detail::text, ' '), '') FROM business.audit_event").Scan(&everything); err != nil {
		t.Fatal(err)
	}
	for _, personal := range []string{"Anna", "07700", "anna.example", "IG-0613", "birthday"} {
		if strings.Contains(everything, personal) {
			t.Errorf("audit holds %q", personal)
		}
	}
	// A claim of the deleted account can no longer be decided.
	if _, err := h.co.DecideClaim(h.ctx, chef, "dc", claim.ID, checkout.ClaimDecisionInput{ExpectedVersion: 2, Decision: "approve", OrderID: upcoming}); conflict(err) != "state_conflict" {
		t.Fatalf("decide a closed claim: %v", err)
	}
	// The tombstone is immutable.
	if err := h.ownerErr(t, "DELETE FROM business.account_tombstone"); err == nil {
		t.Fatal("tombstone deleted")
	}
}

type retentionRow struct {
	Account, Notice string
	Anchor, Due     time.Time
}

func (h *commandHarness) retentionDue(t *testing.T, at time.Time) map[string]retentionRow {
	t.Helper()
	rows, err := h.worker.Query(h.ctx, `SELECT account_id::text, notice, activity_anchor, deletion_due_at
		FROM business.account_retention_due($1, 100)`, at)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]retentionRow{}
	for rows.Next() {
		var r retentionRow
		if err := rows.Scan(&r.Account, &r.Notice, &r.Anchor, &r.Due); err != nil {
			t.Fatal(err)
		}
		out[r.Account] = r
	}
	return out
}

func (h *commandHarness) recordNotice(t *testing.T, account, kind string, anchor, at time.Time) bool {
	t.Helper()
	var ok bool
	if err := h.worker.QueryRow(h.ctx, "SELECT business.account_retention_notice_record($1::uuid, $2, $3, $4)",
		account, kind, anchor, at).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

// K09 inactivity: a warning 24 London months after the last sign-in or new
// order, deletion 30 days after the warning is recorded, a reminder 7 days
// before; a returning customer starts again; staff, disabled and deleted
// accounts are never due; only the worker reads it.
func TestInactivityRetentionSteps(t *testing.T) {
	h := newCommandHarness(t)
	a := h.resolve(t, customerA).AccountID
	feb := h.resolve(t, customerB).AccountID
	staffAccount := h.resolve(t, customerC).AccountID
	h.owner(t, "SELECT business.staff_grant($1::uuid, 'fulfillment', 'test')", customerC)
	london := func(s string) time.Time {
		loc, _ := time.LoadLocation("Europe/London")
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	anchor := london("2024-11-01 12:00")
	h.owner(t, "UPDATE business.account SET last_active_at = $1, created_at = $1", anchor)
	h.owner(t, "UPDATE business.account SET last_active_at = $1 WHERE id = $2::uuid", london("2024-02-29 12:00"), feb)

	if due := h.retentionDue(t, london("2026-10-31 12:00")); len(due) != 1 || due[a].Notice != "" {
		t.Fatalf("due before 24 months (only the leap-day account is): %v", due)
	}
	due := h.retentionDue(t, london("2026-11-02 09:00"))
	if r := due[a]; r.Notice != "warning" || !r.Anchor.Equal(anchor) || !r.Due.Equal(london("2026-12-02 09:00")) {
		t.Fatalf("warning %+v", due)
	}
	if _, ok := due[staffAccount]; ok {
		t.Fatal("a staff account is due")
	}
	// 29 February + 24 months is 28 February (K09 month-end clamp).
	if r := due[feb]; r.Notice != "warning" {
		t.Fatalf("leap-day account %+v", due)
	}
	if _, ok := h.retentionDue(t, london("2026-02-28 11:59"))[feb]; ok {
		t.Fatal("leap-day account due a minute early")
	}
	if r := h.retentionDue(t, london("2026-02-28 12:00"))[feb]; r.Notice != "warning" {
		t.Fatalf("leap-day account at the anniversary: %+v", r)
	}

	if h.recordNotice(t, a, "reminder", anchor, london("2026-11-02 09:00")) {
		t.Fatal("reminder recorded before the warning")
	}
	if !h.recordNotice(t, a, "warning", anchor, london("2026-11-02 09:00")) || h.recordNotice(t, a, "warning", anchor, london("2026-11-02 09:00")) {
		t.Fatal("warning recorded twice or not at all")
	}
	if _, ok := h.retentionDue(t, london("2026-11-25 08:59"))[a]; ok {
		t.Fatal("reminder due more than 7 days before deletion")
	}
	if r := h.retentionDue(t, london("2026-11-25 09:00"))[a]; r.Notice != "reminder" || !r.Due.Equal(london("2026-12-02 09:00")) {
		t.Fatalf("reminder %+v", r)
	}
	if !h.recordNotice(t, a, "reminder", anchor, london("2026-11-25 09:00")) {
		t.Fatal("reminder not recorded")
	}
	if _, ok := h.retentionDue(t, london("2026-12-02 08:59"))[a]; ok {
		t.Fatal("deletion due early")
	}
	if r := h.retentionDue(t, london("2026-12-02 09:00"))[a]; r.Notice != "deletion" {
		t.Fatalf("deletion %+v", r)
	}
	// Signing in again starts a new cycle.
	h.owner(t, "UPDATE business.account SET last_active_at = $1 WHERE id = $2::uuid", london("2026-11-30 10:00"), a)
	if _, ok := h.retentionDue(t, london("2026-12-02 09:00"))[a]; ok {
		t.Fatal("still due after the customer came back")
	}
	h.owner(t, "SELECT business.account_set_disabled($1::uuid, true, 'test')", customerB)
	if _, ok := h.retentionDue(t, london("2026-12-02 09:00"))[feb]; ok {
		t.Fatal("a disabled account is due")
	}
	details := h.auditDetails(t, "account.retention_notice_recorded")
	if len(details) != 2 || !strings.Contains(details[0], `"kind" : "warning"`) {
		t.Fatalf("audit %v", details)
	}
	if _, err := h.api.Exec(h.ctx, "SELECT * FROM business.account_retention_due(now(), 10)"); err == nil {
		t.Fatal("gk_api reads retention steps")
	}
}

// waitForLockWait waits until a session of the test database waits on a
// lock (the other connection is blocked).
func (h *harness) waitForLockWait(t *testing.T) {
	t.Helper()
	for range 200 {
		if h.count(t, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`) > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("no session ever waited on a lock")
}

// M1: a checkout that read the account before a deletion committed cannot
// attach its order to the deleted account: its order insert waits for the
// deletion (account_owner_guard) and is then refused; the account never
// owns an order after its deletion. Two connections: the deletion's
// transaction stays open while the checkout runs.
func TestCheckoutRacingADeletionCannotAttachTheOrder(t *testing.T) {
	h := newHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	a := h.resolve(t, customerA)
	q := must[catalog.Result](t)(h.co.CreateQuote(h.ctx, customerA, nil, "q-race", checkout.QuoteInput{Date: "2026-11-14",
		SlotID: f.slot, FulfillmentMode: schedule.Collection, Lines: []checkout.QuoteLine{{VariantID: f.v1000, Quantity: 1}}})).ID
	p := h.deletionPreview(t, customerA)
	in := checkout.DeletionInput{PreviewID: p.PreviewID, PreviewHash: p.PreviewHash}

	conn := h.db.LoginConn(t, "gk_api")
	tx, err := conn.Begin(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(h.ctx)
	var deleted string
	if err := tx.QueryRow(h.ctx, `SELECT result_id::text FROM business.account_delete($1::uuid, 'del', $2, $3::uuid, $4, $5, $6)`,
		customerA, catalog.BodyHash(checkout.CmdAccountDelete, "", in), p.PreviewID, p.PreviewHash, h.now, h.now).Scan(&deleted); err != nil {
		t.Fatalf("deletion in the open transaction: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := h.checkout(customerA, nil, "k-race", q, pol)
		done <- err
	}()
	h.waitForLockWait(t)
	select {
	case err := <-done:
		t.Fatalf("checkout finished while the deletion was open: %v", err)
	default:
	}
	if err := tx.Commit(h.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, catalog.ErrForbidden) {
			t.Fatalf("checkout after the deletion: %v, want refused", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("checkout never finished")
	}
	if n := h.count(t, "SELECT count(*) FROM business.orders WHERE owner_account_id = $1::uuid OR quote_id = $2::uuid", a.AccountID, q); n != 0 {
		t.Fatalf("%d orders attached after the deletion", n)
	}
	// Nothing new can be attached afterwards either, whatever the caller.
	if err := h.ownerErr(t, `UPDATE business.checkout_quote SET owner_account_id = owner_account_id WHERE false`); err != nil {
		t.Fatal(err)
	}
	order, _ := h.guestOrder(t, f, pol, "g-after")
	if err := h.ownerErr(t, "UPDATE business.orders SET owner_account_id = $1::uuid WHERE id = $2::uuid", a.AccountID, order); pgMessage(err) != "account_deleted" {
		t.Fatalf("attach an order to the deleted account directly: %v", err)
	}
}

// m1: the account is locked FOR NO KEY UPDATE, so a customer order command
// that locked the order and then writes a row referencing the account (KEY
// SHARE) is not blocked by a deletion waiting for that order: no deadlock;
// the deletion continues once the command commits.
func TestOrderCommandAndDeletionDoNotDeadlock(t *testing.T) {
	h := newHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	a := h.resolve(t, customerA)
	order := h.signedInOrder(t, f, pol, customerA, "o1")
	// Previews expired more than a day ago are removed when a new one is made.
	for _, age := range []time.Duration{48 * time.Hour, 12 * time.Hour} {
		h.owner(t, `INSERT INTO business.account_deletion_preview (id, account_id, facts, facts_sha256, created_at, expires_at)
			VALUES (gen_random_uuid(), $1::uuid, '{}', repeat('0', 64), $2, $2::timestamptz + interval '15 minutes')`, a.AccountID, h.now.Add(-age))
	}
	p := h.deletionPreview(t, customerA)
	if !p.CanDelete {
		t.Fatalf("preview %+v", p)
	}
	if n := h.count(t, "SELECT count(*) FROM business.account_deletion_preview WHERE account_id = $1::uuid", a.AccountID); n != 2 {
		t.Fatalf("previews kept %d, want the 12-hour-old one and the new one", n)
	}
	cmd := h.db.AdminConn(t)
	tx, err := cmd.Begin(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(h.ctx)
	if _, err := tx.Exec(h.ctx, "SELECT 1 FROM business.orders WHERE id = $1::uuid FOR UPDATE", order); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := h.deleteAccount(customerA, h.now, "del", p)
		done <- err
	}()
	h.waitForLockWait(t) // the deletion holds the account and waits for the order
	ctx, cancel := context.WithTimeout(h.ctx, 5*time.Second)
	defer cancel()
	if _, err := tx.Exec(ctx, `INSERT INTO business.command_receipt (account_id, command, resource, idempotency_key, body_sha256, result_id)
		VALUES ($1::uuid, 'test.order_command', '', 'k', repeat('a', 64), gen_random_uuid())`, a.AccountID); err != nil {
		t.Fatalf("the order command's row referencing the account: %v", err)
	}
	if err := tx.Commit(h.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("deletion: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("deletion never finished")
	}
	if n := h.count(t, "SELECT count(*) FROM business.orders WHERE owner_account_id = $1::uuid", a.AccountID); n != 0 {
		t.Fatalf("orders still linked: %d", n)
	}
}
