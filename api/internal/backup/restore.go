package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Restore restores a backup into the isolated restore database (GK-034
// Milestone B, K11): never into a database that holds anything but the
// fresh schema gk-migrate creates.
//
//  1. Find the backup (newest complete, or Name), check the dump against its
//     manifest, and read its data script offline: every business table's
//     row count must match the manifest, and its migration history must be
//     this build's, file for file (same SHA-256).
//  2. Take the target's restore advisory lock (one restore at a time) and
//     refuse a target that is not empty or exactly a fresh migrated schema.
//  3. Migrate the target up to the backup's schema version (roles, schema,
//     gateway functions) and freeze outgoing effects at once.
//  4. In one transaction, without firing triggers (session_replication_role
//     = replica, as pg_restore --disable-triggers): lock the business tables
//     against writers and check again that they hold only fresh rows,
//     replace every business table's rows with the backup's (migration seed
//     rows such as recovery_freeze included; gk_meta is not restored),
//     revoke every job lease (each generation moves up by GenerationEpoch;
//     jobs leased at backup time are released for a fresh claim), set
//     recovery_freeze to frozen with a reason, check
//     foreign keys and row counts, and commit. So no state ever exists in
//     which the restored jobs are visible and not frozen.
//  5. Verify after the commit: history, row counts, the freeze, and that
//     the freeze and every restored row were written by that one
//     transaction (same xmin).
//  6. Prove the freeze holds (frozen.go): old leases are refused, and a
//     fresh worker login running the worker's own handler chain
//     (internal/handlers) with recording providers defers every claimable
//     job, calls no provider, writes no journal envelope and changes no
//     business table but job. A failed check has already changed the
//     target (jobs claimed and released): empty it before restoring again.
type Restore struct {
	Env        string // environment of the backups (APP_ENV)
	Target     string // migration credential of the restore database (never logged)
	Name       string // backup stamp; empty selects the newest
	PgRestore  string // pg_restore executable (default "pg_restore"), 17 or newer
	Store      Store
	Sealer     Sealer
	Migrations fs.FS // this build's migrations
	Now        func() time.Time

	beforeLoad   func(context.Context) error         // test hook: runs after the migration, before the load
	beforeCommit func(context.Context, pgx.Tx) error // test hook: runs before the load commits
}

// GenerationEpoch is added to every restored job's lease generation: a lease
// issued after the backup was taken (the lost window) carries a generation
// below it, so it is refused even if its worker ID came back.
const GenerationEpoch = 1_000_000

// restoreLockKey is the session advisory lock one restore holds on its
// target from the emptiness check to the end ("gk-rstr").
const restoreLockKey int64 = 0x676b2d7273747200

// RestoreReport is the machine-readable result: names, versions, counts and
// timings only.
type RestoreReport struct {
	Result            string         `json:"result"` // "ok" or "failed"
	Stage             string         `json:"stage,omitempty"`
	Error             string         `json:"error,omitempty"`
	Backup            string         `json:"backup,omitempty"`
	BackupCreatedAt   *time.Time     `json:"backup_created_at,omitempty"`
	RestoredAt        *time.Time     `json:"restored_at,omitempty"`
	DataLossWindowSec int64          `json:"data_loss_window_s,omitempty"`
	SchemaVersion     string         `json:"schema_version,omitempty"`
	Migrations        int            `json:"migrations,omitempty"`
	MigrationsApplied int            `json:"migrations_applied"`
	Tables            int            `json:"tables"`
	Rows              int64          `json:"rows"`
	Frozen            bool           `json:"frozen"`
	FreezeSameTxn     bool           `json:"freeze_same_transaction"`
	LeasesInBackup    int            `json:"leases_in_backup"`
	LeasesRevoked     int64          `json:"leases_revoked"`
	GenerationEpoch   int64          `json:"generation_epoch"`
	Synthetic         *FrozenReport  `json:"synthetic_dispatch,omitempty"`
	TimingsMS         map[string]int `json:"timings_ms"`
}

// freezeReasonPrefix starts every restore freeze reason.
const freezeReasonPrefix = "gk-backup restore of "

// freshRows lists the rows gk-migrate itself inserts into business tables
// (an integration test keeps it in step with the migrations). A target
// whose business tables hold more is not fresh and is refused.
var freshRows = map[string]int64{"recovery_freeze": 1}

// Run restores the backup. The report is filled as far as the restore got.
func (r *Restore) Run(ctx context.Context) (rep RestoreReport, err error) {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	rep.TimingsMS = map[string]int{}
	start := now()
	mark := start
	step := func(name string) {
		t := now()
		rep.TimingsMS[name] = int(t.Sub(mark).Milliseconds())
		mark = t
	}
	defer func() {
		rep.TimingsMS["total"] = int(now().Sub(start).Milliseconds())
		if err != nil {
			rep.Result, rep.Error = "failed", safeError(err).Error()
			err = safeError(err)
		} else {
			rep.Result, rep.Stage = "ok", ""
		}
	}()

	rep.Stage = "select"
	m, err := Find(ctx, r.Store, r.Sealer, r.Env, r.Name)
	if err != nil {
		return rep, err
	}
	created := m.CreatedAt
	rep.Backup, rep.BackupCreatedAt = strings.TrimSuffix(m.Dump, "dump.sealed"), &created
	rep.SchemaVersion, rep.Migrations = m.SchemaVersion, len(m.Migrations)
	step("select")

	rep.Stage = "fetch"
	archive, err := openDump(ctx, r.Store, r.Sealer, m)
	if err != nil {
		return rep, err
	}
	step("fetch")

	rep.Stage = "validate"
	pgRestore := r.PgRestore
	if pgRestore == "" {
		pgRestore = "pg_restore"
	}
	var sum dumpSummary
	if err := dataScript(ctx, pgRestore, archive, func(script io.Reader) error {
		sum, err = summarize(script)
		return err
	}); err != nil {
		return rep, err
	}
	if err := r.checkSummary(m, sum); err != nil {
		return rep, err
	}
	rep.LeasesInBackup = len(sum.Leases)
	step("validate")

	rep.Stage = "target"
	cfg, err := pgx.ParseConfig(r.Target)
	if err != nil {
		return rep, errors.New("backup: invalid restore target URL")
	}
	cfg.RuntimeParams["application_name"] = "gk-backup-restore"
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return rep, fmt.Errorf("backup: connect to the restore target: %w", err)
	}
	defer conn.Close(context.WithoutCancel(ctx))
	// One restore at a time on a target, from the check to the end (the
	// lock ends with the session).
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", restoreLockKey).Scan(&locked); err != nil {
		return rep, fmt.Errorf("backup: lock the restore target: %w", err)
	}
	if !locked {
		return rep, errors.New("backup: another restore is running on this target")
	}
	defer func() { _, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", restoreLockKey) }()
	if err := checkTarget(ctx, conn, sum.History); err != nil {
		return rep, err
	}
	step("target")

	rep.Stage = "migrate"
	mrep, err := migrate.Apply(ctx, conn, r.Migrations, m.SchemaVersion)
	if err != nil {
		return rep, fmt.Errorf("backup: migrate the target: %w", err)
	}
	if len(mrep.Ahead) > 0 {
		return rep, fmt.Errorf("backup: the target has migrations %v the backup does not", mrep.Ahead)
	}
	rep.MigrationsApplied = len(mrep.Applied)
	reason := freezeReasonPrefix + rep.Backup + ": outgoing effects frozen until reconciled (K11)"
	// The fresh schema is frozen before any data arrives, too.
	if err := ownerTx(ctx, conn, false, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "SELECT business.recovery_set_freeze(true, $1)", reason)
		return err
	}); err != nil {
		return rep, fmt.Errorf("backup: freeze the target: %w", err)
	}
	if r.beforeLoad != nil {
		if err := r.beforeLoad(ctx); err != nil {
			return rep, err
		}
	}
	step("migrate")

	rep.Stage = "load"
	xid, revoked, err := r.load(ctx, conn, pgRestore, archive, m, reason)
	if err != nil {
		return rep, err
	}
	rep.LeasesRevoked, rep.GenerationEpoch = revoked, GenerationEpoch
	step("load")

	rep.Stage = "verify"
	v, err := verifyRestored(ctx, conn, m, sum.History, xid)
	if err != nil {
		return rep, err
	}
	rep.Tables, rep.Rows, rep.Frozen, rep.FreezeSameTxn = v.tables, v.rows, v.frozen, v.sameTxn
	restored := now().UTC().Truncate(time.Second)
	rep.RestoredAt, rep.DataLossWindowSec = &restored, int64(restored.Sub(m.CreatedAt).Seconds())
	step("verify")

	rep.Stage = "synthetic_dispatch"
	fr, err := checkFrozen(ctx, conn, r.Target, r.Env, sum.Leases)
	rep.Synthetic = &fr
	if err != nil {
		return rep, err
	}
	// The check may only have moved jobs; everything else is as restored.
	if v, err = verifyRestored(ctx, conn, m, sum.History, ""); err != nil {
		return rep, fmt.Errorf("after the synthetic dispatch: %w", err)
	}
	if !v.frozen {
		return rep, errors.New("backup: the restore is no longer frozen after the synthetic dispatch")
	}
	step("synthetic_dispatch")
	return rep, nil
}

