package railway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testService = "f06ffbb1-27cf-4a21-9d72-fcc937644f77"
	testEnv     = "42765d05-8efe-4a5a-8a5d-cd18c6b9b8e1"
	testImage   = "ghcr.io/chichasasha/gk-cakes-api@sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// fakeRailway records requests and answers the three operations the deployer uses.
type fakeRailway struct {
	mu        sync.Mutex
	requests  []gqlRequest
	statuses  []string // returned by successive deployment status polls
	polls     int
	updateErr string
	pollFails int    // first N status polls answer with a GraphQL error
	metaImage string // image reported in deployment meta, if any
	// executions are the instance executions reported for the deployment
	// ("" for the default single RUNNING one); executionsErr refuses the query.
	executions    []string
	executionsErr bool
}

func (f *fakeRailway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Project-Access-Token") != "project-token" || r.Header.Get("Authorization") != "" {
		_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": "Not Authorized"}}})
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req gqlRequest
	_ = json.Unmarshal(body, &req)
	f.requests = append(f.requests, req)
	switch {
	case strings.Contains(req.Query, "serviceInstanceUpdate"):
		if f.updateErr != "" {
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": f.updateErr}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"serviceInstanceUpdate": true}})
	case strings.Contains(req.Query, "serviceInstanceDeployV2"):
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"serviceInstanceDeployV2": "dep-123"}})
	case strings.Contains(req.Query, "deploymentInstanceExecutions"):
		if f.executionsErr {
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": "Not Authorized"}}})
			return
		}
		input, _ := req.Variables["input"].(map[string]any)
		if input["serviceId"] != testService || input["environmentId"] != testEnv {
			http.Error(w, "unexpected input", http.StatusBadRequest)
			return
		}
		statuses := f.executions
		if statuses == nil {
			statuses = []string{"RUNNING"}
		}
		edges := []map[string]any{{"node": map[string]any{"deploymentId": "older-deployment", "status": "REMOVED"}}}
		for _, st := range statuses {
			edges = append(edges, map[string]any{"node": map[string]any{"deploymentId": "dep-123", "status": st}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"deploymentInstanceExecutions": map[string]any{"edges": edges}}})
	case strings.Contains(req.Query, "deployment("):
		if f.pollFails > 0 {
			f.pollFails--
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": "temporarily unavailable", "traceId": "t-1"}}})
			return
		}
		status := f.statuses[len(f.statuses)-1]
		if f.polls < len(f.statuses) {
			status = f.statuses[f.polls]
		}
		f.polls++
		dep := map[string]any{"id": "dep-123", "status": status}
		if f.metaImage != "" {
			dep["meta"] = map[string]any{"image": f.metaImage}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"deployment": dep}})
	default:
		http.Error(w, "unexpected operation", http.StatusBadRequest)
	}
}

func deployer(t *testing.T, f *fakeRailway) *Deployer {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &Deployer{
		Endpoint:     srv.URL,
		ProjectToken: "project-token",
		HTTP:         srv.Client(),
		PollInterval: time.Millisecond,
		Timeout:      2 * time.Second,
	}
}

