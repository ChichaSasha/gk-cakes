package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestUnpinnedImageFailsWithoutContactingRailway(t *testing.T) {
	env := map[string]string{"RAILWAY_TOKEN": "token", "RAILWAY_API_URL": "http://127.0.0.1:1"}
	var out bytes.Buffer
	code := run(context.Background(), []string{
		"-service", "svc", "-environment", "env", "-image", "ghcr.io/chichasasha/gk-cakes-ui:latest",
	}, func(k string) string { return env[k] }, &out)
	if code != 1 || !strings.Contains(out.String(), "digest") {
		t.Fatalf("exit %d, output %q; want failure about the digest", code, out.String())
	}
	if strings.Contains(out.String(), "token") {
		t.Errorf("output mentions the token: %q", out.String())
	}
}

// A provider error inside a warning must stay one annotation: no line break
// may end it early or start another workflow command.
func TestWarningIsEscapedForWorkflowCommands(t *testing.T) {
	got := escapeData("deployment log not readable: 100% denied\r\n::error::forged\nnext")
	if want := "deployment log not readable: 100%25 denied%0D%0A::error::forged%0Anext"; got != want {
		t.Fatalf("escapeData = %q, want %q", got, want)
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Fatal("escaped text still contains a line break")
	}
}
