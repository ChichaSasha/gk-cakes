//go:build integration

package notifications_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/ChichaSasha/gk-cakes/api/internal/httpapi"
	"github.com/ChichaSasha/gk-cakes/api/internal/notifications"
	"github.com/jackc/pgx/v5"
)

var webhookSecret = "whsec_" + base64.StdEncoding.EncodeToString([]byte("gk-test-webhook-signing-key-32b!"))

func (h *harness) webhook() *notifications.Webhook {
	h.t.Helper()
	pool, err := database.OpenPool(h.ctx, config.NewSecret(h.db.LoginURL(h.t, "gk_api")), "gk-api-test")
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(pool.Close)
	w, err := notifications.NewWebhook(notifications.WebhookConfig{DB: pool, Secret: webhookSecret, Environment: "test", Logger: h.logger()})
	if err != nil {
		h.t.Fatal(err)
	}
	return w
}

var svixSeq struct {
	sync.Mutex
	n int
}

func nextSvixID() string {
	svixSeq.Lock()
	defer svixSeq.Unlock()
	svixSeq.n++
	return fmt.Sprintf("msg_test%06d", svixSeq.n)
}

func signed(t *testing.T, id string, body string) http.Header {
	t.Helper()
	key, err := notifications.ParseWebhookSecret(webhookSecret)
	if err != nil {
		t.Fatal(err)
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "." + body))
	h := http.Header{}
	h.Set("svix-id", id)
	h.Set("svix-timestamp", ts)
	h.Set("svix-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return h
}

// event is a Resend email event as delivered, including personal data the
// inbox must not keep.
func event(typ, messageID, intentID, env, extra string) string {
	return fmt.Sprintf(`{"type":%q,"created_at":"2026-11-03T12:00:00.000Z","data":{"created_at":"2026-11-03T11:59:59.000Z",`+
		`"email_id":%q,"from":"GK Cakes <orders@gkcake.com>","to":[%q],"subject":%q,"tags":{"intent":%q,"env":%q}%s}}`,
		typ, messageID, recipient, subject, intentID, env, extra)
}

func (h *harness) deliver(w *notifications.Webhook, id, body string) string {
	h.t.Helper()
	got, err := w.Receive(h.ctx, signed(h.t, id, body), []byte(body))
	if err != nil {
		h.t.Fatalf("Receive: %v", err)
	}
	return got
}

type eventRow struct {
	Type        string
	Correlation string
	Intent      *string
	Applied     *string
	Detail      *string
	Hash        string
}

