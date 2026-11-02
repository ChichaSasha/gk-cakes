package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	for _, args := range [][]string{
		{"-verify", "-component", "web", "-commit", strings.Repeat("a", 40), "-digest", "sha256:" + strings.Repeat("b", 64)},
		{"-verify", "-component", "ui", "-commit", "main", "-digest", "sha256:" + strings.Repeat("b", 64)},
		{"-verify", "-component", "ui", "-commit", strings.Repeat("a", 40), "-digest", "latest"},
	} {
		var out bytes.Buffer
		if code := run(context.Background(), args, func(k string) string { return env[k] }, &out); code != exitDenied {
			t.Errorf("%v: exit %d, want %d; output %q", args, code, exitDenied, out.String())
		}
	}
}
