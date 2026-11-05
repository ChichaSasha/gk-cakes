//go:build integration

package checkout_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
)

// GK-025 A3c2: the minimum full-return core (migration 0016): refund goals,
// refund operations dispatched to Stripe under K11, refund reconciliation
// and the quarantine of external refund observations (contracts r6.5 K04,
// K05, K11).

// tenThousandOrder places a guest order for one made-to-order cake priced
// 10000p (the K05 fixture amount) and returns it with its guest digest.
func (h *payHarness) tenThousandOrder(t *testing.T, f fixture, pol, key string) (string, []byte) {
	t.Helper()
	res := must[catalog.Result](t)
	fl := res(h.cat.CreateFlavour(h.ctx, chef, "f-"+key, catalog.FlavourInput{Name: "Dark Chocolate " + key})).ID
	n := res(h.cat.CreateNotice(h.ctx, chef, "n-"+key, catalog.NoticeInput{Body: "Kitchen notice " + key})).ID
	v := res(h.cat.CreateVariant(h.ctx, chef, "v-"+key, f.cake, catalog.VariantInput{FlavourID: fl, SizeCode: "xl", StockMode: catalog.MadeToOrder})).ID
	res(h.cat.CreateRecipeVersion(h.ctx, chef, "r-"+key, catalog.RecipeVersionInput{VariantID: v, ExpectedVersion: 1,
		PriceMinor: 10000, ServingsMin: 20, ServingsMax: 25, Coating: "Swiss meringue buttercream",
		Composition: []string{"Chocolate sponge"}, NoticeID: n, Allergens: []catalog.Allergen{{Code: "eggs", Label: "EGGS"}}}))
	quote, d := h.guestQuote(t, "q-"+key, checkout.QuoteInput{Date: "2026-11-14", SlotID: f.slot,
		FulfillmentMode: schedule.Collection, Lines: []checkout.QuoteLine{{VariantID: v, Quantity: 1}}})
	return must[catalog.Result](t)(h.checkout("", d, "k-"+key, quote, pol)).ID, d
}

// paidCollection pays a fresh 10000p order through the fake Stripe (it is
// confirmed) and returns the order, its succeeded collection and the
// payment intent.
func (h *payHarness) paidCollection(t *testing.T, f fixture, pol, key string) (order, coll, pi string) {
	t.Helper()
	order, d := h.tenThousandOrder(t, f, pol, key)
	coll, session := h.started(t, order, d)
	h.stripe.pay(session, time.Date(2026, 11, 5, 10, 9, 0, 0, time.UTC))
	h.deliver(t, "evt_paid_"+key, "checkout.session.completed", "checkout.session", session, coll, order)
	if errs := h.drain(t); len(errs) != 0 || h.collection(t, coll).State != "succeeded" {
		t.Fatalf("paid: %v %+v", errs, h.collection(t, coll))
	}
	return order, coll, "pi_fake_" + session
}

// lateCapture releases an unpaid 10000p order at its hold's end, then
// records the payment that arrives afterwards: the paid confirmation is
// refused (attention) and the full return is recorded. It returns the
// order, its guest digest and the collection.
func (h *payHarness) lateCapture(t *testing.T, f fixture, pol, key string) (order string, d []byte, coll string) {
	t.Helper()
	order, d = h.tenThousandOrder(t, f, pol, key)
	coll, session := h.started(t, order, d)
	h.age(t, coll)
	h.endHold(t, order)
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 || h.orderRow(t, order).Lifecycle != "cancelled" {
		t.Fatalf("release: %v %+v", errs, h.orderRow(t, order))
	}
	// The reconcile of a cancelled collection checks nothing more (A3c1), so
	// the late paid verdict is recorded directly, as a reconcile that began
	// before the cancellation would.
	h.owner(t, "SELECT business.payment_schedule_reconcile($1::uuid, now())", coll)
	job := h.claimOnly(t, h.reconcileJob(t, coll))
	at := time.Date(2026, 11, 5, 10, 20, 0, 0, time.UTC)
	h.stripe.pay(session, at)
	var state string
	var cmd *string
	if err := h.worker.QueryRow(h.ctx, `SELECT collection_state, command_id::text FROM business.payment_reconcile_record(
		$1::uuid, $2::uuid, $3, $4::uuid, 'paid', $5, 'complete', 'paid', $6, $7::json, $8, NULL)`,
		job.ID, job.Worker, job.Generation, coll, session, "pi_fake_"+session, `{"session_id":"`+session+`"}`, at).Scan(&state, &cmd); err != nil {
		t.Fatal(err)
	}
	h.finish(t, job, nil)
	errs := withoutRefundDispatch(h.drain(t))
	if got := attention(errs); len(got) != 1 || !strings.Contains(got[0], "full return") || len(errs) != 1 {
		t.Fatalf("late capture: %v", errs)
	}
	if c := h.collection(t, coll); c.State != "succeeded" {
		t.Fatalf("collection %+v", c)
	}
	return order, d, coll
}