// checkSummary compares the data script with the manifest and this build.
func (r *Restore) checkSummary(m Manifest, sum dumpSummary) error {
	if !maps.Equal(sum.Rows, m.Rows) {
		for t, want := range m.Rows {
			if got, ok := sum.Rows[t]; !ok || got != want {
				return fmt.Errorf("backup: the dump holds %d rows of %s, the manifest %d", got, t, want)
			}
		}
		return fmt.Errorf("backup: the dump holds %d business tables, the manifest lists %d", len(sum.Rows), len(m.Rows))
	}
	versions := make([]string, len(sum.History))
	for i, h := range sum.History {
		versions[i] = h.Version
	}
	if !slices.Equal(versions, m.Migrations) {
		return fmt.Errorf("backup: the dump's migration history %v differs from its manifest %v", versions, m.Migrations)
	}
	known, err := migrate.Load(r.Migrations)
	if err != nil {
		return err
	}
	sums := map[string]string{}
	for _, k := range known {
		sums[k.Version] = k.SHA256
	}
	for _, h := range sum.History {
		switch s, ok := sums[h.Version]; {
		case !ok:
			return fmt.Errorf("backup: the backup's migration %s is unknown to this build: restore with the release that took it", h.Version)
		case s != h.SHA256:
			return fmt.Errorf("backup: the backup's migration %s differs from this build's file", h.Version)
		}
	}
	return nil
}

