//go:build integration

package backup

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
	"github.com/jackc/pgx/v5"
)

// Fixed IDs of the seeded source.
const (
	seedOldWorker = "0b1d0000-0000-4000-8000-000000000001"
	seedRunning   = "0b1d0000-0000-4000-8000-0000000000a6"
)

// seedDispatchable writes one job of every dispatchable kind into a migrated
// database, as the domain gateways would leave them (inserted directly, as
// the superuser, with triggers off): an order command, a Stripe collection
// dispatch, a refund dispatch, a hold review and a notification, all due;
// a notification whose job is leased by a worker at backup time (running,
// generation 3); a notification on a manager hold (suspended); and a
// notification scheduled for later.
func seedDispatchable(t *testing.T, db *dbtest.DB) {
	t.Helper()
	admin := db.AdminConn(t)
	_, err := admin.Exec(context.Background(), `
BEGIN;
SET LOCAL session_replication_role = replica;
INSERT INTO business.account (id, provider_subject) VALUES ('0b1d0000-0000-4000-8000-0000000000c1', '0b1d0000-0000-4000-8000-0000000000c2');
INSERT INTO business.policy_version (id, source_decision_id, source_revision_sha256, summary, approver_evidence, effective_at, recorded_by)
VALUES ('0b1d0000-0000-4000-8000-0000000000d1', 'restore-test', repeat('a', 64), 'Restore test policy', 'restore test', now(), '0b1d0000-0000-4000-8000-0000000000c1');
INSERT INTO business.checkout_quote (id, owner_account_id, idempotency_key, body_sha256, handover_day, slot_id, slot_starts_at,
  slot_ends_at, schedule_revision, fulfillment_mode, subtotal_minor, discount_minor, total_minor, sent_at, expires_at)
VALUES ('0b1d0000-0000-4000-8000-0000000000e1', '0b1d0000-0000-4000-8000-0000000000c1', 'restore-quote', repeat('b', 64), '2026-12-01',
  gen_random_uuid(), '2026-12-01 10:00Z', '2026-12-01 11:00Z', 1, 'collection', 4200, 0, 4200, now(), now() + interval '24 hours');
INSERT INTO business.orders (id, quote_id, body_sha256, customer_full_name, contact_email, payment_requirement, fulfillment_mode,
  handover_day, slot_id, original_handover_at, handover_ends_at, full_refund_deadline_at, policy_version_id, subtotal_minor,
  discount_minor, total_minor, placed_at)
VALUES ('0b1d0000-0000-4000-8000-0000000000f1', '0b1d0000-0000-4000-8000-0000000000e1', repeat('c', 64), 'Synthetic Customer',
  'synthetic@example.com', 'full', 'collection', '2026-12-01', gen_random_uuid(), '2026-12-01 10:00Z', '2026-12-01 11:00Z',
  timestamptz '2026-12-01 10:00Z' - interval '120 hours', '0b1d0000-0000-4000-8000-0000000000d1', 4200, 0, 4200, now());
INSERT INTO business.payment_collection (id, order_id, amount_minor, environment, provider_account_id, livemode, actor_kind,
  actor_account_id, request_key, request_sha256, return_origin, initiated_at)
VALUES ('0b1d0000-0000-4000-8000-0000000000b1', '0b1d0000-0000-4000-8000-0000000000f1', 4200, 'test', 'acct_test1', false, 'customer',
  '0b1d0000-0000-4000-8000-0000000000c1', 'restore-pay', repeat('d', 64), 'https://example.com', now());
INSERT INTO business.refund_goal (id, order_id, target_minor, reason, source)
VALUES ('0b1d0000-0000-4000-8000-0000000000b2', '0b1d0000-0000-4000-8000-0000000000f1', 4200, 'late_capture_terminal', 'system');
INSERT INTO business.refund_operation (id, order_id, collection_id, goal_id, goal_version, amount_minor, environment, provider_account_id, livemode)
VALUES ('0b1d0000-0000-4000-8000-0000000000b3', '0b1d0000-0000-4000-8000-0000000000f1', '0b1d0000-0000-4000-8000-0000000000b1',
  '0b1d0000-0000-4000-8000-0000000000b2', 1, 4200, 'test', 'acct_test1', false);
-- A job's operation is its domain row (the command, collection, refund, order or intent).
INSERT INTO business.job (id, operation_id, next_attempt_at) VALUES
  ('0b1d0000-0000-4000-8000-0000000000a1', '0b1d0000-0000-4000-8000-000000000011', now() - interval '1 minute'),
  ('0b1d0000-0000-4000-8000-0000000000a2', '0b1d0000-0000-4000-8000-0000000000b1', now() - interval '1 minute'),
  ('0b1d0000-0000-4000-8000-0000000000a3', '0b1d0000-0000-4000-8000-0000000000b3', now() - interval '1 minute'),
  ('0b1d0000-0000-4000-8000-0000000000a4', '0b1d0000-0000-4000-8000-0000000000f1', now() - interval '1 minute'),
  ('0b1d0000-0000-4000-8000-0000000000a5', '0b1d0000-0000-4000-8000-000000000015', now() - interval '1 minute'),
  ('0b1d0000-0000-4000-8000-0000000000a7', '0b1d0000-0000-4000-8000-000000000017', now() + interval '2 hours'),
  ('0b1d0000-0000-4000-8000-0000000000a8', '0b1d0000-0000-4000-8000-000000000018', now() - interval '1 minute');
INSERT INTO business.job (id, operation_id, next_attempt_at, state, lease_owner, lease_until, generation)
VALUES ('`+seedRunning+`', '0b1d0000-0000-4000-8000-000000000016', now() - interval '1 minute', 'running', '`+seedOldWorker+`', now() + interval '10 minutes', 3);
INSERT INTO business.order_command (id, order_id, command, actor_kind, actor_account_id, idempotency_key, body_sha256, prior_version,
  received_at, payload, payload_sha256, envelope_key, job_id)
VALUES ('0b1d0000-0000-4000-8000-000000000011', '0b1d0000-0000-4000-8000-0000000000f1', 'free_confirm', 'customer', '0b1d0000-0000-4000-8000-0000000000c1',
  'restore-cmd', repeat('e', 64), 1, now(), '{}', repeat('f', 64), 'restore-envelope', '0b1d0000-0000-4000-8000-0000000000a1');
INSERT INTO business.payment_job (job_id, collection_id, purpose)
VALUES ('0b1d0000-0000-4000-8000-0000000000a2', '0b1d0000-0000-4000-8000-0000000000b1', 'dispatch');
INSERT INTO business.refund_job (job_id, collection_id, purpose, refund_operation_id)
VALUES ('0b1d0000-0000-4000-8000-0000000000a3', '0b1d0000-0000-4000-8000-0000000000b1', 'dispatch', '0b1d0000-0000-4000-8000-0000000000b3');
INSERT INTO business.order_hold_review (order_id, hold_until) VALUES ('0b1d0000-0000-4000-8000-0000000000f1', now() + interval '15 minutes');
INSERT INTO business.order_hold_review_job (job_id, order_id) VALUES ('0b1d0000-0000-4000-8000-0000000000a4', '0b1d0000-0000-4000-8000-0000000000f1');
INSERT INTO business.notification_intent (id, transition_id, recipient_id, template_revision, idempotency_key, payload, payload_hash, job_id)
SELECT i, gen_random_uuid(), gen_random_uuid(), 'restore-test.v1', gen_random_uuid(), '{}', repeat('0', 64), j
FROM unnest(ARRAY['0b1d0000-0000-4000-8000-000000000015', '0b1d0000-0000-4000-8000-000000000016', '0b1d0000-0000-4000-8000-000000000017',
                   '0b1d0000-0000-4000-8000-000000000018']::uuid[],
            ARRAY['0b1d0000-0000-4000-8000-0000000000a5', '`+seedRunning+`', '0b1d0000-0000-4000-8000-0000000000a7',
                  '0b1d0000-0000-4000-8000-0000000000a8']::uuid[]) AS u(i, j);
UPDATE business.notification_intent SET suspended_reason = 'manager hold' WHERE id = '0b1d0000-0000-4000-8000-000000000018';
COMMIT`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// restoreSource builds a migrated, seeded source and its backup.
func restoreSource(t *testing.T) (*memStore, Sealer, Manifest, string) {
	t.Helper()
	ctx := context.Background()
	pgDump, pgRestore := pgTools(t)
	source := dbtest.New(t)
	if _, err := migrate.Run(ctx, source.MigratorConn(t), migrations.FS); err != nil {
		t.Fatal(err)
	}
	seedDispatchable(t, source)
	store, sealer := newMemStore(), testSealer(t)
	d := &Dumper{Env: "test", DB: source.LoginURL(t, "gk_backup"), PgDump: pgDump, Store: store, Sealer: sealer}
	m, err := d.Dump(ctx)
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	return store, sealer, m, pgRestore
}

// restoreTarget is a fresh database whose migration role may disable
// triggers for the load, as Supabase's postgres role may.
func restoreTarget(t *testing.T) *dbtest.DB {
	t.Helper()
	db := dbtest.New(t)
	if _, err := db.AdminConn(t).Exec(context.Background(), "GRANT SET ON PARAMETER session_replication_role TO gk_migrator_test"); err != nil {
		t.Fatal(err)
	}
	return db
}

// A backup restores into the isolated target with every count matching,
// frozen by the load transaction itself; old leases are refused; and a
// fresh worker running the real handlers defers every dispatchable job
// without calling a provider, writing a journal envelope or changing any
// business table but job. A second restore over it is refused.
func TestRestoreIsFrozenInTheSameTransaction(t *testing.T) {
	ctx := context.Background()
	store, sealer, m, pgRestore := restoreSource(t)
	target := restoreTarget(t)
	r := &Restore{Env: "test", Target: target.MigratorURL(), PgRestore: pgRestore, Store: store, Sealer: sealer, Migrations: migrations.FS}
	// While the load is still open, another session sees nothing of it.
	r.beforeCommit = func(ctx context.Context, _ pgx.Tx) error {
		var jobs int
		err := target.AdminConn(t).QueryRow(ctx, "SELECT count(*) FROM business.job").Scan(&jobs)
		if err != nil || jobs != 0 {
			t.Errorf("before the commit another session sees %d jobs (%v)", jobs, err)
		}
		return nil
	}
	rep, err := r.Run(ctx)
	if err != nil {
		t.Fatalf("restore: %v (%+v)", err, rep)
	}
	var rows int64
	for _, n := range m.Rows {
		rows += n
	}
	if rep.Result != "ok" || rep.Tables != len(m.Rows) || rep.Rows != rows || rep.SchemaVersion != m.SchemaVersion ||
		rep.MigrationsApplied != len(m.Migrations) || !rep.Frozen || !rep.FreezeSameTxn || rep.LeasesInBackup != 1 || rep.LeasesRevoked != 1 {
		t.Fatalf("report = %+v", rep)
	}
	s := rep.Synthetic
	wantKinds := map[string]int{"order_command": 1, "payment": 1, "refund": 1, "hold_review": 1, "notification": 2}
	if s.StaleLeaseCalls != 5 || s.StaleLeaseRefused != 5 || s.Claimable != 7 || s.NotDue != 1 || s.Claimed != 7 ||
		s.DeferredFrozen != 6 || !maps.Equal(s.DeferredByKind, wantKinds) || s.HeldForAttention != 1 || s.Unowned != 0 || s.Violations != nil ||
		s.ProviderCalls != 0 || s.JournalWrites != 0 || !s.TablesUnchanged || !s.RecoveryOperationsUnchanged {
		t.Fatalf("synthetic dispatch = %+v", s)
	}

	admin := target.AdminConn(t)
	var frozen bool
	var reason string
	if err := admin.QueryRow(ctx, "SELECT frozen, reason FROM business.recovery_freeze").Scan(&frozen, &reason); err != nil ||
		!frozen || !strings.HasPrefix(reason, freezeReasonPrefix+"database/test/") {
		t.Fatalf("freeze = %v %q %v", frozen, reason, err)
	}
	// No job finished or escalated; the old lease is gone for good.
	var finished, running, oldLease int
	if err := admin.QueryRow(ctx, `SELECT count(*) FILTER (WHERE state IN ('completed', 'attention')),
		count(*) FILTER (WHERE state = 'running'), count(*) FILTER (WHERE lease_owner = $1) FROM business.job`, seedOldWorker).
		Scan(&finished, &running, &oldLease); err != nil || finished != 0 || running != 0 || oldLease != 0 {
		t.Fatalf("jobs: %d finished, %d running, %d with the old lease (%v)", finished, running, oldLease, err)
	}
	var generation int64
	if err := admin.QueryRow(ctx, "SELECT generation FROM business.job WHERE id = $1", seedRunning).Scan(&generation); err != nil || generation <= 3 {
		t.Fatalf("restored lease generation %d (%v)", generation, err)
	}
	// The synthetic worker login is gone.
	var logins int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM pg_roles WHERE rolname LIKE 'gk_restore_check_%'").Scan(&logins); err != nil || logins != 0 {
		t.Fatalf("%d synthetic logins left (%v)", logins, err)
	}

	// A restored (non-empty) target is refused, and stays as it was.
	rep2, err := r.Run(ctx)
	if err == nil || rep2.Stage != "target" || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("restore over a restored target: %v (%+v)", err, rep2)
	}
	var jobs int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM business.job").Scan(&jobs); err != nil || int64(jobs) != m.Rows["job"] {
		t.Fatalf("after the refusal the target has %d jobs (%v)", jobs, err)
	}
}

