//go:build integration

package migrate_test

import (
	"context"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
	"github.com/jackc/pgx/v5"
)

func versions(t *testing.T, fsys fs.FS) []string {
	t.Helper()
	ms, err := migrate.Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Version
	}
	return out
}

func recorded(t *testing.T, conn *pgx.Conn) []string {
	t.Helper()
	rows, err := conn.Query(context.Background(), "SELECT version FROM gk_meta.schema_migration ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestMigrateEmptyDatabaseThenNoOp(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t).MigratorConn(t)
	want := versions(t, migrations.FS)

	applied, err := migrate.Run(ctx, conn, migrations.FS)
	if err != nil {
		t.Fatalf("migrate empty database: %v", err)
	}
	if strings.Join(applied, ",") != strings.Join(want, ",") {
		t.Fatalf("applied %v, want %v", applied, want)
	}
	again, err := migrate.Run(ctx, conn, migrations.FS)
	if err != nil || len(again) != 0 {
		t.Fatalf("second run applied %v, err %v; want no-op", again, err)
	}
	if got := recorded(t, conn); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("history %v, want %v", got, want)
	}
}

// TestMigratePreviousSchemaToLatest upgrades a database at the previous
// release's schema (all but the newest migration) to the current one.
func TestMigratePreviousSchemaToLatest(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t).MigratorConn(t)
	all := versions(t, migrations.FS)
	if len(all) < 2 {
		t.Skip("previous-schema fixture needs at least two migrations")
	}
	previous := all[len(all)-2]
	if _, err := migrate.RunTo(ctx, conn, migrations.FS, previous); err != nil {
		t.Fatalf("migrate to previous schema %s: %v", previous, err)
	}
	applied, err := migrate.Run(ctx, conn, migrations.FS)
	if err != nil {
		t.Fatalf("upgrade from %s: %v", previous, err)
	}
	if len(applied) != 1 || applied[0] != all[len(all)-1] {
		t.Fatalf("upgrade applied %v, want [%s]", applied, all[len(all)-1])
	}
}

func TestConcurrentMigrationsSerialize(t *testing.T) {
	ctx := context.Background()
	db := dbtest.New(t)
	conns := []*pgx.Conn{db.MigratorConn(t), db.MigratorConn(t), db.MigratorConn(t)}

	var wg sync.WaitGroup
	results := make([][]string, len(conns))
	errs := make([]error, len(conns))
	for i, c := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = migrate.Run(ctx, c, migrations.FS)
		}()
	}
	wg.Wait()

	total := 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent run %d: %v", i, err)
		}
		total += len(results[i])
	}
	want := versions(t, migrations.FS)
	if total != len(want) {
		t.Fatalf("concurrent runs applied %d migrations in total, want %d (each exactly once)", total, len(want))
	}
	if got := recorded(t, conns[0]); len(got) != len(want) {
		t.Fatalf("history %v, want %v", got, want)
	}
}

func TestMigrateRejectsModifiedAppliedMigration(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t).MigratorConn(t)
	if _, err := migrate.Run(ctx, conn, migrations.FS); err != nil {
		t.Fatal(err)
	}
	modified := fstest.MapFS{}
	for _, v := range versions(t, migrations.FS) {
		body, err := fs.ReadFile(migrations.FS, v+".sql")
		if err != nil {
			t.Fatal(err)
		}
		modified[v+".sql"] = &fstest.MapFile{Data: body}
	}
	first := versions(t, migrations.FS)[0] + ".sql"
	modified[first] = &fstest.MapFile{Data: append(modified[first].Data, []byte("\n-- edited\n")...)}

	_, err := migrate.Run(ctx, conn, modified)
	if err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("modified applied migration: err = %v, want modification error", err)
	}
}

