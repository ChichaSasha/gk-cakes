package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

const subject = "22ac712e-4e38-48bb-a56d-fa7809ccc13f"

func TestParseRejectsInvalidCommands(t *testing.T) {
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"no action":            {[]string{"-subject", subject}, "-action must be"},
		"unknown action":       {[]string{"-action", "delete", "-subject", subject}, "-action must be"},
		"upper-case subject":   {[]string{"-action", "show", "-subject", strings.ToUpper(subject)}, "-subject"},
		"not a subject":        {[]string{"-action", "show", "-subject", "admin"}, "-subject"},
		"unknown capability":   {[]string{"-action", "grant", "-subject", subject, "-capability", "owner", "-note", "x"}, "-capability must be"},
		"capability on enable": {[]string{"-action", "enable", "-subject", subject, "-capability", "chef", "-note", "x"}, "does not apply"},
		"grant without note":   {[]string{"-action", "grant", "-subject", subject, "-capability", "chef"}, "-note"},
		"multi-line note":      {[]string{"-action", "disable", "-subject", subject, "-note", "a\nb"}, "-note"},
		"long note":            {[]string{"-action", "disable", "-subject", subject, "-note", strings.Repeat("x", 201)}, "-note"},
		"extra argument":       {[]string{"-action", "show", "-subject", subject, "now"}, "unexpected arguments"},
	} {
		var out bytes.Buffer
		code := run(context.Background(), c.args, func(string) string { return "" }, &out)
		if code != 2 || !strings.Contains(out.String(), c.want) {
			t.Errorf("%s: exit %d, %q; want exit 2 mentioning %q", name, code, out.String(), c.want)
		}
	}
	if _, err := parse([]string{"-action", "show", "-subject", subject}, &bytes.Buffer{}); err != nil {
		t.Fatalf("show needs no note: %v", err)
	}
}

func TestRunNeedsTheMigrationCredential(t *testing.T) {
	var out bytes.Buffer
	env := map[string]string{"APP_ENV": "staging", "DATABASE_URL": "postgres://u:p@db.example.com/x?sslmode=verify-full"}
	code := run(context.Background(), []string{"-action", "show", "-subject", subject}, func(k string) string { return env[k] }, &out)
	if code != 2 || !strings.Contains(out.String(), "MIGRATION_DATABASE_URL") || strings.Contains(out.String(), "u:p@") {
		t.Fatalf("exit %d, %q; want a refusal naming MIGRATION_DATABASE_URL without echoing any URL", code, out.String())
	}
}
