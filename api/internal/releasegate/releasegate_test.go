package releasegate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	mainSHA   = "1111111111111111111111111111111111111111"
	sideSHA   = "2222222222222222222222222222222222222222"
	tagObjSHA = "3333333333333333333333333333333333333333"
	oldDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	testRepo  = "ChichaSasha/gk-cakes"
	testOwner = "ChichaSasha"

	dockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
	ociIndex       = "application/vnd.oci.image.index.v1+json"
)

// fakeImage is a single-platform image as the registry stores it: manifest
// and config bytes addressed by their sha256 digests.
type fakeImage struct {
	mediaType, manifest, config string
	digest, configDigest        string
}

func sha256Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// newImage builds an image whose revision label is revision (none if empty).
func newImage(component, revision string) fakeImage {
	labels := map[string]string{"org.opencontainers.image.description": "GK Cakes " + component}
	if revision != "" {
		labels["org.opencontainers.image.revision"] = revision
	}
	cfg, _ := json.Marshal(map[string]any{"architecture": "amd64", "os": "linux", "config": map[string]any{"Labels": labels}})
	cfgDigest := sha256Digest(cfg)
	man, _ := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     dockerManifest,
		"config":        map[string]any{"mediaType": "application/vnd.docker.container.image.v1+json", "size": len(cfg), "digest": cfgDigest},
		"layers":        []any{},
	})
	return fakeImage{mediaType: dockerManifest, manifest: string(man), config: string(cfg), digest: sha256Digest(man), configDigest: cfgDigest}
}

var (
	apiImage  = newImage("api", mainSHA)
	uiImage   = newImage("ui", mainSHA)
	apiDigest = apiImage.digest
	uiDigest  = uiImage.digest
)

// fakeGitHub models the GitHub REST endpoints and the ghcr.io registry API
// the gate reads.
type fakeGitHub struct {
	tags            map[string]tagRef        // tag name -> ref object
	annotated       map[string]tagRef        // annotated tag object sha -> next object
	compare         map[string]string        // commit sha -> compare status against main
	runs            map[string][]workflowRun // commit sha -> Components main-push runs, newest first
	manifests       map[string]string        // "<repo>:<tag>" -> registry digest
	images          map[string]fakeImage     // "<repo>@<manifest digest>" -> image
	blobs           map[string]string        // "<repo>@<blob digest>" -> blob bytes
	migrations      map[string][]string      // commit sha -> files in api/migrations
	deployments     map[string][]deployment  // environment -> deployments, newest first
	statuses        map[int64][]string       // deployment id -> status states, newest first
	privateRegistry bool                     // registry refuses anonymous pull tokens
	failDeployments int32                    // 1-based deployments-list call from which the fake answers 500 (0: never)
	deploymentCalls atomic.Int32
}

// record appends a deployment record (newest last in call order, so call it
// newest first) with the task record-deployment.sh uses for version.
func (f *fakeGitHub) record(env string, id int64, version, digest, state string) {
	task := "deploy"
	if tagPattern.MatchString(version) {
		task = ReleaseTask
	}
	f.deployments[env] = append(f.deployments[env], deployment{ID: id, Task: task, Payload: deploymentPayload{ArtifactDigest: digest, Version: version}})
	f.statuses[id] = []string{state}
}

type tagRef struct{ Type, SHA string }

func newFake() *fakeGitHub {
	f := &fakeGitHub{
		tags:      map[string]tagRef{"api-v1.2.3": {"commit", mainSHA}, "ui-v0.4.0": {"tag", tagObjSHA}},
		annotated: map[string]tagRef{tagObjSHA: {"commit", mainSHA}},
		compare:   map[string]string{mainSHA: "ahead", sideSHA: "diverged"},
		runs: map[string][]workflowRun{
			mainSHA: {{HeadSHA: mainSHA, Event: "push", HeadBranch: "main", Status: "completed", Conclusion: "success"}},
		},
		manifests:   map[string]string{},
		images:      map[string]fakeImage{},
		blobs:       map[string]string{},
		migrations:  map[string][]string{mainSHA: {"0001_roles_and_schema.sql", "0002_job_gateway.sql", "migrations.go"}},
		deployments: map[string][]deployment{},
		statuses:    map[int64][]string{},
	}
	f.push("chichasasha/gk-cakes-api", "sha-"+mainSHA, apiImage)
	f.push("chichasasha/gk-cakes-ui", "sha-"+mainSHA, uiImage)
	return f
}

