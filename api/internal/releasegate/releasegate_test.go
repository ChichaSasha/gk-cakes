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
	mainSHA   = "1111111111111111111111111111111111111111"
	sideSHA   = "2222222222222222222222222222222222222222"
	tagObjSHA = "3333333333333333333333333333333333333333"
	apiDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	uiDigest  = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	oldDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	testRepo  = "ChichaSasha/gk-cakes"
	testOwner = "ChichaSasha"
)

// fakeGitHub models the GitHub REST endpoints and the ghcr.io registry API
// the gate reads.
type fakeGitHub struct {
	tags        map[string]tagRef        // tag name -> ref object
	annotated   map[string]tagRef        // annotated tag object sha -> next object
	compare     map[string]string        // commit sha -> compare status against main
	runs        map[string][]workflowRun // commit sha -> Components main-push runs, newest first
	manifests   map[string]string        // "<repo>:<tag>" -> registry digest
	migrations  map[string][]string      // commit sha -> files in api/migrations
	deployments map[string][]deployment  // environment -> deployments, newest first
	statuses    map[int64][]string       // deployment id -> status states, newest first
}

type tagRef struct{ Type, SHA string }

func newFake() *fakeGitHub {
	return &fakeGitHub{
		tags:      map[string]tagRef{"api-v1.2.3": {"commit", mainSHA}, "ui-v0.4.0": {"tag", tagObjSHA}},
		annotated: map[string]tagRef{tagObjSHA: {"commit", mainSHA}},
		compare:   map[string]string{mainSHA: "ahead", sideSHA: "diverged"},
		runs: map[string][]workflowRun{
			mainSHA: {{HeadSHA: mainSHA, Event: "push", HeadBranch: "main", Status: "completed", Conclusion: "success"}},
		},
		manifests: map[string]string{
			"chichasasha/gk-cakes-api:sha-" + mainSHA: apiDigest,
			"chichasasha/gk-cakes-ui:sha-" + mainSHA:  uiDigest,
		},
		migrations:  map[string][]string{mainSHA: {"0001_roles_and_schema.sql", "0002_job_gateway.sql", "migrations.go"}},
		deployments: map[string][]deployment{},
		statuses:    map[int64][]string{},
	}
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	q := r.URL.Query()
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	notFound := func() { http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound) }

	// Registry API (ghcr.io): anonymous token, then manifest lookup.
	if p == "/registry/token" {
		reply(map[string]string{"token": "registry-token"})
		return
	}
	if strings.HasPrefix(p, "/registry/v2/") {
		if r.Header.Get("Authorization") != "Bearer registry-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		rest := strings.TrimPrefix(p, "/registry/v2/")
		repo, tag, ok := strings.Cut(rest, "/manifests/")
		if !ok || !strings.Contains(r.Header.Get("Accept"), "application/vnd.oci.image.manifest.v1+json") {
			http.Error(w, "bad manifest request", http.StatusBadRequest)
			return
		}
		digest, found := f.manifests[repo+":"+tag]
		if !found {
			notFound()
			return
		}
		w.Header().Set("Docker-Content-Digest", digest)
		return
	}

	if r.Header.Get("Authorization") != "Bearer test-token" {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		return
	}
	switch {
	case strings.HasPrefix(p, "/repos/"+testRepo+"/git/ref/tags/"):
		ref, ok := f.tags[strings.TrimPrefix(p, "/repos/"+testRepo+"/git/ref/tags/")]
		if !ok {
			notFound()
			return
		}
		reply(map[string]any{"object": map[string]string{"type": ref.Type, "sha": ref.SHA}})
	case strings.HasPrefix(p, "/repos/"+testRepo+"/git/tags/"):
		next := f.annotated[strings.TrimPrefix(p, "/repos/"+testRepo+"/git/tags/")]
		reply(map[string]any{"object": map[string]string{"type": next.Type, "sha": next.SHA}})
	case strings.HasPrefix(p, "/repos/"+testRepo+"/compare/"):
		base, head, _ := strings.Cut(strings.TrimPrefix(p, "/repos/"+testRepo+"/compare/"), "...")
		status, ok := f.compare[base]
		if head != "main" || !ok {
			notFound()
			return
		}
		reply(map[string]string{"status": status})
	case p == "/repos/"+testRepo+"/actions/workflows/components.yml/runs":
		if q.Get("event") != "push" || q.Get("branch") != "main" {
			reply(map[string]any{"workflow_runs": []workflowRun{}})
			return
		}
		reply(map[string]any{"workflow_runs": f.runs[q.Get("head_sha")]})
	case p == "/repos/"+testRepo+"/contents/api/migrations":
		files, ok := f.migrations[q.Get("ref")]
		if !ok {
			notFound()
			return
		}
		var out []map[string]string
		for _, name := range files {
			out = append(out, map[string]string{"name": name, "type": "file"})
		}
		reply(out)
	case p == "/repos/"+testRepo+"/deployments":
		reply(f.deployments[q.Get("environment")])
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

func client(t *testing.T, f *fakeGitHub) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, RegistryURL: srv.URL + "/registry", Token: "test-token", HTTP: srv.Client()}
}