func (h *harness) events() []eventRow {
	h.t.Helper()
	rows, err := h.admin.Query(h.ctx, `SELECT event_type, correlation, intent_id::text, applied_state, detail::text, payload_hash
		FROM business.email_event ORDER BY received_at, id`)
	if err != nil {
		h.t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (eventRow, error) {
		var e eventRow
		return e, r.Scan(&e.Type, &e.Correlation, &e.Intent, &e.Applied, &e.Detail, &e.Hash)
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return out
}

// sent enqueues a notice and has the worker send it (provider message re-msg-N).
func (h *harness) sent() (notifications.Notice, string) {
	h.t.Helper()
	n := newNotice(h.t, "test")
	h.enqueue(n)
	h.runOnce(1)
	row := h.intent(n.IntentID)
	if row.State != "accepted" || row.ProviderMessageID == nil {
		h.t.Fatalf("intent %+v, want accepted", row)
	}
	return n, *row.ProviderMessageID
}

// Signed evidence is stored, deduplicated by svix-id and applied before the
// acknowledgment; the inbox keeps no personal data.
func TestDeliveredEvidenceIsStoredOnceBeforeTheAcknowledgment(t *testing.T) {
	h := newHarness(t)
	w := h.webhook()
	n, msg := h.sent()
	id, body := nextSvixID(), event("email.delivered", msg, n.IntentID, "test", "")
	if got := h.deliver(w, id, body); got != notifications.WebhookRecorded {
		t.Fatalf("first delivery: %q", got)
	}
	if got := h.intent(n.IntentID).State; got != "delivered_to_server" {
		t.Fatalf("intent %s, want delivered_to_server", got)
	}
	if got := h.deliver(w, id, body); got != notifications.WebhookDuplicate {
		t.Fatalf("retry with the same svix-id: %q, want duplicate", got)
	}
	ev := h.events()
	sum := sha256.Sum256([]byte(body))
	if len(ev) != 1 || ev[0].Correlation != "matched" || ev[0].Applied == nil || *ev[0].Applied != "delivered_to_server" ||
		ev[0].Hash != hex.EncodeToString(sum[:]) || ev[0].Detail != nil {
		t.Fatalf("events %+v, want one matched, applied event with the payload hash", ev)
	}
	var stored string
	if err := h.admin.QueryRow(h.ctx, "SELECT row_to_json(e)::text FROM business.email_event e").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	for _, pii := range []string{recipient, subject, "orders@gkcake.com"} {
		if strings.Contains(stored, pii) {
			t.Errorf("stored event contains %q: %s", pii, stored)
		}
	}
	for _, pii := range []string{recipient, subject} {
		if strings.Contains(h.logs.String(), pii) {
			t.Errorf("logs contain %q", pii)
		}
	}
}

// The HTTP endpoint acknowledges only verified, stored deliveries.
func TestWebhookEndpointRefusesUnsignedAndTamperedDeliveries(t *testing.T) {
	h := newHarness(t)
	n, msg := h.sent()
	srv := httptest.NewServer(httpapi.NewHandler(httpapi.Options{UIOrigin: "https://gkcake.com", DB: h.pool, Logger: h.logger(), EmailWebhook: h.webhook()}))
	defer srv.Close()
	post := func(hdr http.Header, body string) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/email", strings.NewReader(body))
		for k, v := range hdr {
			req.Header[k] = v
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	body := event("email.bounced", msg, n.IntentID, "test", `,"bounce":{"type":"Permanent","subType":"General"}`)
	id := nextSvixID()
	for name, tc := range map[string]struct {
		hdr  http.Header
		body string
	}{
		"unsigned":       {http.Header{}, body},
		"tampered":       {signed(t, id, body), strings.Replace(body, "Permanent", "Transient", 1)},
		"another secret": {signedWith(t, "whsec_"+base64.StdEncoding.EncodeToString([]byte("attacker-chosen-signing-key!!")), id, body), body},
	} {
		if got := post(tc.hdr, tc.body); got != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, got)
		}
	}
	if got := len(h.events()); got != 0 || h.intent(n.IntentID).State != "accepted" {
		t.Fatalf("refused deliveries left %d events, intent %s", got, h.intent(n.IntentID).State)
	}
	if got := post(signed(t, id, body), body); got != http.StatusOK {
		t.Fatalf("signed delivery: status %d, want 200", got)
	}
	if got := h.intent(n.IntentID).State; got != "bounced" {
		t.Fatalf("intent %s, want bounced", got)
	}
	if ev := h.events(); len(ev) != 1 || ev[0].Detail == nil || *ev[0].Detail != `{"bounce_sub_type":"General","bounce_type":"Permanent"}` {
		t.Fatalf("events %+v, want the bounce categories only", ev)
	}
}

func signedWith(t *testing.T, secret, id, body string) http.Header {
	t.Helper()
	key, err := notifications.ParseWebhookSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "." + body))
	h := http.Header{}
	h.Set("svix-id", id)
	h.Set("svix-timestamp", ts)
	h.Set("svix-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return h
}

