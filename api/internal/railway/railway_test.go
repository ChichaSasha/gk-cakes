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
	// logs is the deployment's runtime log (nil: one container start and a
	// line of its own output); logFails refuses the first N log queries
	// (negative: all), logReads counts them.
	logs     []string
	logPages [][]string // when set, read i answers logPages[i] (the last repeats)
	logFails int
	logReads int
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
	case strings.Contains(req.Query, "deploymentLogs"):
		f.logReads++
		if f.logFails != 0 {
			if f.logFails > 0 {
				f.logFails--
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": "Not Authorized"}}})
			return
		}
		if req.Variables["deploymentId"] != "dep-123" || req.Variables["limit"] != float64(MaxLogLines) {
			http.Error(w, "unexpected variables", http.StatusBadRequest)
			return
		}
		lines := f.logs
		if f.logPages != nil {
			lines = f.logPages[min(f.logReads-1, len(f.logPages)-1)]
		}
		if lines == nil {
			lines = []string{"Starting Container", `{"level":"INFO","msg":"worker started"}`}
		}
		var out []map[string]any
		for _, l := range lines {
			out = append(out, map[string]any{"message": l})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"deploymentLogs": out}})
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
			{"timestamp": "2026-11-03T12:00:01Z", "message": "email provider call", "attributes": []map[string]string{
				{"key": "result", "value": `"accepted"`}, {"key": "http_status", "value": "200"}, {"key": "reason", "value": `"a \"quoted\" reason"`},
				{"key": "odd", "value": `"unterminated`}}},
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
	// JSON-encoded attribute values are decoded; anything else is kept as sent.
	if err != nil || !complete || len(lines) != 2 || lines[0] != `email provider call result=accepted http_status=200 reason=a "quoted" reason odd="unterminated` || lines[1] != "request" {
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
// it SUCCESS, so the settle period also counts the container starts in the
// deployment's log.
func TestSettleCatchesACrashLoopBehindASuccessStatus(t *testing.T) {
	req := Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage, Settle: 50 * time.Millisecond}
	app := `{"level":"ERROR","msg":"database login","error":"refused"}`
	for name, logs := range map[string][]string{
		"restarted":         {"Starting Container", app, "Starting Container"},
		"restarted, spaced": {"Starting Container", app, "  starting container\n"},
		"crash loop":        {"Starting Container", app, "Starting Container", app, "Starting Container"},
	} {
		f := &fakeRailway{statuses: []string{"SUCCESS"}, logs: logs}
		if _, err := deployer(t, f).Deploy(context.Background(), req); err == nil || !strings.Contains(err.Error(), "not running steadily") {
			t.Errorf("%s: err = %v, want the restart reported", name, err)
		}
	}
	steady := &fakeRailway{statuses: []string{"SUCCESS"}}
	if res, err := deployer(t, steady).Deploy(context.Background(), req); err != nil || len(res.Warnings) != 0 || steady.logReads == 0 {
		t.Fatalf("steady: %+v, %v after %d log reads", res, err, steady.logReads)
	}
	quoted := &fakeRailway{statuses: []string{"SUCCESS"}, logs: []string{"Starting Container", `{"msg":"Starting Container"}`}}
	if res, err := deployer(t, quoted).Deploy(context.Background(), req); err != nil || len(res.Warnings) != 0 {
		t.Fatalf("an application line mentioning the phrase: %+v, %v; want one start counted", res, err)
	}
	noSettle := &fakeRailway{statuses: []string{"SUCCESS"}, logs: []string{"Starting Container", "Starting Container"}}
	if _, err := deployer(t, noSettle).Deploy(context.Background(), Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage}); err != nil || noSettle.logReads != 0 {
		t.Fatalf("without a settle period: %v after %d log reads; want no log check", err, noSettle.logReads)
	}

	blind := &fakeRailway{statuses: []string{"SUCCESS"}, logFails: -1}
	res, err := deployer(t, blind).Deploy(context.Background(), req)
	if err != nil || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "not readable") || !strings.Contains(res.Warnings[0], "Not Authorized") {
		t.Fatalf("log unreadable: %+v, %v; want success with one warning naming the error", res, err)
	}
	transient := &fakeRailway{statuses: []string{"SUCCESS"}, logFails: 2}
	if res, err := deployer(t, transient).Deploy(context.Background(), req); err != nil || len(res.Warnings) != 0 {
		t.Fatalf("two transient errors: %+v, %v; want the check to continue", res, err)
	}
	absent := &fakeRailway{statuses: []string{"SUCCESS"}, logs: []string{app}}
	res, err = deployer(t, absent).Deploy(context.Background(), req)
	if err != nil || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "inconclusive: no start line") {
		t.Fatalf("no start line: %+v, %v; want success with a warning, never a silent pass", res, err)
	}
	full := make([]string, MaxLogLines)
	full[0] = "Starting Container"
	for i := 1; i < len(full); i++ {
		full[i] = app
	}
	truncated := &fakeRailway{statuses: []string{"SUCCESS"}, logs: full}
	res, err = deployer(t, truncated).Deploy(context.Background(), req)
	if err != nil || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "inconclusive: the deployment log page was full") {
		t.Fatalf("a full log page: %+v, %v; want it treated as unknown, not as one start", res, err)
	}
	// deploymentLogs returns the newest lines: after one steady read, a full
	// page may hide the first start, so the latest read decides.
	later := &fakeRailway{statuses: []string{"SUCCESS"}, logPages: [][]string{{"Starting Container"}, full}}
	res, err = deployer(t, later).Deploy(context.Background(), req)
	if err != nil || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "page was full") || later.logReads < 2 {
		t.Fatalf("steady, then full pages: %+v, %v after %d reads; want a warning, not a silent pass", res, err, later.logReads)
	}
}

