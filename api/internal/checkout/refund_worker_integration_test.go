//go:build integration

package checkout_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
)

// GK-025 A3c2: the refund worker end to end (payments.Refunds chained with
// the payment worker) against a fake Stripe refund API, on real PostgreSQL.

// fakeRefunds is Stripe's refund API: idempotent creates by key (another
// body under a used key is an idempotency_error; an amount above what is
// still refundable is an invalid_request_error), retrieve by ID and the
// payment intent's list, newest first, paginated with starting_after.
type fakeRefunds struct {
	byID      map[string]*fakeRefund
	order     []string          // refund IDs, oldest first
	byKey     map[string]string // idempotency key -> refund ID
	forms     map[string]string // idempotency key -> form
	creates   []string          // idempotency key of every create received
	lists     int               // list requests received
	drop      int               // drop this many create responses after creating
	status    int               // answer creates with this status (nothing created)
	newStatus string            // status of created refunds (default succeeded)
	page      int               // force this page size on lists (0: as asked)
	lie       int64             // report this amount in create answers (0: the real one)
	listFail  int               // answer lists with this status (0: list)
}

type fakeRefund struct {
	id, status, pi, failure string
	amount, created         int64
	metadata                map[string]string
}

func newFakeRefunds() fakeRefunds {
	return fakeRefunds{byID: map[string]*fakeRefund{}, byKey: map[string]string{}, forms: map[string]string{}}
}

// serveRefunds answers /v1/refunds (f.mu held).
func (f *fakeStripe) serveRefunds(w http.ResponseWriter, r *http.Request) {
	fr := &f.refunds
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/refunds":
		body, _ := io.ReadAll(r.Body)
		form, key := string(body), r.Header.Get("Idempotency-Key")
		fr.creates = append(fr.creates, key)
		if fr.status != 0 {
			w.WriteHeader(fr.status)
			_, _ = io.WriteString(w, `{"error":{"type":"api_error"}}`)
			return
		}
		id, seen := fr.byKey[key]
		if seen && fr.forms[key] != form {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":{"type":"idempotency_error"}}`)
			return
		}
		if !seen {
			v, err := url.ParseQuery(form)
			amount, aerr := strconv.ParseInt(v.Get("amount"), 10, 64)
			captured := f.captured(v.Get("payment_intent"))
			if err != nil || aerr != nil || amount <= 0 || captured == 0 {
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"No such payment_intent"}}`)
				return
			}
			if amount > captured-f.refunded(v.Get("payment_intent")) {
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","code":"amount_too_large"}}`)
				return
			}
			status := fr.newStatus
			if status == "" {
				status = "succeeded"
			}
			md := map[string]string{}
			for k := range v {
				if strings.HasPrefix(k, "metadata[") {
					md[strings.TrimSuffix(strings.TrimPrefix(k, "metadata["), "]")] = v.Get(k)
				}
			}
			id = f.addRefund(v.Get("payment_intent"), amount, status, md)
			fr.byKey[key], fr.forms[key] = id, form
		}
		if fr.drop > 0 {
			fr.drop--
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		answer := fr.json(fr.byID[id])
		if fr.lie != 0 {
			var m map[string]any
			_ = json.Unmarshal(answer, &m)
			m["amount"] = fr.lie
			answer, _ = json.Marshal(m)
		}
		_, _ = w.Write(answer)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/refunds/"):
		re, ok := fr.byID[strings.TrimPrefix(r.URL.Path, "/v1/refunds/")]
		if !ok {
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error"}}`)
			return
		}
		_, _ = w.Write(fr.json(re))
	case r.Method == http.MethodGet && r.URL.Path == "/v1/refunds":
		fr.lists++
		if fr.listFail != 0 {
			w.WriteHeader(fr.listFail)
			_, _ = io.WriteString(w, `{"error":{"type":"api_error"}}`)
			return
		}
		q := r.URL.Query()
		pi, after := q.Get("payment_intent"), q.Get("starting_after")
		limit, _ := strconv.Atoi(q.Get("limit"))
		if fr.page > 0 {
			limit = fr.page
		}
		var data []json.RawMessage
		started := after == ""
		more := false
		for i := len(fr.order) - 1; i >= 0; i-- {
			re := fr.byID[fr.order[i]]
			if re.pi != pi {
				continue
			}
			if !started {
				started = re.id == after
				continue
			}
			if len(data) == limit {
				more = true
				break
			}
			data = append(data, fr.json(re))
		}
		b, _ := json.Marshal(map[string]any{"object": "list", "url": "/v1/refunds", "has_more": more, "data": data})
		_, _ = w.Write(b)
	default:
		w.WriteHeader(404)
	}
}