// AC2: after a lost acceptance response, provider evidence (matched by the
// intent tag) records the acceptance and delivery truthfully; the worker's
// replay and a later window expiry never regress it.
func TestLateAcceptanceEvidenceAfterALostResponse(t *testing.T) {
	h := newHarness(t)
	w := h.webhook()
	n := newNotice(t, "test")
	h.enqueue(n)
	h.provider.queue("lose")
	h.runOnce(1)
	if got := h.intent(n.IntentID); got.State != "unknown" || got.ProviderMessageID != nil {
		t.Fatalf("after the lost response: %+v", got)
	}
	// A delay notice names the message without proving acceptance.
	h.deliver(w, nextSvixID(), event("email.delivery_delayed", "re-msg-1", n.IntentID, "test", ""))
	if got := h.intent(n.IntentID); got.State != "unknown" || got.ProviderMessageID == nil || *got.ProviderMessageID != "re-msg-1" {
		t.Fatalf("after email.delivery_delayed: %+v, want unknown with the message recorded", got)
	}
	h.deliver(w, nextSvixID(), event("email.sent", "re-msg-1", "", "test", "")) // matched by message ID alone
	row := h.intent(n.IntentID)
	if row.State != "accepted" || row.ProviderMessageID == nil || *row.ProviderMessageID != "re-msg-1" {
		t.Fatalf("after email.sent: %+v, want accepted as re-msg-1", row)
	}
	h.deliver(w, nextSvixID(), event("email.delivered", "re-msg-1", n.IntentID, "test", ""))
	// The replay window ends before the worker learns the outcome itself.
	h.advance(23*time.Hour + time.Minute)
	h.due(row.JobID)
	h.runOnce(1)
	if got := h.intent(n.IntentID).State; got != "delivered_to_server" {
		t.Fatalf("after reconciliation: intent %s, want the delivery evidence kept", got)
	}
	if _, reqs := h.provider.counts(); reqs != 1 {
		t.Fatalf("provider requests %d, want 1", reqs)
	}
}

// The worker's later acceptance never overwrites earlier delivery evidence.
func TestWorkerAcceptanceAfterDeliveryEvidenceKeepsTheDelivery(t *testing.T) {
	h := newHarness(t)
	w := h.webhook()
	n := newNotice(t, "test")
	h.enqueue(n)
	h.provider.queue("lose")
	h.runOnce(1)
	h.deliver(w, nextSvixID(), event("email.delivered", "re-msg-1", n.IntentID, "test", ""))
	h.due(h.intent(n.IntentID).JobID)
	h.runOnce(1) // replay: Resend returns the original message
	row := h.intent(n.IntentID)
	if row.State != "delivered_to_server" || *row.ProviderMessageID != "re-msg-1" || strings.Join(row.Attempts, ",") != "unknown,accepted" {
		t.Fatalf("intent %+v, want delivered_to_server kept after the worker's acceptance", row)
	}
	if got := h.recoveryState(n.IdempotencyKey); got != "secured" {
		t.Fatalf("recovery %s, want secured", got)
	}
}

// Adverse facts win and are never overwritten by later, milder ones; every
// event is retained.
func TestAdverseEvidenceIsNeverOverwritten(t *testing.T) {
	h := newHarness(t)
	w := h.webhook()
	a, msgA := h.sent()
	h.deliver(w, nextSvixID(), event("email.bounced", msgA, a.IntentID, "test", `,"bounce":{"type":"Permanent","subType":"General"}`))
	h.deliver(w, nextSvixID(), event("email.delivered", msgA, a.IntentID, "test", ""))
	if got := h.intent(a.IntentID).State; got != "bounced" {
		t.Fatalf("bounce then delivery: %s, want bounced", got)
	}
	b, msgB := h.sent()
	h.deliver(w, nextSvixID(), event("email.delivered", msgB, b.IntentID, "test", ""))
	h.deliver(w, nextSvixID(), event("email.delivery_delayed", msgB, b.IntentID, "test", ""))
	h.deliver(w, nextSvixID(), event("email.bounced", msgB, b.IntentID, "test", `,"bounce":{"type":"Permanent","subType":"Suppressed"}`))
	if got := h.intent(b.IntentID).State; got != "bounced" {
		t.Fatalf("delivery then bounce: %s, want bounced", got)
	}
	c, msgC := h.sent()
	h.deliver(w, nextSvixID(), event("email.failed", msgC, c.IntentID, "test", `,"failed":{"reason":"reached_daily_quota"}`))
	h.deliver(w, nextSvixID(), event("email.sent", msgC, c.IntentID, "test", ""))
	h.deliver(w, nextSvixID(), event("email.bounced", msgC, c.IntentID, "test", "")) // equally adverse: the first stands
	if got := h.intent(c.IntentID).State; got != "failed" {
		t.Fatalf("failure then sent and bounced: %s, want failed", got)
	}
	ev := h.events()
	var applied []string
	for _, e := range ev {
		if e.Applied != nil {
			applied = append(applied, *e.Applied)
		} else {
			applied = append(applied, "-")
		}
	}
	if len(ev) != 8 || strings.Join(applied, ",") != "bounced,-,delivered_to_server,-,bounced,failed,-,-" {
		t.Fatalf("applied states %v of %d events, want every event kept", applied, len(ev))
	}
	// Each applied bounce or failure is audited once; the overridden ones are not.
	for intent, want := range map[string]string{a.IntentID: "bounced", b.IntentID: "bounced", c.IntentID: "failed"} {
		var states []string
		rows, err := h.admin.Query(h.ctx, `SELECT detail->>'state' FROM business.audit_event
			WHERE action = 'notification.adverse_evidence' AND subject_id = $1::uuid`, intent)
		if err != nil {
			t.Fatal(err)
		}
		if states, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || len(states) != 1 || states[0] != want {
			t.Errorf("adverse audits for %s = %v (%v), want one %q", intent, states, err, want)
		}
	}
}