// The process's own start line is a second count, so a restart is caught
// even without Railway's container-start line; it is only counted when named.
func TestSettleCountsTheProcessStartLine(t *testing.T) {
	req := Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage, Settle: 50 * time.Millisecond, StartLine: "worker starting"}
	for name, logs := range map[string][]string{
		"own line twice":            {"worker starting", "worker starting"},
		"own line twice, one start": {"Starting Container", "worker starting", "x", "worker starting"},
	} {
		f := &fakeRailway{statuses: []string{"SUCCESS"}, logs: logs}
		if _, err := deployer(t, f).Deploy(context.Background(), req); err == nil || !strings.Contains(err.Error(), "started 2 times") {
			t.Errorf("%s: %v, want the restart reported", name, err)
		}
	}
	own := &fakeRailway{statuses: []string{"SUCCESS"}, logs: []string{"worker starting", "database login verified"}}
	if res, err := deployer(t, own).Deploy(context.Background(), req); err != nil || len(res.Warnings) != 0 {
		t.Fatalf("own line once, no platform line: %+v, %v; want a steady pass", res, err)
	}
	both := &fakeRailway{statuses: []string{"SUCCESS"}, logs: []string{"Starting Container", "worker starting", "database login verified"}}
	if res, err := deployer(t, both).Deploy(context.Background(), req); err != nil || len(res.Warnings) != 0 {
		t.Fatalf("both lines once: %+v, %v; want a steady pass", res, err)
	}
	raw := &fakeRailway{statuses: []string{"SUCCESS"}, logs: []string{"Starting Container",
		`{"time":"2026-11-04T00:00:00Z","level":"INFO","msg":"worker starting"}`, `{"level":"INFO","msg":"worker starting"}`}}
	if _, err := deployer(t, raw).Deploy(context.Background(), req); err == nil || !strings.Contains(err.Error(), "started 2 times") {
		t.Fatalf("own line twice as unparsed JSON: %v, want the restart reported", err)
	}
	// A named start line that never matches (renamed, or not parsed) must not
	// leave the check resting silently on Railway's line.
	partial := &fakeRailway{statuses: []string{"SUCCESS"}, logs: []string{"Starting Container", "worker starting now", "worker starting now"}}
	res, err := deployer(t, partial).Deploy(context.Background(), req)
	if err != nil || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], `start line "worker starting" did not appear`) {
		t.Fatalf("only lines merely starting with the start line: %+v, %v; want them not counted and an inconclusive warning", res, err)
	}
	unnamed := &fakeRailway{statuses: []string{"SUCCESS"}, logs: []string{"worker starting", "worker starting"}}
	req.StartLine = ""
	res, err = deployer(t, unnamed).Deploy(context.Background(), req)
	if err != nil || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "no start line") {
		t.Fatalf("start line not named: %+v, %v; want those lines ignored (inconclusive)", res, err)
	}
}

// A settle period whose reads all failed, though fewer than maxPollErrors in a
// row, names the read error rather than a missing start line.
func TestSettleWarningNamesReadErrors(t *testing.T) {
	f := &fakeRailway{statuses: []string{"SUCCESS"}, logFails: -1}
	d := deployer(t, f)
	d.PollInterval = 30 * time.Millisecond
	res, err := d.Deploy(context.Background(), Request{ServiceID: testService, EnvironmentID: testEnv, Image: testImage, Settle: 50 * time.Millisecond})
	if err != nil || f.logReads >= maxPollErrors {
		t.Fatalf("setup: %v after %d reads; want fewer than %d reads", err, f.logReads, maxPollErrors)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "not readable") || !strings.Contains(res.Warnings[0], "Not Authorized") {
		t.Fatalf("warnings %q; want one naming the read error", res.Warnings)
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