// checkTarget refuses a target that holds anything but an empty database
// or the fresh schema of a migration run that is a prefix of the backup's.
func checkTarget(ctx context.Context, conn *pgx.Conn, history []MigrationRecord) error {
	var business, meta bool
	if err := conn.QueryRow(ctx, `SELECT to_regnamespace('business') IS NOT NULL,
		to_regclass('gk_meta.schema_migration') IS NOT NULL`).Scan(&business, &meta); err != nil {
		return fmt.Errorf("backup: inspect the target: %w", err)
	}
	var applied []MigrationRecord
	if meta {
		rows, err := conn.Query(ctx, "SELECT version, sha256 FROM gk_meta.schema_migration ORDER BY version")
		if err != nil {
			return fmt.Errorf("backup: read the target's migrations: %w", err)
		}
		if applied, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (MigrationRecord, error) {
			var m MigrationRecord
			return m, r.Scan(&m.Version, &m.SHA256)
		}); err != nil {
			return fmt.Errorf("backup: read the target's migrations: %w", err)
		}
	}
	if len(applied) > len(history) || !slices.Equal(applied, history[:len(applied)]) {
		return errors.New("backup: the target's migration history is not a prefix of the backup's: refusing a non-empty target")
	}
	if !business {
		return nil
	}
	if len(applied) == 0 {
		return errors.New("backup: the target has a business schema but no migration history: refusing a non-empty target")
	}
	return ownerTx(ctx, conn, true, func(tx pgx.Tx) error { return refuseData(ctx, tx) })
}

// refuseData fails when any business table holds more than the rows
// migrations insert.
func refuseData(ctx context.Context, tx pgx.Tx) error {
	counts, err := countRows(ctx, tx)
	if err != nil {
		return err
	}
	for _, t := range slices.Sorted(maps.Keys(counts)) {
		if counts[t] > freshRows[t] {
			return fmt.Errorf("backup: the target is not empty (business.%s has %d rows): refusing to restore over it", t, counts[t])
		}
	}
	return nil
}

