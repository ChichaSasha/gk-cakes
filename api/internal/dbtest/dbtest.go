//go:build integration

// Package dbtest creates disposable PostgreSQL databases for integration
// tests. GK_TEST_DATABASE_URL must point at a throwaway local cluster as a
// superuser; tests refuse any non-loopback host.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// EnvAdminURL names the variable holding the disposable cluster's admin URL.
const EnvAdminURL = "GK_TEST_DATABASE_URL"

// migratorRole mirrors the production migration credential: a non-superuser
// with CREATEROLE that owns the database, as Supabase's postgres role does.
const migratorRole = "gk_migrator_test"

// DB is one disposable database.
type DB struct {
	Name     string
	adminURL *url.URL
}

// New creates an empty database owned by the test migrator and drops it at
// cleanup. Tests that only need a migrated database should use NewMigrated.
func New(t *testing.T) *DB {
	t.Helper()
	return create(t, "")
}

// create makes a database owned by the test migrator, copied from template
// when it is not empty, and drops it at cleanup.
func create(t *testing.T, template string) *DB {
	t.Helper()
	adminURL := adminURLFromEnv(t)
	d := &DB{Name: "gk_test_" + randomHex(6), adminURL: adminURL}

	admin := d.connect(t, adminURL)
	ctx := context.Background()
	ignoreDuplicate(t, admin, "CREATE ROLE "+migratorRole+" LOGIN CREATEROLE PASSWORD '"+migratorRole+"'")
	sql := "CREATE DATABASE " + d.Name + " OWNER " + migratorRole
	if template != "" {
		sql += " TEMPLATE " + template
	}
	exec(t, admin, sql)
	t.Cleanup(func() {
		admin := d.connect(t, adminURL)
		if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+d.Name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop %s: %v", d.Name, err)
		}
	})
	return d
}

func adminURLFromEnv(t *testing.T) *url.URL {
	t.Helper()
	raw := os.Getenv(EnvAdminURL)
	if raw == "" {
		t.Fatalf("%s is required for integration tests", EnvAdminURL)
	}
	adminURL, err := url.Parse(raw)
	if err != nil || !loopback(adminURL.Hostname()) {
		t.Fatalf("%s must be a URL for a local disposable PostgreSQL cluster", EnvAdminURL)
	}
	return adminURL
}

// MigratorConn connects as the non-superuser migration role.
func (d *DB) MigratorConn(t *testing.T) *pgx.Conn {
	t.Helper()
	return d.connect(t, d.urlFor(migratorRole, migratorRole))
}

// MigratorURL is the migration role's connection URL, for commands that
// read MIGRATION_DATABASE_URL.
func (d *DB) MigratorURL() string { return d.urlFor(migratorRole, migratorRole).String() }

// LoginConn creates a login user that is a member of group (for example
// gk_api) and connects as it. The user is dropped at cleanup.
func (d *DB) LoginConn(t *testing.T, group string) *pgx.Conn {
	t.Helper()
	return d.connect(t, d.loginURL(t, group))
}

// LoginURL creates a login user in group and returns its connection URL.
func (d *DB) LoginURL(t *testing.T, group string) string {
	t.Helper()
	return d.loginURL(t, group).String()
}

func (d *DB) loginURL(t *testing.T, group string) *url.URL {
	t.Helper()
	user := group + "_login_" + randomHex(4)
	password := randomHex(12)
	admin := d.connect(t, d.adminURL)
	exec(t, admin, "CREATE ROLE "+pgx.Identifier{user}.Sanitize()+" LOGIN PASSWORD '"+password+"' IN ROLE "+pgx.Identifier{group}.Sanitize())
	t.Cleanup(func() {
		admin := d.connect(t, d.adminURL)
		_, _ = admin.Exec(context.Background(), "DROP ROLE IF EXISTS "+pgx.Identifier{user}.Sanitize())
	})
	return d.urlFor(user, password)
}

// AdminConn connects to this database as the cluster superuser, for
// assertions that inspect catalogs or simulate operator actions.
func (d *DB) AdminConn(t *testing.T) *pgx.Conn {
	t.Helper()
	u := *d.adminURL
	u.Path = "/" + d.Name
	return d.connect(t, &u)
}

// AdminURL is this database's connection URL as the cluster superuser.
func (d *DB) AdminURL() string {
	u := *d.adminURL
	u.Path = "/" + d.Name
	return u.String()
}

// UserURL returns this database's connection URL for an existing user, for
// tests that provision their own roles.
func (d *DB) UserURL(user, password string) string { return d.urlFor(user, password).String() }

func (d *DB) urlFor(user, password string) *url.URL {
	u := *d.adminURL
	u.User = url.UserPassword(user, password)
	u.Path = "/" + d.Name
	return &u
}

func (d *DB) connect(t *testing.T, u *url.URL) *pgx.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatalf("connect as %s: %v", u.User.Username(), err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func exec(t *testing.T, conn *pgx.Conn, sql string) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), sql); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func ignoreDuplicate(t *testing.T, conn *pgx.Conn, sql string) {
	t.Helper()
	_, err := conn.Exec(context.Background(), sql)
	if err != nil && !duplicate(err) {
		t.Fatalf("exec: %v", err)
	}
}

// duplicate reports a duplicate object or unique violation (a concurrent
// creation of the same cluster-wide role).
func duplicate(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "42710" || pgErr.Code == "23505")
}

// SQLState returns the PostgreSQL error code of err, or "" when absent.
func SQLState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
