package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// On GitHub Actions, provider text in the status line, the error and the
// step outputs stays on its own line: no forged workflow command or output.
func TestRunKeepsProviderTextOnOneLine(t *testing.T) {
	img := "ghcr.io/chichasasha/gk-cakes-api@sha256:" + strings.Repeat("a", 64)
	forged := "denied 100%\n::add-mask::x\r\n::stop-commands::y"
	for name, c := range map[string]struct {
		deployID string
		errMsg   string
		want     string
	}{
		"provider error": {errMsg: forged, want: "::error::railway: set image: denied 100%25%0A::add-mask::x%0D%0A::stop-commands::y"},
		"deployment ID":  {deployID: "dep-1\nforged=1", want: "deployment dep-1%0Aforged=1 status SUCCESS"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			switch q := string(body); {
			case c.errMsg != "":
				_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": c.errMsg}}})
			case strings.Contains(q, "serviceInstanceUpdate"):
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"serviceInstanceUpdate": true}})
			case strings.Contains(q, "serviceInstanceDeployV2"):
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"serviceInstanceDeployV2": c.deployID}})
			default:
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"deployment": map[string]any{"status": "SUCCESS"}}})
			}
		}))
		output := filepath.Join(t.TempDir(), "github_output")
		env := map[string]string{"RAILWAY_TOKEN": "token", "RAILWAY_API_URL": srv.URL, "GITHUB_ACTIONS": "true", "GITHUB_OUTPUT": output}
		var out bytes.Buffer
		run(context.Background(), []string{"-service", "svc", "-environment", "env", "-image", img}, func(k string) string { return env[k] }, &out)
		srv.Close()
		if !strings.Contains(out.String(), c.want) {
			t.Errorf("%s: output %q, want it to contain %q", name, out.String(), c.want)
		}
		for _, line := range strings.Split(out.String(), "\n") {
			if strings.HasPrefix(line, "::") && !strings.HasPrefix(line, "::error::") {
				t.Errorf("%s: forged workflow command line %q", name, line)
			}
		}
		if b, _ := os.ReadFile(output); strings.Contains(string(b), "forged") {
			t.Errorf("%s: GITHUB_OUTPUT %q carries a forged output", name, b)
		}
	}
}

// -start-line reaches the settle check: the process's own start line logged
// twice fails the deploy.
func TestRunPassesTheStartLine(t *testing.T) {
	img := "ghcr.io/chichasasha/gk-cakes-api@sha256:" + strings.Repeat("a", 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch q := string(body); {
		case strings.Contains(q, "serviceInstanceUpdate"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"serviceInstanceUpdate": true}})
		case strings.Contains(q, "serviceInstanceDeployV2"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"serviceInstanceDeployV2": "dep-1"}})
		case strings.Contains(q, "deploymentLogs"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"deploymentLogs": []map[string]string{
				{"message": "worker starting"}, {"message": "worker starting"}}}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"deployment": map[string]any{"status": "SUCCESS"}}})
		}
	}))
	defer srv.Close()
	env := map[string]string{"RAILWAY_TOKEN": "token", "RAILWAY_API_URL": srv.URL}
	var out bytes.Buffer
	code := run(context.Background(), []string{"-service", "svc", "-environment", "env", "-image", img,
		"-settle", "1m", "-start-line", "worker starting"}, func(k string) string { return env[k] }, &out)
	if code != 1 || !strings.Contains(out.String(), "started 2 times") {
		t.Fatalf("exit %d, output %q; want the restart reported", code, out.String())
	}
}