// Events that cannot be correlated safely are kept and change nothing.
func TestUnmatchedAndConflictingEventsChangeNothing(t *testing.T) {
	h := newHarness(t)
	w := h.webhook()
	a, msgA := h.sent()
	b, _ := h.sent()
	h.deliver(w, nextSvixID(), event("email.delivered", "re-unknown", nextUUID(), "test", "")) // unknown intent and message
	h.deliver(w, nextSvixID(), event("email.bounced", "re-other", a.IntentID, "test", ""))     // tag A, another message
	h.deliver(w, nextSvixID(), event("email.bounced", msgA, b.IntentID, "test", ""))           // message A, tag B
	h.deliver(w, nextSvixID(), event("email.delivered", "", "", "test", ""))                   // no references
	q := newNotice(t, "test")
	h.enqueue(q)
	h.deliver(w, nextSvixID(), event("email.delivered", "re-early", q.IntentID, "test", "")) // never dispatched
	var got []string
	for _, e := range h.events() {
		got = append(got, e.Correlation)
		if e.Applied != nil {
			t.Errorf("event %+v changed an intent", e)
		}
	}
	if !slices.Equal(got, []string{"unmatched", "conflict", "conflict", "unmatched", "matched"}) {
		t.Fatalf("correlations %v", got)
	}
	if h.intent(a.IntentID).State != "accepted" || h.intent(b.IntentID).State != "accepted" {
		t.Fatal("a conflicting event changed an intent")
	}
	if row := h.intent(q.IntentID); row.State != "queued" || row.ProviderMessageID != nil {
		t.Fatalf("queued intent %+v changed by evidence", row)
	}
}

// Concurrent retries of one delivery store it once.
func TestConcurrentRetriesOfOneDeliveryAreStoredOnce(t *testing.T) {
	h := newHarness(t)
	w := h.webhook()
	n, msg := h.sent()
	id, body := nextSvixID(), event("email.delivered", msg, n.IntentID, "test", "")
	results := make(chan string, 6)
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := w.Receive(h.ctx, signed(t, id, body), []byte(body))
			if err != nil {
				t.Error(err)
			}
			results <- got
		}()
	}
	wg.Wait()
	close(results)
	counts := map[string]int{}
	for r := range results {
		counts[r]++
	}
	if counts[notifications.WebhookRecorded] != 1 || counts[notifications.WebhookDuplicate] != 5 || len(h.events()) != 1 {
		t.Fatalf("results %v, events %d; want one recorded and five duplicates", counts, len(h.events()))
	}
}