// reconcileJob is the collection's waiting payment reconcile job.
func (h *payHarness) reconcileJob(t *testing.T, coll string) string {
	t.Helper()
	var id string
	if err := h.admin.QueryRow(h.ctx, `SELECT j.id::text FROM business.payment_job pj JOIN business.job j ON j.id = pj.job_id
		WHERE pj.collection_id = $1::uuid AND pj.purpose = 'reconcile' AND j.state IN ('queued', 'retry')`, coll).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

type balanceRow struct {
	Paid, Succeeded, Reserved, Local, External, Exposure, Room, Available int64
	Unresolved, Quarantined                                               bool
}

func (h *payHarness) balance(t *testing.T, coll string) balanceRow {
	t.Helper()
	var b balanceRow
	if err := h.admin.QueryRow(h.ctx, `SELECT paid_minor, succeeded_minor, reserved_minor, local_minor, external_minor,
		exposure_minor, room_minor, unresolved, quarantined, business.refund_available($1::uuid)
		FROM business.refund_balance($1::uuid)`, coll).Scan(&b.Paid, &b.Succeeded, &b.Reserved, &b.Local, &b.External,
		&b.Exposure, &b.Room, &b.Unresolved, &b.Quarantined, &b.Available); err != nil {
		t.Fatal(err)
	}
	return b
}

// sentOperation inserts, as the owner, a refund operation of amount for the
// collection that was dispatched (body fixed, call admitted) and is in
// state.
func (h *payHarness) sentOperation(t *testing.T, order, coll, goal, pi, state string, amount int64) string {
	t.Helper()
	op := payments.NewID()
	settled := "NULL"
	if state == "succeeded" || state == "failed" {
		settled = "now()"
	}
	refund := "NULL"
	if state == "succeeded" {
		refund = "'re_local_" + strings.ReplaceAll(op, "-", "") + "'"
	}
	h.owner(t, `INSERT INTO business.refund_operation (id, order_id, collection_id, goal_id, goal_version, state, amount_minor,
		environment, provider_account_id, livemode, payment_intent_id, payload, payload_sha256, first_dispatch_at, replay_until,
		dispatch_started_at, provider_refund_id, settled_at)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 1, $5, $6, 'test', $7, false, $8, '{}', repeat('0', 64),
		        now() - interval '1 hour', now() + interval '22 hours', now() - interval '1 hour', `+refund+`, `+settled+`)`,
		op, order, coll, goal, state, amount, stripeAccount, pi)
	return op
}

// K05 fixture (the card's red test, written before migration 0016): a
// 10000p collection, a local refund operation of 10000p whose outcome is
// unknown and an external succeeded return of 5000p seen at Stripe: the
// allocation stays 10000p, the observation 5000p, nothing is available and
// the possible exposure is 15000p; no new operation is reserved and nothing
// is dispatched (no clipping, no second call).
func TestRefundAccountingPreservesPossibleExposure(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, coll, pi := h.paidCollection(t, f, pol, "fx")
	goal := payments.NewID()
	h.owner(t, `INSERT INTO business.refund_goal (id, order_id, target_minor, reason, source)
		VALUES ($1::uuid, $2::uuid, 10000, 'late_capture_terminal', 'system')`, goal, order)
	op := h.sentOperation(t, order, coll, goal, pi, "unknown", 10000)
	h.owner(t, `INSERT INTO business.refund_observation (id, collection_id, provider_account_id, livemode, provider_refund_id,
		amount_minor, currency, status, quarantined, quarantine_reason, source)
		VALUES (gen_random_uuid(), $1::uuid, $2, false, 're_external_dashboard', 5000, 'gbp', 'succeeded', true, 'unmatched', 'reconcile')`,
		coll, stripeAccount)
	jobsBefore := h.count(t, "SELECT count(*) FROM business.job")

	b := h.balance(t, coll)
	want := balanceRow{Paid: 10000, Succeeded: 5000, Reserved: 10000, Local: 10000, External: 5000, Exposure: 15000,
		Room: 0, Available: 0, Unresolved: true, Quarantined: true}
	if b != want {
		t.Fatalf("balance %+v, want %+v", b, want)
	}
	if got := h.count(t, "SELECT business.refund_reserve($1::uuid)", order); got != 0 {
		t.Fatalf("reserved %d operations", got)
	}
	if got := h.count(t, "SELECT business.refund_goal_remaining($1::uuid)", order); got != 0 {
		t.Fatalf("goal remaining %d", got)
	}
	if h.count(t, "SELECT count(*) FROM business.refund_operation WHERE collection_id = $1::uuid", coll) != 1 ||
		h.count(t, "SELECT count(*) FROM business.job") != jobsBefore {
		t.Fatal("a new refund operation or job was created")
	}

	// A definitive failure of the local operation reopens its 10000p, but the
	// unresolved quarantine still blocks every new dispatch.
	h.owner(t, "UPDATE business.refund_operation SET state = 'failed', settled_at = now() WHERE id = $1::uuid", op)
	if b := h.balance(t, coll); b.Room != 5000 || b.Available != 0 || b.Exposure != 5000 || !b.Quarantined || b.Unresolved {
		t.Fatalf("after the failure %+v", b)
	}
	if got := h.count(t, "SELECT business.refund_reserve($1::uuid)", order); got != 0 {
		t.Fatalf("reserved %d operations under quarantine", got)
	}
	// Once a manager resolves the observation, only the remainder net of the
	// external return (5000p) is available: never the whole 10000p again.
	h.owner(t, `UPDATE business.refund_observation SET resolved_at = now(), resolution_note = 'dashboard refund confirmed'
		WHERE collection_id = $1::uuid`, coll)
	if b := h.balance(t, coll); b.Available != 5000 || b.Quarantined {
		t.Fatalf("after the resolution %+v", b)
	}
	if got := h.count(t, "SELECT business.refund_reserve($1::uuid)", order); got != 1 {
		t.Fatalf("reserved %d operations after the resolution", got)
	}
	if got := h.count(t, `SELECT amount_minor FROM business.refund_operation WHERE collection_id = $1::uuid AND state = 'reserved'`, coll); got != 5000 {
		t.Fatalf("reserved %dp", got)
	}
}

// A payment captured after the order was released records, once, a full
// return goal of everything paid and one reserved refund operation with
// its dispatch job; the order stays cancelled and the paid confirmation job
// still asks for a manager.
func TestLateCaptureRecordsTheFullReturnGoal(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, _, coll := h.lateCapture(t, f, pol, "late")

	var goal, reason, source string
	var version int
	var target int64
	if err := h.admin.QueryRow(h.ctx, `SELECT id::text, version, target_minor, reason, source FROM business.refund_goal
		WHERE order_id = $1::uuid`, order).Scan(&goal, &version, &target, &reason, &source); err != nil {
		t.Fatal(err)
	}
	if version != 1 || target != 10000 || reason != "late_capture_terminal" || source != "system" {
		t.Fatalf("goal v%d %dp %s %s", version, target, reason, source)
	}
	var op, state, pi string
	var amount int64
	if err := h.admin.QueryRow(h.ctx, `SELECT id::text, state, amount_minor, coalesce(payment_intent_id, '')
		FROM business.refund_operation WHERE collection_id = $1::uuid AND goal_id = $2::uuid AND goal_version = 1`,
		coll, goal).Scan(&op, &state, &amount, &pi); err != nil {
		t.Fatal(err)
	}
	if state != "reserved" || amount != 10000 || !strings.HasPrefix(pi, "pi_fake_") {
		t.Fatalf("operation %s %dp %q", state, amount, pi)
	}
	if got := h.count(t, `SELECT count(*) FROM business.refund_job rj JOIN business.job j ON j.id = rj.job_id
		WHERE rj.refund_operation_id = $1::uuid AND rj.purpose = 'dispatch' AND j.operation_id = $1::uuid`, op); got != 1 {
		t.Fatalf("%d dispatch jobs", got)
	}
	if o := h.orderRow(t, order); o.Lifecycle != "cancelled" || o.CancelReason != "payment_not_completed" {
		t.Fatalf("order %+v", o)
	}
	if b := h.balance(t, coll); b.Reserved != 10000 || b.Room != 0 || b.Available != 0 || !b.Unresolved {
		t.Fatalf("balance %+v", b)
	}

	// Idempotent: recording it again changes nothing.
	h.owner(t, "SELECT business.refund_late_capture($1::uuid)", order)
	if h.count(t, "SELECT count(*) FROM business.refund_operation WHERE order_id = $1::uuid", order) != 1 ||
		h.count(t, "SELECT version FROM business.refund_goal WHERE order_id = $1::uuid", order) != 1 ||
		h.count(t, "SELECT count(*) FROM business.audit_event WHERE subject_id = $1::uuid AND action = 'refund.goal_recorded'", order) != 1 ||
		h.count(t, "SELECT count(*) FROM business.audit_event WHERE subject_id = $1::uuid AND action = 'refund.reserved'", order) != 1 {
		t.Fatal("recording the late capture again was not idempotent")
	}
}

// At most one unresolved refund operation per collection; a higher goal
// recorded while one is unresolved reserves nothing more; once it is
// definitively failed the whole remaining allocation is reserved once.
func TestOneUnresolvedRefundOperationPerCollection(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, _, coll := h.lateCapture(t, f, pol, "one")
	var goal, first string
	if err := h.admin.QueryRow(h.ctx, `SELECT goal_id::text, id::text FROM business.refund_operation WHERE collection_id = $1::uuid`,
		coll).Scan(&goal, &first); err != nil {
		t.Fatal(err)
	}
	// The database refuses a second unresolved operation outright.
	err := h.ownerErr(t, `INSERT INTO business.refund_operation (id, order_id, collection_id, goal_id, goal_version, amount_minor,
		environment, provider_account_id, livemode) VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid, 1, 1, 'test', $4, false)`,
		order, coll, goal, stripeAccount)
	if dbtest.SQLState(err) != "23505" {
		t.Fatalf("second unresolved operation: %v", err)
	}
	if got := h.count(t, "SELECT business.refund_reserve($1::uuid)", order); got != 0 {
		t.Fatalf("reserved %d while one is unresolved", got)
	}
	// A raised goal while the operation waits: no new operation.
	h.owner(t, "UPDATE business.refund_goal SET target_minor = 10000, version = version + 1 WHERE order_id = $1::uuid", order)
	if got := h.count(t, "SELECT business.refund_reserve($1::uuid)", order); got != 0 {
		t.Fatalf("reserved %d under a raised goal", got)
	}
	// A partial success of 4000p then a definitive failure of the rest:
	// only the remaining 6000p is reserved, once.
	h.owner(t, "UPDATE business.refund_operation SET state = 'cancelled', settled_at = now() WHERE id = $1::uuid", first)
	h.sentOperation(t, order, coll, goal, "pi_fake_x", "succeeded", 4000)
	failed := h.sentOperation(t, order, coll, goal, "pi_fake_x", "failed", 6000)
	if b := h.balance(t, coll); b.Succeeded != 4000 || b.Reserved != 0 || b.Room != 6000 || b.Available != 6000 {
		t.Fatalf("balance %+v", b)
	}
	if got := h.count(t, "SELECT business.refund_reserve($1::uuid)", order); got != 1 {
		t.Fatalf("reserved %d after the failure", got)
	}
	if got := h.count(t, "SELECT business.refund_reserve($1::uuid)", order); got != 0 {
		t.Fatalf("reserved %d again", got)
	}
	var amount int64
	var version int
	if err := h.admin.QueryRow(h.ctx, `SELECT amount_minor, goal_version FROM business.refund_operation
		WHERE collection_id = $1::uuid AND state = 'reserved' AND id <> $2::uuid`, coll, failed).Scan(&amount, &version); err != nil {
		t.Fatal(err)
	}
	if amount != 6000 || version != 2 {
		t.Fatalf("reserved %dp under goal v%d", amount, version)
	}
}

// Q2: a payment that awaits a chef/customer agreement creates no goal.
func TestPaidAwaitingAgreementCreatesNoRefundGoal(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.paidOrder(t, f, pol)
	coll, session := h.started(t, order, d)
	h.stripe.pay(session, time.Date(2026, 11, 6, 12, 30, 1, 0, time.UTC))
	h.deliver(t, "evt_completed", "checkout.session.completed", "checkout.session", session, coll, order)
	if got := attention(h.drain(t)); len(got) != 1 || !strings.Contains(got[0], "paid_awaiting_agreement") {
		t.Fatalf("late payment: %v", got)
	}
	if h.count(t, "SELECT count(*) FROM business.refund_goal WHERE order_id = $1::uuid", order) != 0 ||
		h.count(t, "SELECT count(*) FROM business.refund_operation WHERE order_id = $1::uuid", order) != 0 {
		t.Fatal("a refund was recorded for an order awaiting agreement")
	}
}

// A refused paid confirmation on an order that is not cancelled records no
// full return: a late success of an earlier (expired) collection on an
// order confirmed by a later one goes to a manager only; the confirmed
// collection is never refunded automatically. refund_late_capture itself
// refuses an order that is not cancelled (defence in depth).
func TestRefusalOnAConfirmedOrderRecordsNoFullReturn(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d := h.tenThousandOrder(t, f, pol, "conf")
	first, session1 := h.started(t, order, d)
	h.stripe.session(session1).status = "expired"
	h.deliver(t, "evt_expired_conf", "checkout.session.expired", "checkout.session", session1, first, order)
	if errs := h.drain(t); len(errs) != 0 || h.collection(t, first).State != "cancelled" {
		t.Fatalf("expired: %v %+v", errs, h.collection(t, first))
	}
	res, err := h.start(order, d, "pay-2", 1)
	if err != nil {
		t.Fatal(err)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("second dispatch: %v", errs)
	}
	second := res.ID
	session2 := h.collection(t, second).SessionID
	h.stripe.pay(session2, time.Date(2026, 11, 5, 10, 9, 0, 0, time.UTC))
	h.deliver(t, "evt_paid_conf", "checkout.session.completed", "checkout.session", session2, second, order)
	if errs := h.drain(t); len(errs) != 0 || h.orderRow(t, order).Lifecycle != "confirmed" {
		t.Fatalf("confirmed: %v %+v", errs, h.orderRow(t, order))
	}
	// The expired session's payment succeeds after all (recorded late).
	at := time.Date(2026, 11, 5, 10, 20, 0, 0, time.UTC)
	ev := `{"session_id":"` + session1 + `","payment_status":"paid","charge_status":"succeeded"}`
	if got := h.recordDirect(t, first, session1, "paid", ev, &at, nil); got != "succeeded" {
		t.Fatalf("late success recorded as %s", got)
	}
	h.withRefunds()
	errs := h.drain(t)
	if got := attention(errs); len(got) != 1 || len(errs) != 1 || !strings.Contains(got[0], "manager or chef decides") {
		t.Fatalf("refused confirmation: %v", errs)
	}
	if o := h.orderRow(t, order); o.Lifecycle != "confirmed" {
		t.Fatalf("order %+v", o)
	}
	if h.count(t, "SELECT count(*) FROM business.refund_goal WHERE order_id = $1::uuid", order) != 0 ||
		h.count(t, "SELECT count(*) FROM business.refund_operation WHERE order_id = $1::uuid", order) != 0 ||
		len(h.stripe.refundCreates()) != 0 {
		t.Fatal("a full return was recorded for a confirmed order")
	}
	h.owner(t, "SELECT business.refund_late_capture($1::uuid)", order)
	if h.count(t, "SELECT count(*) FROM business.refund_goal WHERE order_id = $1::uuid", order) != 0 {
		t.Fatal("refund_late_capture recorded a goal for a confirmed order")
	}
	if v, raw := h.view(t, order, d); v.Refund != nil {
		t.Fatalf("view %s", raw)
	}
}
