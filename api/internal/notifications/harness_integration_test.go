//go:build integration

package notifications_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/internal/notifications"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	apiKey        = "re_test_0123456789"
	recipient     = "ms.example@example.org"
	subject       = "Your cake order GK-1042 is confirmed"
	bodyText      = "Dear Ms Example, your order for collection on Saturday is confirmed."
	echoedMessage = "Invalid `to` field: ms.example@example.org"
)

// fakeResend models the Resend send endpoint, including idempotency: a key
// seen before with the same body returns the original message without a
// second send; with a different body it is rejected (409
// invalid_idempotent_request). Each request takes the next scripted
// behaviour (default "ok").
type fakeResend struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	script   []string
	byKey    map[string]sentEmail
	sends    int
	requests []providerRequest
	arrived  chan struct{}
}

type sentEmail struct {
	id   string
	body []byte
}

type providerRequest struct {
	key  string
	body []byte
}

func newFakeResend(t *testing.T) *fakeResend {
	f := &fakeResend{t: t, byKey: map[string]sentEmail{}, arrived: make(chan struct{}, 100)}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeResend) queue(behaviours ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script = append(f.script, behaviours...)
}

// process applies Resend's idempotency rules; it returns the message ID, or
// "" for a body mismatch.
func (f *fakeResend) process(key string, body []byte) string {
	if prev, ok := f.byKey[key]; ok {
		if !bytes.Equal(prev.body, body) {
			return ""
		}
		return prev.id
	}
	f.sends++
	id := fmt.Sprintf("re-msg-%d", f.sends)
	f.byKey[key] = sentEmail{id: id, body: body}
	return id
}

func (f *fakeResend) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if r.Method != http.MethodPost || r.URL.Path != "/emails" || r.Header.Get("Authorization") != "Bearer "+apiKey ||
		r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, `{"name":"not_found"}`, http.StatusNotFound)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	f.mu.Lock()
	f.requests = append(f.requests, providerRequest{key: key, body: body})
	behaviour := "ok"
	if len(f.script) > 0 {
		behaviour, f.script = f.script[0], f.script[1:]
	}
	var id string
	if behaviour == "ok" || behaviour == "lose" || behaviour == "hang" {
		id = f.process(key, body)
	}
	f.mu.Unlock()
	f.arrived <- struct{}{}

	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case (behaviour == "ok" || behaviour == "lose" || behaviour == "hang") && id == "":
		reply(http.StatusConflict, map[string]any{"statusCode": 409, "name": "invalid_idempotent_request", "message": "body changed"})
	case behaviour == "ok":
		reply(http.StatusOK, map[string]string{"id": id})
	case behaviour == "lose": // sent, but the response never arrives
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	case behaviour == "hang": // sent; the response is held until the caller gives up
		<-r.Context().Done()
	case behaviour == "reject":
		reply(http.StatusUnprocessableEntity, map[string]any{"statusCode": 422, "name": "validation_error", "message": echoedMessage})
	case behaviour == "refuse":
		reply(http.StatusUnauthorized, map[string]any{"statusCode": 401, "name": "validation_error", "message": "API key is invalid"})
	case behaviour == "ratelimit":
		reply(http.StatusTooManyRequests, map[string]any{"statusCode": 429, "name": "rate_limit_exceeded", "message": "slow down"})
	case behaviour == "fail500":
		reply(http.StatusInternalServerError, map[string]any{"statusCode": 500, "name": "application_error"})
	default:
		f.t.Errorf("unknown behaviour %q", behaviour)
	}
}

func (f *fakeResend) counts() (sends, requests int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sends, len(f.requests)
}

func (f *fakeResend) allRequests() []providerRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]providerRequest(nil), f.requests...)
}