// The API reaches the inbox only through email_event_record; no runtime role
// reads it, and the worker cannot record events.
func TestOnlyTheAPIRecordsEvents(t *testing.T) {
	h := newHarness(t)
	for _, group := range []string{"gk_api", "gk_worker", "gk_backup"} {
		rows, err := h.admin.Query(h.ctx, `SELECT replace(p.oid::regprocedure::text, ',', ', ') FROM pg_proc p
			JOIN pg_namespace ns ON ns.oid = p.pronamespace
			WHERE ns.nspname = 'business' AND p.proname LIKE 'email%' AND has_function_privilege($1, p.oid, 'EXECUTE')`, group)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		want := []string{}
		if group == "gk_api" {
			want = []string{"business.email_event_record(text, text, timestamp with time zone, text, uuid, json, text)"}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s can execute %v, want %v", group, got, want)
		}
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			var can bool
			if err := h.admin.QueryRow(h.ctx, "SELECT has_table_privilege($1, 'business.email_event', $2)", group, priv).Scan(&can); err != nil {
				t.Fatal(err)
			}
			if want := group == "gk_backup" && priv == "SELECT"; can != want {
				t.Errorf("%s %s on business.email_event = %v, want %v", group, priv, can, want)
			}
		}
	}
	rows, err := h.admin.Query(h.ctx, `SELECT replace(p.oid::regprocedure::text, ',', ', ') FROM pg_proc p
		JOIN pg_namespace ns ON ns.oid = p.pronamespace
		WHERE ns.nspname = 'business' AND has_function_privilege('gk_api', p.oid, 'EXECUTE')`)
	if err != nil {
		t.Fatal(err)
	}
	all, err := pgx.CollectRows(rows, pgx.RowTo[string])
	wantAPI := []string{ // GK-021 identity, GK-022 catalog, GK-023 schedule and lots, GK-025 codes, quotes, orders
		// Customer accounts (0033): my orders, profile, claims, deletion.
		"business.account_claim_get(uuid, uuid)",
		"business.account_claim_list(uuid)",
		"business.account_delete(uuid, text, text, uuid, text, timestamp with time zone, timestamp with time zone)",
		"business.account_deletion_preview_create(uuid, timestamp with time zone)",
		"business.account_order_page(uuid, timestamp with time zone, uuid, integer)",
		"business.account_profile_get(uuid)",
		"business.account_profile_update(uuid, text, text, integer, boolean, text, boolean, text, boolean, text)",
		"business.capacity_day_limit_set(uuid, text, text, date, integer, integer)",
		"business.catalog_cake_create(uuid, text, text, text, text)",
		"business.catalog_cake_create(uuid, text, text, text, text, uuid)",
		"business.catalog_cake_set_state(uuid, text, text, uuid, integer, text)",
		"business.catalog_cake_update(uuid, text, text, uuid, integer, text, text)",
		"business.catalog_cake_update(uuid, text, text, uuid, integer, text, text, boolean, uuid)",
		// GK-028 colours (0028).
		"business.catalog_colour_create(uuid, text, text, text, text, integer)",
		"business.catalog_colour_update(uuid, text, text, uuid, integer, text, integer, boolean)",
		"business.catalog_flavour_create(uuid, text, text, text)",
		"business.catalog_notice_create(uuid, text, text, text)",
		"business.catalog_public_cake(uuid)",
		"business.catalog_public_cake_colours(uuid[])",
		"business.catalog_public_cake_page(timestamp with time zone, uuid, integer)",
		"business.catalog_public_variants(uuid[])",
		"business.catalog_recipe_version_create(uuid, text, text, uuid, integer, bigint, integer, integer, text, text[], uuid, json, json, text)",
		"business.catalog_staff_cake(uuid, uuid)",
		"business.catalog_staff_cake_colours(uuid, uuid[])",
		"business.catalog_staff_cake_page(uuid, timestamp with time zone, uuid, integer)",
		"business.catalog_staff_colours(uuid, uuid)",
		"business.catalog_staff_flavours(uuid)",
		"business.catalog_staff_notice_current(uuid)",
		"business.catalog_staff_recipe_version(uuid, uuid)",
		"business.catalog_staff_variants(uuid, uuid[])",
		"business.catalog_variant_archive(uuid, text, text, uuid, integer)",
		"business.catalog_variant_create(uuid, text, text, uuid, uuid, text, text)",
		"business.checkout_create(uuid, bytea, text, text, uuid, integer, uuid, text, text, text, timestamp with time zone)",
		// GK delivery payment (0035): the customer's separate delivery collection.
		"business.delivery_payment_initiate(uuid, bytea, uuid, text, text, integer, uuid, uuid, text, text, text, timestamp with time zone)",
		"business.discount_code_archive(uuid, text, text, uuid, integer)",
		"business.discount_code_create(uuid, text, text, text, text, integer, timestamp with time zone, integer, integer)",
		"business.discount_code_revise(uuid, text, text, uuid, integer, text, integer, timestamp with time zone, integer, integer)",
		"business.discount_staff_list(uuid, uuid)",
		"business.email_event_record(text, text, timestamp with time zone, text, uuid, json, text)",
		"business.identity_resolve(uuid)",
		"business.identity_resolve_account(uuid)",
		"business.identity_staff_list(uuid)",
		"business.lot_create(uuid, text, text, uuid, text, integer, bigint, bigint, timestamp with time zone, timestamp with time zone)",
		"business.lot_public_list(timestamp with time zone)",
		"business.lot_set_state(uuid, text, text, uuid, integer, text)",
		"business.lot_staff_list(uuid, boolean, uuid)",
		"business.manual_payout_list(uuid, uuid)",
		"business.manual_payout_outcome_request(uuid, uuid, text, text, integer, uuid, text, text, text, bigint, text, text, timestamp with time zone)",
		"business.manual_payout_preview(uuid, uuid, uuid, text, text, timestamp with time zone)",
		"business.manual_payout_request(uuid, uuid, text, text, integer, uuid, text, text, timestamp with time zone)",
		// Order notices (0030): the API records the UI origin of order links.
		"business.notice_settings_record(text, text, text[])",
		// Appointment changes (0031): the chef's amendment, the customer's
		// same-day move and its schedule choices.
		"business.order_appointment_change_request(uuid, uuid, text, text, integer, date, uuid, text, timestamp with time zone, text, text, text, timestamp with time zone)",
		"business.order_cancel_request(uuid, bytea, uuid, text, text, integer, uuid, text, text, timestamp with time zone)",
		"business.order_cancellation_preview(uuid, bytea, uuid, timestamp with time zone)",
		"business.order_chef_cancel_request(uuid, uuid, text, text, integer, text, text, timestamp with time zone)",
		"business.order_claim_decide(uuid, text, text, uuid, integer, text, uuid, text, timestamp with time zone)",
		"business.order_claim_staff_list(uuid, text, timestamp with time zone, uuid, integer)",
		"business.order_claim_submit(uuid, text, text, text, text, text, text, bytea, timestamp with time zone)",
		"business.order_claim_withdraw(uuid, text, text, uuid, integer, timestamp with time zone)",
		// Q5 choice (0036): the customer's choice and the chef's "GK can't deliver".
		"business.order_delivery_choice_request(uuid, bytea, uuid, text, text, integer, text, uuid, text, timestamp with time zone)",
		"business.order_delivery_not_possible(uuid, uuid, text, text, integer, text, timestamp with time zone)",
		// GK delivery quote and free-delivery waiver (0035, chef).
		"business.order_delivery_quote(uuid, uuid, text, text, integer, bigint, text, timestamp with time zone)",
		"business.order_delivery_waiver(uuid, uuid, text, text, integer, integer, text, timestamp with time zone)",
		"business.order_free_confirm_request(uuid, bytea, uuid, text, text, integer, text, timestamp with time zone)",
		"business.order_fulfillment_transition(uuid, uuid, text, text, integer, text, text, timestamp with time zone)",
		"business.order_get(uuid, uuid, bytea)",
		"business.order_handover_request(uuid, uuid, text, text, integer, text, text, timestamp with time zone)",
		// Late-payment agreement (0034): the customer's answer and the chef's offer.
		"business.order_late_payment_answer(uuid, bytea, uuid, text, text, integer, uuid, text, text, timestamp with time zone)",
		"business.order_late_payment_offer(uuid, uuid, text, text, integer, text, date, uuid, timestamp with time zone)",
		"business.order_late_payment_request(uuid, uuid, text, text, integer, text, text, timestamp with time zone, text, text, timestamp with time zone)",
		"business.order_owner_assign(uuid, text, text, uuid, integer, uuid, uuid, text, timestamp with time zone)",
		"business.order_pickup_move_request(uuid, bytea, uuid, text, text, integer, uuid, text, timestamp with time zone)",
		"business.order_production_start(uuid, uuid, text, text, integer, timestamp with time zone)",
		"business.order_staff_get(uuid, uuid)",
		"business.order_staff_list(uuid, date, date, text[], boolean, timestamp with time zone, timestamp with time zone, uuid, integer)",
		"business.payment_event_record(text, text, boolean, text, text, timestamp with time zone, text, text, uuid, text, text)",
		"business.payment_initiate(uuid, bytea, uuid, text, text, integer, uuid, text, text, text, timestamp with time zone)",
		"business.payment_mismatch_clear(uuid, text, text, uuid, integer, uuid, text)",
		"business.policy_current(timestamp with time zone)",
		"business.policy_record(uuid, text, text, text, text, text, timestamp with time zone, text)",
		"business.quote_create(uuid, bytea, text, text, date, uuid, text, json, text, timestamp with time zone)",
		"business.quote_get(uuid, uuid, bytea)",
		"business.refund_attention_list(uuid)",
		"business.refund_delivery_failure_record(uuid, text, text, uuid, uuid)",
		"business.refund_delivery_pay(uuid, text, text, uuid, integer, uuid)",
		"business.refund_exception_record(uuid, text, text, uuid, integer, bigint, text, uuid[], bigint[], text)",
		"business.refund_mismatch_clear(uuid, text, text, uuid, integer, uuid, text, text)",
		"business.refund_observation_resolve(uuid, text, text, uuid, integer, uuid, text, uuid, text)",
		"business.refund_operation_decide(uuid, text, text, uuid, integer, uuid, text, text, text)",
		"business.refund_remainder_reserve(uuid, text, text, uuid, integer, uuid, text)",
		"business.schedule_move_slots(uuid, bytea, uuid, timestamp with time zone)",
		"business.schedule_public_days(date, date, timestamp with time zone, integer)",
		"business.schedule_public_slots(date, text, timestamp with time zone, integer)",
		"business.schedule_staff_days(uuid, date, date, timestamp with time zone)",
		"business.schedule_staff_version(uuid, uuid)",
		"business.schedule_version_create(uuid, text, text, integer, integer, date, json, date[])",
	}
	slices.Sort(all)
	if err != nil || !slices.Equal(all, wantAPI) {
		t.Fatalf("gk_api can execute %v (%v), want exactly %v", all, err, wantAPI)
	}
	conn := h.db.LoginConn(t, "gk_api")
	for _, sql := range []string{"SELECT count(*) FROM business.email_event", "SELECT count(*) FROM business.notification_intent"} {
		if _, err := conn.Exec(h.ctx, sql); err == nil || !strings.Contains(err.Error(), "42501") {
			t.Errorf("gk_api: %s: %v, want 42501", sql, err)
		}
	}
}