// push stores img in repo and points tag at it.
func (f *fakeGitHub) push(repo, tag string, img fakeImage) {
	f.manifests[repo+":"+tag] = img.digest
	f.images[repo+"@"+img.digest] = img
	f.blobs[repo+"@"+img.configDigest] = img.config
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	q := r.URL.Query()
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	notFound := func() { http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound) }

	// Registry API (ghcr.io): pull token (anonymous unless the fake demands
	// the GitHub token), then manifest and blob lookups.
	if p == "/registry/token" {
		if f.privateRegistry {
			user, pass, ok := r.BasicAuth()
			if !ok || user != "x-access-token" || pass != "test-token" {
				http.Error(w, "denied", http.StatusForbidden)
				return
			}
		}
		reply(map[string]string{"token": "registry-token"})
		return
	}
	if strings.HasPrefix(p, "/registry/v2/") {
		if r.Header.Get("Authorization") != "Bearer registry-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		rest := strings.TrimPrefix(p, "/registry/v2/")
		if repo, digest, ok := strings.Cut(rest, "/blobs/"); ok {
			blob, found := f.blobs[repo+"@"+digest]
			if !found {
				notFound()
				return
			}
			_, _ = w.Write([]byte(blob))
			return
		}
		repo, ref, ok := strings.Cut(rest, "/manifests/")
		if !ok || !strings.Contains(r.Header.Get("Accept"), "application/vnd.oci.image.manifest.v1+json") {
			http.Error(w, "bad manifest request", http.StatusBadRequest)
			return
		}
		digest := ref
		if !strings.HasPrefix(ref, "sha256:") {
			var found bool
			if digest, found = f.manifests[repo+":"+ref]; !found {
				notFound()
				return
			}
		}
		img, found := f.images[repo+"@"+digest]
		if !found {
			notFound()
			return
		}
		w.Header().Set("Docker-Content-Digest", digest)
		w.Header().Set("Content-Type", img.mediaType)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(img.manifest))
		}
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
		if n := f.deploymentCalls.Add(1); f.failDeployments > 0 && n >= f.failDeployments {
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
			return
		}
		var list []deployment
		for _, d := range f.deployments[q.Get("environment")] {
			if task := q.Get("task"); task == "" || d.Task == task {
				list = append(list, d)
			}
		}
		perPage, page := 30, 1 // GitHub defaults
		if v, err := strconv.Atoi(q.Get("per_page")); err == nil {
			perPage = v
		}
		if v, err := strconv.Atoi(q.Get("page")); err == nil {
			page = v
		}
		start := min((page-1)*perPage, len(list))
		reply(list[start:min(start+perPage, len(list))])
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

// TestReplayIsKeyedOnReleaseIdentity: a repeated event for a release whose
// tag and digest were already deployed successfully is skipped, even after
// later deploys; failed attempts and other releases do not count.
func TestReplayIsKeyedOnReleaseIdentity(t *testing.T) {
	f := newFake()
	f.record("production-api", 9, "api-v1.2.4", oldDigest, "success") // newer release
	f.record("production-api", 7, "api-v1.2.3", apiDigest, "success")
	if d := evaluate(t, f, "release", publish("api-v1.2.3")); d.Action != ActionSkip || !strings.Contains(d.Reason, "already deployed") {
		t.Fatalf("replay after a newer release = %q (%s), want skip", d.Action, d.Reason)
	}

	// Only a failed attempt of this release (and nothing newer): deploy again.
	f.deployments["production-api"] = f.deployments["production-api"][1:]
	f.statuses[7] = []string{"failure"}
	if d := evaluate(t, f, "release", publish("api-v1.2.3")); d.Action != ActionDeploy {
		t.Fatalf("after failed attempt: action = %q (%s), want deploy", d.Action, d.Reason)
	}

	// Same digest deployed under another identity (e.g. a main deploy) is
	// not this release.
	f.deployments["production-api"] = nil
	f.record("production-api", 5, "main-111111111111", apiDigest, "success")
	if d := evaluate(t, f, "release", publish("api-v1.2.3")); d.Action != ActionDeploy {
		t.Fatalf("digest deployed under another identity: action = %q (%s), want deploy", d.Action, d.Reason)
	}
}