func evaluate(t *testing.T, f *fakeGitHub, eventName string, event []byte) Decision {
	t.Helper()
	d, err := Evaluate(context.Background(), Input{EventName: eventName, Event: event, Repository: testRepo}, client(t, f))
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	return d
}

func publish(tag string) []byte { return releaseEventJSON("published", tag, false, false) }

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
		{"plain v tag", "release", publish("v1.2.3"), nil, "is not ui-vX.Y.Z or api-vX.Y.Z"},
		{"unknown component", "release", publish("web-v1.2.3"), nil, "is not ui-vX.Y.Z or api-vX.Y.Z"},
		{"incomplete version", "release", publish("api-v1.2"), nil, "is not ui-vX.Y.Z or api-vX.Y.Z"},
		{"leading zero", "release", publish("api-v1.02.3"), nil, "is not ui-vX.Y.Z or api-vX.Y.Z"},
		{"suffix", "release", publish("api-v1.2.3-rc1"), nil, "is not ui-vX.Y.Z or api-vX.Y.Z"},
		{"tag missing", "release", publish("api-v9.9.9"), nil, "does not exist"},
		{"untrusted commit not on main", "release", publish("api-v1.2.3"),
			func(f *fakeGitHub) { f.tags["api-v1.2.3"] = tagRef{"commit", sideSHA} }, "not on main"},
		{"commit behind is not ancestry", "release", publish("api-v1.2.3"),
			func(f *fakeGitHub) { f.compare[mainSHA] = "behind" }, "not on main"},
		{"red main CI", "release", publish("api-v1.2.3"),
			func(f *fakeGitHub) { f.runs[mainSHA][0].Conclusion = "failure" }, "main CI"},
		{"main CI still running", "release", publish("api-v1.2.3"),
			func(f *fakeGitHub) { f.runs[mainSHA][0].Status, f.runs[mainSHA][0].Conclusion = "in_progress", "" }, "main CI"},
		{"main CI missing", "release", publish("api-v1.2.3"),
			func(f *fakeGitHub) { delete(f.runs, mainSHA) }, "main CI"},
		{"CI success only for another commit", "release", publish("api-v1.2.3"),
			func(f *fakeGitHub) { f.runs[mainSHA][0].HeadSHA = sideSHA }, "main CI"},
		{"newest run red although an older run passed", "release", publish("api-v1.2.3"),
			func(f *fakeGitHub) {
				f.runs[mainSHA] = append([]workflowRun{{HeadSHA: mainSHA, Event: "push", HeadBranch: "main", Status: "completed", Conclusion: "failure"}}, f.runs[mainSHA]...)
			}, "main CI"},
		{"API commit without migrations", "release", publish("api-v1.2.3"),
			func(f *fakeGitHub) { f.migrations[mainSHA] = []string{"migrations.go"} }, "migration"},
		{"no image for commit", "release", publish("api-v1.2.3"),
			func(f *fakeGitHub) { delete(f.manifests, "chichasasha/gk-cakes-api:sha-"+mainSHA) }, "artifact"},
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

func TestAnnotatedTagChainsAreBoundedAndMustEndAtACommit(t *testing.T) {
	f := newFake()
	f.annotated[tagObjSHA] = tagRef{"tag", tagObjSHA} // loops forever if unbounded
	if _, err := Evaluate(context.Background(), Input{EventName: "release", Event: publish("ui-v0.4.0"), Repository: testRepo}, client(t, f)); err == nil {
		t.Error("cyclic annotated tag chain accepted")
	}
	f.annotated[tagObjSHA] = tagRef{"tree", sideSHA}
	if _, err := Evaluate(context.Background(), Input{EventName: "release", Event: publish("ui-v0.4.0"), Repository: testRepo}, client(t, f)); err == nil {
		t.Error("tag pointing at a tree accepted")
	}
}

func TestValidReleaseDeploysExactArtifactForOneComponent(t *testing.T) {
	d := evaluate(t, newFake(), "release", publish("api-v1.2.3"))
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
		SchemaVersion:  "0002_job_gateway",
		APIContract:    "v1",
	}
	if *d.Manifest != want {
		t.Fatalf("manifest = %+v\nwant       %+v", *d.Manifest, want)
	}
}

