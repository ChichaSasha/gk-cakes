//go:build integration

package database_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func migrated(t *testing.T) *dbtest.DB {
	t.Helper()
	db := dbtest.New(t)
	if _, err := migrate.Run(context.Background(), db.MigratorConn(t), migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// login creates a login with the given CREATE ROLE options (after LOGIN) and
// connects as it; extra statements run as the superuser before connecting.
func login(t *testing.T, db *dbtest.DB, name, options string, extra ...string) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	admin := db.AdminConn(t)
	for _, sql := range append([]string{"CREATE ROLE " + name + " LOGIN PASSWORD 'pw' " + options}, extra...) {
		if _, err := admin.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	t.Cleanup(func() {
		a := db.AdminConn(t)
		_, _ = a.Exec(context.Background(), "DROP ROLE IF EXISTS "+name)
	})
	conn, err := pgx.Connect(ctx, db.UserURL(name, "pw"))
	if err != nil {
		t.Fatalf("connect as %s: %v", name, err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// switched runs SET ROLE role on conn (after any setup statements on it), so
// current_user differs from the session's login; the role is reset and
// setup roles dropped at cleanup.
func switched(t *testing.T, conn *pgx.Conn, role string, setup ...string) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	for _, sql := range append(setup, "SET ROLE "+role) {
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "RESET ROLE")
		if len(setup) > 0 {
			_, _ = conn.Exec(context.Background(), "DROP ROLE IF EXISTS "+role)
		}
	})
	return conn
}

// TestCheckLoginRealRoles: only a login holding and inheriting exactly its
// group, with no elevated attribute and acting as itself, passes;
// administrator, migration and other-service logins, logins that cannot use
// their group's grants, and sessions that switched role are refused.
func TestCheckLoginRealRoles(t *testing.T) {
	ctx := context.Background()
	db := migrated(t)
	suffix := strings.TrimPrefix(db.Name, "gk_test_")

	for _, group := range []string{"gk_api", "gk_worker"} {
		if err := database.CheckLogin(ctx, db.LoginConn(t, group), group); err != nil {
			t.Errorf("%s login: %v, want pass", group, err)
		}
	}

	refused := map[string]struct {
		conn  *pgx.Conn
		group string
		want  string
	}{
		"other service's login": {db.LoginConn(t, "gk_worker"), "gk_api", "holds gk_worker"},
		"superuser":             {db.AdminConn(t), "gk_api", "elevated attribute"},
		"migration credential":  {db.MigratorConn(t), "gk_api", "elevated attribute"},
		"no group":              {login(t, db, "gk_bare_"+suffix, ""), "gk_api", "holds no role"},
		"two groups": {login(t, db, "gk_two_"+suffix, "IN ROLE gk_api, gk_worker"), "gk_api",
			"holds gk_api, gk_worker"},
		"group through another role": {login(t, db, "gk_nested_"+suffix, "IN ROLE gk_api",
			"CREATE ROLE gk_bridge_"+suffix+" NOLOGIN IN ROLE gk_owner",
			"GRANT gk_bridge_"+suffix+" TO gk_nested_"+suffix,
		), "gk_api", "gk_owner"},
		"createdb":   {login(t, db, "gk_createdb_"+suffix, "CREATEDB IN ROLE gk_api"), "gk_api", "elevated attribute"},
		"createrole": {login(t, db, "gk_createrole_"+suffix, "CREATEROLE IN ROLE gk_api"), "gk_api", "elevated attribute"},
		"bypassrls":  {login(t, db, "gk_bypassrls_"+suffix, "BYPASSRLS IN ROLE gk_api"), "gk_api", "elevated attribute"},
		"replication": {login(t, db, "gk_replication_"+suffix, "REPLICATION IN ROLE gk_api"), "gk_api",
			"elevated attribute"},
		"noinherit login": {login(t, db, "gk_noinherit_"+suffix, "NOINHERIT IN ROLE gk_api"), "gk_api",
			"does not inherit"},
		"grant without inherit": {login(t, db, "gk_noinhgrant_"+suffix, "",
			"GRANT gk_api TO gk_noinhgrant_"+suffix+" WITH INHERIT FALSE"), "gk_api", "does not inherit"},
		"login switched to its group": {switched(t, db.LoginConn(t, "gk_api"), "gk_api"), "gk_api", "acts as role gk_api"},
		"superuser acting as a login": {switched(t, db.AdminConn(t), "gk_api_acting_"+suffix,
			"CREATE ROLE gk_api_acting_"+suffix+" LOGIN IN ROLE gk_api"), "gk_api", "acts as role gk_api_acting_"},
	}
	// A login that owns an object or holds a direct grant passes every
	// membership check, yet bypasses its group; each kind is refused.
	direct := "owns database objects or holds privileges granted to it directly"
	for name, grant := range map[string]struct{ setup, undo string }{
		"owns a table":    {"CREATE TABLE business.gk_owned_%[1]s (x int); ALTER TABLE business.gk_owned_%[1]s OWNER TO %[2]s", "DROP TABLE IF EXISTS business.gk_owned_%[1]s"},
		"owns a schema":   {"CREATE SCHEMA gk_schema_%[1]s AUTHORIZATION %[2]s", "DROP SCHEMA IF EXISTS gk_schema_%[1]s"},
		"owns a function": {"CREATE FUNCTION business.gk_fn_%[1]s() RETURNS int LANGUAGE sql AS 'SELECT 1'; ALTER FUNCTION business.gk_fn_%[1]s() OWNER TO %[2]s", "DROP FUNCTION IF EXISTS business.gk_fn_%[1]s()"},
		"table grant":     {"GRANT SELECT ON business.job TO %[2]s", "REVOKE ALL ON business.job FROM %[2]s"},
		"column grant":    {"GRANT SELECT (id) ON business.job TO %[2]s", "REVOKE ALL (id) ON business.job FROM %[2]s"},
		"schema grant":    {"GRANT USAGE ON SCHEMA business TO %[2]s", "REVOKE ALL ON SCHEMA business FROM %[2]s"},
		"function grant":  {"GRANT EXECUTE ON FUNCTION business.job_claim(uuid, interval) TO %[2]s", "REVOKE ALL ON FUNCTION business.job_claim(uuid, interval) FROM %[2]s"},
		"database grant":  {"GRANT TEMPORARY ON DATABASE " + db.Name + " TO %[2]s", "REVOKE ALL ON DATABASE " + db.Name + " FROM %[2]s"},
	} {
		key := strings.ReplaceAll(strings.ReplaceAll(name, " ", ""), "a", "")
		role := "gk_direct_" + key + "_" + suffix
		conn := login(t, db, role, "IN ROLE gk_api", fmt.Sprintf(grant.setup, key+suffix, role))
		t.Cleanup(func() {
			a := db.AdminConn(t)
			_, _ = a.Exec(context.Background(), fmt.Sprintf(grant.undo, key+suffix, role))
		})
		err := database.CheckLogin(ctx, conn, "gk_api")
		if !errors.Is(err, database.ErrLogin) || !strings.Contains(err.Error(), direct) {
			t.Errorf("%s: %v, want ErrLogin mentioning %q", name, err, direct)
		}
	}
	for name, c := range refused {
		err := database.CheckLogin(ctx, c.conn, c.group)
		if !errors.Is(err, database.ErrLogin) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want ErrLogin mentioning %q", name, err, c.want)
		}
	}
	t.Cleanup(func() {
		a := db.AdminConn(t)
		_, _ = a.Exec(context.Background(), "DROP ROLE IF EXISTS gk_bridge_"+suffix)
	})
}

// TestLoginGateRealPool: the readiness gate passes for the right group and
// stays unavailable (logging the refusal once) for a wrong one.
func TestLoginGateRealPool(t *testing.T) {
	ctx := context.Background()
	db := migrated(t)
	pool, err := pgxpool.New(ctx, db.LoginURL(t, "gk_api"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	ok := &database.LoginGate{Pool: pool, Group: "gk_api", Logger: logger}
	for i := 0; i < 2; i++ {
		if err := ok.Ping(ctx); err != nil {
			t.Fatalf("gk_api gate: %v", err)
		}
	}
	wrong := &database.LoginGate{Pool: pool, Group: "gk_worker", Logger: logger}
	for i := 0; i < 3; i++ {
		if err := wrong.Ping(ctx); !errors.Is(err, database.ErrLogin) {
			t.Fatalf("gk_worker gate on a gk_api login: %v, want ErrLogin", err)
		}
	}
	if n := strings.Count(logs.String(), "database login refused"); n != 1 {
		t.Errorf("refusal logged %d times, want once:\n%s", n, logs.String())
	}
	if n := strings.Count(logs.String(), "database login verified"); n != 1 {
		t.Errorf("verification logged %d times, want once:\n%s", n, logs.String())
	}

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := database.WaitLogin(waitCtx, pool, "gk_worker", 10*time.Millisecond); !errors.Is(err, database.ErrLogin) {
		t.Errorf("WaitLogin with the wrong group: %v, want ErrLogin at once", err)
	}
}