// TestOlderReleaseIsDeniedAsDowngrade covers AC2's "cannot downgrade
// unexpectedly": a release older than any release already recorded in the
// same environment is denied, whatever that record's status (a record marked
// failed after Railway switched traffic may be live). Versions compare
// numerically; main deploys and other components do not count; a rollback
// uses a new, higher tag.
func TestOlderReleaseIsDeniedAsDowngrade(t *testing.T) {
	type rec struct {
		version string
		state   string
	}
	cases := []struct {
		name       string
		env        string
		prerelease bool
		records    []rec // newest first
		tag        string
		want       string
		reason     string
	}{
		{"newer release deployed", "production-api", false, []rec{{"api-v1.2.4", "success"}}, "api-v1.2.3", ActionDeny, "older than api-v1.2.4"},
		{"numeric, not string, order", "production-api", false, []rec{{"api-v1.10.0", "success"}}, "api-v1.9.0", ActionDeny, "older than api-v1.10.0"},
		{"newer major deployed", "production-api", false, []rec{{"api-v2.0.0", "success"}}, "api-v1.99.99", ActionDeny, "older than api-v2.0.0"},
		{"highest of several is reported", "production-api", false,
			[]rec{{"api-v1.3.0", "success"}, {"api-v1.5.0", "success"}, {"api-v1.4.0", "success"}}, "api-v1.2.3", ActionDeny, "older than api-v1.5.0"},
		{"prerelease older than staging", "staging-api", true, []rec{{"api-v1.2.4", "success"}}, "api-v1.2.3", ActionDeny, "older than api-v1.2.4"},
		{"newer attempt failed (may be live)", "production-api", false, []rec{{"api-v1.2.4", "failure"}}, "api-v1.2.3", ActionDeny, "older than api-v1.2.4"},
		{"newer attempt still running", "production-api", false, []rec{{"api-v1.2.4", "in_progress"}}, "api-v1.2.3", ActionDeny, "older than api-v1.2.4"},
		{"upgrade is allowed", "production-api", false, []rec{{"api-v1.9.0", "success"}}, "api-v1.10.0", ActionDeploy, ""},
		{"older failed and older success", "production-api", false,
			[]rec{{"api-v1.2.2", "failure"}, {"api-v1.2.1", "success"}}, "api-v1.2.3", ActionDeploy, ""},
		{"main deploys are not releases", "staging-api", true, []rec{{"main-" + mainSHA, "success"}}, "api-v1.2.3", ActionDeploy, ""},
		{"other component ignored", "production-api", false, []rec{{"ui-v9.0.0", "success"}}, "api-v1.2.3", ActionDeploy, ""},
		{"rollback with a new higher tag", "production-api", false, []rec{{"api-v1.2.4", "success"}}, "api-v1.2.5", ActionDeploy, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			f.tags[tc.tag] = tagRef{"commit", mainSHA}
			for i, r := range tc.records {
				f.record(tc.env, int64(100+i), r.version, oldDigest, r.state)
			}
			d := evaluate(t, f, "release", releaseEventJSON("published", tc.tag, false, tc.prerelease))
			if d.Action != tc.want {
				t.Fatalf("action = %q (%s), want %q", d.Action, d.Reason, tc.want)
			}
			if !strings.Contains(d.Reason, tc.reason) {
				t.Errorf("reason %q does not mention %q", d.Reason, tc.reason)
			}
			if tc.want == ActionDeny && d.Manifest != nil {
				t.Errorf("denied decision carries a manifest: %+v", d.Manifest)
			}
		})
	}
}

// TestReplayOfSupersededReleaseIsSkippedNotDenied: an exact replay of an
// older release that was deployed before is a no-op, not a downgrade error.
func TestReplayOfSupersededReleaseIsSkippedNotDenied(t *testing.T) {
	f := newFake()
	f.record("production-api", 9, "api-v1.2.4", oldDigest, "success")
	f.record("production-api", 7, "api-v1.2.3", apiDigest, "success")
	if d := evaluate(t, f, "release", publish("api-v1.2.3")); d.Action != ActionSkip {
		t.Fatalf("replay of superseded release = %q (%s), want skip", d.Action, d.Reason)
	}
}