func TestAnnotatedUITagResolvesToCommitAndSelectsOnlyUI(t *testing.T) {
	d := evaluate(t, newFake(), "release", publish("ui-v0.4.0"))
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
// target the nonproduction staging record for their component.
func TestPrereleaseRoutesToStaging(t *testing.T) {
	f := newFake()
	d := evaluate(t, f, "release", releaseEventJSON("published", "api-v1.2.3", false, true))
	if d.Action != ActionDeploy || d.Manifest.Environment != "staging-api" || d.Manifest.ArtifactDigest != apiDigest {
		t.Fatalf("prerelease decision = %q (%s) manifest %+v; want deploy to staging-api", d.Action, d.Reason, d.Manifest)
	}
	f.runs[mainSHA][0].Conclusion = "failure"
	if d := evaluate(t, f, "release", releaseEventJSON("published", "api-v1.2.3", false, true)); d.Action != ActionDeny {
		t.Fatalf("prerelease with red main CI = %q, want deny", d.Action)
	}
}

// TestReplaySkipsOnlyWhenTheLatestSuccessfulDeploymentMatches: a repeated
// event for the artifact that is currently deployed is skipped; redeploying
// an older artifact after a newer one (a rollback) is not mistaken for a replay.
func TestReplaySkipsOnlyWhenTheLatestSuccessfulDeploymentMatches(t *testing.T) {
	f := newFake()
	f.deployments["production-api"] = []deployment{
		{ID: 9, Payload: deploymentPayload{ArtifactDigest: oldDigest}}, // newest attempt, failed
		{ID: 7, Payload: deploymentPayload{ArtifactDigest: apiDigest}},
	}
	f.statuses[9] = []string{"failure"}
	f.statuses[7] = []string{"success"}
	if d := evaluate(t, f, "release", publish("api-v1.2.3")); d.Action != ActionSkip || !strings.Contains(d.Reason, "already deployed") {
		t.Fatalf("replay = %q (%s), want skip as already deployed", d.Action, d.Reason)
	}

	// A newer artifact has since been deployed successfully: deploying this
	// older one again is a rollback, not a replay.
	f.statuses[9] = []string{"success"}
	if d := evaluate(t, f, "release", publish("api-v1.2.3")); d.Action != ActionDeploy {
		t.Fatalf("rollback to an older artifact = %q (%s), want deploy", d.Action, d.Reason)
	}

	// Only a failed earlier attempt: deploy again.
	f.deployments["production-api"] = f.deployments["production-api"][1:]
	f.statuses[7] = []string{"failure"}
	if d := evaluate(t, f, "release", publish("api-v1.2.3")); d.Action != ActionDeploy {
		t.Fatalf("after failed attempt: action = %q (%s), want deploy", d.Action, d.Reason)
	}
}

func TestVerifyArtifactChecksCommitCIAndDigest(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	c := client(t, f)
	if err := c.VerifyArtifact(ctx, testRepo, testOwner, "api", mainSHA, apiDigest); err != nil {
		t.Fatalf("valid artifact rejected: %v", err)
	}
	cases := map[string]struct {
		component, sha, digest string
		mutate                 func(*fakeGitHub)
		want                   string
	}{
		"wrong digest":       {"api", mainSHA, oldDigest, nil, "digest"},
		"other component":    {"web", mainSHA, apiDigest, nil, "component"},
		"commit not on main": {"api", sideSHA, apiDigest, nil, "not on main"},
		"red main CI": {"api", mainSHA, apiDigest,
			func(f *fakeGitHub) { f.runs[mainSHA][0].Conclusion = "failure" }, "main CI"},
		"malformed digest": {"api", mainSHA, "sha256:abc", nil, "digest"},
		"malformed sha":    {"api", "main", apiDigest, nil, "commit"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFake()
			if tc.mutate != nil {
				tc.mutate(f)
			}
			err := client(t, f).VerifyArtifact(ctx, testRepo, testOwner, tc.component, tc.sha, tc.digest)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want mention of %q", err, tc.want)
			}
		})
	}
}

func TestGitHubErrorsFailClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := Evaluate(context.Background(), Input{EventName: "release", Event: publish("api-v1.2.3"), Repository: testRepo},
		&Client{BaseURL: srv.URL, RegistryURL: srv.URL, Token: "test-token", HTTP: srv.Client()})
	if err == nil {
		t.Fatal("Evaluate succeeded although GitHub returned 500")
	}
}

func TestEventForAnotherRepositoryIsDenied(t *testing.T) {
	d, err := Evaluate(context.Background(), Input{EventName: "release", Event: publish("api-v1.2.3"), Repository: "someone/else"}, client(t, newFake()))
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != ActionDeny || !strings.Contains(d.Reason, "repository") {
		t.Fatalf("decision = %q (%s), want deny for repository mismatch", d.Action, d.Reason)
	}
}
