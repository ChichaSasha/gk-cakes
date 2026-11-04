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
	got := escapeData("deployment log not readable: 100% denied\r\n::error::forged\nnext ##[add-mask]x")
	if want := "deployment log not readable: 100%25 denied%0D%0A::error::forged%0Anext # #[add-mask]x"; got != want {
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

// fakeDeploy answers a deploy whose status is status and whose log reads all
// fail with logErr (when set).
func fakeDeploy(t *testing.T, status, logErr string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch q := string(body); {
		case strings.Contains(q, "serviceInstanceUpdate"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"serviceInstanceUpdate": true}})
		case strings.Contains(q, "serviceInstanceDeployV2"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"serviceInstanceDeployV2": "dep-1"}})
		case strings.Contains(q, "deploymentLogs"):
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": logErr}}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"deployment": map[string]any{"status": status}}})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A settle warning carries provider text (the log read error): on GitHub
// Actions it stays one escaped annotation.
func TestRunEscapesWarnings(t *testing.T) {
	img := "ghcr.io/chichasasha/gk-cakes-api@sha256:" + strings.Repeat("a", 64)
	srv := fakeDeploy(t, "SUCCESS", "denied 100%\n::error::forged ##[add-mask]x")
	env := map[string]string{"RAILWAY_TOKEN": "token", "RAILWAY_API_URL": srv.URL, "GITHUB_ACTIONS": "true"}
	var out bytes.Buffer
	code := run(context.Background(), []string{"-service", "svc", "-environment", "env", "-image", img,
		"-settle", "100ms", "-poll-interval", "5ms"}, func(k string) string { return env[k] }, &out)
	if want := "::warning::deployment log not readable; settle checked the deployment status only: denied 100%25%0A::error::forged # #[add-mask]x"; code != 0 || !strings.Contains(out.String(), want) {
		t.Fatalf("exit %d, output %q; want success with the warning %q", code, out.String(), want)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "::") && !strings.HasPrefix(line, "::warning::") || strings.Contains(line, "##[") {
			t.Errorf("forged workflow command in %q", line)
		}
	}
}

// Only a plain status value reaches GITHUB_OUTPUT.
func TestRunKeepsAForgedStatusOutOfStepOutputs(t *testing.T) {
	img := "ghcr.io/chichasasha/gk-cakes-api@sha256:" + strings.Repeat("a", 64)
	srv := fakeDeploy(t, "SUCCESS\nforged=1", "")
	output := filepath.Join(t.TempDir(), "github_output")
	env := map[string]string{"RAILWAY_TOKEN": "token", "RAILWAY_API_URL": srv.URL, "GITHUB_ACTIONS": "true", "GITHUB_OUTPUT": output}
	var out bytes.Buffer
	code := run(context.Background(), []string{"-service", "svc", "-environment", "env", "-image", img,
		"-timeout", "20ms", "-poll-interval", "5ms"}, func(k string) string { return env[k] }, &out)
	b, _ := os.ReadFile(output)
	if code != 1 || string(b) != "deployment_id=dep-1\n" {
		t.Fatalf("exit %d, GITHUB_OUTPUT %q; want a timeout and only the deployment ID", code, b)
	}
	if !strings.Contains(out.String(), "status SUCCESS%0Aforged=1") {
		t.Errorf("output %q; want the status escaped on its line", out.String())
	}
}

func TestRunRejectsANonPositivePollInterval(t *testing.T) {
	var out bytes.Buffer
	code := run(context.Background(), []string{"-poll-interval", "0s"}, func(string) string { return "" }, &out)
	if code != 1 || !strings.Contains(out.String(), "-poll-interval must be positive") {
		t.Fatalf("exit %d, output %q", code, out.String())
	}
}
