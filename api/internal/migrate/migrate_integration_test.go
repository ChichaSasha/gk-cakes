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

func TestMigrateRefusesOlderBuildOnNewerDatabase(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t).MigratorConn(t)
	if _, err := migrate.Run(ctx, conn, migrations.FS); err != nil {
		t.Fatal(err)
	}
	all := versions(t, migrations.FS)
	older := fstest.MapFS{}
	for _, v := range all[:len(all)-1] {
		body, err := fs.ReadFile(migrations.FS, v+".sql")
		if err != nil {
			t.Fatal(err)
		}
		older[v+".sql"] = &fstest.MapFile{Data: body}
	}
	_, err := migrate.Run(ctx, conn, older)
	if err == nil || !strings.Contains(err.Error(), "unknown to this build") {
		t.Fatalf("older build on newer database: err = %v, want refusal", err)
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
