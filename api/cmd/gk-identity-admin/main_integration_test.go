//go:build integration

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
)

// The operator commands work end to end with the migration credential.
func TestOperatorCommands(t *testing.T) {
	ctx := context.Background()
	db := dbtest.NewMigrated(t, "")
	env := map[string]string{"APP_ENV": "test", "MIGRATION_DATABASE_URL": db.MigratorURL()}
	getenv := func(k string) string { return env[k] }
	for _, step := range []struct {
		args []string
		want string
	}{
		{[]string{"-action", "show", "-subject", subject}, "none yet"},
		{[]string{"-action", "grant", "-subject", subject, "-capability", "chef", "-note", "first manager (GK-010)"}, "granted"},
		{[]string{"-action", "grant", "-subject", subject, "-capability", "chef", "-note", "again"}, "already held"},
		{[]string{"-action", "show", "-subject", subject}, "enabled; chef"},
		{[]string{"-action", "disable", "-subject", subject, "-note", "test"}, "disabled"},
		{[]string{"-action", "show", "-subject", subject}, "disabled; chef"},
		{[]string{"-action", "enable", "-subject", subject, "-note", "test"}, "enabled"},
		{[]string{"-action", "revoke", "-subject", subject, "-capability", "chef", "-note", "test"}, "revoked"},
		{[]string{"-action", "revoke", "-subject", subject, "-capability", "chef", "-note", "test"}, "not held"},
		{[]string{"-action", "show", "-subject", subject}, "no staff capability"},
	} {
		var out bytes.Buffer
		if code := run(ctx, step.args, getenv, &out); code != 0 || !strings.Contains(out.String(), step.want) {
			t.Fatalf("%v: exit %d, %q; want %q", step.args, code, out.String(), step.want)
		}
	}
	var n int
	if err := db.AdminConn(t).QueryRow(ctx, `SELECT count(*) FROM business.audit_event
		WHERE action IN ('staff.capability_granted', 'staff.capability_revoked', 'account.disabled', 'account.enabled')`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("audited changes = %d (%v), want 4", n, err)
	}
}