// A load that fails before its commit leaves nothing behind but the fresh,
// frozen schema, which a new restore accepts; a target migrated only part
// of the way is completed up to the backup's version.
func TestFailedLoadLeavesAFreshTarget(t *testing.T) {
	ctx := context.Background()
	store, sealer, m, pgRestore := restoreSource(t)
	target := restoreTarget(t)
	if _, err := migrate.RunTo(ctx, target.MigratorConn(t), migrations.FS, m.Migrations[4]); err != nil {
		t.Fatal(err)
	}
	r := &Restore{Env: "test", Target: target.MigratorURL(), PgRestore: pgRestore, Store: store, Sealer: sealer, Migrations: migrations.FS}
	boom := errors.New("injected failure before the commit")
	r.beforeCommit = func(context.Context, pgx.Tx) error { return boom }
	if rep, err := r.Run(ctx); !errors.Is(err, boom) || rep.Stage != "load" || rep.MigrationsApplied != len(m.Migrations)-5 {
		t.Fatalf("injected failure: %v (%+v)", err, rep)
	}
	admin := target.AdminConn(t)
	var jobs int
	var frozen bool
	if err := admin.QueryRow(ctx, "SELECT (SELECT count(*) FROM business.job), (SELECT frozen FROM business.recovery_freeze)").
		Scan(&jobs, &frozen); err != nil || jobs != 0 || !frozen {
		t.Fatalf("after a failed load: %d jobs, frozen %v (%v)", jobs, frozen, err)
	}
	r.beforeCommit = nil
	rep, err := r.Run(ctx)
	if err != nil || rep.MigrationsApplied != 0 || !rep.FreezeSameTxn {
		t.Fatalf("restore after a failed load: %v (%+v)", err, rep)
	}
}

