// Package migrate applies embedded SQL migrations once, in order, under a
// database advisory lock so concurrent runs serialize. An older build on a
// newer database (a rollback) verifies its history and applies nothing.
package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// lockKey identifies the migration advisory lock ("gk-migr").
const lockKey int64 = 0x676b2d6d69677200

var fileName = regexp.MustCompile(`^[0-9]{4}_[a-z0-9_]+\.sql$`)

const metaDDL = `
CREATE SCHEMA IF NOT EXISTS gk_meta;
REVOKE ALL ON SCHEMA gk_meta FROM PUBLIC;
CREATE TABLE IF NOT EXISTS gk_meta.schema_migration (
	version    text PRIMARY KEY,
	sha256     char(64) NOT NULL,
	applied_at timestamptz NOT NULL DEFAULT now()
);`

// Migration is one versioned SQL file.
type Migration struct {
	Version string
	SHA256  string
	SQL     string
}

// Load reads and orders the migrations in fsys.
func Load(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var out []Migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if !fileName.MatchString(e.Name()) {
			return nil, fmt.Errorf("migration %q must be named NNNN_lower_snake.sql", e.Name())
		}
		body, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", e.Name(), err)
		}
		sum := sha256.Sum256(body)
		out = append(out, Migration{
			Version: strings.TrimSuffix(e.Name(), ".sql"),
			SHA256:  hex.EncodeToString(sum[:]),
			SQL:     string(body),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i := 1; i < len(out); i++ {
		if out[i].Version[:4] == out[i-1].Version[:4] {
			return nil, fmt.Errorf("migrations %s and %s share a number", out[i-1].Version, out[i].Version)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no migrations found")
	}
	return out, nil
}

// Report describes one migration run.
type Report struct {
	Applied []string // versions applied by this run, in order
	Ahead   []string // applied versions this build does not know (the database is newer)
}

// Run applies every pending migration and returns the versions applied.
func Run(ctx context.Context, conn *pgx.Conn, fsys fs.FS) ([]string, error) {
	r, err := Apply(ctx, conn, fsys, "")
	return r.Applied, err
}

// RunTo applies pending migrations up to and including target (all when
// target is empty) and returns the versions applied.
func RunTo(ctx context.Context, conn *pgx.Conn, fsys fs.FS, target string) ([]string, error) {
	r, err := Apply(ctx, conn, fsys, target)
	return r.Applied, err
}

// Apply applies pending migrations up to and including target (all when
// target is empty) under the advisory lock. It validates the whole recorded
// history first and refuses to run when an applied file has changed or when
// the histories diverged. A database that is ahead of this build — every
// migration of this build applied unchanged, plus newer versions this build
// does not know — is the rollback case (an older release deploying after a
// newer one): nothing is applied and the newer versions are reported in
// Report.Ahead. Migrations are forward-only, so a rollback never removes
// schema or data.
func Apply(ctx context.Context, conn *pgx.Conn, fsys fs.FS, target string) (report Report, err error) {
	migrations, err := Load(fsys)
	if err != nil {
		return Report{}, err
	}
	if target != "" && !containsVersion(migrations, target) {
		return Report{}, fmt.Errorf("unknown target migration %q", target)
	}

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return Report{}, fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		if _, unlockErr := conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", lockKey); unlockErr != nil && err == nil {
			err = fmt.Errorf("release migration lock: %w", unlockErr)
		}
	}()

	if _, err := conn.Exec(ctx, metaDDL); err != nil {
		return Report{}, fmt.Errorf("prepare migration history: %w", err)
	}
	done, err := appliedVersions(ctx, conn)
	if err != nil {
		return Report{}, err
	}
	// Validate the whole history before changing anything.
	latest := ""
	for _, m := range migrations {
		if sum, ok := done[m.Version]; ok {
			if sum != m.SHA256 {
				return Report{}, fmt.Errorf("migration %s was modified after it was applied", m.Version)
			}
			latest = m.Version
		}
	}
	var ahead []string
	for version := range done {
		if !containsVersion(migrations, version) {
			ahead = append(ahead, version)
		}
	}
	sort.Strings(ahead)
	if len(ahead) > 0 {
		newest := migrations[len(migrations)-1].Version
		for _, m := range migrations {
			if _, ok := done[m.Version]; !ok {
				return Report{}, fmt.Errorf("histories diverged: database has %v unknown to this build, but this build's %s is not applied", ahead, m.Version)
			}
		}
		if ahead[0] < newest {
			return Report{}, fmt.Errorf("histories diverged: database migration %s is unknown to this build and older than its newest, %s", ahead[0], newest)
		}
		return Report{Ahead: ahead}, nil
	}
	for _, m := range migrations {
		if _, ok := done[m.Version]; !ok && m.Version < latest {
			return Report{}, fmt.Errorf("migration %s is numbered below already-applied %s; renumber it after the latest", m.Version, latest)
		}
	}

	for _, m := range migrations {
		if _, ok := done[m.Version]; !ok {
			if err := apply(ctx, conn, m); err != nil {
				return report, err
			}
			report.Applied = append(report.Applied, m.Version)
		}
		if m.Version == target {
			break
		}
	}
	return report, nil
}

func apply(ctx context.Context, conn *pgx.Conn, m Migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("apply %s: begin: %w", m.Version, err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	steps := []struct {
		sql  string
		args []any
	}{
		{"SET LOCAL lock_timeout = '5s'", nil},
		{m.SQL, nil},
		{"RESET ROLE", nil},
		{"INSERT INTO gk_meta.schema_migration (version, sha256) VALUES ($1, $2)", []any{m.Version, m.SHA256}},
	}
	for _, s := range steps {
		if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
			return fmt.Errorf("apply %s: %w", m.Version, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("apply %s: commit: %w", m.Version, err)
	}
	return nil
}

func appliedVersions(ctx context.Context, conn *pgx.Conn) (map[string]string, error) {
	rows, err := conn.Query(ctx, "SELECT version, sha256 FROM gk_meta.schema_migration")
	if err != nil {
		return nil, fmt.Errorf("read migration history: %w", err)
	}
	done := map[string]string{}
	for rows.Next() {
		var version, sum string
		if err := rows.Scan(&version, &sum); err != nil {
			return nil, fmt.Errorf("read migration history: %w", err)
		}
		done[version] = sum
	}
	return done, rows.Err()
}

func containsVersion(ms []Migration, version string) bool {
	for _, m := range ms {
		if m.Version == version {
			return true
		}
	}
	return false
}