// ownerTx runs f in a transaction as gk_owner (the migration credential may
// SET ROLE to it, migration 0001).
func ownerTx(ctx context.Context, conn *pgx.Conn, readOnly bool, f func(pgx.Tx) error) error {
	opts := pgx.TxOptions{}
	if readOnly {
		opts = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	}
	tx, err := conn.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE gk_owner"); err != nil {
		return fmt.Errorf("backup: act as gk_owner: %w", err)
	}
	if err := f(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// load replaces the business data with the backup's, revokes old leases and
// freezes, in one transaction. It returns the transaction's ID (as xmin
// text) and the number of leases revoked.
func (r *Restore) load(ctx context.Context, conn *pgx.Conn, pgRestore string, archive []byte, m Manifest, reason string) (string, int64, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	for _, s := range []string{"SET LOCAL lock_timeout = '10s'", "SET LOCAL statement_timeout = '15min'"} {
		if _, err := tx.Exec(ctx, s); err != nil {
			return "", 0, err
		}
	}
	// Triggers (immutability, no-delete guards) and foreign-key checks are
	// off for the load, as with pg_restore --disable-triggers; the foreign
	// keys are checked explicitly below. Needs superuser or
	// GRANT SET ON PARAMETER session_replication_role (Supabase's postgres
	// role may set it).
	if _, err := tx.Exec(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
		return "", 0, fmt.Errorf("backup: disable triggers for the load (session_replication_role): %w", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE gk_owner"); err != nil {
		return "", 0, fmt.Errorf("backup: act as gk_owner: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT c.relname::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'business' AND c.relkind IN ('r', 'p') ORDER BY 1`)
	if err != nil {
		return "", 0, err
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", 0, err
	}
	if !slices.Equal(tables, slices.Sorted(maps.Keys(m.Rows))) {
		return "", 0, fmt.Errorf("backup: the migrated target has business tables %v, the backup %v", tables, slices.Sorted(maps.Keys(m.Rows)))
	}
	// Writers wait from here to the commit; then the target is checked again
	// under the lock, so nothing written since the first check is deleted.
	quoted := make([]string, len(tables))
	for i, t := range tables {
		quoted[i] = pgx.Identifier{"business", t}.Sanitize()
	}
	if _, err := tx.Exec(ctx, "LOCK TABLE "+strings.Join(quoted, ", ")+" IN EXCLUSIVE MODE"); err != nil {
		return "", 0, fmt.Errorf("backup: lock the business tables: %w", err)
	}
	if err := refuseData(ctx, tx); err != nil {
		return "", 0, err
	}
	for _, t := range tables {
		if _, err := tx.Exec(ctx, "DELETE FROM business."+pgx.Identifier{t}.Sanitize()); err != nil {
			return "", 0, fmt.Errorf("backup: clear business.%s: %w", t, err)
		}
	}
	pg := tx.Conn().PgConn()
	if err := dataScript(ctx, pgRestore, archive, func(script io.Reader) error {
		return parseScript(script, func(b copyBlock, data io.Reader) error {
			if b.Schema != "business" {
				return nil // the history comes from this build's migrations
			}
			tag, err := pg.CopyFrom(ctx, data, b.Statement())
			if err != nil {
				return fmt.Errorf("backup: load business.%s: %w", b.Table, err)
			}
			if tag.RowsAffected() != m.Rows[b.Table] {
				return fmt.Errorf("backup: loaded %d rows of business.%s, the manifest %d", tag.RowsAffected(), b.Table, m.Rows[b.Table])
			}
			return nil
		})
	}); err != nil {
		return "", 0, err
	}
	// Leases are revoked: every job's generation moves up by the epoch, so a
	// lease held at backup time or issued after it (the lost window) fences
	// out, and jobs leased at backup time are released for a fresh claim.
	var revoked int64
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM business.job WHERE state = 'running'").Scan(&revoked); err != nil {
		return "", 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE business.job SET generation = generation + $1,
		state = CASE WHEN state = 'running' THEN 'retry' ELSE state END,
		next_attempt_at = CASE WHEN state = 'running' THEN now() ELSE next_attempt_at END,
		lease_owner = NULL, lease_until = NULL, version = version + 1, updated_at = now()`, GenerationEpoch); err != nil {
		return "", 0, fmt.Errorf("backup: revoke restored leases: %w", err)
	}
	if tag, err := tx.Exec(ctx, "UPDATE business.recovery_freeze SET frozen = true, reason = $1, changed_at = now() WHERE singleton", reason); err != nil {
		return "", 0, fmt.Errorf("backup: freeze: %w", err)
	} else if tag.RowsAffected() != 1 {
		return "", 0, errors.New("backup: the backup has no recovery_freeze row to freeze")
	}
	if err := checkForeignKeys(ctx, tx); err != nil {
		return "", 0, err
	}
	counts, err := countRows(ctx, tx)
	if err != nil {
		return "", 0, err
	}
	if !maps.Equal(counts, m.Rows) {
		return "", 0, errors.New("backup: row counts after the load differ from the manifest")
	}
	var xid string
	if err := tx.QueryRow(ctx, "SELECT xmin::text FROM business.recovery_freeze WHERE singleton").Scan(&xid); err != nil {
		return "", 0, err
	}
	if r.beforeCommit != nil {
		if err := r.beforeCommit(ctx, tx); err != nil {
			return "", 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", 0, fmt.Errorf("backup: commit the load: %w", err)
	}
	return xid, revoked, nil
}

// checkForeignKeys checks every business foreign key (MATCH SIMPLE: rows
// with a NULL in the key are not checked), since the load ran without
// constraint triggers.
func checkForeignKeys(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `
		SELECT c.conname::text, quote_ident(cn.nspname) || '.' || quote_ident(cr.relname),
		       quote_ident(pn.nspname) || '.' || quote_ident(pr.relname),
		       array(SELECT quote_ident(a.attname) FROM unnest(c.conkey) WITH ORDINALITY k(n, i)
		             JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.n ORDER BY k.i),
		       array(SELECT quote_ident(a.attname) FROM unnest(c.confkey) WITH ORDINALITY k(n, i)
		             JOIN pg_attribute a ON a.attrelid = c.confrelid AND a.attnum = k.n ORDER BY k.i)
		FROM pg_constraint c
		JOIN pg_class cr ON cr.oid = c.conrelid JOIN pg_namespace cn ON cn.oid = cr.relnamespace
		JOIN pg_class pr ON pr.oid = c.confrelid JOIN pg_namespace pn ON pn.oid = pr.relnamespace
		WHERE c.contype = 'f' AND cn.nspname = 'business' ORDER BY 1`)
	if err != nil {
		return fmt.Errorf("backup: list foreign keys: %w", err)
	}
	type fk struct {
		name, child, parent string
		cols, refs          []string
	}
	fks, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (fk, error) {
		var f fk
		return f, r.Scan(&f.name, &f.child, &f.parent, &f.cols, &f.refs)
	})
	if err != nil {
		return fmt.Errorf("backup: list foreign keys: %w", err)
	}
	for _, f := range fks {
		if len(f.cols) == 0 || len(f.cols) != len(f.refs) {
			return fmt.Errorf("backup: foreign key %s has an unexpected shape", f.name)
		}
		notNull, match := make([]string, len(f.cols)), make([]string, len(f.cols))
		for i := range f.cols {
			notNull[i] = "c." + f.cols[i] + " IS NOT NULL"
			match[i] = "p." + f.refs[i] + " = c." + f.cols[i]
		}
		var broken int64
		q := "SELECT count(*) FROM " + f.child + " c WHERE " + strings.Join(notNull, " AND ") +
			" AND NOT EXISTS (SELECT 1 FROM " + f.parent + " p WHERE " + strings.Join(match, " AND ") + ")"
		if err := tx.QueryRow(ctx, q).Scan(&broken); err != nil {
			return fmt.Errorf("backup: check foreign key %s: %w", f.name, err)
		}
		if broken > 0 {
			return fmt.Errorf("backup: %d rows break foreign key %s", broken, f.name)
		}
	}
	return nil
}

type verified struct {
	tables  int
	rows    int64
	frozen  bool
	sameTxn bool
}

// verifyRestored reads the committed target: migration history (with
// checksums), row counts, the freeze, no live lease, and, when xid is set,
// that every business row and the freeze were written by that transaction.
func verifyRestored(ctx context.Context, conn *pgx.Conn, m Manifest, history []MigrationRecord, xid string) (verified, error) {
	var v verified
	rows, err := conn.Query(ctx, "SELECT version, sha256 FROM gk_meta.schema_migration ORDER BY version")
	if err != nil {
		return v, fmt.Errorf("backup: restored migration history: %w", err)
	}
	applied, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (MigrationRecord, error) {
		var m MigrationRecord
		return m, r.Scan(&m.Version, &m.SHA256)
	})
	if err != nil || !slices.Equal(applied, history) {
		return v, fmt.Errorf("backup: the restored migration history differs from the backup's (%v)", err)
	}
	err = ownerTx(ctx, conn, true, func(tx pgx.Tx) error {
		counts, err := countRows(ctx, tx)
		if err != nil {
			return err
		}
		if !maps.Equal(counts, m.Rows) {
			for t, want := range m.Rows {
				if counts[t] != want {
					return fmt.Errorf("backup: business.%s holds %d rows, the manifest %d", t, counts[t], want)
				}
			}
			return fmt.Errorf("backup: %d business tables restored, the manifest lists %d", len(counts), len(m.Rows))
		}
		for _, n := range counts {
			v.rows += n
		}
		v.tables = len(counts)
		var reason *string
		var freezeXID string
		if err := tx.QueryRow(ctx, "SELECT frozen, reason, xmin::text FROM business.recovery_freeze WHERE singleton").
			Scan(&v.frozen, &reason, &freezeXID); err != nil {
			return fmt.Errorf("backup: read the freeze: %w", err)
		}
		if !v.frozen || reason == nil || !strings.HasPrefix(*reason, freezeReasonPrefix) {
			return errors.New("backup: the restored database is not frozen by the restore")
		}
		var live int64
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM business.job WHERE state = 'running' AND lease_until > now()").Scan(&live); err != nil {
			return err
		}
		if xid == "" {
			return nil
		}
		if live > 0 {
			return fmt.Errorf("backup: %d restored jobs still hold a lease", live)
		}
		if freezeXID != xid {
			return errors.New("backup: the freeze was not committed by the restore transaction")
		}
		for _, t := range slices.Sorted(maps.Keys(counts)) {
			var other int64
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM business."+pgx.Identifier{t}.Sanitize()+" WHERE xmin::text <> $1", xid).Scan(&other); err != nil {
				return err
			}
			if other > 0 {
				return fmt.Errorf("backup: %d rows of business.%s were not written by the restore transaction", other, t)
			}
		}
		v.sameTxn = true
		return nil
	})
	return v, err
}

// dataScript runs pg_restore offline (archive on stdin, the data-only text
// script on stdout, no database connection) and hands the script to
// consume as it is produced.
func dataScript(ctx context.Context, tool string, archive []byte, consume func(io.Reader) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, "--data-only", "--exit-on-error", "--file=-")
	cmd.Env = toolEnv("", "gk-backup-restore")
	cmd.Stdin = bytes.NewReader(archive)
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, n: 8 << 10}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("backup: pg_restore: %w", err)
	}
	cerr := consume(stdout)
	if cerr != nil {
		cancel() // stop pg_restore; its error would only hide ours
		_ = cmd.Wait()
		return cerr
	}
	if _, err := io.Copy(io.Discard, stdout); err != nil {
		_ = cmd.Wait()
		return err
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("backup: pg_restore: %w: %s", err, toolError(stderr.String(), ""))
	}
	return nil
}

// safeError keeps errors free of row data: PostgreSQL's data-exception and
// integrity-violation messages (SQLSTATE classes 22 and 23) can quote
// values, so for those only the code, table and constraint remain.
func safeError(err error) error {
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) {
		return err
	}
	if class := pgErr.Code[:2]; class != "22" && class != "23" {
		return err
	}
	msg := "backup: PostgreSQL refused the data (SQLSTATE " + pgErr.Code
	if pgErr.TableName != "" {
		msg += ", table " + pgErr.TableName
	}
	if pgErr.ConstraintName != "" {
		msg += ", constraint " + pgErr.ConstraintName
	}
	if pgErr.ColumnName != "" {
		msg += ", column " + pgErr.ColumnName
	}
	return errors.New(msg + ")")
}
