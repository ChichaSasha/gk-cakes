//go:build integration

package dbtest

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
	"github.com/jackc/pgx/v5"
)

// templateMaxAge is how old another run's template database must be before
// a new run drops it. Templates live for one test binary; the CI test step
// times out after 25 minutes, so two hours leaves a wide margin for
// concurrent runs from other worktrees sharing a local cluster.
const templateMaxAge = 2 * time.Hour

// templateName is gk_tpl_<unix seconds>_<label>_<random>.
var templateName = regexp.MustCompile(`^gk_tpl_([0-9]+)_[a-z0-9]+_[0-9a-f]+$`)

var templates struct {
	sync.Mutex
	byTarget map[string]string
}

// NewMigrated creates a database with the embedded migrations applied up to
// and including target (all when target is empty), owned by the test
// migrator, and drops it at cleanup.
//
// The first call for a target in a test binary migrates a template database
// once; every call then copies it with CREATE DATABASE ... TEMPLATE, which is
// far faster than migrating each time. A copy has the same schema, grants,
// rows and migration history as a database migrated a few minutes earlier
// in the same run. Tests about migration behaviour itself (ordering,
// backfills, upgrades from an older version) must keep using New and the
// migrate package, or migrate further from a NewMigrated older target.
func NewMigrated(t *testing.T, target string) *DB {
	t.Helper()
	return create(t, migratedTemplate(t, target))
}

// migratedTemplate returns this binary's template database for target,
// building it on first use.
func migratedTemplate(t *testing.T, target string) string {
	t.Helper()
	templates.Lock()
	defer templates.Unlock()
	if name, ok := templates.byTarget[target]; ok {
		return name
	}
	adminURL := adminURLFromEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	admin, err := pgx.Connect(ctx, adminURL.String())
	if err != nil {
		t.Fatalf("connect as %s: %v", adminURL.User.Username(), err)
	}
	defer func() { _ = admin.Close(context.Background()) }()
	sweepTemplates(ctx, t, admin, time.Now())
	if _, err := admin.Exec(ctx, "CREATE ROLE "+migratorRole+" LOGIN CREATEROLE PASSWORD '"+migratorRole+"'"); err != nil && !duplicate(err) {
		t.Fatalf("create %s: %v", migratorRole, err)
	}

	label := "all"
	if target != "" {
		if len(target) < 4 {
			t.Fatalf("unknown target migration %q", target)
		}
		label = target[:4]
	}
	name := fmt.Sprintf("gk_tpl_%d_%s_%s", time.Now().Unix(), label, randomHex(3))
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name+" OWNER "+migratorRole); err != nil {
		t.Fatalf("create template %s: %v", name, err)
	}
	if err := migrateTemplate(ctx, adminURL, name, target); err != nil {
		dropTemplate(ctx, admin, name)
		t.Fatalf("migrate template %s: %v", name, err)
	}
	// No one may connect to the template any more, so copies never wait for
	// (or fail on) another session using it.
	if _, err := admin.Exec(ctx, "ALTER DATABASE "+name+" WITH IS_TEMPLATE true ALLOW_CONNECTIONS false"); err != nil {
		dropTemplate(ctx, admin, name)
		t.Fatalf("mark template %s: %v", name, err)
	}
	if templates.byTarget == nil {
		templates.byTarget = map[string]string{}
	}
	templates.byTarget[target] = name
	return name
}

// migrateTemplate applies the migrations as the test migrator, as New plus
// migrate.RunTo would, and closes the session before the copy.
func migrateTemplate(ctx context.Context, adminURL *url.URL, name, target string) error {
	u := *adminURL
	u.User = url.UserPassword(migratorRole, migratorRole)
	u.Path = "/" + name
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	_, err = migrate.RunTo(ctx, conn, migrations.FS, target)
	return err
}

// sweepTemplates drops template databases older than templateMaxAge, left
// behind by earlier test binaries (each binary keeps its templates until it
// exits). Failures only log: a sweep never fails a test.
func sweepTemplates(ctx context.Context, t *testing.T, admin *pgx.Conn, now time.Time) {
	t.Helper()
	rows, err := admin.Query(ctx, "SELECT datname FROM pg_catalog.pg_database WHERE datname LIKE 'gk\\_tpl\\_%'")
	if err != nil {
		t.Logf("list template databases: %v", err)
		return
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Logf("list template databases: %v", err)
		return
	}
	for _, name := range names {
		if stale(name, now) {
			dropTemplate(ctx, admin, name)
		}
	}
}

// stale reports whether name is a template database created more than
// templateMaxAge before now. Names that do not match are never stale.
func stale(name string, now time.Time) bool {
	m := templateName.FindStringSubmatch(name)
	if m == nil {
		return false
	}
	created, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return false
	}
	return now.Sub(time.Unix(created, 0)) > templateMaxAge
}

func dropTemplate(ctx context.Context, admin *pgx.Conn, name string) {
	ident := pgx.Identifier{name}.Sanitize()
	_, _ = admin.Exec(ctx, "ALTER DATABASE "+ident+" WITH IS_TEMPLATE false")
	_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+ident+" WITH (FORCE)")
}
