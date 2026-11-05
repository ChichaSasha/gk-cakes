//go:build integration

package notifications_test

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/notifications"
)

func noticeTo(t *testing.T, to string) notifications.Notice {
	t.Helper()
	msg := message()
	msg.To = to
	n, err := notifications.NewNotice("test", nextUUID(), nextUUID(), "order-confirmed.v1", msg)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Outside production, EMAIL_RECIPIENT_ALLOWLIST suppresses every other
// recipient: the provider is never called, the intent is suspended with a
// fixed reason, audited without personal data, and its job completes.
func TestAllowlistSuppressesOtherRecipients(t *testing.T) {
	h := newHarness(t)
	h.allow = notifications.Allowlist{"@gkcake.com", "tester@example.org"}
	cases := []struct {
		to   string
		sent bool
	}{
		{recipient, false},
		{"Chef@GKCake.com", true},
		{"tester@example.org", true},
		{"a.tester@example.org", false},
		{"x@sub.gkcake.com", false},
		{"x@gkcake.com.evil.example", false},
	}
	sends := 0
	for _, c := range cases {
		n := noticeTo(t, c.to)
		h.enqueue(n)
		h.runOnce(1)
		row := h.intent(n.IntentID)
		var reason *string
		if err := h.admin.QueryRow(h.ctx, "SELECT suspended_reason FROM business.notification_intent WHERE id = $1::uuid", n.IntentID).Scan(&reason); err != nil {
			t.Fatal(err)
		}
		if j := h.job(row.JobID); j.State != "completed" {
			t.Errorf("%s: job %+v, want completed", c.to, j)
		}
		if c.sent {
			sends++
			if row.State != "accepted" || reason != nil {
				t.Errorf("%s: intent %+v (suspended %v), want accepted", c.to, row, reason)
			}
		} else if row.State != "queued" || len(row.Attempts) != 0 || reason == nil || !strings.Contains(*reason, "EMAIL_RECIPIENT_ALLOWLIST") {
			t.Errorf("%s: intent %+v (suspended %v), want queued, never attempted, suspended for the allowlist", c.to, row, reason)
		}
		if got, _ := h.provider.counts(); got != sends {
			t.Fatalf("%s: provider sends %d, want %d", c.to, got, sends)
		}
	}
	var details []string
	rows, err := h.admin.Query(h.ctx, "SELECT detail::text FROM business.audit_event WHERE action = 'notification.suppressed' ORDER BY occurred_at")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		details = append(details, d)
	}
	if len(details) != 4 || details[0] != `{"intent_state" : "queued", "result" : "suppressed"}` {
		t.Fatalf("suppression audit %v", details)
	}
	for _, s := range []string{"example", "gkcake", subject} {
		if strings.Contains(strings.Join(details, " "), s) || strings.Contains(h.logs.String(), s) {
			t.Errorf("%q in the audit or the logs", s)
		}
	}
}

// An intent already dispatched under an earlier configuration is suspended
// too, but a manager must know that an earlier attempt may have been sent.
func TestAllowlistAfterAnEarlierAttempt(t *testing.T) {
	h := newHarness(t)
	n := newNotice(t, "test")
	h.enqueue(n)
	h.provider.queue("lose")
	h.runOnce(1)
	if row := h.intent(n.IntentID); row.State != "unknown" {
		t.Fatalf("after the lost response %+v", row)
	}
	h.allow = notifications.Allowlist{"@gkcake.com"}
	row := h.intent(n.IntentID)
	h.due(row.JobID)
	h.runOnce(2)
	j := h.job(row.JobID)
	if j.State != "attention" || j.Reason == nil || !strings.Contains(*j.Reason, "EMAIL_RECIPIENT_ALLOWLIST") {
		t.Fatalf("job %+v, want attention for a suppression after an attempt", j)
	}
	if _, reqs := h.provider.counts(); reqs != 1 {
		t.Fatalf("provider requests %d, want only the first", reqs)
	}
	// The lease fences the gateway like the others.
	_, err := h.db.LoginConn(t, "gk_worker").Exec(h.ctx, "SELECT business.notification_dispatch_suppress($1::uuid, $2::uuid, 1)",
		row.JobID, workerUUID(3))
	if dbtest.SQLState(err) != "GK001" {
		t.Fatalf("suppress without the lease: %v, want GK001", err)
	}
}

// Production refuses an allowlist (real customers are never filtered).
func TestAllowlistIsRefusedInProduction(t *testing.T) {
	h := newHarness(t)
	coord := *h.coord
	coord.Environment = "production"
	_, err := notifications.NewDispatcher(notifications.DispatcherConfig{DB: h.pool, Coordinator: &coord, Sender: &notifications.Resend{},
		Environment: "production", Allowlist: notifications.Allowlist{"@gkcake.com"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err == nil || !strings.Contains(err.Error(), "production") {
		t.Fatalf("production dispatcher with an allowlist: %v", err)
	}
	if !(notifications.Allowlist{}).Allows(recipient) || (notifications.Allowlist{"@gkcake.com"}).Allows("a@b@gkcake.com") {
		t.Fatal("Allows")
	}
}