// TestOlderBuildOnNewerDatabaseAppliesNothing is the API rollback path
// (GK-034 AC2): a previous release's gk-migrate runs against a database that
// already has newer migrations. It verifies its own history, applies nothing,
// reports the newer versions and leaves data and history untouched.
func TestOlderBuildOnNewerDatabaseAppliesNothing(t *testing.T) {
	ctx := context.Background()
	db := dbtest.New(t)
	conn := db.MigratorConn(t)
	if _, err := migrate.Run(ctx, conn, migrations.FS); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE gk_owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SELECT business.job_enqueue(gen_random_uuid(), now() + interval '1 day')"); err != nil {
		t.Fatalf("seed a job: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	admin := db.AdminConn(t) // reads business tables the migrator may not
	snapshot := func() string {
		t.Helper()
		var s string
		err := admin.QueryRow(ctx, `SELECT (SELECT string_agg(j::text, ';' ORDER BY id) FROM business.job j) || '|' ||
			(SELECT string_agg(version || ':' || sha256, ';' ORDER BY version) FROM gk_meta.schema_migration)`).Scan(&s)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := snapshot()

	all := versions(t, migrations.FS)
	older := fstest.MapFS{}
	for _, v := range all[:len(all)-1] {
		body, err := fs.ReadFile(migrations.FS, v+".sql")
		if err != nil {
			t.Fatal(err)
		}
		older[v+".sql"] = &fstest.MapFile{Data: body}
	}
	report, err := migrate.Apply(ctx, conn, older, "")
	if err != nil {
		t.Fatalf("older build on newer database: %v", err)
	}
	if len(report.Applied) != 0 || strings.Join(report.Ahead, ",") != all[len(all)-1] {
		t.Fatalf("report = %+v, want nothing applied and ahead [%s]", report, all[len(all)-1])
	}
	if after := snapshot(); after != before {
		t.Fatalf("rollback build changed data or history:\nbefore %s\nafter  %s", before, after)
	}
	// And the newer build still sees a complete, unchanged history.
	if again, err := migrate.Run(ctx, conn, migrations.FS); err != nil || len(again) != 0 {
		t.Fatalf("current build after rollback run: applied %v, err %v; want no-op", again, err)
	}
}

// TestDivergentHistoriesAreRefused: an older build may only run when its
// whole migration set is applied unchanged and every unknown version is newer
// than its own; anything else means the histories diverged.
func TestDivergentHistoriesAreRefused(t *testing.T) {
	file := func(sql string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(sql)} }
	a := file("CREATE TABLE gk_meta.a (id integer);")
	cases := []struct {
		name         string
		database     fstest.MapFS // applied first, by a build with this set
		build        fstest.MapFS // then run
		wantContains string
	}{
		{"build has a pending migration the database skipped",
			fstest.MapFS{"0001_a.sql": a, "0003_c.sql": file("CREATE TABLE gk_meta.c (id integer);")},
			fstest.MapFS{"0001_a.sql": a, "0002_b.sql": file("CREATE TABLE gk_meta.b (id integer);")},
			"diverged"},
		{"build pending and database unknown at the same number",
			fstest.MapFS{"0001_a.sql": a, "0002_x.sql": file("CREATE TABLE gk_meta.x (id integer);"), "0003_c.sql": file("CREATE TABLE gk_meta.c (id integer);")},
			fstest.MapFS{"0001_a.sql": a, "0002_y.sql": file("CREATE TABLE gk_meta.x (id integer);"), "0003_c.sql": file("CREATE TABLE gk_meta.c (id integer);")},
			"diverged"},
		{"whole build applied, but an unknown database migration sorts below its newest",
			fstest.MapFS{"0001_a.sql": a, "0002_x.sql": file("CREATE TABLE gk_meta.x (id integer);"), "0003_c.sql": file("CREATE TABLE gk_meta.c (id integer);")},
			fstest.MapFS{"0001_a.sql": a, "0003_c.sql": file("CREATE TABLE gk_meta.c (id integer);")},
			"not newer than its newest"},
		{"older build whose applied file changed",
			fstest.MapFS{"0001_a.sql": a, "0002_b.sql": file("CREATE TABLE gk_meta.b (id integer);")},
			fstest.MapFS{"0001_a.sql": file("CREATE TABLE gk_meta.a (id integer); -- edited")},
			"modified"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			conn := dbtest.New(t).MigratorConn(t)
			if _, err := migrate.Run(ctx, conn, tc.database); err != nil {
				t.Fatal(err)
			}
			before := recorded(t, conn)
			report, err := migrate.Apply(ctx, conn, tc.build, "")
			if err == nil || !strings.Contains(err.Error(), tc.wantContains) {
				t.Fatalf("report %+v, err %v; want refusal mentioning %q", report, err, tc.wantContains)
			}
			if after := recorded(t, conn); strings.Join(after, ",") != strings.Join(before, ",") || len(report.Applied) != 0 {
				t.Fatalf("refused run changed history %v -> %v (applied %v)", before, after, report.Applied)
			}
		})
	}
}

