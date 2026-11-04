//go:build integration

package backup

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
)

// pgTools finds pg_dump/pg_restore of at least the server's major version
// (GK_TEST_PG_BIN, then PATH). CI sets GK_REQUIRE_PG_TOOLS=1 so a missing
// tool fails instead of skipping.
func pgTools(t *testing.T) (dump, restore string) {
	t.Helper()
	dirs := []string{os.Getenv("GK_TEST_PG_BIN"), ""}
	for _, d := range dirs {
		dump, restore = "pg_dump", "pg_restore"
		if d != "" {
			dump, restore = filepath.Join(d, "pg_dump"), filepath.Join(d, "pg_restore")
		}
		out, err := exec.Command(dump, "--version").Output()
		if err != nil {
			continue
		}
		var major int
		for _, f := range strings.Fields(string(out)) {
			if n, ok := leadingInt(f); ok {
				major = n
				break
			}
		}
		if major >= 17 {
			return dump, restore
		}
	}
	if os.Getenv("GK_REQUIRE_PG_TOOLS") == "1" {
		t.Fatal("pg_dump/pg_restore 17 or newer are required (GK_TEST_PG_BIN or PATH)")
	}
	t.Skip("pg_dump 17+ not found; set GK_TEST_PG_BIN")
	return "", ""
}

func leadingInt(s string) (int, bool) {
	n, digits := 0, 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n, digits = n*10+int(c-'0'), digits+1
	}
	return n, digits > 0
}

// A backup taken with the real backup login restores into an empty database
// with the same migrations and the same row count in every business table;
// a manifest that disagrees with the restored data, or a target that is not
// empty, fails the check.
func TestBackupRestoresWithTheSameRows(t *testing.T) {
	ctx := context.Background()
	pgDump, pgRestore := pgTools(t)
	source := dbtest.New(t)
	if _, err := migrate.Run(ctx, source.MigratorConn(t), migrations.FS); err != nil {
		t.Fatal(err)
	}
	// Business rows through the owner, as domain commands would.
	owner := source.MigratorConn(t)
	if _, err := owner.Exec(ctx, `BEGIN; SET LOCAL ROLE gk_owner;
		SELECT business.job_enqueue(gen_random_uuid(), NULL);
		SELECT business.job_enqueue(gen_random_uuid(), NULL);
		SELECT business.staff_grant('22ac712e-4e38-48bb-a56d-fa7809ccc13f', 'chef', 'backup test');
		COMMIT`); err != nil {
		t.Fatal(err)
	}
	store, sealer := newMemStore(), testSealer(t)
	at := time.Date(2026, 11, 4, 2, 30, 0, 0, time.UTC)
	d := &Dumper{Env: "test", DB: source.LoginURL(t, "gk_backup"), PgDump: pgDump, Store: store, Sealer: sealer, Now: func() time.Time { return at }}
	// A job written after the counts but before pg_dump starts: the dump
	// uses the same snapshot as the counts, so it must not contain it.
	d.beforeDump = func(ctx context.Context) {
		if _, err := owner.Exec(ctx, "BEGIN; SET LOCAL ROLE gk_owner; SELECT business.job_enqueue(gen_random_uuid(), NULL); COMMIT"); err != nil {
			t.Errorf("concurrent write: %v", err)
		}
	}
	m, err := d.Dump(ctx)
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	stamp := at.Format(stampLayout)
	if want := []string{Root("test") + stamp + "/dump.sealed", manifestName("test", stamp)}; !slices.Equal(store.puts, want) {
		t.Fatalf("uploads %v, want the dump, then the manifest", store.puts)
	}
	if m.Rows["job"] != 2 || m.Rows["account"] != 1 || m.Rows["staff_capability"] != 1 || m.Rows["audit_event"] < 2 ||
		m.SchemaVersion != m.Migrations[len(m.Migrations)-1] || !strings.HasPrefix(m.SchemaVersion, "0008_") || m.DumpBytes == 0 {
		t.Fatalf("manifest = %+v", m)
	}
	for name, data := range store.objs {
		if strings.Contains(string(data), "schema_migration") || strings.Contains(string(data), "staff_capability") {
			t.Fatalf("%s is stored in the clear", name)
		}
	}
	r := &Restorer{Env: "test", Target: dbtest.New(t).AdminURL(), PgRestore: pgRestore, Store: store, Sealer: sealer,
		Now: func() time.Time { return at.Add(time.Hour) }}
	rep, err := r.Check(ctx)
	if err != nil {
		t.Fatalf("restore check: %v", err)
	}
	if rep.Tables != len(m.Rows) || rep.Rows < 6 {
		t.Fatalf("report = %+v", rep)
	}

	// Restoring over existing objects fails (the target must be empty).
	if _, err := r.Check(ctx); err == nil || !strings.Contains(err.Error(), "pg_restore") {
		t.Fatalf("restore into a non-empty target: %v", err)
	}

	// A manifest that disagrees with the data fails the comparison.
	m.Rows["job"]++
	body, _ := json.Marshal(m)
	name := manifestName("test", at.Format(stampLayout))
	sealed, _ := sealer.Seal(name, body)
	_ = store.Put(ctx, name, sealed)
	r.Target = dbtest.New(t).AdminURL()
	if _, err := r.Check(ctx); err == nil || !strings.Contains(err.Error(), "table job restored 2 rows, manifest 3") {
		t.Fatalf("manifest disagreeing with the data: %v", err)
	}
	// A manifest listing other migrations than the restored history fails too.
	m.Rows["job"]--
	m.Migrations = append(m.Migrations, "0099_future")
	body, _ = json.Marshal(m)
	sealed, _ = sealer.Seal(name, body)
	_ = store.Put(ctx, name, sealed)
	r.Target = dbtest.New(t).AdminURL()
	if _, err := r.Check(ctx); err == nil || !strings.Contains(err.Error(), "restored migrations") {
		t.Fatalf("manifest listing other migrations: %v", err)
	}
	// A restored table the manifest does not list fails as well.
	m.Migrations = m.Migrations[:len(m.Migrations)-1]
	delete(m.Rows, "audit_event")
	body, _ = json.Marshal(m)
	sealed, _ = sealer.Seal(name, body)
	_ = store.Put(ctx, name, sealed)
	r.Target = dbtest.New(t).AdminURL()
	if _, err := r.Check(ctx); err == nil || !strings.Contains(err.Error(), "business tables, manifest lists") {
		t.Fatalf("restored table missing from the manifest: %v", err)
	}
}

// The API and worker logins cannot take a backup: the migration history is
// readable by the backup login only.
func TestOnlyTheBackupLoginReadsTheHistory(t *testing.T) {
	ctx := context.Background()
	pgDump, _ := pgTools(t)
	db := dbtest.New(t)
	if _, err := migrate.Run(ctx, db.MigratorConn(t), migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{"gk_api", "gk_worker"} {
		d := &Dumper{Env: "test", DB: db.LoginURL(t, group), PgDump: pgDump, Store: newMemStore(), Sealer: testSealer(t)}
		if _, err := d.Dump(ctx); err == nil || !strings.Contains(err.Error(), "migration history") {
			t.Errorf("%s took a backup: %v", group, err)
		}
	}
}
