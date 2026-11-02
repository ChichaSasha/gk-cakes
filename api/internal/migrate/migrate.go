// Package migrate applies embedded SQL migrations once, in order, under a
// database advisory lock so concurrent runs serialize.
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

// Run applies every pending migration and returns the versions applied.
func Run(ctx context.Context, conn *pgx.Conn, fsys fs.FS) ([]string, error) {
	return RunTo(ctx, conn, fsys, "")
}

// RunTo applies pending migrations up to and including target (all when
// target is empty). It refuses to run when the database contains a version
// this build does not know or an applied file has changed.
func RunTo(ctx context.Context, conn *pgx.Conn, fsys fs.FS, target string) (applied []string, err error) {
	migrations, err := Load(fsys)
	if err != nil {
		return nil, err
	}
	if target != "" && !containsVersion(migrations, target) {
		return nil, fmt.Errorf("unknown target migration %q", target)
	}

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		if _, unlockErr := conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", lockKey); unlockErr != nil && err == nil {
			err = fmt.Errorf("release migration lock: %w", unlockErr)
		}
	}()

	if _, err := conn.Exec(ctx, metaDDL); err != nil {
		return nil, fmt.Errorf("prepare migration history: %w", err)
	}
	done, err := appliedVersions(ctx, conn)
	if err != nil {
		return nil, err
	}
	for version := range done {
		if !containsVersion(migrations, version) {
			return nil, fmt.Errorf("database has migration %s unknown to this build; refusing to run an older migration set", version)
		}
	}

	for _, m := range migrations {
		if sum, ok := done[m.Version]; ok {
			if sum != m.SHA256 {
				return applied, fmt.Errorf("migration %s was modified after it was applied", m.Version)
			}
		} else {
			if err := apply(ctx, conn, m); err != nil {
				return applied, err
			}
			applied = append(applied, m.Version)
		}
		if m.Version == target {
			break
		}
	}
	return applied, nil
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
