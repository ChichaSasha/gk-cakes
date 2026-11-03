package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/releasegate"
)

func TestDeniedEventExitsThreeWithoutManifest(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	if err := os.WriteFile(eventPath, []byte(`{"ref":"refs/heads/main"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(dir, "output")
	manifestPath := filepath.Join(dir, "manifest.json")
	env := map[string]string{
		"GITHUB_TOKEN":      "unused-for-denied-push",
		"GITHUB_EVENT_NAME": "push",
		"GITHUB_EVENT_PATH": eventPath,
		"GITHUB_REPOSITORY": "ChichaSasha/gk-cakes",
		"GITHUB_OUTPUT":     outputPath,
		"GITHUB_API_URL":    "http://127.0.0.1:1", // never contacted for a push
	}
	var out bytes.Buffer
	code := run(context.Background(), []string{"-manifest", manifestPath}, func(k string) string { return env[k] }, &out)
	if code != exitDenied {
		t.Fatalf("exit code = %d, want %d; output: %s", code, exitDenied, out.String())
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Errorf("manifest written for a denied event (stat err = %v)", err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil || strings.TrimSpace(string(got)) != "decision=deny" {
		t.Errorf("GITHUB_OUTPUT = %q, %v; want decision=deny", got, err)
	}
}

func TestMissingTokenIsAnError(t *testing.T) {
	var out bytes.Buffer
	if code := run(context.Background(), nil, func(string) string { return "" }, &out); code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
}

func TestVerifyRejectsMalformedArtifactWithoutNetwork(t *testing.T) {
	env := map[string]string{
		"GITHUB_TOKEN":            "token",
		"GITHUB_REPOSITORY":       "ChichaSasha/gk-cakes",
		"GITHUB_REPOSITORY_OWNER": "ChichaSasha",
		"GITHUB_API_URL":          "http://127.0.0.1:1",
		"GK_REGISTRY_URL":         "http://127.0.0.1:1",
	}
	release := []string{"-environment", "staging-ui", "-version", "ui-v1.0.0"}
	for _, args := range [][]string{
		append([]string{"-verify", "-component", "web", "-commit", strings.Repeat("a", 40), "-digest", "sha256:" + strings.Repeat("b", 64)}, release...),
		append([]string{"-verify", "-component", "ui", "-commit", "main", "-digest", "sha256:" + strings.Repeat("b", 64)}, release...),
		append([]string{"-verify", "-component", "ui", "-commit", strings.Repeat("a", 40), "-digest", "latest"}, release...),
		// A deploy job must never skip the replay and downgrade checks.
		{"-verify", "-component", "ui", "-commit", strings.Repeat("a", 40), "-digest", "sha256:" + strings.Repeat("b", 64)},
	} {
		var out bytes.Buffer
		if code := run(context.Background(), args, func(k string) string { return env[k] }, &out); code != exitDenied {
			t.Errorf("%v: exit %d, want %d; output %q", args, code, exitDenied, out.String())
		}
	}
}

func TestGHTokenIsAcceptedAsFallback(t *testing.T) {
	env := map[string]string{
		"GH_TOKEN":          "token",
		"GITHUB_EVENT_NAME": "push",
		"GITHUB_REPOSITORY": "ChichaSasha/gk-cakes",
	}
	var out bytes.Buffer
	if code := run(context.Background(), nil, func(k string) string { return env[k] }, &out); code != exitDenied {
		t.Fatalf("exit %d with only GH_TOKEN, want %d (evaluated and denied); output %q", code, exitDenied, out.String())
	}
}

// TestVerifyExitCodes pins the -verify contract staging.yml relies on:
// 0 deploy, 4 replay (skip), 3 rejected (incl. downgrade), 1 lookup error.
func TestVerifyExitCodes(t *testing.T) {
	cases := []struct {
		res    releasegate.VerifyResult
		err    error
		code   int
		prefix string
	}{
		{releasegate.VerifyResult{Outcome: releasegate.VerifyDeploy, Reason: "ok"}, nil, exitOK, "verified: "},
		{releasegate.VerifyResult{Outcome: releasegate.VerifyReplay, Reason: "x"}, nil, exitReleased, "replay: "},
		{releasegate.VerifyResult{Outcome: releasegate.VerifyRejected, Reason: "older"}, nil, exitDenied, "not verified: "},
		{releasegate.VerifyResult{Outcome: releasegate.VerifyDeploy}, errors.New("downgrade check: boom"), exitError, "error: "},
		{releasegate.VerifyResult{}, nil, exitError, "error: unknown"},
	}
	for _, tc := range cases {
		code, line := verifyExit(tc.res, tc.err)
		if code != tc.code || !strings.HasPrefix(line, tc.prefix) {
			t.Errorf("verifyExit(%+v, %v) = %d %q; want %d %q...", tc.res, tc.err, code, line, tc.code, tc.prefix)
		}
	}
}
