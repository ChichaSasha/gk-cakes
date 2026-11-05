package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestUsageAndRestoreConfiguration(t *testing.T) {
	var out bytes.Buffer
	if code := run(context.Background(), []string{"restore-everything"}, func(string) string { return "" }, &out); code != 2 ||
		!strings.Contains(out.String(), "dump | restore-check | restore") {
		t.Fatalf("usage: %d %q", code, out.String())
	}
	const canary = "c4n4ry-secret"
	env := map[string]string{"APP_ENV": "staging", "BACKUP_B2_KEY_ID": "0031234abcd000000000000a1", "BACKUP_B2_KEY": canary,
		"BACKUP_ENCRYPTION_KEYS": canary, "RESTORE_TARGET_DATABASE_URL": "postgres://postgres:" + canary + "@db.example.com/postgres?sslmode=require"}
	out.Reset()
	if code := run(context.Background(), []string{"restore"}, func(k string) string { return env[k] }, &out); code != 2 ||
		!strings.Contains(out.String(), "RESTORE_TARGET_DATABASE_URL must use sslmode=verify-full") || strings.Contains(out.String(), canary) {
		t.Fatalf("restore configuration: %d %q", code, out.String())
	}
}
