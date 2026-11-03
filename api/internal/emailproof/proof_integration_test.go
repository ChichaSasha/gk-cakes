//go:build integration

package emailproof_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/emailproof"
	"github.com/ChichaSasha/gk-cakes/api/internal/httpapi"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/internal/notifications"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

var webhookSecret = "whsec_" + base64.StdEncoding.EncodeToString([]byte("gk-proof-webhook-signing-key-32!"))

// syncBuffer collects the API and worker logs, as the environment's logs.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) Since(context.Context, time.Time) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Split(strings.TrimSpace(b.buf.String()), "\n"), nil
}

// sandbox is a local stand-in for staging: real PostgreSQL, the worker with
// the real dispatcher, the API with the real webhook, and a fake Resend that
// answers sends and then posts signed events the way Resend does
// (delivered@ -> sent + delivered, bounced@ -> sent + bounced).
type sandbox struct {
	t        *testing.T
	ctx      context.Context
	owner    string // migrator URL
	api      *httptest.Server
	logs     *syncBuffer
	leak     bool // the fake API logs the recipient (negative test)
	bounceAs string
}

func newSandbox(t *testing.T, withWebhook bool) (*sandbox, *dbtest.DB) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	db := dbtest.New(t)
	if _, err := migrate.Run(ctx, db.MigratorConn(t), migrations.FS); err != nil {
		t.Fatal(err)
	}
	s := &sandbox{t: t, ctx: ctx, logs: &syncBuffer{}, bounceAs: "email.bounced"}
	logger := slog.New(slog.NewJSONHandler(s.logs, nil))
	pool := func(group, app string) *pgxpool.Pool {
		p, err := database.OpenPool(ctx, config.NewSecret(db.LoginURL(t, group)), app)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		return p
	}
	apiPool := pool("gk_api", "gk-api-proof")
	opts := httpapi.Options{UIOrigin: "https://gkcake.com", DB: apiPool, Logger: logger}
	if withWebhook {
		w, err := notifications.NewWebhook(notifications.WebhookConfig{DB: apiPool, Secret: webhookSecret, Environment: "test", Logger: logger})
		if err != nil {
			t.Fatal(err)
		}
		opts.EmailWebhook = w
	}
	s.api = httptest.NewServer(httpapi.NewHandler(opts))
	t.Cleanup(s.api.Close)

	resend := httptest.NewServer(http.HandlerFunc(s.resend))
	t.Cleanup(resend.Close)
	client, err := notifications.NewResend(notifications.ResendConfig{APIKey: "re_proof", BaseURL: resend.URL})
	if err != nil {
		t.Fatal(err)
	}
	sealer, err := recovery.NewSealer("k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	d, err := notifications.NewDispatcher(notifications.DispatcherConfig{DB: pool("gk_worker", "gk-worker-proof"),
		Coordinator: &recovery.Coordinator{Journal: recovery.NewMemoryJournal(), Sealer: sealer, Environment: "test"},
		Sender:      client, Environment: "test", Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	w := jobs.New(pool("gk_worker", "gk-worker-proof"), jobs.Options{Handler: d.Handle, Logger: logger, ShutdownTimeout: time.Second, PollInterval: 20 * time.Millisecond})
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return s, db
}

var msgSeq struct {
	sync.Mutex
	n int
}

func (s *sandbox) resend(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		To   []string `json:"to"`
		Tags []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"tags"`
	}
	_ = json.Unmarshal(body, &req)
	msgSeq.Lock()
	msgSeq.n++
	id := fmt.Sprintf("re-proof-%d", msgSeq.n)
	msgSeq.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"id":"` + id + `"}`))
	tags := map[string]string{}
	for _, t := range req.Tags {
		tags[t.Name] = t.Value
	}
	if s.leak {
		fmt.Fprintf(s.logs, `{"msg":"debug","to":%q}`+"\n", req.To[0])
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.post("email.sent", id, req.To[0], tags)
		switch {
		case strings.HasPrefix(req.To[0], "delivered+"):
			s.post("email.delivered", id, req.To[0], tags)
		case strings.HasPrefix(req.To[0], "bounced+"):
			s.post(s.bounceAs, id, req.To[0], tags)
		default:
			s.post("email.delivered", id, req.To[0], tags)
		}
	}()
}

func (s *sandbox) post(typ, id, to string, tags map[string]string) {
	payload, _ := json.Marshal(map[string]any{"type": typ, "created_at": time.Now().UTC().Format(time.RFC3339Nano),
		"data": map[string]any{"email_id": id, "to": []string{to}, "subject": "proof", "tags": tags,
			"bounce": map[string]string{"type": "Permanent", "subType": "General", "message": to + " unknown"}}})
	key, _ := notifications.ParseWebhookSecret(webhookSecret)
	svixID := fmt.Sprintf("msg_%s_%d", strings.ReplaceAll(id, "-", ""), time.Now().UnixNano())
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(svixID + "." + ts + "."))
	mac.Write(payload)
	req, _ := http.NewRequestWithContext(s.ctx, http.MethodPost, s.api.URL+"/webhooks/email", bytes.NewReader(payload))
	req.Header.Set("svix-id", svixID)
	req.Header.Set("svix-timestamp", ts)
	req.Header.Set("svix-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}
}

func (s *sandbox) run(t *testing.T, db *dbtest.DB, extra string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := emailproof.Run(s.ctx, emailproof.Config{DB: db.MigratorConn(t), Environment: "test", APIURL: s.api.URL,
		HTTP: s.api.Client(), Logs: s.logs, Label: "run42", ExtraRecipient: extra, Wait: 15 * time.Second,
		Poll: 100 * time.Millisecond, Out: &out})
	return out.String(), err
}

func TestProofPassesOnAWorkingEnvironment(t *testing.T) {
	s, db := newSandbox(t, true)
	report, err := s.run(t, db, "gk.cake.london@example.org")
	if err != nil {
		t.Fatalf("proof failed: %v\n%s", err, report)
	}
	for _, want := range []string{"**delivered_to_server**", "**bounced**", "Result: **pass**", "| unsigned | 401 | 401 |",
		"| signed with another secret | 401 | 401 |", "| stale timestamp | 401 | 401 |", "| oversized | 413 | 413 |",
		"Refused probes stored: 0.", "email.bounced:matched:bounced"} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}
	for _, pii := range []string{"run42@resend.dev", "gk.cake.london@example.org", "GK Cakes sandbox proof"} {
		if strings.Contains(report, pii) {
			t.Errorf("report contains %q", pii)
		}
	}
}

func TestProofFailsWithoutTheWebhook(t *testing.T) {
	s, db := newSandbox(t, false)
	if _, err := s.run(t, db, ""); err == nil || !strings.Contains(err.Error(), "EMAIL_WEBHOOK_SECRET is not set") {
		t.Fatalf("err = %v, want the missing webhook reported", err)
	}
}

func TestProofFailsOnPersonalDataInLogs(t *testing.T) {
	s, db := newSandbox(t, true)
	s.leak = true
	report, err := s.run(t, db, "")
	if err == nil || !strings.Contains(report, "contain a proof recipient or the subject") {
		t.Fatalf("err = %v, want the leak reported:\n%s", err, report)
	}
}

func TestProofFailsWhenEvidenceIsMissing(t *testing.T) {
	s, db := newSandbox(t, true)
	s.bounceAs = "email.delivery_delayed" // the bounce never arrives
	report, err := s.run(t, db, "")
	if err == nil || !strings.Contains(report, `bounced: intent is "accepted"`) {
		t.Fatalf("err = %v, want the missing bounce reported:\n%s", err, report)
	}
}