// A complaint proves delivery to the server, never a bounce; a suppression is
// a definitive failure. Adverse evidence is audited without personal data.
func TestComplaintAndSuppressionEvidence(t *testing.T) {
	h := newHarness(t)
	w := h.webhook()
	a, msgA := h.sent()
	h.deliver(w, nextSvixID(), event("email.complained", msgA, a.IntentID, "test", ""))
	if got := h.intent(a.IntentID).State; got != "delivered_to_server" {
		t.Fatalf("after a complaint: %s, want delivered_to_server", got)
	}
	b, msgB := h.sent()
	h.deliver(w, nextSvixID(), event("email.suppressed", msgB, b.IntentID, "test",
		`,"suppressed":{"message":"`+recipient+` is on the account suppression list","type":"OnAccountSuppressionList"}`))
	if got := h.intent(b.IntentID).State; got != "failed" {
		t.Fatalf("after a suppression: %s, want failed", got)
	}
	ev := h.events()
	if len(ev) != 2 || ev[1].Detail == nil || *ev[1].Detail != `{"suppressed_type":"OnAccountSuppressionList"}` {
		t.Fatalf("events %+v, want the suppression category only", ev)
	}
	var audits []string
	rows, err := h.admin.Query(h.ctx, `SELECT subject_id::text || ' ' || detail::text FROM business.audit_event
		WHERE action = 'notification.adverse_evidence' ORDER BY occurred_at`)
	if err != nil {
		t.Fatal(err)
	}
	audits, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	want := b.IntentID + ` {"state" : "failed", "event_type" : "email.suppressed"}`
	if len(audits) != 1 || audits[0] != want {
		t.Fatalf("adverse evidence audits %q, want only %q", audits, want)
	}
}

