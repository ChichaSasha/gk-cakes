package releasegate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

const (
	mainSHA    = "1111111111111111111111111111111111111111"
	sideSHA    = "2222222222222222222222222222222222222222"
	tagObjSHA  = "3333333333333333333333333333333333333333"
	apiDigest  = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	uiDigest   = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testRepo   = "ChichaSasha/gk-cakes"
	testOwner  = "ChichaSasha"
	testSchema = "0002_job_gateway"
)

// fakeGitHub models the GitHub REST endpoints the gate reads.
type fakeGitHub struct {
	tags        map[string]tagRef        // tag name -> ref object
	annotated   map[string]string        // annotated tag object sha -> commit sha
	compare     map[string]string        // commit sha -> compare status against main
	runs        map[string][]workflowRun // commit sha -> Components runs on main push
	packages    map[string][]pkgVersion  // package name -> versions
	migrations  map[string][]string      // commit sha -> files in api/migrations
	deployments map[string][]deployment  // environment -> deployments
	statuses    map[int64][]string       // deployment id -> status states (newest first)
	requests    []string
}

type tagRef struct{ Type, SHA string }

func newFake() *fakeGitHub {
	return &fakeGitHub{
		tags:      map[string]tagRef{"api-v1.2.3": {"commit", mainSHA}, "ui-v0.4.0": {"tag", tagObjSHA}},
		annotated: map[string]string{tagObjSHA: mainSHA},
		compare:   map[string]string{mainSHA: "ahead", sideSHA: "diverged"},
		runs: map[string][]workflowRun{
			mainSHA: {{HeadSHA: mainSHA, Event: "push", HeadBranch: "main", Status: "completed", Conclusion: "success"}},
		},
		packages: map[string][]pkgVersion{
			"gk-cakes-api": {{Name: apiDigest, Metadata: pkgMetadata{Container: pkgContainer{Tags: []string{"sha-" + mainSHA}}}}},
			"gk-cakes-ui":  {{Name: uiDigest, Metadata: pkgMetadata{Container: pkgContainer{Tags: []string{"sha-" + mainSHA}}}}},
		},
		migrations:  map[string][]string{mainSHA: {"0001_roles_and_schema.sql", "0002_job_gateway.sql", "migrations.go"}},
		deployments: map[string][]deployment{},
		statuses:    map[int64][]string{},
	}
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	if r.Header.Get("Authorization") != "Bearer test-token" {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		return
	}
	p := r.URL.Path
	q := r.URL.Query()
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch {
	case strings.HasPrefix(p, "/repos/"+testRepo+"/git/ref/tags/"):
		ref, ok := f.tags[strings.TrimPrefix(p, "/repos/"+testRepo+"/git/ref/tags/")]
		if !ok {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		reply(map[string]any{"object": map[string]string{"type": ref.Type, "sha": ref.SHA}})
	case strings.HasPrefix(p, "/repos/"+testRepo+"/git/tags/"):
		commit := f.annotated[strings.TrimPrefix(p, "/repos/"+testRepo+"/git/tags/")]
		reply(map[string]any{"object": map[string]string{"type": "commit", "sha": commit}})
	case strings.HasPrefix(p, "/repos/"+testRepo+"/compare/"):
		spec := strings.TrimPrefix(p, "/repos/"+testRepo+"/compare/")
		base, head, _ := strings.Cut(spec, "...")
		if head != "main" {
			http.Error(w, `{"message":"unexpected compare head"}`, http.StatusBadRequest)
			return
		}
		status, ok := f.compare[base]
		if !ok {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		reply(map[string]string{"status": status})
	case p == "/repos/"+testRepo+"/actions/workflows/components.yml/runs":
		if q.Get("event") != "push" || q.Get("branch") != "main" {
			reply(map[string]any{"workflow_runs": []workflowRun{}})
			return
		}
		reply(map[string]any{"workflow_runs": f.runs[q.Get("head_sha")]})
	case strings.HasPrefix(p, "/users/"+testOwner+"/packages/container/") && strings.HasSuffix(p, "/versions"):
		name := strings.TrimSuffix(strings.TrimPrefix(p, "/users/"+testOwner+"/packages/container/"), "/versions")
		reply(f.packages[name])
	case p == "/repos/"+testRepo+"/contents/api/migrations":
		files, ok := f.migrations[q.Get("ref")]
		if !ok {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		var out []map[string]string
		for _, name := range files {
			out = append(out, map[string]string{"name": name, "type": "file"})
		}
		reply(out)
	case p == "/repos/"+testRepo+"/deployments":
		var out []deployment
		for _, d := range f.deployments[q.Get("environment")] {
			if d.SHA == q.Get("sha") {
				out = append(out, d)
			}
		}
		reply(out)
	case strings.HasPrefix(p, "/repos/"+testRepo+"/deployments/") && strings.HasSuffix(p, "/statuses"):
		id, _ := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(p, "/repos/"+testRepo+"/deployments/"), "/statuses"), 10, 64)
		var out []map[string]string
		for _, s := range f.statuses[id] {
			out = append(out, map[string]string{"state": s})
		}
		reply(out)
	default:
		http.Error(w, `{"message":"unexpected path"}`, http.StatusNotFound)
	}
}

func releaseEventJSON(action, tag string, draft, prerelease bool) []byte {
	b, _ := json.Marshal(map[string]any{
		"action":  action,
		"release": map[string]any{"tag_name": tag, "draft": draft, "prerelease": prerelease},
		"repository": map[string]any{
			"full_name": testRepo,
			"owner":     map[string]string{"login": testOwner, "type": "User"},
		},
	})
	return b
}

func evaluate(t *testing.T, f *fakeGitHub, eventName string, event []byte) Decision {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	d, err := Evaluate(context.Background(), Input{
		EventName:  eventName,
		Event:      event,
		Repository: testRepo,
	}, &Client{BaseURL: srv.URL, Token: "test-token", HTTP: srv.Client()})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	return d
}

// TestDeniedEventsNeverDeploy covers AC1's non-deployable fixtures.
func TestDeniedEventsNeverDeploy(t *testing.T) {
	cases := []struct {
		name      string
		eventName string
		event     []byte
		mutate    func(*fakeGitHub)
		reason    string
	}{
		{"branch push", "push", []byte(`{"ref":"refs/heads/main"}`), nil, "not a published release"},
		{"release created, not published", "release", releaseEventJSON("created", "api-v1.2.3", false, false), nil, "not a published release"},
		{"release edited", "release", releaseEventJSON("edited", "api-v1.2.3", false, false), nil, "not a published release"},
		{"draft release", "release", releaseEventJSON("published", "api-v1.2.3", true, false), nil, "draft"},
		{"plain v tag", "release", releaseEventJSON("published", "v1.2.3", false, false), nil, "tag"},
		{"unknown component", "release", releaseEventJSON("published", "web-v1.2.3", false, false), nil, "tag"},
		{"incomplete version", "release", releaseEventJSON("published", "api-v1.2", false, false), nil, "tag"},
		{"leading zero", "release", releaseEventJSON("published", "api-v1.02.3", false, false), nil, "tag"},
		{"suffix", "release", releaseEventJSON("published", "api-v1.2.3-rc1", false, false), nil, "tag"},
		{"tag missing", "release", releaseEventJSON("published", "api-v9.9.9", false, false), nil, "tag"},
		{
			"untrusted commit not on main", "release", releaseEventJSON("published", "api-v1.2.3", false, false),
			func(f *fakeGitHub) { f.tags["api-v1.2.3"] = tagRef{"commit", sideSHA} }, "not on main",
		},
		{
			"red main CI", "release", releaseEventJSON("published", "api-v1.2.3", false, false),
			func(f *fakeGitHub) { f.runs[mainSHA][0].Conclusion = "failure" }, "main CI",
		},
		{
			"main CI still running", "release", releaseEventJSON("published", "api-v1.2.3", false, false),
			func(f *fakeGitHub) { f.runs[mainSHA][0].Status, f.runs[mainSHA][0].Conclusion = "in_progress", "" }, "main CI",
		},
		{
			"main CI missing", "release", releaseEventJSON("published", "api-v1.2.3", false, false),
			func(f *fakeGitHub) { delete(f.runs, mainSHA) }, "main CI",
		},
		{
			"CI success only for another commit", "release", releaseEventJSON("published", "api-v1.2.3", false, false),
			func(f *fakeGitHub) { f.runs[mainSHA][0].HeadSHA = sideSHA }, "main CI",
		},
		{
			"API commit without migrations", "release", releaseEventJSON("published", "api-v1.2.3", false, false),
			func(f *fakeGitHub) { f.migrations[mainSHA] = []string{"migrations.go"} }, "migration",
		},
		{
			"no image for commit", "release", releaseEventJSON("published", "api-v1.2.3", false, false),
			func(f *fakeGitHub) { f.packages["gk-cakes-api"] = nil }, "artifact",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			if tc.mutate != nil {
				tc.mutate(f)
			}
			d := evaluate(t, f, tc.eventName, tc.event)
			if d.Action != ActionDeny {
				t.Fatalf("action = %q (%s), want deny", d.Action, d.Reason)
			}
			if !strings.Contains(d.Reason, tc.reason) {
				t.Errorf("reason %q does not mention %q", d.Reason, tc.reason)
			}
			if d.Manifest != nil {
				t.Errorf("denied decision carries a manifest: %+v", d.Manifest)
			}
		})
	}
}