// TestEvaluateFailsClosedWhenDowngradeLookupFails: the replay lookup (first
// deployments call) succeeds, the downgrade lookup (second) fails.
func TestEvaluateFailsClosedWhenDowngradeLookupFails(t *testing.T) {
	f := newFake()
	f.failDeployments = 2
	d, err := Evaluate(context.Background(), Input{EventName: "release", Event: publish("api-v1.2.3"), Repository: testRepo}, client(t, f))
	if err == nil {
		t.Fatalf("Evaluate = %q (%s) although the downgrade lookup failed, want error", d.Action, d.Reason)
	}
}

// TestReleaseRecordsArePagedAndBounded: lookups list only release records
// (task filter), follow every page, and fail closed past the page cap.
func TestReleaseRecordsArePagedAndBounded(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	for i := 0; i < 250; i++ { // main deploys are filtered out by task
		f.record("staging-ui", int64(10000+i), "main-"+mainSHA, oldDigest, "success")
	}
	for i := 0; i < deploymentsPerPage+20; i++ { // two pages of release records
		f.record("staging-ui", int64(1+i), "ui-v0.4."+strconv.Itoa(i), oldDigest, "success")
	}
	f.deployments["staging-ui"] = append(f.deployments["staging-ui"], deployment{ID: 999, Task: ReleaseTask,
		Payload: deploymentPayload{ArtifactDigest: uiDigest, Version: "ui-v0.9.0"}}) // oldest, on page 2
	f.statuses[999] = []string{"success"}
	c := client(t, f)
	if got, err := c.NewerReleased(ctx, testRepo, "staging-ui", "ui-v0.5.0"); err != nil || got != "ui-v0.9.0" {
		t.Errorf("NewerReleased = %q, %v; want ui-v0.9.0 from page 2", got, err)
	}
	if got, err := c.AlreadyReleased(ctx, testRepo, "staging-ui", "ui-v0.9.0", uiDigest); err != nil || !got {
		t.Errorf("AlreadyReleased of a page-2 record = %v, %v; want true", got, err)
	}
	if _, err := c.NewerReleased(ctx, testRepo, "staging-ui", "ui-v1"); err == nil {
		t.Error("NewerReleased accepted a malformed tag")
	}

	// The cap is 50 pages of 100 release records; a literal keeps this test
	// independent of the constant it checks.
	const pastCap = 5001
	big := newFake()
	for i := 0; i < pastCap; i++ {
		big.deployments["staging-ui"] = append(big.deployments["staging-ui"], deployment{ID: int64(i + 1), Task: ReleaseTask,
			Payload: deploymentPayload{ArtifactDigest: oldDigest, Version: "ui-v0.0." + strconv.Itoa(i)}})
	}
	if _, err := client(t, big).NewerReleased(ctx, testRepo, "staging-ui", "ui-v9.0.0"); err == nil || !strings.Contains(err.Error(), "pages") {
		t.Errorf("history past the page cap: err = %v, want fail-closed page error", err)
	}

	// Main deploys never count toward the cap: thousands of them plus one
	// release record still resolve on one page.
	busy := newFake()
	for i := 0; i < pastCap; i++ {
		busy.record("staging-ui", int64(i+1), "main-"+mainSHA, oldDigest, "success")
	}
	busy.record("staging-ui", 99999, "ui-v0.9.0", uiDigest, "success")
	if got, err := client(t, busy).NewerReleased(ctx, testRepo, "staging-ui", "ui-v0.5.0"); err != nil || got != "ui-v0.9.0" {
		t.Errorf("busy main history: NewerReleased = %q, %v; want ui-v0.9.0 without hitting the cap", got, err)
	}
}

