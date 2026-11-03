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