func TestValidReleaseDeploysExactArtifactForOneComponent(t *testing.T) {
	f := newFake()
	d := evaluate(t, f, "release", releaseEventJSON("published", "api-v1.2.3", false, false))
	if d.Action != ActionDeploy {
		t.Fatalf("action = %q (%s), want deploy", d.Action, d.Reason)
	}
	want := Manifest{
		Component:      "api",
		Version:        "1.2.3",
		Tag:            "api-v1.2.3",
		CommitSHA:      mainSHA,
		Image:          "ghcr.io/chichasasha/gk-cakes-api",
		ArtifactDigest: apiDigest,
		Environment:    "production-api",
		SchemaVersion:  testSchema,
		APIContract:    "v1",
	}
	if *d.Manifest != want {
		t.Fatalf("manifest = %+v\nwant       %+v", *d.Manifest, want)
	}
}

func TestAnnotatedUITagResolvesToCommitAndSelectsOnlyUI(t *testing.T) {
	f := newFake()
	d := evaluate(t, f, "release", releaseEventJSON("published", "ui-v0.4.0", false, false))
	if d.Action != ActionDeploy {
		t.Fatalf("action = %q (%s), want deploy", d.Action, d.Reason)
	}
	m := d.Manifest
	if m.Component != "ui" || m.CommitSHA != mainSHA || m.ArtifactDigest != uiDigest ||
		m.Image != "ghcr.io/chichasasha/gk-cakes-ui" || m.Environment != "production-ui" || m.SchemaVersion != "" {
		t.Fatalf("unexpected UI manifest: %+v", *m)
	}
}

