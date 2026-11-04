//go:build integration

package migrate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
	"github.com/jackc/pgx/v5"
)

const insufficientPrivilege = "42501"

func migrated(t *testing.T) *dbtest.DB {
	t.Helper()
	db := dbtest.New(t)
	if _, err := migrate.Run(context.Background(), db.MigratorConn(t), migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func expectDenied(t *testing.T, role string, conn *pgx.Conn, statements []string) {
	t.Helper()
	for _, sql := range statements {
		_, err := conn.Exec(context.Background(), sql)
		if got := dbtest.SQLState(err); got != insufficientPrivilege {
			t.Errorf("%s: %q: got SQLSTATE %q (err=%v), want %s permission denial", role, sql, got, err, insufficientPrivilege)
		}
	}
}

// TestRuntimeRolesCannotBypassGateway is the K00 r3 real-role check: each
// forbidden operation is attempted with a real runtime login and must be
// denied, then one permitted atomic gateway command must succeed.
func TestRuntimeRolesCannotBypassGateway(t *testing.T) {
	ctx := context.Background()
	db := migrated(t)
	api := db.LoginConn(t, "gk_api")
	worker := db.LoginConn(t, "gk_worker")

	forbiddenForAll := []string{
		"INSERT INTO business.job (id, operation_id, next_attempt_at) VALUES (gen_random_uuid(), gen_random_uuid(), now())",
		"UPDATE business.job SET state = 'completed'",
		"DELETE FROM business.job",
		"TRUNCATE business.job",
		"ALTER TABLE business.job ADD COLUMN intruder integer",
		"DROP TABLE business.job",
		"CREATE TABLE business.intruder (id integer)",
		"CREATE TABLE public.intruder (id integer)",
		"CREATE FUNCTION business.intruder() RETURNS integer LANGUAGE sql AS 'SELECT 1'",
		"SET ROLE gk_owner",
		"SELECT business.job_enqueue(gen_random_uuid(), NULL)",
	}
	expectDenied(t, "gk_api", api, append(forbiddenForAll,
		"SELECT * FROM business.job",
		"SELECT * FROM business.job_claim(gen_random_uuid(), interval '60 seconds')",
		"SELECT business.job_complete(gen_random_uuid(), gen_random_uuid(), 1)",
	))
	expectDenied(t, "gk_worker", worker, forbiddenForAll)

	// Seed one job the way a future domain command would: as the owner.
	owner := db.MigratorConn(t)
	var operationID string
	if err := owner.QueryRow(ctx, "SELECT gen_random_uuid()::text").Scan(&operationID); err != nil {
		t.Fatal(err)
	}
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE gk_owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SELECT business.job_enqueue($1::uuid, NULL)", operationID); err != nil {
		t.Fatalf("owner enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// Permitted atomic command: the worker claims, completes, and a stale
	// finalization with the same generation is fenced.
	const workerID = "6f1c1c0e-6a5e-4c55-9a43-0d3b8b7a2f10"
	var jobID, claimedOperation string
	var generation int64
	err = worker.QueryRow(ctx,
		"SELECT job_id::text, job_operation_id::text, job_generation FROM business.job_claim($1::uuid, interval '60 seconds')",
		workerID).Scan(&jobID, &claimedOperation, &generation)
	if err != nil {
		t.Fatalf("worker claim: %v", err)
	}
	if claimedOperation != operationID || generation != 1 {
		t.Fatalf("claimed operation %s generation %d, want %s generation 1", claimedOperation, generation, operationID)
	}
	var completed bool
	if err := worker.QueryRow(ctx, "SELECT business.job_complete($1::uuid, $2::uuid, $3)", jobID, workerID, generation).Scan(&completed); err != nil || !completed {
		t.Fatalf("worker complete = %v, %v; want true", completed, err)
	}
	if err := worker.QueryRow(ctx, "SELECT business.job_complete($1::uuid, $2::uuid, $3)", jobID, workerID, generation).Scan(&completed); err != nil || completed {
		t.Fatalf("repeated complete = %v, %v; want false (fenced)", completed, err)
	}
	var state string
	if err := worker.QueryRow(ctx, "SELECT state FROM business.job WHERE id = $1::uuid", jobID).Scan(&state); err != nil || state != "completed" {
		t.Fatalf("job state = %q, %v; want completed", state, err)
	}
}

// TestMigrationRefusesPreexistingPrivilegedGroups covers cluster-wide roles
// that already exist with unsafe memberships, including indirect ones.
func TestMigrationRefusesPreexistingPrivilegedGroups(t *testing.T) {
	ctx := context.Background()
	migrated(t) // ensure the group roles exist in this cluster
	admin := dbtest.New(t).AdminConn(t)

	cases := []struct {
		name  string
		setup []string
		undo  []string
		// asOperator runs the migration as the cluster superuser. A role that
		// created the groups is automatically their admin member, so only a
		// migration role that did not create them can have a group as member.
		asOperator bool
	}{
		{
			name:  "indirect gk_owner membership",
			setup: []string{"CREATE ROLE gk_test_bridge NOLOGIN", "GRANT gk_owner TO gk_test_bridge", "GRANT gk_test_bridge TO gk_worker"},
			undo:  []string{"REVOKE gk_test_bridge FROM gk_worker", "DROP ROLE IF EXISTS gk_test_bridge"},
		},
		{
			name:  "predefined write role",
			setup: []string{"GRANT pg_write_all_data TO gk_api"},
			undo:  []string{"REVOKE pg_write_all_data FROM gk_api"},
		},
		{
			name: "member of the migration role",
			// Start from an operator that is not yet a gk_owner member, as on
			// a first run; earlier operator migrations may have granted it.
			setup:      []string{"REVOKE gk_owner FROM postgres", "GRANT postgres TO gk_api"},
			undo:       []string{"REVOKE postgres FROM gk_api"},
			asOperator: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() {
				for _, sql := range tc.undo {
					if _, err := admin.Exec(ctx, sql); err != nil {
						t.Errorf("cleanup %q: %v", sql, err)
					}
				}
			})
			for _, sql := range tc.setup {
				if _, err := admin.Exec(ctx, sql); err != nil {
					t.Fatalf("setup %q: %v", sql, err)
				}
			}
			db := dbtest.New(t)
			conn := db.MigratorConn(t)
			if tc.asOperator {
				conn = db.AdminConn(t)
			}
			_, err := migrate.Run(ctx, conn, migrations.FS)
			if err == nil || !strings.Contains(err.Error(), "must not inherit gk_owner, the migration role or predefined data roles") {
				t.Fatalf("migration with %s: err = %v, want refusal", tc.name, err)
			}
		})
	}
}

// TestGatewayCatalogInvariants inspects the catalogs for the K00 gateway rules.
func TestGatewayCatalogInvariants(t *testing.T) {
	ctx := context.Background()
	db := migrated(t)
	admin := db.AdminConn(t)

	var canLogin, super bool
	if err := admin.QueryRow(ctx, "SELECT rolcanlogin, rolsuper FROM pg_roles WHERE rolname = 'gk_owner'").Scan(&canLogin, &super); err != nil {
		t.Fatal(err)
	}
	if canLogin || super {
		t.Errorf("gk_owner rolcanlogin=%v rolsuper=%v, want nonlogin non-superuser", canLogin, super)
	}

	violations := map[string]string{
		"business objects not owned by gk_owner": `
			SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'business' AND pg_get_userbyid(c.relowner) <> 'gk_owner'
			UNION ALL
			SELECT p.proname FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = 'business' AND pg_get_userbyid(p.proowner) <> 'gk_owner'`,
		"functions executable by PUBLIC": `
			SELECT p.oid::regprocedure::text FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = 'business' AND has_function_privilege('public', p.oid, 'EXECUTE')`,
		"SECURITY DEFINER functions without fixed search_path": `
			SELECT p.oid::regprocedure::text FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = 'business' AND p.prosecdef
			  AND NOT coalesce('search_path=pg_catalog, pg_temp' = ANY (p.proconfig), false)`,
		"business functions that are not SECURITY DEFINER": `
			SELECT p.oid::regprocedure::text FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = 'business' AND NOT p.prosecdef`,
		"runtime write or create privileges": `
			SELECT r || ' on ' || c.relname FROM unnest(ARRAY['gk_api', 'gk_worker', 'gk_backup']) r
			CROSS JOIN pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'business' AND c.relkind IN ('r', 'p')
			  AND has_table_privilege(r, c.oid, 'INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER')
			UNION ALL
			SELECT r || ' CREATE on schema ' || s FROM unnest(ARRAY['gk_api', 'gk_worker', 'gk_backup', 'public']) r
			CROSS JOIN unnest(ARRAY['business', 'public']) s
			WHERE has_schema_privilege(r, s, 'CREATE')`,
		"PUBLIC usage of private schemas": `
			SELECT s FROM unnest(ARRAY['business', 'gk_meta']) s WHERE has_schema_privilege('public', s, 'USAGE')`,
		// GK-034: only the backup login reads the migration history, read only.
		"runtime access to the migration history": `
			SELECT r || ' uses gk_meta' FROM unnest(ARRAY['gk_api', 'gk_worker']) r WHERE has_schema_privilege(r, 'gk_meta', 'USAGE')
			UNION ALL
			SELECT 'gk_backup writes gk_meta.schema_migration'
			WHERE has_table_privilege('gk_backup', 'gk_meta.schema_migration', 'INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER')
			   OR has_schema_privilege('gk_backup', 'gk_meta', 'CREATE')
			UNION ALL
			SELECT 'gk_backup cannot read gk_meta.schema_migration'
			WHERE NOT has_table_privilege('gk_backup', 'gk_meta.schema_migration', 'SELECT')`,
	}
	for name, query := range violations {
		rows, err := admin.Query(ctx, query)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		found, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(found) > 0 {
			t.Errorf("%s: %v", name, found)
		}
	}
}