// syncBuffer collects worker logs.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type harness struct {
	t        *testing.T
	ctx      context.Context
	db       *dbtest.DB
	owner    *pgx.Conn // migrator; SET LOCAL ROLE gk_owner per transaction
	admin    *pgx.Conn // superuser observer
	pool     *pgxpool.Pool
	provider *fakeResend
	journal  *recovery.MemoryJournal
	coord    *recovery.Coordinator
	logs     *syncBuffer

	clockMu sync.Mutex
	skew    time.Duration
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	db := dbtest.New(t)
	owner := db.MigratorConn(t)
	if _, err := migrate.Run(ctx, owner, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := &harness{t: t, ctx: ctx, db: db, owner: owner, admin: db.AdminConn(t), pool: workerPool(t, db),
		provider: newFakeResend(t), journal: recovery.NewMemoryJournal(), logs: &syncBuffer{}}
	sealer, err := recovery.NewSealer("k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	h.coord = &recovery.Coordinator{Journal: h.journal, Sealer: sealer, Environment: "test", Now: h.now}
	return h
}

func workerPool(t *testing.T, db *dbtest.DB) *pgxpool.Pool {
	t.Helper()
	cfg, err := config.LoadWorker(func(k string) string {
		return map[string]string{"APP_ENV": "test", "DATABASE_URL": db.LoginURL(t, "gk_worker")}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.OpenPool(context.Background(), cfg.DatabaseURL, "gk-worker-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func (h *harness) now() time.Time {
	h.clockMu.Lock()
	defer h.clockMu.Unlock()
	return time.Now().Add(h.skew)
}

// advance moves the coordinator's clock (the provider window check).
func (h *harness) advance(d time.Duration) {
	h.clockMu.Lock()
	defer h.clockMu.Unlock()
	h.skew += d
}

func (h *harness) logger() *slog.Logger { return slog.New(slog.NewJSONHandler(h.logs, nil)) }

func (h *harness) dispatcher() *notifications.Dispatcher {
	h.t.Helper()
	client, err := notifications.NewResend(notifications.ResendConfig{APIKey: apiKey, BaseURL: h.provider.srv.URL})
	if err != nil {
		h.t.Fatal(err)
	}
	d, err := notifications.NewDispatcher(notifications.DispatcherConfig{DB: h.pool, Coordinator: h.coord, Sender: client,
		Environment: "test", Logger: h.logger()})
	if err != nil {
		h.t.Fatal(err)
	}
	return d
}

func workerUUID(n int) string { return fmt.Sprintf("99999999-0000-4000-8000-%012d", n) }

func (h *harness) worker(n int, opts jobs.Options) *jobs.Worker {
	opts.Handler = h.dispatcher().Handle
	opts.Logger = h.logger()
	opts.WorkerID = workerUUID(n)
	if opts.ShutdownTimeout == 0 {
		opts.ShutdownTimeout = time.Second
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = 20 * time.Millisecond
	}
	return jobs.New(h.pool, opts)
}

func (h *harness) runOnce(n int) {
	h.t.Helper()
	ok, err := h.worker(n, jobs.Options{}).RunOnce(h.ctx)
	if err != nil || !ok {
		h.t.Fatalf("RunOnce: processed %v, err %v; want one job processed", ok, err)
	}
}

var seq struct {
	sync.Mutex
	n int
}

func nextUUID() string {
	seq.Lock()
	defer seq.Unlock()
	seq.n++
	return fmt.Sprintf("11111111-0000-4000-8000-%012d", seq.n)
}

func message() notifications.Message {
	return notifications.Message{To: recipient, Subject: subject, Text: bodyText}
}

func newNotice(t *testing.T, env string) notifications.Notice {
	t.Helper()
	n, err := notifications.NewNotice(env, nextUUID(), nextUUID(), "order-confirmed.v1", message())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// ownerTx runs fn in a transaction as gk_owner, as a domain gateway function
// runs, and commits it.
func (h *harness) ownerTx(fn func(tx pgx.Tx) error) error {
	tx, err := h.owner.Begin(h.ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(h.ctx)
	if _, err := tx.Exec(h.ctx, "SET LOCAL ROLE gk_owner"); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(h.ctx)
}

func (h *harness) enqueue(n notifications.Notice) {
	h.t.Helper()
	if err := h.ownerTx(func(tx pgx.Tx) error { return notifications.Enqueue(h.ctx, tx, n, "system", "") }); err != nil {
		h.t.Fatalf("enqueue: %v", err)
	}
}

func (h *harness) owned(sql string, args ...any) {
	h.t.Helper()
	if err := h.ownerTx(func(tx pgx.Tx) error { _, err := tx.Exec(h.ctx, sql, args...); return err }); err != nil {
		h.t.Fatalf("%s: %v", sql, err)
	}
}

type intentRow struct {
	State             string
	ProviderMessageID *string
	FirstDispatch     *time.Time
	JobID             string
	Payload           []byte
	Attempts          []string
}

func (h *harness) intent(id string) intentRow {
	h.t.Helper()
	var (
		r       intentRow
		payload string
	)
	err := h.admin.QueryRow(h.ctx, `SELECT state, provider_message_id, first_dispatch_at, job_id::text, payload::text,
		coalesce((SELECT array_agg(result ORDER BY attempt_no) FROM business.notification_attempt a WHERE a.intent_id = i.id), '{}')
		FROM business.notification_intent i WHERE id = $1::uuid`, id).
		Scan(&r.State, &r.ProviderMessageID, &r.FirstDispatch, &r.JobID, &payload, &r.Attempts)
	if err != nil {
		h.t.Fatal(err)
	}
	r.Payload = []byte(payload)
	return r
}

type jobRow struct {
	State      string
	Failures   int
	Reason     *string
	Generation int64
	Next       time.Duration
}

func (h *harness) job(id string) jobRow {
	h.t.Helper()
	var r jobRow
	err := h.admin.QueryRow(h.ctx, `SELECT state, failures, attention_reason, generation, next_attempt_at - now()
		FROM business.job WHERE id = $1::uuid`, id).Scan(&r.State, &r.Failures, &r.Reason, &r.Generation, &r.Next)
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

// due makes a scheduled retry claimable now.
func (h *harness) due(jobID string) {
	h.t.Helper()
	if _, err := h.admin.Exec(h.ctx, "UPDATE business.job SET next_attempt_at = now() WHERE id = $1::uuid AND state = 'retry'", jobID); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) recoveryState(opID string) string {
	h.t.Helper()
	var s string
	if err := h.admin.QueryRow(h.ctx, "SELECT state FROM business.recovery_operation WHERE id = $1::uuid", opID).Scan(&s); err != nil {
		h.t.Fatal(err)
	}
	return s
}

func (h *harness) count(sql string, args ...any) int {
	h.t.Helper()
	var n int
	if err := h.admin.QueryRow(h.ctx, sql, args...).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

func storedRequest(t *testing.T, n notifications.Notice, payload []byte) []byte {
	t.Helper()
	d, err := notifications.ParseDispatch(payload, "test", n.IntentID, n.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	return d.Request
}

// sameKeyAndBody checks that every provider request carried the intent's
// key and the stored request bytes.
func sameKeyAndBody(t *testing.T, reqs []providerRequest, key string, body []byte) {
	t.Helper()
	for i, r := range reqs {
		if r.key != key || !bytes.Equal(r.body, body) {
			t.Errorf("provider request %d: key %q, body %s; want key %q and the stored body %s", i, r.key, r.body, key, body)
		}
	}
}

func lines(s string) []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewBufferString(s))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}
