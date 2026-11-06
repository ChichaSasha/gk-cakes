//go:build integration

package dbtest

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
	"github.com/jackc/pgx/v5"
)

func TestStale(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	old := now.Add(-templateMaxAge - time.Second).Unix()
	fresh := now.Add(-templateMaxAge + time.Minute).Unix()
	for name, want := range map[string]bool{
		"gk_tpl_" + itoa(old) + "_all_a1b2c3":   true,
		"gk_tpl_" + itoa(old) + "_0014_a1b2c3":  true,
		"gk_tpl_" + itoa(fresh) + "_all_a1b2c3": false,
		"gk_tpl_x_all_a1b2c3":                   false,
		"gk_test_0123456789ab":                  false,
		"gk_tpl_" + itoa(old) + "_all":          false,
		"postgres":                              false,
	} {
		if got := stale(name, now); got != want {
			t.Errorf("stale(%q) = %v, want %v", name, got, want)
		}
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// A copy has the whole migration history and the template is reused, not
// rebuilt, within one test binary; copies are independent databases.
func TestNewMigrated(t *testing.T) {
	ctx := context.Background()
	a := NewMigrated(t, "")
	b := NewMigrated(t, "")
	if a.Name == b.Name {
		t.Fatal("copies share a database")
	}
	tpl := migratedTemplate(t, "")
	all, err := migrate.Load(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []*DB{a, b} {
		var applied int
		if err := d.MigratorConn(t).QueryRow(ctx, "SELECT count(*) FROM gk_meta.schema_migration").Scan(&applied); err != nil {
			t.Fatal(err)
		}
		if applied != len(all) {
			t.Fatalf("%s has %d migrations applied, want %d", d.Name, applied, len(all))
		}
	}
	if _, err := a.AdminConn(t).Exec(ctx, "CREATE TABLE public.only_in_a (id int)"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := b.AdminConn(t).QueryRow(ctx, "SELECT count(*) FROM pg_catalog.pg_tables WHERE tablename = 'only_in_a'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("copies are not independent: %d %v", n, err)
	}
	var isTemplate, allowConn bool
	if err := a.AdminConn(t).QueryRow(ctx, "SELECT datistemplate, datallowconn FROM pg_catalog.pg_database WHERE datname = $1", tpl).
		Scan(&isTemplate, &allowConn); err != nil || !isTemplate || allowConn {
		t.Fatalf("template %s: is_template=%v allow_connections=%v %v", tpl, isTemplate, allowConn, err)
	}
	if again := migratedTemplate(t, ""); again != tpl {
		t.Fatalf("template rebuilt: %s then %s", tpl, again)
	}

	older := NewMigrated(t, "0002_job_gateway")
	if err := older.MigratorConn(t).QueryRow(ctx, "SELECT count(*) FROM gk_meta.schema_migration").Scan(&n); err != nil || n != 2 {
		t.Fatalf("target 0002: %d migrations applied, %v", n, err)
	}
}

// fingerprint describes what tests can observe of a migrated database:
// relations, columns and defaults, constraints, triggers, functions,
// schemas, owners and privileges (including default privileges and the
// database's own), extensions, sequence positions and the seeded rows.
const fingerprint = `
SELECT 'rel ' || n.nspname || '.' || c.relname || ' ' || c.relkind::text || ' ' || pg_get_userbyid(c.relowner) || ' ' || coalesce(c.relacl::text, '')
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg\_toast%' AND n.nspname NOT LIKE 'pg\_temp%'
UNION ALL
SELECT 'col ' || a.attrelid::regclass::text || '.' || a.attname || ' ' || format_type(a.atttypid, a.atttypmod) || ' ' || a.attnotnull
       || ' ' || coalesce(pg_get_expr(d.adbin, d.adrelid), '') || ' ' || coalesce(a.attacl::text, '')
  FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
  LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
 WHERE a.attnum > 0 AND NOT a.attisdropped AND n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg\_toast%'
UNION ALL
SELECT 'con ' || conrelid::regclass::text || ' ' || conname || ' ' || pg_get_constraintdef(oid) FROM pg_constraint
 WHERE connamespace NOT IN ('pg_catalog'::regnamespace, 'information_schema'::regnamespace)
UNION ALL
SELECT 'trg ' || tgrelid::regclass::text || ' ' || pg_get_triggerdef(oid) FROM pg_trigger WHERE NOT tgisinternal
UNION ALL
SELECT 'fn ' || p.oid::regprocedure::text || ' ' || pg_get_userbyid(p.proowner) || ' ' || p.prosecdef || ' '
       || coalesce(p.proacl::text, '') || ' ' || coalesce(p.proconfig::text, '') || ' ' || md5(p.prosrc)
  FROM pg_proc p WHERE p.pronamespace NOT IN ('pg_catalog'::regnamespace, 'information_schema'::regnamespace)
UNION ALL
SELECT 'nsp ' || nspname || ' ' || pg_get_userbyid(nspowner) || ' ' || coalesce(nspacl::text, '') FROM pg_namespace
 WHERE nspname NOT LIKE 'pg\_%' AND nspname <> 'information_schema'
UNION ALL
SELECT 'defacl ' || defaclrole::regrole::text || ' ' || coalesce(defaclnamespace::regnamespace::text, '-') || ' ' || defaclobjtype::text || ' ' || defaclacl::text
  FROM pg_default_acl
UNION ALL
SELECT 'db ' || pg_get_userbyid(datdba) || ' ' || coalesce(datacl::text, '') FROM pg_database WHERE datname = current_database()
UNION ALL
SELECT 'dbset ' || coalesce(setrole::regrole::text, '-') || ' ' || setconfig::text FROM pg_db_role_setting
 WHERE setdatabase = (SELECT oid FROM pg_database WHERE datname = current_database())
UNION ALL
SELECT 'ext ' || extname || ' ' || extversion FROM pg_extension
UNION ALL
SELECT 'seq ' || schemaname || '.' || sequencename || ' ' || coalesce(last_value::text, '') FROM pg_sequences
UNION ALL
SELECT 'mig ' || version || ' ' || sha256 FROM gk_meta.schema_migration
UNION ALL
SELECT 'freeze ' || (to_jsonb(f) - 'changed_at')::text FROM business.recovery_freeze f
ORDER BY 1`

func fingerprintOf(t *testing.T, d *DB) string {
	t.Helper()
	rows, err := d.AdminConn(t).Query(context.Background(), fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

// A copy of the template is indistinguishable from a database migrated
// directly, as far as the catalog and seeded rows show.
func TestTemplateCopyMatchesFreshMigration(t *testing.T) {
	fresh := New(t)
	if _, err := migrate.Run(context.Background(), fresh.MigratorConn(t), migrations.FS); err != nil {
		t.Fatal(err)
	}
	cp := NewMigrated(t, "")
	want, got := fingerprintOf(t, fresh), fingerprintOf(t, cp)
	if len(want) < 1000 {
		t.Fatalf("fingerprint is implausibly small:\n%s", want)
	}
	if got != want {
		w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
		for i := 0; i < len(w) || i < len(g); i++ {
			var wl, gl string
			if i < len(w) {
				wl = w[i]
			}
			if i < len(g) {
				gl = g[i]
			}
			if wl != gl {
				t.Fatalf("copy differs from a fresh migration at line %d:\nfresh: %s\ncopy:  %s", i+1, wl, gl)
			}
		}
	}
	// The fingerprint notices a privilege difference.
	if _, err := cp.AdminConn(t).Exec(context.Background(), "GRANT SELECT ON gk_meta.schema_migration TO PUBLIC"); err != nil {
		t.Fatal(err)
	}
	if fingerprintOf(t, cp) == want {
		t.Fatal("the fingerprint does not see a changed grant")
	}
}

// The sweep drops templates older than templateMaxAge and keeps newer ones
// and databases that are not templates of this scheme.
func TestSweepTemplates(t *testing.T) {
	ctx := context.Background()
	admin := New(t).AdminConn(t)
	now := time.Now()
	old := fmt.Sprintf("gk_tpl_%d_all_%s", now.Add(-templateMaxAge-time.Hour).Unix(), randomHex(3))
	fresh := fmt.Sprintf("gk_tpl_%d_all_%s", now.Unix(), randomHex(3))
	other := "gk_tplx_" + randomHex(3)
	for _, name := range []string{old, fresh, other} {
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, "ALTER DATABASE "+name+" WITH IS_TEMPLATE true ALLOW_CONNECTIONS false"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { dropTemplate(ctx, admin, name) })
	}
	sweepTemplates(ctx, t, admin, now)
	for name, want := range map[string]bool{old: false, fresh: true, other: true} {
		var exists bool
		if err := admin.QueryRow(ctx, "SELECT EXISTS (SELECT FROM pg_database WHERE datname = $1)", name).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != want {
			t.Errorf("%s exists = %v after the sweep, want %v", name, exists, want)
		}
	}
}