// TestPrereleaseRoutesToStaging: prereleases pass the same trust checks but
// target the nonproduction staging environment, never production.
func TestPrereleaseRoutesToStaging(t *testing.T) {
	f := newFake()
	d := evaluate(t, f, "release", releaseEventJSON("published", "api-v1.2.3", false, true))
	if d.Action != ActionDeploy || d.Manifest.Environment != "staging" || d.Manifest.ArtifactDigest != apiDigest {
		t.Fatalf("prerelease decision = %q (%s) manifest %+v; want deploy to staging", d.Action, d.Reason, d.Manifest)
	}
	f.runs[mainSHA][0].Conclusion = "failure"
	if d := evaluate(t, f, "release", releaseEventJSON("published", "api-v1.2.3", false, true)); d.Action != ActionDeny {
		t.Fatalf("prerelease with red main CI = %q, want deny", d.Action)
	}
}

// TestReplayedReleaseIsSkipped: a repeated event for an already deployed
// digest must not migrate or deploy again.
func TestReplayedReleaseIsSkipped(t *testing.T) {
	f := newFake()
	f.deployments["production-api"] = []deployment{
		{ID: 7, SHA: mainSHA, Payload: deploymentPayload{ArtifactDigest: apiDigest}},
	}
	f.statuses[7] = []string{"success"}
	d := evaluate(t, f, "release", releaseEventJSON("published", "api-v1.2.3", false, false))
	if d.Action != ActionSkip || !strings.Contains(d.Reason, "already deployed") {
		t.Fatalf("decision = %q (%s), want skip as already deployed", d.Action, d.Reason)
	}

	// A failed earlier attempt is not a completed deployment: deploy again.
	f.statuses[7] = []string{"failure"}
	if d := evaluate(t, f, "release", releaseEventJSON("published", "api-v1.2.3", false, false)); d.Action != ActionDeploy {
		t.Fatalf("after failed attempt: action = %q (%s), want deploy", d.Action, d.Reason)
	}
}

func TestGitHubErrorsFailClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := Evaluate(context.Background(), Input{
		EventName: "release", Event: releaseEventJSON("published", "api-v1.2.3", false, false),
		Repository: testRepo,
	}, &Client{BaseURL: srv.URL, Token: "test-token", HTTP: srv.Client()})
	if err == nil {
		t.Fatal("Evaluate succeeded although GitHub returned 500")
	}
}

func TestEventForAnotherRepositoryIsDenied(t *testing.T) {
	f := newFake()
	event := releaseEventJSON("published", "api-v1.2.3", false, false)
	srv := httptest.NewServer(f)
	defer srv.Close()
	d, err := Evaluate(context.Background(), Input{
		EventName: "release", Event: event, Repository: "someone/else",
	}, &Client{BaseURL: srv.URL, Token: "test-token", HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != ActionDeny || !strings.Contains(d.Reason, "repository") {
		t.Fatalf("decision = %q (%s), want deny for repository mismatch", d.Action, d.Reason)
	}
}
