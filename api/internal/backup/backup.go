// Package backup takes the daily independent database backups and verifies
// that they restore (GK-034 Milestone B, contracts K11: independent daily
// restore points kept 30 days, separate from current order records).
//
// A backup is a pg_dump archive of the business schema and the migration
// history, taken by the read-only backup login, plus a manifest: the schema
// version, the applied migrations, and the row count of every business table,
// all read in the same database snapshot as the dump. Both are sealed with
// AES-256-GCM (bound to their object names) before they leave the process,
// and stored in B2 under database/<environment>/<yyyymmdd-hhmmss>z/ (UTC). The
// manifest is written last, so a backup without one is incomplete and
// ignored. Retention (30 days) is a B2 lifecycle rule on the database/
// prefix; this code never deletes.
package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/jackc/pgx/v5"
)

// Store is where backups live (recovery.B2 with the "database/" area).
type Store interface {
	Put(ctx context.Context, name string, data []byte) error
	Get(ctx context.Context, name string) ([]byte, error)
	List(ctx context.Context, prefix string) ([]string, error)
}

// Sealer encrypts and authenticates objects (recovery.Sealer).
type Sealer interface {
	Seal(name string, plaintext []byte) ([]byte, error)
	Open(name string, data []byte) ([]byte, error)
}

// DefaultMaxBytes bounds a dump (44 MiB): it is held in memory for sealing,
// and the sealed object (base64 in JSON, about 4/3 larger) must stay within
// what the store reads back (recovery.MaxObjectBytes, 64 MiB). Streaming
// encryption replaces this limit when the database grows.
const DefaultMaxBytes = 44 << 20

// Manifest describes one backup. It holds no business data: names, versions,
// counts and hashes only.
type Manifest struct {
	V             int              `json:"v"`
	Environment   string           `json:"environment"`
	CreatedAt     time.Time        `json:"created_at"`
	SchemaVersion string           `json:"schema_version"`
	Migrations    []string         `json:"migrations"`
	Dump          string           `json:"dump"`
	DumpSHA256    string           `json:"dump_sha256"`
	DumpBytes     int              `json:"dump_bytes"`
	Rows          map[string]int64 `json:"rows"`
	PgDump        string           `json:"pg_dump"`
	ServerVersion string           `json:"server_version"`
}

// Root is the store prefix of an environment's backups.
func Root(env string) string { return "database/" + env + "/" }

// Dumper takes one backup.
type Dumper struct {
	Env      string
	DB       string // the backup login's connection URL (never logged)
	PgDump   string // pg_dump executable (default "pg_dump")
	Store    Store
	Sealer   Sealer
	MaxBytes int
	Now      func() time.Time

	beforeDump func(context.Context) // test hook: runs after the counts, before pg_dump
}

