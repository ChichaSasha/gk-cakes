//go:build integration

package dbtest

import (
	"context"
	"strconv"
	"testing"
	"time"
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
	var applied, latest int
	for _, d := range []*DB{a, b} {
		if err := d.MigratorConn(t).QueryRow(ctx, "SELECT count(*) FROM gk_meta.schema_migration").Scan(&applied); err != nil {
			t.Fatal(err)
		}
		if applied == 0 || (latest != 0 && applied != latest) {
			t.Fatalf("%s has %d migrations applied", d.Name, applied)
		}
		latest = applied
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