// TestVerifySequencesArtifactReplayAndDowngrade covers the deploy-time
// re-check that enforces ordering between concurrent releases.
func TestVerifySequencesArtifactReplayAndDowngrade(t *testing.T) {
	base := VerifyRequest{Repository: testRepo, Owner: testOwner, Component: "ui", CommitSHA: mainSHA, Digest: uiDigest,
		Environment: "staging-ui", Tag: "ui-v0.4.0"}
	cases := []struct {
		name    string
		mutate  func(*VerifyRequest, *fakeGitHub)
		want    VerifyOutcome
		reason  string
		wantErr string
	}{
		{"new release", nil, VerifyDeploy, "verified", ""},
		{"artifact only, no replay checks", func(r *VerifyRequest, _ *fakeGitHub) { r.Environment, r.Tag = "", "" }, VerifyDeploy, "verified", ""},
		{"replay of an older, superseded release", func(_ *VerifyRequest, f *fakeGitHub) {
			f.record("staging-ui", 2, "ui-v0.5.0", oldDigest, "success")
			f.record("staging-ui", 1, "ui-v0.4.0", uiDigest, "success")
		}, VerifyReplay, "already deployed", ""},
		{"older than a recorded release", func(_ *VerifyRequest, f *fakeGitHub) {
			f.record("staging-ui", 2, "ui-v0.5.0", oldDigest, "failure")
		}, VerifyRejected, "older than ui-v0.5.0", ""},
		{"wrong digest", func(r *VerifyRequest, _ *fakeGitHub) { r.Digest = oldDigest }, VerifyRejected, "digest", ""},
		{"tag of another component", func(r *VerifyRequest, _ *fakeGitHub) { r.Tag = "api-v0.0.0" }, VerifyRejected, "not a ui release", ""},
		{"environment of another component", func(r *VerifyRequest, _ *fakeGitHub) { r.Environment = "staging-api" }, VerifyRejected, "environment", ""},
		{"tag without environment", func(r *VerifyRequest, _ *fakeGitHub) { r.Environment = "" }, VerifyRejected, "together", ""},
		{"malformed tag", func(r *VerifyRequest, _ *fakeGitHub) { r.Tag = "ui-v0.4" }, VerifyRejected, "is not ui-vX.Y.Z", ""},
		{"replay lookup fails", func(_ *VerifyRequest, f *fakeGitHub) { f.failDeployments = 1 }, "", "", "replay check"},
		{"downgrade lookup fails", func(_ *VerifyRequest, f *fakeGitHub) { f.failDeployments = 2 }, "", "", "downgrade check"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			r := base
			if tc.mutate != nil {
				tc.mutate(&r, f)
			}
			res, err := client(t, f).Verify(context.Background(), r)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Verify = %+v, err %v; want error mentioning %q", res, err, tc.wantErr)
				}
				return
			}
			if err != nil || res.Outcome != tc.want || !strings.Contains(res.Reason, tc.reason) {
				t.Fatalf("Verify = %+v, err %v; want %q mentioning %q", res, err, tc.want, tc.reason)
			}
		})
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