// Dump takes a backup and returns its manifest. The dump and the row counts
// come from one exported snapshot, held open until the dump has finished.
func (d *Dumper) Dump(ctx context.Context) (Manifest, error) {
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	maxBytes := d.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	conn, err := pgx.Connect(ctx, d.DB)
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: connect: %w", err)
	}
	defer conn.Close(context.WithoutCancel(ctx))
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	m := Manifest{V: 1, Environment: d.Env, CreatedAt: now().UTC().Truncate(time.Second), Rows: map[string]int64{}}
	var snapshot string
	// This connection sits idle in its transaction while pg_dump runs; a
	// server timeout must not end it (that would fail the backup).
	if _, err := tx.Exec(ctx, "SET LOCAL idle_in_transaction_session_timeout = 0"); err != nil {
		return Manifest{}, fmt.Errorf("backup: session settings: %w", err)
	}
	if err := tx.QueryRow(ctx, "SELECT pg_export_snapshot()").Scan(&snapshot); err != nil {
		return Manifest{}, fmt.Errorf("backup: export snapshot: %w", err)
	}
	if err := tx.QueryRow(ctx, "SHOW server_version").Scan(&m.ServerVersion); err != nil {
		return Manifest{}, fmt.Errorf("backup: server version: %w", err)
	}
	rows, err := tx.Query(ctx, "SELECT version FROM gk_meta.schema_migration ORDER BY version")
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: migration history: %w", err)
	}
	if m.Migrations, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || len(m.Migrations) == 0 {
		return Manifest{}, fmt.Errorf("backup: migration history: %v (%d versions)", err, len(m.Migrations))
	}
	m.SchemaVersion = m.Migrations[len(m.Migrations)-1]
	if m.Rows, err = countRows(ctx, tx); err != nil {
		return Manifest{}, err
	}

	pgDump := d.PgDump
	if pgDump == "" {
		pgDump = "pg_dump"
	}
	if m.PgDump, err = toolVersion(ctx, pgDump); err != nil {
		return Manifest{}, err
	}
	if d.beforeDump != nil {
		d.beforeDump(ctx)
	}
	archive, err := run(ctx, pgDump, d.DB, maxBytes, "--format=custom", "--snapshot="+snapshot,
		"--schema=business", "--schema=gk_meta", "--no-password")
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: pg_dump: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Manifest{}, fmt.Errorf("backup: release snapshot: %w", err)
	}

	stamp := m.CreatedAt.Format(stampLayout)
	m.Dump = Root(d.Env) + stamp + "/dump.sealed"
	sum := sha256.Sum256(archive)
	m.DumpSHA256, m.DumpBytes = hex.EncodeToString(sum[:]), len(archive)
	sealedDump, err := d.Sealer.Seal(m.Dump, archive)
	if err != nil {
		return Manifest{}, err
	}
	if len(sealedDump) > recovery.MaxObjectBytes {
		return Manifest{}, fmt.Errorf("backup: sealed dump is %d bytes, more than the store reads back (%d)", len(sealedDump), recovery.MaxObjectBytes)
	}
	if err := d.Store.Put(ctx, m.Dump, sealedDump); err != nil {
		return Manifest{}, fmt.Errorf("backup: store dump: %w", err)
	}
	body, err := json.Marshal(m)
	if err != nil {
		return Manifest{}, err
	}
	name := manifestName(d.Env, stamp)
	sealedManifest, err := d.Sealer.Seal(name, body)
	if err != nil {
		return Manifest{}, err
	}
	if err := d.Store.Put(ctx, name, sealedManifest); err != nil {
		return Manifest{}, fmt.Errorf("backup: store manifest: %w", err)
	}
	return m, nil
}

// stampLayout names a backup by its UTC time, fixed width and lower case
// (object names are lower case), so names sort by time.
const stampLayout = "20060102-150405z"

func manifestName(env, stamp string) string { return Root(env) + stamp + "/manifest.sealed" }