func TestDeployPinsImageDigestAndWaitsForSuccess(t *testing.T) {
	f := &fakeRailway{statuses: []string{"INITIALIZING", "DEPLOYING", "SUCCESS"}}
	res, err := deployer(t, f).Deploy(context.Background(), Request{
		ServiceID: testService, EnvironmentID: testEnv, Image: testImage, HealthcheckPath: "/healthz",
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if res.DeploymentID != "dep-123" || res.Status != "SUCCESS" {
		t.Fatalf("result = %+v, want dep-123 SUCCESS", res)
	}
	if len(f.requests) < 3 {
		t.Fatalf("requests = %d, want update, deploy and polls", len(f.requests))
	}
	update := f.requests[0]
	if !strings.Contains(update.Query, "serviceInstanceUpdate") {
		t.Fatalf("first operation is not serviceInstanceUpdate: %s", update.Query)
	}
	input, _ := update.Variables["input"].(map[string]any)
	source, _ := input["source"].(map[string]any)
	if update.Variables["serviceId"] != testService || update.Variables["environmentId"] != testEnv ||
		source["image"] != testImage || input["healthcheckPath"] != "/healthz" {
		t.Fatalf("update variables = %+v", update.Variables)
	}
	deploy := f.requests[1]
	if !strings.Contains(deploy.Query, "serviceInstanceDeployV2") ||
		deploy.Variables["serviceId"] != testService || deploy.Variables["environmentId"] != testEnv {
		t.Fatalf("second operation = %s %+v", deploy.Query, deploy.Variables)
	}
}

func TestDeployRejectsImagesWithoutDigestBeforeCallingRailway(t *testing.T) {
	for _, image := range []string{
		"ghcr.io/chichasasha/gk-cakes-api:latest",
		"ghcr.io/chichasasha/gk-cakes-api:sha-1111111111111111111111111111111111111111",
		"ghcr.io/chichasasha/gk-cakes-api@sha256:abc",
		"ghcr.io/chichasasha/gk-cakes-api@sha256:" + strings.Repeat("A", 64),
	} {
		f := &fakeRailway{statuses: []string{"SUCCESS"}}
		_, err := deployer(t, f).Deploy(context.Background(), Request{ServiceID: testService, EnvironmentID: testEnv, Image: image})
		if err == nil || !strings.Contains(err.Error(), "digest") {
			t.Errorf("image %q: err = %v, want digest error", image, err)
		}
		if len(f.requests) != 0 {
			t.Errorf("image %q: %d requests sent to Railway, want 0", image, len(f.requests))
		}
	}
}

func TestDeployFailsOnTerminalFailureStatus(t *testing.T) {
	for _, status := range []string{"FAILED", "CRASHED", "REMOVED", "SKIPPED"} {
		f := &fakeRailway{statuses: []string{"DEPLOYING", status}}
		res, err := deployer(t, f).Deploy(context.Background(), Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage})
		if err == nil || res.Status != status {
			t.Errorf("status %s: result %+v, err %v; want failure reporting %s", status, res, err, status)
		}
	}
}

func TestDeployReportsGraphQLErrors(t *testing.T) {
	f := &fakeRailway{statuses: []string{"SUCCESS"}, updateErr: "Not Authorized"}
	_, err := deployer(t, f).Deploy(context.Background(), Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage})
	if err == nil || !strings.Contains(err.Error(), "Not Authorized") {
		t.Fatalf("err = %v, want the GraphQL error surfaced", err)
	}
	if len(f.requests) != 1 {
		t.Fatalf("requests after failed update = %d, want 1 (no deploy)", len(f.requests))
	}
}

func TestDeployTimesOutWithoutCancellingTheDeployment(t *testing.T) {
	f := &fakeRailway{statuses: []string{"DEPLOYING"}}
	d := deployer(t, f)
	d.Timeout = 50 * time.Millisecond
	res, err := d.Deploy(context.Background(), Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage})
	if err == nil || !strings.Contains(err.Error(), "timed out") || res.DeploymentID != "dep-123" {
		t.Fatalf("result %+v, err %v; want timeout naming the deployment", res, err)
	}
	for _, r := range f.requests {
		if strings.Contains(r.Query, "deploymentCancel") || strings.Contains(r.Query, "deploymentStop") {
			t.Fatalf("deployer cancelled or stopped the deployment: %s", r.Query)
		}
	}
}

func TestMissingTokenOrIDsAreRejected(t *testing.T) {
	f := &fakeRailway{statuses: []string{"SUCCESS"}}
	d := deployer(t, f)
	d.ProjectToken = ""
	if _, err := d.Deploy(context.Background(), Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage}); err == nil {
		t.Error("deploy without token accepted")
	}
	d.ProjectToken = "project-token"
	if _, err := d.Deploy(context.Background(), Request{EnvironmentID: testEnv, Image: testImage}); err == nil {
		t.Error("deploy without service ID accepted")
	}
	if len(f.requests) != 0 {
		t.Errorf("%d requests sent with invalid input", len(f.requests))
	}
}