func (fr *fakeRefunds) json(re *fakeRefund) []byte {
	out := map[string]any{"id": re.id, "object": "refund", "amount": re.amount, "currency": "gbp", "status": re.status,
		"payment_intent": re.pi, "charge": "ch_fake_" + strings.TrimPrefix(re.pi, "pi_fake_"), "created": re.created,
		"metadata": re.metadata, "reason": "requested_by_customer", "failure_reason": nil}
	if re.failure != "" {
		out["failure_reason"] = re.failure
	}
	b, _ := json.Marshal(out)
	return b
}

// captured is what the payment intent captured (0 when unknown or unpaid).
func (f *fakeStripe) captured(pi string) int64 {
	for _, s := range f.sessions {
		if s.pi != nil && s.pi["id"] == pi && s.pi["status"] == "succeeded" {
			return s.amount
		}
	}
	return 0
}

// refunded is what the payment intent's pending or succeeded refunds take.
func (f *fakeStripe) refunded(pi string) int64 {
	var n int64
	for _, re := range f.refunds.byID {
		if re.pi == pi && (re.status == "succeeded" || re.status == "pending" || re.status == "requires_action") {
			n += re.amount
		}
	}
	return n
}

func (f *fakeStripe) addRefund(pi string, amount int64, status string, md map[string]string) string {
	fr := &f.refunds
	id := fmt.Sprintf("re_fake%d", len(fr.order)+1)
	fr.byID[id] = &fakeRefund{id: id, status: status, pi: pi, amount: amount, created: time.Now().Unix(), metadata: md}
	fr.order = append(fr.order, id)
	return id
}

// dashboardRefund is a refund made outside GK Cakes (no metadata).
func (f *fakeStripe) dashboardRefund(pi string, amount int64, status string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addRefund(pi, amount, status, map[string]string{})
}

func (f *fakeStripe) setRefund(id, status, failure string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refunds.byID[id].status, f.refunds.byID[id].failure = status, failure
}

func (f *fakeStripe) refundCreates() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.refunds.creates...)
}

func (f *fakeStripe) refundCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.refunds.order)
}

func (f *fakeStripe) refundForm(key string) url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, _ := url.ParseQuery(f.refunds.forms[key])
	return v
}

// withRefunds chains the refund worker (after the late capture: its
// dispatch job waits for its first retry, a minute out).
func (h *payHarness) withRefunds() {
	h.handler = jobs.Chain(h.cmds.Handle, h.pay.Handle, h.refunds.Handle, h.resolves.Handle, h.reviews.Handle, h.productions.Handle)
}