// countRows counts every business table in the transaction's snapshot.
func countRows(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}) (map[string]int64, error) {
	rows, err := q.Query(ctx, `SELECT c.relname::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'business' AND c.relkind IN ('r', 'p') ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("backup: list tables: %w", err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("backup: list tables: %w", err)
	}
	counts := map[string]int64{}
	for _, t := range tables {
		var n int64
		if err := q.QueryRow(ctx, "SELECT count(*) FROM business."+pgx.Identifier{t}.Sanitize()).Scan(&n); err != nil {
			return nil, fmt.Errorf("backup: count %s: %w", t, err)
		}
		counts[t] = n
	}
	return counts, nil
}

// toolVersion returns "pg_dump (PostgreSQL) 17.x" or similar.
func toolVersion(ctx context.Context, tool string) (string, error) {
	out, err := exec.CommandContext(ctx, tool, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("backup: %s --version: %w", tool, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// run executes a PostgreSQL client tool against dbURL. The password goes in
// PGPASSWORD, never on the command line; the tool gets no other PG* or backup
// variables; stdout is capped at maxBytes; errors keep one line (toolError).
func run(ctx context.Context, tool, dbURL string, maxBytes int, args ...string) ([]byte, error) {
	u, err := url.Parse(dbURL)
	if err != nil {
		return nil, errors.New("invalid database URL")
	}
	password, _ := u.User.Password()
	u.User = url.User(u.User.Username())
	cmd := exec.CommandContext(ctx, tool, append(args, "--dbname="+u.String())...)
	cmd.Env = toolEnv(password, "gk-backup")
	var stdout limitedBuffer
	stdout.max = maxBytes
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &limitedWriter{w: &stderr, n: 8 << 10}
	err = cmd.Run()
	switch {
	case stdout.over:
		return nil, fmt.Errorf("output exceeds %d bytes", maxBytes)
	case err != nil:
		return nil, fmt.Errorf("%w: %s", err, toolError(stderr.String(), password))
	}
	return stdout.Bytes(), nil
}

// toolEnv is the environment for a PostgreSQL client tool: this process's,
// without any PG* (PGHOST, PGHOSTADDR, PGSERVICE...) or backup variables,
// plus the password and an application name.
func toolEnv(password, app string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "PG") || strings.HasPrefix(k, "BACKUP_") || strings.HasPrefix(k, "RESTORE_") {
			continue
		}
		env = append(env, kv)
	}
	if password != "" {
		env = append(env, "PGPASSWORD="+password)
	}
	return append(env, "PGAPPNAME="+app)
}

// toolError keeps the first line that names the error. PostgreSQL's DETAIL,
// CONTEXT, LINE, HINT and "Command was" lines can quote row values, and the
// workflow logs are public, so they are dropped.
func toolError(stderr, password string) string {
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "error:") && !strings.HasPrefix(line, "ERROR:") && !strings.Contains(line, "FATAL:") {
			continue
		}
		if password != "" {
			line = strings.ReplaceAll(line, password, "[redacted]")
		}
		if len(line) > 300 {
			line = line[:300] + "…"
		}
		return line
	}
	return "no error message"
}

// limitedBuffer keeps at most max bytes and records an overflow. The buffer
// is a named field, not embedded: an embedded bytes.Buffer would give it a
// ReadFrom method that io.Copy prefers, bypassing the limit in Write.
type limitedBuffer struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > b.max {
		b.over = true
		return 0, errors.New("output limit reached")
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) Bytes() []byte { return b.buf.Bytes() }

type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		_, _ = l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}

// Latest returns the newest complete backup's manifest.
func Latest(ctx context.Context, store Store, sealer Sealer, env string) (Manifest, error) {
	names, err := store.List(ctx, Root(env))
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: list: %w", err)
	}
	var manifests []string
	for _, n := range names {
		if strings.HasSuffix(n, "/manifest.sealed") {
			manifests = append(manifests, n)
		}
	}
	if len(manifests) == 0 {
		return Manifest{}, errors.New("backup: no complete backup found")
	}
	slices.Sort(manifests) // names start with a fixed-width UTC timestamp
	name := manifests[len(manifests)-1]
	sealed, err := store.Get(ctx, name)
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: read manifest: %w", err)
	}
	body, err := sealer.Open(name, sealed)
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: open manifest %s: %w", name, err)
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil || m.V != 1 {
		return Manifest{}, fmt.Errorf("backup: manifest %s is not readable", name)
	}
	stamp := m.CreatedAt.UTC().Format(stampLayout)
	if m.Environment != env || name != manifestName(env, stamp) || m.Dump != Root(env)+stamp+"/dump.sealed" {
		return Manifest{}, fmt.Errorf("backup: manifest %s does not belong to %s at its own time", name, env)
	}
	return m, nil
}

// Report is the result of a restore check.
type Report struct {
	Manifest Manifest
	Age      time.Duration
	Tables   int
	Rows     int64
}

// Restorer checks that the newest backup restores into a disposable database.
type Restorer struct {
	Env       string
	Target    string // disposable, empty database (loopback only; see config)
	PgRestore string // pg_restore executable (default "pg_restore")
	Store     Store
	Sealer    Sealer
	MaxAge    time.Duration // newest backup must be younger (default 26h)
	Now       func() time.Time
}

// Check restores the newest backup into Target and compares it with its
// manifest: migrations and the row count of every business table.
func (r *Restorer) Check(ctx context.Context) (Report, error) {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	maxAge := r.MaxAge
	if maxAge <= 0 {
		maxAge = 26 * time.Hour
	}
	m, err := Latest(ctx, r.Store, r.Sealer, r.Env)
	if err != nil {
		return Report{}, err
	}
	rep := Report{Manifest: m, Age: now().Sub(m.CreatedAt)}
	if rep.Age < -5*time.Minute {
		// A future-dated backup would be chosen forever and hide stopped backups.
		return rep, fmt.Errorf("backup: the newest backup is dated %s in the future", (-rep.Age).Round(time.Minute))
	}
	if rep.Age > maxAge {
		return rep, fmt.Errorf("backup: the newest backup is %s old (more than %s): backups have stopped", rep.Age.Round(time.Minute), maxAge)
	}
	sealed, err := r.Store.Get(ctx, m.Dump)
	if err != nil {
		return rep, fmt.Errorf("backup: read dump: %w", err)
	}
	archive, err := r.Sealer.Open(m.Dump, sealed)
	if err != nil {
		return rep, fmt.Errorf("backup: open dump: %w", err)
	}
	sum := sha256.Sum256(archive)
	if hex.EncodeToString(sum[:]) != m.DumpSHA256 || len(archive) != m.DumpBytes {
		return rep, errors.New("backup: the dump does not match its manifest")
	}
	pgRestore := r.PgRestore
	if pgRestore == "" {
		pgRestore = "pg_restore"
	}
	if err := restore(ctx, pgRestore, r.Target, archive); err != nil {
		return rep, fmt.Errorf("backup: pg_restore: %w", err)
	}
	conn, err := pgx.Connect(ctx, r.Target)
	if err != nil {
		return rep, fmt.Errorf("backup: connect to the restore target: %w", err)
	}
	defer conn.Close(context.WithoutCancel(ctx))
	vrows, err := conn.Query(ctx, "SELECT version FROM gk_meta.schema_migration ORDER BY version")
	if err != nil {
		return rep, fmt.Errorf("backup: restored migration history: %w", err)
	}
	versions, err := pgx.CollectRows(vrows, pgx.RowTo[string])
	if err != nil || !slices.Equal(versions, m.Migrations) {
		return rep, fmt.Errorf("backup: restored migrations %v, manifest %v (%v)", versions, m.Migrations, err)
	}
	counts, err := countRows(ctx, conn)
	if err != nil {
		return rep, err
	}
	if len(counts) != len(m.Rows) {
		return rep, fmt.Errorf("backup: restored %d business tables, manifest lists %d", len(counts), len(m.Rows))
	}
	for t, want := range m.Rows {
		if got, ok := counts[t]; !ok || got != want {
			return rep, fmt.Errorf("backup: table %s restored %d rows, manifest %d", t, got, want)
		}
		rep.Rows += want
	}
	rep.Tables = len(counts)
	return rep, nil
}

// restore loads archive into target without owners or privileges (the
// disposable target has none of the production roles), in one transaction.
func restore(ctx context.Context, tool, target string, archive []byte) error {
	u, err := url.Parse(target)
	if err != nil {
		return errors.New("invalid restore URL")
	}
	password, _ := u.User.Password()
	u.User = url.User(u.User.Username())
	cmd := exec.CommandContext(ctx, tool, "--no-owner", "--no-privileges", "--exit-on-error", "--single-transaction",
		"--no-password", "--dbname="+u.String())
	cmd.Env = toolEnv(password, "gk-backup-restore")
	cmd.Stdin = bytes.NewReader(archive)
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, n: 8 << 10}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, toolError(stderr.String(), password))
	}
	return nil
}