func TestTransientStatusErrorsAreTolerated(t *testing.T) {
	f := &fakeRailway{statuses: []string{"SUCCESS"}, pollFails: maxPollErrors - 1}
	if res, err := deployer(t, f).Deploy(context.Background(), Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage}); err != nil || res.Status != "SUCCESS" {
		t.Fatalf("result %+v, err %v; want success after transient errors", res, err)
	}
	f = &fakeRailway{statuses: []string{"SUCCESS"}, pollFails: maxPollErrors}
	_, err := deployer(t, f).Deploy(context.Background(), Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage})
	if err == nil || !strings.Contains(err.Error(), "trace t-1") {
		t.Fatalf("err = %v, want failure after %d errors naming the trace", err, maxPollErrors)
	}
}

func TestDeploymentRunningAnotherImageFails(t *testing.T) {
	other := strings.Replace(testImage, "aaaa", "bbbb", 1)
	f := &fakeRailway{statuses: []string{"SUCCESS"}, metaImage: other}
	_, err := deployer(t, f).Deploy(context.Background(), Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage})
	if err == nil || !strings.Contains(err.Error(), "not the requested") {
		t.Fatalf("err = %v, want image mismatch failure", err)
	}
	for _, reported := range []string{testImage, "docker.io/" + testImage, "ghcr.io/chichasasha/gk-cakes-api"} {
		f = &fakeRailway{statuses: []string{"SUCCESS"}, metaImage: reported}
		if _, err := deployer(t, f).Deploy(context.Background(), Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage}); err != nil {
			t.Errorf("meta image %q rejected: %v", reported, err)
		}
	}
}

func TestStartCommandOverridesTheEntrypoint(t *testing.T) {
	f := &fakeRailway{statuses: []string{"SUCCESS"}}
	if _, err := deployer(t, f).Deploy(context.Background(), Request{
		ServiceID: testService, EnvironmentID: testEnv, Image: testImage, StartCommand: "/usr/local/bin/gk-worker",
	}); err != nil {
		t.Fatal(err)
	}
	input, _ := f.requests[0].Variables["input"].(map[string]any)
	if input["startCommand"] != "/usr/local/bin/gk-worker" {
		t.Fatalf("update input = %+v, want the start command", input)
	}
	if _, ok := input["healthcheckPath"]; ok {
		t.Fatalf("update input = %+v, want no health check for a worker", input)
	}
	for _, bad := range []string{"gk-worker", "/bin/sh -c 'curl x | sh'", "/usr/local/bin/gk-worker; rm -rf /"} {
		g := &fakeRailway{statuses: []string{"SUCCESS"}}
		if _, err := deployer(t, g).Deploy(context.Background(), Request{
			ServiceID: testService, EnvironmentID: testEnv, Image: testImage, StartCommand: bad,
		}); err == nil || len(g.requests) != 0 {
			t.Errorf("start command %q: err %v after %d requests, want refusal before calling Railway", bad, err, len(g.requests))
		}
	}
}

func TestSettleCatchesACrashAfterSuccess(t *testing.T) {
	crash := &fakeRailway{statuses: []string{"DEPLOYING", "SUCCESS", "SUCCESS", "CRASHED"}}
	d := deployer(t, crash)
	_, err := d.Deploy(context.Background(), Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage, Settle: time.Second})
	if err == nil || !strings.Contains(err.Error(), "CRASHED within") {
		t.Fatalf("crash during settle: err = %v, want failure", err)
	}
	steady := &fakeRailway{statuses: []string{"SUCCESS"}}
	start := time.Now()
	res, err := deployer(t, steady).Deploy(context.Background(), Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage, Settle: 50 * time.Millisecond})
	if err != nil || res.Status != "SUCCESS" || time.Since(start) < 50*time.Millisecond || steady.polls < 2 {
		t.Fatalf("steady deployment: %+v, %v after %s and %d polls; want SUCCESS after the settle period", res, err, time.Since(start), steady.polls)
	}
	// Without a settle period, SUCCESS returns at once.
	quick := &fakeRailway{statuses: []string{"SUCCESS", "CRASHED"}}
	if _, err := deployer(t, quick).Deploy(context.Background(), Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage}); err != nil || quick.polls != 1 {
		t.Fatalf("no settle: err %v after %d polls, want SUCCESS at the first poll", err, quick.polls)
	}
}

