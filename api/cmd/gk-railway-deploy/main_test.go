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