// Targets that are not empty or fresh are refused before anything changes.
func TestRestoreRefusesNonEmptyTargets(t *testing.T) {
	ctx := context.Background()
	store, sealer, _, pgRestore := restoreSource(t)
	run := func(db *dbtest.DB) error {
		r := &Restore{Env: "test", Target: db.MigratorURL(), PgRestore: pgRestore, Store: store, Sealer: sealer, Migrations: migrations.FS}
		_, err := r.Run(ctx)
		return err
	}
	// A migrated database holding business rows.
	withData := restoreTarget(t)
	if _, err := migrate.Run(ctx, withData.MigratorConn(t), migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := withData.AdminConn(t).Exec(ctx, "INSERT INTO business.account (id, provider_subject) VALUES (gen_random_uuid(), gen_random_uuid())"); err != nil {
		t.Fatal(err)
	}
	if err := run(withData); err == nil || !strings.Contains(err.Error(), "business.account has 1 rows") {
		t.Errorf("target with data: %v", err)
	}
	// A history that is not the backup's.
	diverged := restoreTarget(t)
	if _, err := migrate.RunTo(ctx, diverged.MigratorConn(t), migrations.FS, "0002_job_gateway"); err != nil {
		t.Fatal(err)
	}
	if _, err := diverged.AdminConn(t).Exec(ctx, "UPDATE gk_meta.schema_migration SET sha256 = repeat('0', 64) WHERE version = '0002_job_gateway'"); err != nil {
		t.Fatal(err)
	}
	if err := run(diverged); err == nil || !strings.Contains(err.Error(), "not a prefix") {
		t.Errorf("diverged target: %v", err)
	}
	// A business schema without a migration history.
	stray := restoreTarget(t)
	if _, err := stray.AdminConn(t).Exec(ctx, "CREATE SCHEMA business"); err != nil {
		t.Fatal(err)
	}
	if err := run(stray); err == nil || !strings.Contains(err.Error(), "no migration history") {
		t.Errorf("stray business schema: %v", err)
	}
}

// freshRows lists exactly the rows migrations insert into business tables.
func TestFreshRowsMatchTheMigrations(t *testing.T) {
	ctx := context.Background()
	db := dbtest.New(t)
	if _, err := migrate.Run(ctx, db.MigratorConn(t), migrations.FS); err != nil {
		t.Fatal(err)
	}
	counts, err := countRows(ctx, db.AdminConn(t))
	if err != nil {
		t.Fatal(err)
	}
	seeded := map[string]int64{}
	for table, n := range counts {
		if n > 0 {
			seeded[table] = n
		}
	}
	if !maps.Equal(seeded, freshRows) {
		t.Fatalf("migrations seed %v, freshRows lists %v", seeded, freshRows)
	}
}

// A dump whose counts differ from its manifest is refused before the target
// is touched.
func TestRestoreRefusesADumpThatDisagreesWithItsManifest(t *testing.T) {
	ctx := context.Background()
	store, sealer, m, pgRestore := restoreSource(t)
	m.Rows["job"]++
	resealManifest(t, store, sealer, m)
	target := restoreTarget(t)
	r := &Restore{Env: "test", Target: target.MigratorURL(), PgRestore: pgRestore, Store: store, Sealer: sealer, Migrations: migrations.FS,
		Now: func() time.Time { return time.Now() }}
	rep, err := r.Run(ctx)
	if err == nil || rep.Stage != "validate" || !strings.Contains(err.Error(), "rows of job") {
		t.Fatalf("manifest disagreeing with the dump: %v (%+v)", err, rep)
	}
	var business bool
	if err := target.AdminConn(t).QueryRow(ctx, "SELECT to_regnamespace('business') IS NOT NULL").Scan(&business); err != nil || business {
		t.Fatalf("the target was touched: %v %v", business, err)
	}
}

// The synthetic dispatch check has teeth: on the same restored data without
// the freeze, the handlers go ahead and the check fails.
func TestSyntheticDispatchCatchesAnUnfrozenRestore(t *testing.T) {
	ctx := context.Background()
	store, sealer, _, pgRestore := restoreSource(t)
	target := restoreTarget(t)
	r := &Restore{Env: "test", Target: target.MigratorURL(), PgRestore: pgRestore, Store: store, Sealer: sealer, Migrations: migrations.FS}
	// The load commits unfrozen (as if the freeze were forgotten).
	r.beforeCommit = func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE business.recovery_freeze SET frozen = false, reason = NULL")
		return err
	}
	if _, err := r.Run(ctx); err == nil || !strings.Contains(err.Error(), "not frozen") {
		t.Fatalf("an unfrozen load passed verification: %v", err)
	}
	conn := target.MigratorConn(t)
	rep, err := checkFrozen(ctx, conn, target.MigratorURL(), "test", nil)
	if err == nil || rep.Violations["hold_review:completed"] != 1 || rep.Violations["notification:attention"] == 0 ||
		rep.DeferredFrozen != 0 || rep.TablesUnchanged {
		t.Fatalf("unfrozen jobs passed the synthetic dispatch check: %v (%+v)", err, rep)
	}
}