// TestUnknownVersionWithTheBuildsNewestNumberIsDiverged: versions compare by
// their NNNN number, so an unknown "0002_z" next to the build's "0002_b" is a
// divergence, not a newer database (string order would say it is newer).
func TestUnknownVersionWithTheBuildsNewestNumberIsDiverged(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t).MigratorConn(t)
	build := fstest.MapFS{
		"0001_a.sql": &fstest.MapFile{Data: []byte("CREATE TABLE gk_meta.a (id integer);")},
		"0002_b.sql": &fstest.MapFile{Data: []byte("CREATE TABLE gk_meta.b (id integer);")},
	}
	if _, err := migrate.Run(ctx, conn, build); err != nil {
		t.Fatal(err)
	}
	// A history row no build of this line produced (simulated divergence).
	if _, err := conn.Exec(ctx, "INSERT INTO gk_meta.schema_migration (version, sha256) VALUES ('0002_z', repeat('0', 64))"); err != nil {
		t.Fatal(err)
	}
	report, err := migrate.Apply(ctx, conn, build, "")
	if err == nil || !strings.Contains(err.Error(), "diverged") {
		t.Fatalf("report %+v, err %v; want diverged refusal", report, err)
	}
}

// TestMigrateValidatesHistoryBeforeApplying covers a file inserted below the
// newest applied migration: nothing may be applied, out of order or otherwise.
func TestMigrateValidatesHistoryBeforeApplying(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t).MigratorConn(t)
	file := func(sql string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(sql)} }

	first := fstest.MapFS{
		"0001_a.sql": file("CREATE TABLE gk_meta.a (id integer);"),
		"0003_c.sql": file("CREATE TABLE gk_meta.c (id integer);"),
	}
	if _, err := migrate.Run(ctx, conn, first); err != nil {
		t.Fatal(err)
	}
	inserted := fstest.MapFS{
		"0001_a.sql": first["0001_a.sql"],
		"0002_b.sql": file("CREATE TABLE gk_meta.b (id integer);"),
		"0003_c.sql": first["0003_c.sql"],
		"0004_d.sql": file("CREATE TABLE gk_meta.d (id integer);"),
	}
	applied, err := migrate.Run(ctx, conn, inserted)
	if err == nil || !strings.Contains(err.Error(), "numbered below") {
		t.Fatalf("out-of-order migration: applied %v, err %v; want refusal", applied, err)
	}
	var b, d bool
	if err := conn.QueryRow(ctx, "SELECT to_regclass('gk_meta.b') IS NOT NULL, to_regclass('gk_meta.d') IS NOT NULL").Scan(&b, &d); err != nil {
		t.Fatal(err)
	}
	if b || d || len(applied) != 0 {
		t.Fatalf("refused run still applied migrations: b=%v d=%v applied=%v", b, d, applied)
	}
}

func TestFailedMigrationLeavesNoPartialSchema(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t).MigratorConn(t)
	broken := fstest.MapFS{
		"0001_partial.sql": &fstest.MapFile{Data: []byte("CREATE TABLE gk_meta.partial (id integer);\nSELECT 1 / 0;")},
	}
	_, err := migrate.Run(ctx, conn, broken)
	if got := dbtest.SQLState(err); got != "22012" {
		t.Fatalf("broken migration: SQLSTATE %q (err=%v), want 22012 division_by_zero", got, err)
	}
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT to_regclass('gk_meta.partial') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("failed migration left a partial table behind")
	}
	if got := recorded(t, conn); len(got) != 0 {
		t.Fatalf("failed migration recorded as applied: %v", got)
	}
}
