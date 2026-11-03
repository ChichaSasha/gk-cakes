//go:build integration

package recovery_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
	"github.com/jackc/pgx/v5"
)

var errCrash = errors.New("injected crash")

// fakeProvider models a provider with Resend-style idempotency keys: a repeat
// of the same key within the window returns the original message without a
// second send.
type fakeProvider struct {
	mu     sync.Mutex
	window time.Duration
	byKey  map[string]string
	first  map[string]time.Time
	sends  int
}

func newProvider() *fakeProvider {
	return &fakeProvider{window: 24 * time.Hour, byKey: map[string]string{}, first: map[string]time.Time{}}
}

func (p *fakeProvider) send(key string, now time.Time) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id, ok := p.byKey[key]; ok && now.Before(p.first[key].Add(p.window)) {
		return id
	}
	p.sends++
	id := fmt.Sprintf("msg-%d", p.sends)
	p.byKey[key], p.first[key] = id, now
	return id
}

func (p *fakeProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sends
}

// crashStore wraps a Store; an entry in after makes that method perform its
// database change and then report a crash, one in before crashes first.
type crashStore struct {
	recovery.Store
	before, after map[string]bool
}

func (c *crashStore) hit(m map[string]bool, name string) bool {
	if m[name] {
		delete(m, name)
		return true
	}
	return false
}

func (c *crashStore) MarkPrepared(ctx context.Context, op recovery.Operation) (recovery.State, error) {
	if c.hit(c.before, "prepared") {
		return "", errCrash
	}
	s, err := c.Store.MarkPrepared(ctx, op)
	if err == nil && c.hit(c.after, "prepared") {
		return "", errCrash
	}
	return s, err
}

func (c *crashStore) MarkCommitted(ctx context.Context, op recovery.Operation, o recovery.Outcome) (recovery.State, error) {
	if c.hit(c.before, "committed") {
		return "", errCrash
	}
	s, err := c.Store.MarkCommitted(ctx, op, o)
	if err == nil && c.hit(c.after, "committed") {
		return "", errCrash
	}
	return s, err
}

func (c *crashStore) MarkSecured(ctx context.Context, op recovery.Operation) (recovery.State, error) {
	if c.hit(c.before, "secured") {
		return "", errCrash
	}
	return c.Store.MarkSecured(ctx, op)
}

type harness struct {
	t        *testing.T
	owner    *pgx.Conn
	admin    *pgx.Conn
	journal  *recovery.MemoryJournal
	coord    *recovery.Coordinator
	provider *fakeProvider
	now      time.Time
}