// operations is the collection's refund operations, oldest first.
func (h *payHarness) operations(t *testing.T, coll string) []refundRow {
	t.Helper()
	rows, err := h.admin.Query(h.ctx, `SELECT id::text, state, coalesce(provider_refund_id, ''), coalesce(mismatch_reason, ''),
		dispatch_started_at IS NOT NULL FROM business.refund_operation WHERE collection_id = $1::uuid ORDER BY created_at, id`, coll)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []refundRow
	for rows.Next() {
		var r refundRow
		if err := rows.Scan(&r.ID, &r.State, &r.RefundID, &r.Mismatch, &r.Started); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// refundEvent delivers a signed refund event whose object is the fake
// refund id (its metadata names the collection when GK Cakes created it).
func (h *payHarness) refundEvent(t *testing.T, eventID, eventType, refundID string) string {
	t.Helper()
	h.stripe.mu.Lock()
	re := h.stripe.refunds.byID[refundID]
	coll, order := re.metadata["collection_id"], re.metadata["order_id"]
	h.stripe.mu.Unlock()
	return h.deliver(t, eventID, eventType, "refund", refundID, coll, order)
}

func reasons(errs []error) string {
	var b strings.Builder
	for _, err := range errs {
		var ae *jobs.AttentionError
		var de *jobs.DeferError
		switch {
		case errors.As(err, &ae):
			b.WriteString("attention: " + ae.Reason + "\n")
		case errors.As(err, &de):
			b.WriteString("defer: " + err.Error() + "\n")
		default:
			b.WriteString("error: " + err.Error() + "\n")
		}
	}
	return b.String()
}

// Late capture after the release -> one refund of the whole 10000p sent with
// the exact request under the operation's key -> succeeded -> returned; the
// order stays cancelled and nothing more is ever reserved.
func TestLateCaptureIsReturnedThroughStripe(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "e2e")
	r := h.reservedRefund(t, coll)
	h.withRefunds()
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("dispatch:\n%s", reasons(errs))
	}
	creates := h.stripe.refundCreates()
	if len(creates) != 1 || creates[0] != r.ID || h.stripe.refundCount() != 1 {
		t.Fatalf("creates %v", creates)
	}
	form := h.stripe.refundForm(r.ID)
	want := url.Values{"payment_intent": {r.PI}, "amount": {"10000"}, "reason": {"requested_by_customer"},
		"metadata[order_id]": {order}, "metadata[collection_id]": {coll}, "metadata[refund_operation_id]": {r.ID},
		"metadata[environment]": {"test"}}
	if form.Encode() != want.Encode() {
		t.Fatalf("request %s, want %s", form.Encode(), want.Encode())
	}
	ops := h.operations(t, coll)
	if len(ops) != 1 || ops[0].State != "succeeded" || ops[0].RefundID != "re_fake1" || h.recoveryState(t, r.ID) != "secured" {
		t.Fatalf("operations %+v (%s)", ops, h.recoveryState(t, r.ID))
	}
	v, raw := h.view(t, order, d)
	if v.Lifecycle != "cancelled" || v.Refund == nil || v.Refund.State != "returned" || v.Refund.SucceededMinor != 10000 ||
		v.Refund.TargetMinor != 10000 {
		t.Fatalf("view %s", raw)
	}
	// The refund events that follow are recorded once and change nothing.
	h.refundEvent(t, "evt_re_created", "refund.created", "re_fake1")
	if got := h.refundEvent(t, "evt_re_created", "refund.created", "re_fake1"); got != "duplicate" {
		t.Fatalf("second delivery %s", got)
	}
	h.deliver(t, "evt_ch_refunded", "charge.refunded", "charge", "ch_fake_x", coll, order)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("after events:\n%s", reasons(errs))
	}
	if b := h.balance(t, coll); b.Succeeded != 10000 || b.Exposure != 10000 || b.External != 0 || b.Available != 0 || b.Quarantined {
		t.Fatalf("balance %+v", b)
	}
	if h.count(t, "SELECT count(*) FROM business.refund_observation WHERE collection_id = $1::uuid", coll) != 1 ||
		h.count(t, "SELECT count(*) FROM business.refund_observation WHERE collection_id = $1::uuid AND matched_operation_id IS NOT NULL", coll) != 1 ||
		h.count(t, "SELECT business.refund_reserve($1::uuid)", order) != 0 || len(h.stripe.refundCreates()) != 1 {
		t.Fatal("events changed the return")
	}
	if o := h.orderRow(t, order); o.Lifecycle != "cancelled" {
		t.Fatalf("order %+v", o)
	}
	h.noPersonalDataInLogs(t)
}