// Two different events for one email can be applied concurrently: a bounce
// committed while a sent event waits for the intent row is never overwritten.
func TestConcurrentEventsForOneEmailNeverRegressEvidence(t *testing.T) {
	h := newHarness(t)
	w := h.webhook()
	n := newNotice(t, "test")
	h.enqueue(n)
	h.provider.queue("lose")
	h.runOnce(1)
	// Another delivery's transaction, on its own connection (the observer
	// connection must stay outside it to see current activity).
	tx, err := h.db.AdminConn(t).Begin(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(h.ctx)
	if _, err := tx.Exec(h.ctx, "UPDATE business.notification_intent SET state = 'bounced', provider_message_id = 're-msg-1' WHERE id = $1::uuid", n.IntentID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	body := event("email.sent", "re-msg-1", n.IntentID, "test", "")
	go func() {
		_, err := w.Receive(h.ctx, signed(t, nextSvixID(), body), []byte(body))
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for h.count("SELECT count(*) FROM pg_stat_activity WHERE application_name = 'gk-api-test' AND wait_event_type = 'Lock'") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the webhook transaction never waited for the intent row")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := tx.Commit(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := h.intent(n.IntentID).State; got != "bounced" {
		t.Fatalf("intent %s, want the bounce kept", got)
	}
	if ev := h.events(); len(ev) != 1 || ev[0].Applied != nil || ev[0].Correlation != "matched" {
		t.Fatalf("events %+v, want the sent event kept without being applied", ev)
	}
}

// An event naming another message for an intent whose message ID is being
// recorded concurrently is a conflict; the recorded message is never replaced.
func TestConcurrentEvidenceNeverReplacesARecordedMessage(t *testing.T) {
	h := newHarness(t)
	w := h.webhook()
	n := newNotice(t, "test")
	h.enqueue(n)
	h.provider.queue("lose")
	h.runOnce(1)
	tx, err := h.db.AdminConn(t).Begin(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(h.ctx)
	if _, err := tx.Exec(h.ctx, "UPDATE business.notification_intent SET provider_message_id = 're-msg-a' WHERE id = $1::uuid", n.IntentID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	body := event("email.delivery_delayed", "re-msg-b", n.IntentID, "test", "")
	go func() {
		_, err := w.Receive(h.ctx, signed(t, nextSvixID(), body), []byte(body))
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for h.count("SELECT count(*) FROM pg_stat_activity WHERE application_name = 'gk-api-test' AND wait_event_type = 'Lock'") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the webhook transaction never waited for the intent row")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := tx.Commit(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if row := h.intent(n.IntentID); row.ProviderMessageID == nil || *row.ProviderMessageID != "re-msg-a" || row.State != "unknown" {
		t.Fatalf("intent %+v, want re-msg-a kept and still unknown", row)
	}
	if ev := h.events(); len(ev) != 1 || ev[0].Correlation != "conflict" {
		t.Fatalf("events %+v, want one conflict", ev)
	}
}
