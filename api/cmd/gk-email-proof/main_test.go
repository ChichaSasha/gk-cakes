package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestConfigurationIsCheckedBeforeConnecting(t *testing.T) {
	base := map[string]string{
		"APP_ENV":                "staging",
		"MIGRATION_DATABASE_URL": "postgres://u:c4n4ry@db.example.supabase.co:5432/postgres?sslmode=verify-full",
		"API_URL":                "https://staging-api.gkcake.com",
		"PROOF_LABEL":            "gk1",
	}
	for name, override := range map[string]map[string]string{
		"no migration credential": {"MIGRATION_DATABASE_URL": ""},
		"unverified TLS":          {"MIGRATION_DATABASE_URL": "postgres://u:c4n4ry@db.example.supabase.co/postgres?sslmode=require"},
		"plain HTTP API":          {"API_URL": "http://staging-api.gkcake.com"},
		"API URL with a path":     {"API_URL": "https://staging-api.gkcake.com/webhooks"},
		"wait too long":           {"PROOF_WAIT": "2h"},
		"wait not a duration":     {"PROOF_WAIT": "soon"},
	} {
		env := map[string]string{}
		for k, v := range base {
			env[k] = v
		}
		for k, v := range override {
			env[k] = v
		}
		var out bytes.Buffer
		if code := run(context.Background(), func(k string) string { return env[k] }, &out); code != 2 {
			t.Errorf("%s: exit %d, want 2 (%s)", name, code, out.String())
		}
		if strings.Contains(out.String(), "c4n4ry") {
			t.Errorf("%s: output leaks the credential: %s", name, out.String())
		}
	}
}