// A lost create response is retried with the same key and body: one refund.
func TestLostRefundResponseIsRetriedWithTheSameKey(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	_, _, coll := h.lateCapture(t, f, pol, "lost")
	r := h.reservedRefund(t, coll)
	h.withRefunds()
	h.stripe.mu.Lock()
	// Go's transport replays a request carrying an Idempotency-Key once on a
	// dropped reused connection: drop every answer until the retry.
	h.stripe.refunds.drop = 100
	h.stripe.mu.Unlock()
	h.due(t)
	errs := h.drain(t)
	if len(errs) != 1 || len(attention(errs)) != 0 || h.operations(t, coll)[0].State != "unknown" {
		t.Fatalf("lost answer:\n%s %+v %v", reasons(errs), h.operations(t, coll), h.stripe.refundCreates())
	}
	h.stripe.mu.Lock()
	h.stripe.refunds.drop = 0
	h.stripe.mu.Unlock()
	// The dispatch retries first (the collection's reconcile, scheduled for
	// the unanswered call, would find the refund by its metadata as well).
	h.owner(t, "UPDATE business.job SET next_attempt_at = now() WHERE id = $1::uuid", h.refundJob(t, "dispatch", r.ID))
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("retry:\n%s", reasons(errs))
	}
	creates := h.stripe.refundCreates()
	for _, k := range creates {
		if k != r.ID {
			t.Fatalf("another key %s", k)
		}
	}
	if len(creates) < 2 || h.stripe.refundCount() != 1 || len(h.stripe.refunds.forms) != 1 {
		t.Fatalf("creates %v, refunds %d", creates, h.stripe.refundCount())
	}
	if ops := h.operations(t, coll); len(ops) != 1 || ops[0].State != "succeeded" ||
		h.count(t, "SELECT count(*) FROM business.refund_attempt WHERE refund_operation_id = $1::uuid", r.ID) != 2 {
		t.Fatalf("operations %+v", ops)
	}
}

// Unknown past the replay window with no matching refund in Stripe's
// complete list: it stays unknown and goes to a manager; no second POST.
func TestUnknownRefundWithoutAMatchIsNeverResent(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "unk")
	r := h.reservedRefund(t, coll)
	h.withRefunds()
	h.stripe.mu.Lock()
	h.stripe.refunds.status = 503
	h.stripe.refunds.page = 1 // the list is read page by page
	h.stripe.mu.Unlock()
	// Other refunds of other payment intents never confuse the list.
	h.stripe.dashboardRefund("pi_fake_other", 100, "succeeded")
	h.due(t)
	if errs := h.drain(t); len(errs) != 1 || h.operations(t, coll)[0].State != "unknown" {
		t.Fatalf("503:\n%s", reasons(errs))
	}
	h.owner(t, `UPDATE business.refund_operation SET first_dispatch_at = first_dispatch_at - interval '24 hours',
		replay_until = replay_until - interval '24 hours', dispatch_started_at = dispatch_started_at - interval '24 hours'
		WHERE id = $1::uuid`, r.ID)
	h.owner(t, `UPDATE business.recovery_operation SET replay_until = replay_until - interval '24 hours' WHERE id = $1::uuid`, r.ID)
	h.coord.Now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	h.stripe.mu.Lock()
	h.stripe.refunds.status = 0
	h.stripe.mu.Unlock()
	h.due(t)
	errs := h.drain(t)
	got := attention(errs)
	if len(got) == 0 || len(got) != len(errs) {
		t.Fatalf("past the window:\n%s", reasons(errs))
	}
	found := false
	for _, g := range got {
		found = found || strings.Contains(g, "absent from Stripe's complete refund list")
	}
	if !found {
		t.Fatalf("no manager reconciliation:\n%s", reasons(errs))
	}
	if creates := h.stripe.refundCreates(); len(creates) != 1 || h.stripe.refundCount() != 1 {
		t.Fatalf("creates %v", creates)
	}
	if ops := h.operations(t, coll); len(ops) != 1 || ops[0].State != "unknown" {
		t.Fatalf("operations %+v", ops)
	}
	if h.count(t, "SELECT business.refund_reserve($1::uuid)", order) != 0 {
		t.Fatal("an unknown refund was released")
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "needs_attention" || v.Refund.SucceededMinor != 0 {
		t.Fatalf("view %s", raw)
	}
}