func TestEnvironmentLogsReadsTheAnchoredWindow(t *testing.T) {
	var got gqlRequest
	count := 2
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Project-Access-Token") != "project-token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": "Not Authorized"}}})
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		logs := []map[string]any{
			{"timestamp": "2026-11-03T12:00:01Z", "message": "email provider call", "attributes": []map[string]string{{"key": "result", "value": "accepted"}}},
			{"timestamp": "2026-11-03T12:00:02Z", "message": "request", "attributes": []map[string]string{}},
		}
		for len(logs) < count {
			logs = append(logs, map[string]any{"timestamp": "2026-11-03T12:00:03Z", "message": "x", "attributes": []map[string]string{}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"environmentLogs": logs}})
	}))
	defer srv.Close()
	d := &Deployer{Endpoint: srv.URL, ProjectToken: "project-token", HTTP: srv.Client()}
	since := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	until := since.Add(10 * time.Minute)
	lines, complete, err := d.EnvironmentLogs(context.Background(), testEnv, since, until)
	if err != nil || !complete || len(lines) != 2 || lines[0] != "email provider call result=accepted" || lines[1] != "request" {
		t.Fatalf("lines = %q, complete %v, %v", lines, complete, err)
	}
	// Railway's window: logs between beforeDate (oldest) and the anchor, none after it.
	if !strings.Contains(got.Query, "environmentLogs") || !strings.Contains(got.Query, "afterLimit: 0") ||
		got.Variables["environmentId"] != testEnv || got.Variables["beforeDate"] != "2026-11-03T12:00:00Z" ||
		got.Variables["anchorDate"] != "2026-11-03T12:10:00Z" || got.Variables["beforeLimit"] != float64(MaxLogLines) {
		t.Fatalf("request = %+v", got)
	}
	count = MaxLogLines
	if lines, complete, err := d.EnvironmentLogs(context.Background(), testEnv, since, until); err != nil || complete || len(lines) != MaxLogLines {
		t.Fatalf("a full page: %d lines, complete %v, %v; want it reported as possibly truncated", len(lines), complete, err)
	}
	if _, _, err := (&Deployer{Endpoint: srv.URL, ProjectToken: "wrong", HTTP: srv.Client()}).EnvironmentLogs(context.Background(), testEnv, since, until); err == nil {
		t.Fatal("unauthorized log request succeeded")
	}
	if _, _, err := d.EnvironmentLogs(context.Background(), testEnv, until, since); err == nil {
		t.Fatal("an empty window was accepted")
	}
}

// Railway restarts a crashed deployment under its restart policy and keeps
// it SUCCESS, so the settle period also checks the instance executions.
func TestSettleCatchesACrashLoopBehindASuccessStatus(t *testing.T) {
	req := Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage, Settle: 50 * time.Millisecond}
	for name, executions := range map[string][]string{
		"restarted":      {"CRASHED", "RUNNING"},
		"two executions": {"RUNNING", "RUNNING"},
		"exited":         {"EXITED"},
		"restarting":     {"RESTARTING"},
	} {
		f := &fakeRailway{statuses: []string{"SUCCESS"}, executions: executions}
		if _, err := deployer(t, f).Deploy(context.Background(), req); err == nil || !strings.Contains(err.Error(), "not running steadily") {
			t.Errorf("%s: err = %v, want the crash loop reported", name, err)
		}
	}
	steady := &fakeRailway{statuses: []string{"SUCCESS"}}
	if res, err := deployer(t, steady).Deploy(context.Background(), req); err != nil || len(res.Warnings) != 0 {
		t.Fatalf("steady: %+v, %v", res, err)
	}
	blind := &fakeRailway{statuses: []string{"SUCCESS"}, executionsErr: true}
	res, err := deployer(t, blind).Deploy(context.Background(), req)
	if err != nil || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "status only") {
		t.Fatalf("executions unreadable: %+v, %v; want success with a warning", res, err)
	}
}

func TestLateSuccessStillGetsItsSettlePeriod(t *testing.T) {
	f := &fakeRailway{statuses: []string{"DEPLOYING", "SUCCESS"}}
	d := deployer(t, f)
	d.PollInterval = 40 * time.Millisecond
	d.Timeout = 30 * time.Millisecond // SUCCESS arrives after the timeout
	if _, err := d.Deploy(context.Background(), Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage, Settle: 50 * time.Millisecond}); err != nil {
		t.Fatalf("late SUCCESS: %v, want the settle period honoured past the timeout", err)
	}
}