func ownerConn(t *testing.T, db *dbtest.DB) *pgx.Conn {
	t.Helper()
	conn := db.MigratorConn(t)
	if _, err := migrate.Run(context.Background(), conn, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := conn.Exec(context.Background(), "SET ROLE gk_owner"); err != nil {
		t.Fatal(err)
	}
	return conn
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db := dbtest.New(t)
	h := &harness{t: t, owner: ownerConn(t, db), admin: db.AdminConn(t), journal: recovery.NewMemoryJournal(),
		provider: newProvider(), now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	sealer, err := recovery.NewSealer("k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	h.coord = &recovery.Coordinator{Journal: h.journal, Sealer: sealer, Environment: "test", Now: func() time.Time { return h.now }}
	return h
}

func (h *harness) store() recovery.SQLStore { return recovery.SQLStore{DB: h.owner} }

func uuidAt(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }

const secretMarker = "Ms Example, 1 Private Road"

func (h *harness) dispatchOp(n int) recovery.Operation {
	payload, _ := json.Marshal(map[string]string{
		"provider": "resend", "environment": "test", "idempotency_key": uuidAt(n), "body_hash": strings.Repeat("a", 64),
		"recipient": secretMarker,
	})
	return recovery.Operation{ID: uuidAt(n), Kind: recovery.KindDispatch, AggregateID: uuidAt(1000 + n), Payload: payload,
		ReplayUntil: h.now.Add(23 * time.Hour)}
}

// effect sends through the fake provider with the operation ID as the
// idempotency key; loseResponse makes it send and then report a timeout.
func (h *harness) effect(op recovery.Operation, loseResponse *bool) recovery.Effect {
	return func(context.Context) (recovery.Outcome, error) {
		id := h.provider.send(op.ID, h.now)
		if loseResponse != nil && *loseResponse {
			*loseResponse = false
			return recovery.Outcome{}, errors.New("timeout waiting for provider response")
		}
		ev, _ := json.Marshal(map[string]string{"provider_id": id})
		return recovery.Outcome{Status: "accepted", Evidence: ev}, nil
	}
}

func (h *harness) state(id string) string {
	h.t.Helper()
	var s string
	if err := h.admin.QueryRow(context.Background(), "SELECT state FROM business.recovery_operation WHERE id = $1::uuid", id).Scan(&s); err != nil {
		h.t.Fatal(err)
	}
	return s
}

func providerID(t *testing.T, o recovery.Outcome) string {
	t.Helper()
	var ev map[string]string
	if err := json.Unmarshal(o.Evidence, &ev); err != nil {
		t.Fatal(err)
	}
	return ev["provider_id"]
}

// TestDispatchSecuresOneEffectWithEncryptedEnvelopes is the happy path.
func TestDispatchSecuresOneEffectWithEncryptedEnvelopes(t *testing.T) {
	h := newHarness(t)
	op := h.dispatchOp(1)
	out, err := h.coord.Dispatch(context.Background(), h.store(), op, h.effect(op, nil))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if out.Status != "accepted" || providerID(t, out) != "msg-1" || h.provider.count() != 1 || h.state(op.ID) != "secured" {
		t.Fatalf("outcome %+v, sends %d, state %s; want one accepted send, secured", out, h.provider.count(), h.state(op.ID))
	}
	key, _ := h.coord.EnvelopeKey(op)
	names := h.journal.Names()
	if strings.Join(names, ",") != key+".outcome,"+key+".prepared" {
		t.Fatalf("journal names %v, want the prepared and outcome envelopes under %s", names, key)
	}
	for _, name := range names {
		if strings.Contains(name, secretMarker) || strings.Contains(name, "Example") {
			t.Fatalf("journal name %q carries personal data", name)
		}
		data, _ := h.journal.Get(context.Background(), name)
		if bytes.Contains(data, []byte("Private Road")) || bytes.Contains(data, []byte(op.AggregateID)) {
			t.Fatalf("envelope %s is not encrypted", name)
		}
		if h.journal.Puts(name) != 1 {
			t.Fatalf("envelope %s written %d times, want once", name, h.journal.Puts(name))
		}
	}
	// Replaying a secured operation is a no-op returning the same outcome.
	again, err := h.coord.Dispatch(context.Background(), h.store(), op, h.effect(op, nil))
	if err != nil || providerID(t, again) != "msg-1" || h.provider.count() != 1 {
		t.Fatalf("replay of secured operation: %+v, %v, sends %d", again, err, h.provider.count())
	}
}

// TestCrashAtEachPhaseResumesWithExactlyOneEffect covers K11 phase crashes.
func TestCrashAtEachPhaseResumesWithExactlyOneEffect(t *testing.T) {
	cases := []struct {
		name         string
		setup        func(h *harness, cs *crashStore, lose *bool)
		wantFirst    string // state after the failed first attempt
		wantPrepPuts int
	}{
		{"journal down before the prepared envelope", func(h *harness, _ *crashStore, _ *bool) {
			h.journal.Fail = func(op, _ string) error {
				if op == "put" {
					return errors.New("b2 unavailable")
				}
				return nil
			}
		}, "reserved", 1},
		{"crash after the prepared envelope, before marking prepared", func(_ *harness, cs *crashStore, _ *bool) {
			cs.before["prepared"] = true
		}, "reserved", 1},
		{"crash after marking prepared, before the effect", func(_ *harness, cs *crashStore, _ *bool) {
			cs.after["prepared"] = true
		}, "prepared", 1},
		{"provider response lost", func(_ *harness, _ *crashStore, lose *bool) { *lose = true }, "prepared", 1},
		{"crash after the effect, before recording it", func(_ *harness, cs *crashStore, _ *bool) {
			cs.before["committed"] = true
		}, "prepared", 1},
		{"journal down before the outcome envelope", func(h *harness, _ *crashStore, _ *bool) {
			h.journal.Fail = func(op, name string) error {
				if op == "put" && strings.HasSuffix(name, ".outcome") {
					return errors.New("b2 unavailable")
				}
				return nil
			}
		}, "committed", 1},
		{"crash after the outcome envelope, before marking secured", func(_ *harness, cs *crashStore, _ *bool) {
			cs.before["secured"] = true
		}, "committed", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			h := newHarness(t)
			op := h.dispatchOp(2)
			cs := &crashStore{Store: h.store(), before: map[string]bool{}, after: map[string]bool{}}
			lose := false
			tc.setup(h, cs, &lose)

			if _, err := h.coord.Dispatch(ctx, cs, op, h.effect(op, &lose)); err == nil {
				t.Fatal("first attempt succeeded despite the injected failure")
			}
			if got := h.state(op.ID); got != tc.wantFirst {
				t.Fatalf("state after the failure = %s, want %s", got, tc.wantFirst)
			}
			h.journal.Fail = nil
			out, err := h.coord.Dispatch(ctx, cs, op, h.effect(op, &lose))
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			key, _ := h.coord.EnvelopeKey(op)
			if h.provider.count() != 1 || providerID(t, out) != "msg-1" || h.state(op.ID) != "secured" {
				t.Fatalf("after resume: sends %d, outcome %+v, state %s; want exactly one send, secured", h.provider.count(), out, h.state(op.ID))
			}
			if got := h.journal.Puts(key + ".prepared"); got != tc.wantPrepPuts {
				t.Fatalf("prepared envelope written %d times, want %d", got, tc.wantPrepPuts)
			}
			if got := h.journal.Puts(key + ".outcome"); got != 1 {
				t.Fatalf("outcome envelope written %d times, want 1", got)
			}
		})
	}
}

// TestNoEffectWithoutAcknowledgedPreparedEnvelope: while B2 is unavailable
// nothing is sent.
func TestNoEffectWithoutAcknowledgedPreparedEnvelope(t *testing.T) {
	h := newHarness(t)
	op := h.dispatchOp(3)
	h.journal.Fail = func(string, string) error { return errors.New("b2 unavailable") }
	for i := 0; i < 3; i++ {
		if _, err := h.coord.Dispatch(context.Background(), h.store(), op, h.effect(op, nil)); err == nil {
			t.Fatal("Dispatch succeeded without the journal")
		}
	}
	if h.provider.count() != 0 {
		t.Fatalf("%d sends without an acknowledged prepared envelope", h.provider.count())
	}
	if err := h.coord.Ready(context.Background()); err == nil {
		t.Fatal("Ready reported a broken journal as ready")
	}
}

// TestExpiredReplayWindowGoesToReconciliation: after the provider window, a
// prepared dispatch with an unknown outcome is never re-sent.
func TestExpiredReplayWindowGoesToReconciliation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	op := h.dispatchOp(4)
	lose := true
	if _, err := h.coord.Dispatch(ctx, h.store(), op, h.effect(op, &lose)); err == nil {
		t.Fatal("lost response reported success")
	}
	h.now = op.ReplayUntil.Add(time.Second)
	if _, err := h.coord.Dispatch(ctx, h.store(), op, h.effect(op, nil)); !errors.Is(err, recovery.ErrNeedsReconcile) {
		t.Fatalf("after the window: err = %v, want ErrNeedsReconcile", err)
	}
	if h.provider.count() != 1 || h.state(op.ID) != "reconcile" {
		t.Fatalf("sends %d, state %s; want the single original send and reconcile", h.provider.count(), h.state(op.ID))
	}
	// The aggregate stays blocked until a manager resolves it.
	next := h.dispatchOp(5)
	next.AggregateID = op.AggregateID
	if _, err := h.coord.Dispatch(ctx, h.store(), next, h.effect(next, nil)); !errors.Is(err, recovery.ErrBusy) {
		t.Fatalf("second operation on a reconciling aggregate: err = %v, want ErrBusy", err)
	}
}

func TestFreezeStopsEffectsUntilLifted(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	store := h.store()
	op := h.dispatchOp(6)
	if err := store.SetFreeze(ctx, true, "restore in progress"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.coord.Dispatch(ctx, store, op, h.effect(op, nil)); !errors.Is(err, recovery.ErrFrozen) {
		t.Fatalf("frozen: err = %v, want ErrFrozen", err)
	}
	// Frozen between preparing and the effect: nothing is sent.
	if err := store.SetFreeze(ctx, false, ""); err != nil {
		t.Fatal(err)
	}
	cs := &crashStore{Store: store, before: map[string]bool{}, after: map[string]bool{"prepared": true}}
	if _, err := h.coord.Dispatch(ctx, cs, op, h.effect(op, nil)); err == nil {
		t.Fatal("expected the injected crash")
	}
	if err := store.SetFreeze(ctx, true, "restore in progress"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.coord.Dispatch(ctx, store, op, h.effect(op, nil)); !errors.Is(err, recovery.ErrFrozen) || h.provider.count() != 0 {
		t.Fatalf("prepared while frozen: err = %v, sends %d; want ErrFrozen and no send", err, h.provider.count())
	}
	if err := store.SetFreeze(ctx, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.coord.Dispatch(ctx, store, op, h.effect(op, nil)); err != nil || h.provider.count() != 1 {
		t.Fatalf("after unfreezing: err = %v, sends %d", err, h.provider.count())
	}
}

// freezingStore starts a restore freeze while the operation is being
// prepared, as a concurrent restore would.
type freezingStore struct{ recovery.SQLStore }

func (f freezingStore) MarkPrepared(ctx context.Context, op recovery.Operation) (recovery.State, error) {
	state, err := f.SQLStore.MarkPrepared(ctx, op)
	if err == nil {
		err = f.SQLStore.SetFreeze(ctx, true, "restore started")
	}
	return state, err
}

// TestFreezeDuringDispatchStopsTheEffect: the freeze is checked again right
// before the effect, not only when the dispatch starts.
func TestFreezeDuringDispatchStopsTheEffect(t *testing.T) {
	h := newHarness(t)
	op := h.dispatchOp(13)
	_, err := h.coord.Dispatch(context.Background(), freezingStore{h.store()}, op, h.effect(op, nil))
	if !errors.Is(err, recovery.ErrFrozen) || h.provider.count() != 0 || h.state(op.ID) != "prepared" {
		t.Fatalf("freeze during dispatch: err %v, sends %d, state %s; want ErrFrozen, no send, prepared", err, h.provider.count(), h.state(op.ID))
	}
}

func TestOneUnresolvedOperationPerAggregate(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	first := h.dispatchOp(7)
	lose := true
	if _, err := h.coord.Dispatch(ctx, h.store(), first, h.effect(first, &lose)); err == nil {
		t.Fatal("expected unknown outcome")
	}
	second := h.dispatchOp(8)
	second.AggregateID, second.PriorVersion = first.AggregateID, 1
	if _, err := h.coord.Dispatch(ctx, h.store(), second, h.effect(second, nil)); !errors.Is(err, recovery.ErrBusy) {
		t.Fatalf("while the first is unresolved: err = %v, want ErrBusy", err)
	}
	if _, err := h.coord.Dispatch(ctx, h.store(), first, h.effect(first, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.coord.Dispatch(ctx, h.store(), second, h.effect(second, nil)); err != nil {
		t.Fatalf("after the first is secured: %v", err)
	}
	if h.provider.count() != 2 {
		t.Fatalf("sends = %d, want 2", h.provider.count())
	}
}

func TestReusedOperationIDWithDifferentPayloadIsRefused(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	op := h.dispatchOp(9)
	if _, err := h.coord.Dispatch(ctx, h.store(), op, h.effect(op, nil)); err != nil {
		t.Fatal(err)
	}
	changed := op
	changed.Payload = json.RawMessage(`{"provider":"resend","body_hash":"different"}`)
	if _, err := h.coord.Dispatch(ctx, h.store(), changed, h.effect(changed, nil)); err == nil || !strings.Contains(err.Error(), "different identity") {
		t.Fatalf("changed payload under the same ID: err = %v, want identity refusal", err)
	}
	extended := op
	extended.ReplayUntil = op.ReplayUntil.Add(time.Hour)
	if _, err := h.coord.Dispatch(ctx, h.store(), extended, h.effect(extended, nil)); err == nil || !strings.Contains(err.Error(), "different identity") {
		t.Fatalf("changed replay deadline under the same ID: err = %v, want identity refusal", err)
	}
	if h.provider.count() != 1 {
		t.Fatalf("sends = %d, want 1", h.provider.count())
	}
}

// TestPrimaryLossReplaysFromIndependentJournal: the primary database is lost
// after a prepared dispatch; a fresh database plus the B2 journal resumes it
// with the same identity, so the provider sends once. A dispatch whose
// window has passed goes to reconciliation instead.
func TestPrimaryLossReplaysFromIndependentJournal(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	sent, late := h.dispatchOp(10), h.dispatchOp(11)
	late.ReplayUntil = h.now.Add(time.Hour)
	for _, op := range []recovery.Operation{sent, late} {
		lose := true
		if _, err := h.coord.Dispatch(ctx, h.store(), op, h.effect(op, &lose)); err == nil {
			t.Fatal("expected unknown outcome")
		}
	}
	secured := h.dispatchOp(12)
	if _, err := h.coord.Dispatch(ctx, h.store(), secured, h.effect(secured, nil)); err != nil {
		t.Fatal(err)
	}
	if h.provider.count() != 3 {
		t.Fatalf("sends before the loss = %d, want 3", h.provider.count())
	}

	// Primary loss: a new, empty database; the journal survives.
	restored := ownerConn(t, dbtest.New(t))
	store := recovery.SQLStore{DB: restored}
	h.now = h.now.Add(2 * time.Hour)
	pending, err := h.coord.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 || pending[0].ID != sent.ID || pending[1].ID != late.ID {
		t.Fatalf("pending = %+v, want the two unresolved dispatches", pending)
	}
	if !bytes.Equal(pending[0].Payload, sent.Payload) || !pending[0].ReplayUntil.Equal(sent.ReplayUntil) {
		t.Fatalf("pending operation does not match the original identity: %+v", pending[0])
	}
	out, err := h.coord.Dispatch(ctx, store, pending[0], h.effect(pending[0], nil))
	if err != nil || providerID(t, out) != "msg-1" {
		t.Fatalf("replayed dispatch: %+v, %v; want the original message", out, err)
	}
	if _, err := h.coord.Dispatch(ctx, store, pending[1], h.effect(pending[1], nil)); !errors.Is(err, recovery.ErrNeedsReconcile) {
		t.Fatalf("expired dispatch after loss: err = %v, want ErrNeedsReconcile", err)
	}
	if h.provider.count() != 3 {
		t.Fatalf("sends after replay = %d, want still 3 (no duplicates)", h.provider.count())
	}
	if again, _ := h.coord.Pending(ctx); len(again) != 1 || again[0].ID != late.ID {
		t.Fatalf("pending after replay = %+v, want only the reconciling dispatch", again)
	}
}

// TestRuntimeRolesCannotUseRecoveryInternals: the coordinator functions are
// owner-only; only the backup reader may read the tables (as for every
// business table).
func TestRuntimeRolesCannotUseRecoveryInternals(t *testing.T) {
	ctx := context.Background()
	db := dbtest.New(t)
	ownerConn(t, db)
	reads := []string{"SELECT * FROM business.recovery_operation", "SELECT * FROM business.recovery_freeze"}
	calls := []string{
		"SELECT business.recovery_frozen()",
		"SELECT business.recovery_set_freeze(false, NULL)",
		"SELECT business.recovery_mark_secured(gen_random_uuid())",
		"SELECT * FROM business.recovery_reserve(gen_random_uuid(), 'command', gen_random_uuid(), 0, repeat('a', 64), 'k', NULL)",
	}
	for _, group := range []string{"gk_api", "gk_worker", "gk_backup"} {
		conn := db.LoginConn(t, group)
		denied := calls
		if group != "gk_backup" {
			denied = append(append([]string{}, reads...), calls...)
		}
		for _, sql := range denied {
			_, err := conn.Exec(ctx, sql)
			if got := dbtest.SQLState(err); got != "42501" {
				t.Errorf("%s: %q: SQLSTATE %q (err %v), want 42501", group, sql, got, err)
			}
		}
	}
	backup := db.LoginConn(t, "gk_backup")
	for _, sql := range reads {
		if _, err := backup.Exec(ctx, sql); err != nil {
			t.Errorf("gk_backup: %q: %v, want the backup reader to read it", sql, err)
		}
	}
}