// Pending at Stripe -> the refund event brings the reconcile forward ->
// succeeded; duplicate and repeated refund events are counted once.
func TestPendingRefundSucceedsOnItsEvent(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "pend")
	h.withRefunds()
	h.stripe.mu.Lock()
	h.stripe.refunds.newStatus = "pending"
	h.stripe.mu.Unlock()
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 || h.operations(t, coll)[0].State != "pending" {
		t.Fatalf("pending:\n%s %+v", reasons(errs), h.operations(t, coll))
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "pending" || v.Refund.SucceededMinor != 0 {
		t.Fatalf("view %s", raw)
	}
	h.stripe.setRefund("re_fake1", "succeeded", "")
	for range 2 {
		h.refundEvent(t, "evt_re_updated", "refund.updated", "re_fake1")
	}
	h.deliver(t, "evt_ch_refunded", "charge.refunded", "charge", "ch_fake_x", coll, order)
	if n := h.count(t, `SELECT count(*) FROM business.refund_job rj JOIN business.job j ON j.id = rj.job_id
		WHERE rj.collection_id = $1::uuid AND rj.purpose = 'reconcile' AND j.state IN ('queued', 'retry')`, coll); n != 1 {
		t.Fatalf("%d waiting reconciles", n)
	}
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("reconcile:\n%s", reasons(errs))
	}
	if ops := h.operations(t, coll); len(ops) != 1 || ops[0].State != "succeeded" || ops[0].RefundID != "re_fake1" {
		t.Fatalf("operations %+v", ops)
	}
	if b := h.balance(t, coll); b.Succeeded != 10000 || b.Exposure != 10000 || b.Reserved != 0 {
		t.Fatalf("balance %+v", b)
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "returned" {
		t.Fatalf("view %s", raw)
	}
	if h.count(t, "SELECT count(*) FROM business.stripe_event WHERE event_id = 'evt_re_updated'") != 1 ||
		len(h.stripe.refundCreates()) != 1 {
		t.Fatal("counted twice")
	}
}