// TestArtifactMustCarryItsCommitRevision: the image behind sha-<commit> must
// say it was built from that commit (org.opencontainers.image.revision), and
// the manifest and config bytes must match their digests, so a retagged or
// tampered image cannot be released.
func TestArtifactMustCarryItsCommitRevision(t *testing.T) {
	const apiRepo = "chichasasha/gk-cakes-api"
	cases := []struct {
		name    string
		mutate  func(*fakeGitHub)
		denied  string // deny reason; empty means the gate must return an error
		errText string
	}{
		{"image built from another commit", func(f *fakeGitHub) {
			f.push(apiRepo, "sha-"+mainSHA, newImage("api", sideSHA))
		}, "built from commit \"" + sideSHA + "\"", ""},
		{"image without revision label", func(f *fakeGitHub) {
			f.push(apiRepo, "sha-"+mainSHA, newImage("api", ""))
		}, "built from commit \"\"", ""},
		{"manifest bytes do not match digest", func(f *fakeGitHub) {
			img := f.images[apiRepo+"@"+apiDigest]
			img.manifest += " "
			f.images[apiRepo+"@"+apiDigest] = img
		}, "", "manifest does not match"},
		{"config bytes do not match digest", func(f *fakeGitHub) {
			f.blobs[apiRepo+"@"+apiImage.configDigest] = strings.Replace(apiImage.config, mainSHA, sideSHA, 1)
		}, "", "config does not match"},
		{"multi-platform index", func(f *fakeGitHub) {
			img := apiImage
			img.mediaType = ociIndex
			img.manifest = `{"schemaVersion":2,"mediaType":"` + ociIndex + `","manifests":[]}`
			img.digest = sha256Digest([]byte(img.manifest))
			f.push(apiRepo, "sha-"+mainSHA, img)
		}, "", "not a single-platform image manifest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			tc.mutate(f)
			d, err := Evaluate(context.Background(), Input{EventName: "release", Event: publish("api-v1.2.3"), Repository: testRepo}, client(t, f))
			if tc.denied == "" {
				if err == nil || !strings.Contains(err.Error(), tc.errText) {
					t.Fatalf("decision %q (%s), err %v; want error mentioning %q", d.Action, d.Reason, err, tc.errText)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if d.Action != ActionDeny || !strings.Contains(d.Reason, tc.denied) || d.Manifest != nil {
				t.Fatalf("decision %q (%s); want deny mentioning %q without manifest", d.Action, d.Reason, tc.denied)
			}
		})
	}

	// The deploy-time re-check applies the same rule.
	f := newFake()
	f.push(apiRepo, "sha-"+mainSHA, newImage("api", sideSHA))
	retagged := f.manifests[apiRepo+":sha-"+mainSHA]
	err := client(t, f).VerifyArtifact(context.Background(), testRepo, testOwner, "api", mainSHA, retagged)
	if err == nil || !strings.Contains(err.Error(), "built from commit") {
		t.Fatalf("VerifyArtifact of a retagged image: err = %v, want revision mismatch", err)
	}
}

// TestManifestMediaTypeFallbackAndSizeCap: OCI manifests may omit mediaType,
// then the Content-Type header decides; registry objects are size-capped.
func TestManifestMediaTypeFallbackAndSizeCap(t *testing.T) {
	const apiRepo = "chichasasha/gk-cakes-api"
	// withManifest re-addresses apiImage under a different manifest body.
	withManifest := func(servedType string, manifest map[string]any) fakeImage {
		img := apiImage
		b, _ := json.Marshal(manifest)
		img.mediaType, img.manifest, img.digest = servedType, string(b), sha256Digest(b)
		return img
	}
	ociNoType := map[string]any{"schemaVersion": 2, "config": map[string]any{"digest": apiImage.configDigest}, "layers": []any{}}
	cases := []struct {
		name    string
		img     fakeImage
		errText string // empty: deploy
	}{
		{"OCI manifest without mediaType, served as OCI manifest", withManifest(ociManifestType, ociNoType), ""},
		{"no mediaType, served as an index", withManifest(ociIndex, ociNoType), "not a single-platform image manifest"},
		{"oversized manifest", withManifest(ociManifestType, map[string]any{
			"schemaVersion": 2, "mediaType": ociManifestType, "config": map[string]any{"digest": apiImage.configDigest},
			"annotations": map[string]string{"pad": strings.Repeat("x", maxRegistryObject)},
		}), "larger than"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			f.push(apiRepo, "sha-"+mainSHA, tc.img)
			d, err := Evaluate(context.Background(), Input{EventName: "release", Event: publish("api-v1.2.3"), Repository: testRepo}, client(t, f))
			if tc.errText == "" {
				if err != nil || d.Action != ActionDeploy || d.Manifest.ArtifactDigest != tc.img.digest {
					t.Fatalf("decision %q (%s), err %v; want deploy of %s", d.Action, d.Reason, err, tc.img.digest)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.errText) {
				t.Fatalf("decision %q (%s), err %v; want error mentioning %q", d.Action, d.Reason, err, tc.errText)
			}
		})
	}
}

func TestPrivateRegistryUsesGitHubToken(t *testing.T) {
	f := newFake()
	f.privateRegistry = true
	if d := evaluate(t, f, "release", publish("api-v1.2.3")); d.Action != ActionDeploy || d.Manifest.ArtifactDigest != apiDigest {
		t.Fatalf("private registry: action = %q (%s), want deploy with the registry digest", d.Action, d.Reason)
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