// A refund Stripe fails reopens the remaining obligation: never re-sent
// automatically; a later decision reserves one new operation (never two
// unresolved) under a new key, which returns the money.
func TestFailedRefundReopensTheRemainder(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "fail")
	first := h.reservedRefund(t, coll)
	h.withRefunds()
	h.stripe.mu.Lock()
	h.stripe.refunds.newStatus = "pending"
	h.stripe.mu.Unlock()
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("pending:\n%s", reasons(errs))
	}
	h.stripe.setRefund("re_fake1", "failed", "lost_or_stolen_card")
	h.refundEvent(t, "evt_re_failed", "refund.failed", "re_fake1")
	errs := h.drain(t)
	if got := attention(errs); len(got) != 1 || !strings.Contains(got[0], "refund failed") {
		t.Fatalf("failed:\n%s", reasons(errs))
	}
	if ops := h.operations(t, coll); len(ops) != 1 || ops[0].State != "failed" {
		t.Fatalf("operations %+v", ops)
	}
	if b := h.balance(t, coll); b.Succeeded != 0 || b.Reserved != 0 || b.Available != 10000 || b.Unresolved {
		t.Fatalf("balance %+v", b)
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "needs_attention" {
		t.Fatalf("view %s", raw)
	}
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 || len(h.stripe.refundCreates()) != 1 {
		t.Fatalf("re-sent automatically:\n%s", reasons(errs))
	}
	// A later decision (a manager, GK-026 tooling) reserves the remainder once.
	if h.count(t, "SELECT business.refund_reserve($1::uuid)", order) != 1 ||
		h.count(t, "SELECT business.refund_reserve($1::uuid)", order) != 0 {
		t.Fatal("reservation after the failure")
	}
	h.stripe.mu.Lock()
	h.stripe.refunds.newStatus = ""
	h.stripe.mu.Unlock()
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("second operation:\n%s", reasons(errs))
	}
	ops := h.operations(t, coll)
	creates := h.stripe.refundCreates()
	if len(ops) != 2 || ops[1].State != "succeeded" || len(creates) != 2 || creates[0] != first.ID || creates[1] != ops[1].ID {
		t.Fatalf("operations %+v creates %v", ops, creates)
	}
	if b := h.balance(t, coll); b.Succeeded != 10000 || b.Available != 0 {
		t.Fatalf("balance %+v", b)
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "returned" {
		t.Fatalf("view %s", raw)
	}
}

// A refund made in the dashboard is quarantined when its event arrives:
// the reserved operation is never sent while it is unresolved, and the
// possible exposure counts both.
func TestDashboardRefundIsQuarantinedAndBlocksDispatch(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "dash")
	r := h.reservedRefund(t, coll)
	h.withRefunds()
	h.stripe.dashboardRefund(r.PI, 5000, "succeeded")
	// charge.refunded carries the payment intent's metadata (copied to its
	// charge): it brings the collection's refund reconcile forward. The
	// dispatch job still waits for its retry.
	h.deliver(t, "evt_dash", "charge.refunded", "charge", "ch_fake_dash", coll, order)
	errs := h.drain(t)
	if got := attention(errs); len(got) != 1 || len(errs) != 1 || !strings.Contains(got[0], "quarantined") {
		t.Fatalf("reconcile:\n%s", reasons(errs))
	}
	h.due(t)
	errs = h.drain(t)
	if got := attention(errs); len(got) != 1 || len(errs) != 1 || !strings.Contains(got[0], "blocked") {
		t.Fatalf("dispatch:\n%s", reasons(errs))
	}
	if creates := h.stripe.refundCreates(); len(creates) != 0 {
		t.Fatalf("sent under quarantine: %v", creates)
	}
	if b := h.balance(t, coll); b.External != 5000 || b.Reserved != 10000 || b.Exposure != 15000 || b.Available != 0 || !b.Quarantined {
		t.Fatalf("balance %+v", b)
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "needs_attention" {
		t.Fatalf("view %s", raw)
	}
	if n := h.count(t, `SELECT count(*) FROM business.refund_observation WHERE collection_id = $1::uuid AND quarantined
		AND matched_operation_id IS NULL AND resolved_at IS NULL AND amount_minor = 5000`, coll); n != 1 {
		t.Fatalf("%d quarantined observations", n)
	}
	// Seen again (the next event): still one observation.
	h.deliver(t, "evt_dash_2", "charge.refund.updated", "charge", "ch_fake_dash", coll, order)
	h.drain(t)
	if n := h.count(t, "SELECT count(*) FROM business.refund_observation WHERE collection_id = $1::uuid", coll); n != 1 {
		t.Fatalf("%d observations", n)
	}
}

// A restore freeze defers the refund dispatch: nothing is fixed or sent
// until it lifts.
func TestRestoreFreezeDefersRefunds(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	_, _, coll := h.lateCapture(t, f, pol, "frz")
	r := h.reservedRefund(t, coll)
	h.withRefunds()
	h.owner(t, "SELECT business.recovery_set_freeze(true, 'restore drill')")
	h.due(t)
	errs := h.drain(t)
	var de *jobs.DeferError
	if len(errs) != 1 || !errors.As(errs[0], &de) || len(h.stripe.refundCreates()) != 0 ||
		h.count(t, "SELECT count(*) FROM business.recovery_operation WHERE id = $1::uuid", r.ID) != 0 ||
		h.count(t, "SELECT count(*) FROM business.refund_operation WHERE id = $1::uuid AND payload IS NULL", r.ID) != 1 {
		t.Fatalf("frozen:\n%s", reasons(errs))
	}
	h.owner(t, "SELECT business.recovery_set_freeze(false, NULL)")
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 || h.operations(t, coll)[0].State != "succeeded" || len(h.stripe.refundCreates()) != 1 {
		t.Fatalf("after the freeze:\n%s", reasons(errs))
	}
}

// A refund Stripe reports succeeded and later failed is never silently
// kept as returned: the contradiction is recorded on the operation, the
// worker asks a manager, nothing becomes available (no automatic payout)
// and the order view needs attention.
func TestSucceededRefundThatFailsLaterNeedsAManager(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "contra")
	r := h.reservedRefund(t, coll)
	h.withRefunds()
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 || h.refundRow(t, r.ID).State != "succeeded" {
		t.Fatalf("dispatch:\n%s", reasons(errs))
	}
	h.refundEvent(t, "evt_re_created", "refund.created", "re_fake1")
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("first sighting:\n%s", reasons(errs))
	}
	h.stripe.setRefund("re_fake1", "failed", "expired_or_canceled_card")
	h.refundEvent(t, "evt_re_updated", "refund.updated", "re_fake1")
	errs := h.drain(t)
	if got := attention(errs); len(got) != 1 || len(errs) != 1 || !strings.Contains(got[0], "decided refund otherwise") {
		t.Fatalf("contradiction:\n%s", reasons(errs))
	}
	if row := h.refundRow(t, r.ID); row.State != "succeeded" || row.Mismatch == "" {
		t.Fatalf("operation %+v", row)
	}
	if b := h.balance(t, coll); b.Available != 0 {
		t.Fatalf("balance %+v", b)
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "needs_attention" {
		t.Fatalf("view %s", raw)
	}
	if h.count(t, "SELECT business.refund_reserve($1::uuid)", order) != 0 || len(h.stripe.refundCreates()) != 1 {
		t.Fatal("paid out automatically after the contradiction")
	}
	// Seen again: still a manager's, recorded once.
	h.deliver(t, "evt_ch_refund_updated", "charge.refund.updated", "charge", "ch_fake_x", coll, order)
	errs = h.drain(t)
	if got := attention(errs); len(got) != 1 || len(errs) != 1 {
		t.Fatalf("seen again:\n%s", reasons(errs))
	}
	if n := h.count(t, "SELECT count(*) FROM business.audit_event WHERE subject_id = $1::uuid AND action = 'refund.mismatch'", order); n != 1 {
		t.Fatalf("%d mismatch records", n)
	}
	h.noPersonalDataInLogs(t)
}

// A refund naming a local operation but with another amount is quarantined
// and linked to that operation (reporting only). It cannot be the result of
// the operation's key, whose admitted request may still create its own
// refund, so the possible exposure keeps both (K05: no clipping); nothing
// is sent while it stands.
func TestRefundNamingAnOperationWithAnotherAmountAddsToExposure(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, _, coll := h.lateCapture(t, f, pol, "named")
	r := h.reservedRefund(t, coll)
	h.withRefunds()
	h.stripe.mu.Lock()
	h.stripe.refunds.status = 503
	h.stripe.mu.Unlock()
	h.due(t)
	if errs := h.drain(t); len(errs) != 1 || h.refundRow(t, r.ID).State != "unknown" {
		t.Fatalf("503:\n%s", reasons(errs))
	}
	h.stripe.mu.Lock()
	id := h.stripe.addRefund(r.PI, 5000, "succeeded", map[string]string{"order_id": order, "collection_id": coll,
		"refund_operation_id": r.ID, "environment": "test"})
	h.stripe.mu.Unlock()
	h.refundEvent(t, "evt_named", "refund.created", id)
	errs := h.drain(t)
	if got := attention(errs); len(got) != 1 || len(errs) != 1 || !strings.Contains(got[0], "quarantined") {
		t.Fatalf("reconcile:\n%s", reasons(errs))
	}
	if n := h.count(t, `SELECT count(*) FROM business.refund_observation WHERE collection_id = $1::uuid AND quarantined
		AND quarantine_reason = 'amount_mismatch' AND named_operation_id = $2::uuid`, coll, r.ID); n != 1 {
		t.Fatalf("%d linked observations", n)
	}
	if b := h.balance(t, coll); b.Succeeded != 5000 || b.Reserved != 10000 || b.External != 5000 || b.Exposure != 15000 ||
		b.Room != 0 || b.Available != 0 || !b.Unresolved || !b.Quarantined {
		t.Fatalf("balance %+v", b)
	}
	if h.count(t, "SELECT business.refund_reserve($1::uuid)", order) != 0 || len(h.stripe.refundCreates()) != 1 {
		t.Fatal("dispatched or reserved under quarantine")
	}
}

// A refund matched to an operation that failed, which Stripe later reports
// succeeded (a contradiction), is real money: the balance counts it by the
// refund's own status (returned), nothing is available and the order view
// needs attention; nothing is paid out again.
func TestRefundOfAFailedOperationThatSucceedsLaterIsCounted(t *testing.T) {
	h := newPayHarness(t)
	f := h.setup(t)
	pol := h.policy(t, "p1")
	order, d, coll := h.lateCapture(t, f, pol, "revive")
	r := h.reservedRefund(t, coll)
	h.withRefunds()
	h.stripe.mu.Lock()
	h.stripe.refunds.newStatus = "pending"
	h.stripe.mu.Unlock()
	h.due(t)
	if errs := h.drain(t); len(errs) != 0 {
		t.Fatalf("pending:\n%s", reasons(errs))
	}
	h.stripe.setRefund("re_fake1", "failed", "lost_or_stolen_card")
	h.refundEvent(t, "evt_re_failed", "refund.failed", "re_fake1")
	if errs := h.drain(t); len(attention(errs)) != 1 || h.refundRow(t, r.ID).State != "failed" {
		t.Fatalf("failed:\n%s", reasons(errs))
	}
	if b := h.balance(t, coll); b.Exposure != 0 || b.Available != 10000 {
		t.Fatalf("after the failure %+v", b)
	}
	h.stripe.setRefund("re_fake1", "succeeded", "")
	h.refundEvent(t, "evt_re_updated", "refund.updated", "re_fake1")
	errs := h.drain(t)
	if got := attention(errs); len(got) != 1 || len(errs) != 1 || !strings.Contains(got[0], "decided refund otherwise") {
		t.Fatalf("contradiction:\n%s", reasons(errs))
	}
	if row := h.refundRow(t, r.ID); row.State != "failed" || row.Mismatch == "" {
		t.Fatalf("operation %+v", row)
	}
	if b := h.balance(t, coll); b.Succeeded != 10000 || b.Exposure != 10000 || b.Room != 0 || b.Available != 0 {
		t.Fatalf("balance %+v", b)
	}
	if v, raw := h.view(t, order, d); v.Refund == nil || v.Refund.State != "needs_attention" || v.Refund.SucceededMinor != 10000 {
		t.Fatalf("view %s", raw)
	}
	if h.count(t, "SELECT business.refund_reserve($1::uuid)", order) != 0 || len(h.stripe.refundCreates()) != 1 {
		t.Fatal("paid out again")
	}
}
