// Package releasegate decides whether a GitHub release event may deploy a
// component, and which exact artifact it deploys. It implements the delivery
// contract: only a published ui-vX.Y.Z or api-vX.Y.Z release whose tag points
// at a commit on main with a successful main CI run, and an image built from
// that commit, may deploy; a repeated event for an already deployed artifact
// is skipped. Full releases target production-<component>; prereleases target
// staging-<component> only. Artifact digests are read from the container
// registry (ghcr.io), the same source the publish step resolves. Any GitHub API failure is returned as an error so callers fail
// closed.
package releasegate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Actions a decision can take.
const (
	ActionDeploy = "deploy"
	ActionSkip   = "skip"
	ActionDeny   = "deny"
)

// MainWorkflow is the CI workflow whose success on main gates releases.
const MainWorkflow = "components.yml"

var tagPattern = regexp.MustCompile(`^(ui|api)-v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// Decision is the gate's verdict. Manifest is set for deploy and skip only.
type Decision struct {
	Action   string
	Reason   string
	Manifest *Manifest
}

// Manifest identifies exactly what a release deploys.
type Manifest struct {
	Component      string `json:"component"`
	Version        string `json:"version"`
	Tag            string `json:"tag"`
	CommitSHA      string `json:"commit_sha"`
	Image          string `json:"image"`
	ArtifactDigest string `json:"artifact_digest"`
	Environment    string `json:"environment"`
	SchemaVersion  string `json:"schema_version,omitempty"`
	APIContract    string `json:"api_contract"`
}

// Input is the triggering event and repository context.
type Input struct {
	EventName  string
	Event      []byte
	Repository string
}

// Client calls the GitHub REST API and the ghcr.io registry API.
type Client struct {
	BaseURL     string
	RegistryURL string // default https://ghcr.io
	Token       string
	HTTP        *http.Client
}

var (
	shaPattern    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type releaseEvent struct {
	Action  string `json:"action"`
	Release struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	} `json:"release"`
	Repository struct {
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"owner"`
	} `json:"repository"`
}

func deny(format string, args ...any) Decision {
	return Decision{Action: ActionDeny, Reason: fmt.Sprintf(format, args...)}
}

// Evaluate returns the decision for in.
func Evaluate(ctx context.Context, in Input, gh *Client) (Decision, error) {
	if in.EventName != "release" {
		return deny("event %q is not a published release", in.EventName), nil
	}
	var ev releaseEvent
	if err := json.Unmarshal(in.Event, &ev); err != nil {
		return deny("release event payload is unreadable, so not a published release"), nil
	}
	if ev.Action != "published" {
		return deny("release action %q is not a published release", ev.Action), nil
	}
	if ev.Repository.FullName != in.Repository {
		return deny("event repository %q does not match %q", ev.Repository.FullName, in.Repository), nil
	}
	if ev.Release.Draft {
		return deny("release %q is a draft", ev.Release.TagName), nil
	}
	tag := ev.Release.TagName
	m := tagPattern.FindStringSubmatch(tag)
	if m == nil {
		return deny("tag %q is not ui-vX.Y.Z or api-vX.Y.Z", tag), nil
	}
	component, version := m[1], m[2]+"."+m[3]+"."+m[4]

	sha, found, err := gh.resolveTag(ctx, in.Repository, tag)
	if err != nil {
		return Decision{}, err
	}
	if !found {
		return deny("tag %q does not exist in %s", tag, in.Repository), nil
	}
	onMain, err := gh.onMain(ctx, in.Repository, sha)
	if err != nil {
		return Decision{}, err
	}
	if !onMain {
		return deny("commit %s for tag %q is not on main", sha, tag), nil
	}
	passed, err := gh.mainCIPassed(ctx, in.Repository, sha)
	if err != nil {
		return Decision{}, err
	}
	if !passed {
		return deny("no successful main CI run (%s, push to main) for commit %s", MainWorkflow, sha), nil
	}
	pkg := "gk-cakes-" + component
	digest, err := gh.registryDigest(ctx, imagePath(ev.Repository.Owner.Login, component), "sha-"+sha)
	if err != nil {
		return Decision{}, err
	}
	if digest == "" {
		return deny("no artifact %s tagged sha-%s exists", pkg, sha), nil
	}

	manifest := &Manifest{
		Component:      component,
		Version:        version,
		Tag:            tag,
		CommitSHA:      sha,
		Image:          "ghcr.io/" + imagePath(ev.Repository.Owner.Login, component),
		ArtifactDigest: digest,
		Environment:    "production-" + component,
		APIContract:    "v1",
	}
	if ev.Release.Prerelease {
		// Explicit routing: prereleases verify the release path on staging and
		// never reach production.
		manifest.Environment = "staging-" + component
	}
	if component == "api" {
		// Read from the verified commit through the API; the gate never runs
		// code from the tag.
		schema, err := gh.latestMigration(ctx, in.Repository, sha)
		if err != nil {
			return Decision{}, err
		}
		if schema == "" {
			return deny("commit %s has no API migration files", sha), nil
		}
		manifest.SchemaVersion = schema
	}
	released, err := gh.AlreadyReleased(ctx, in.Repository, manifest.Environment, tag, digest)
	if err != nil {
		return Decision{}, err
	}
	if released {
		return Decision{Action: ActionSkip, Reason: fmt.Sprintf("release %s (%s) is already deployed to %s", tag, digest, manifest.Environment), Manifest: manifest}, nil
	}
	newer, err := gh.NewerReleased(ctx, in.Repository, manifest.Environment, tag)
	if err != nil {
		return Decision{}, err
	}
	if newer != "" {
		return deny("release %s is older than %s, already deployed to %s; a rollback needs a new, higher tag", tag, newer, manifest.Environment), nil
	}
	return Decision{Action: ActionDeploy, Reason: "verified release", Manifest: manifest}, nil
}

var errNotFound = errors.New("not found")

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	u := strings.TrimRight(c.BaseURL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("GET %s: decode: %w", path, err)
	}
	return nil
}

type gitObject struct {
	Object struct {
		Type string `json:"type"`
		SHA  string `json:"sha"`
	} `json:"object"`
}

// resolveTag follows lightweight and annotated tags to the commit SHA.
func (c *Client) resolveTag(ctx context.Context, repo, tag string) (string, bool, error) {
	var ref gitObject
	err := c.get(ctx, "/repos/"+repo+"/git/ref/tags/"+tag, nil, &ref)
	if errors.Is(err, errNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	for depth := 0; ref.Object.Type == "tag"; depth++ {
		if depth >= 5 {
			return "", false, fmt.Errorf("tag %q: annotated tag chain too deep", tag)
		}
		next := gitObject{}
		if err := c.get(ctx, "/repos/"+repo+"/git/tags/"+ref.Object.SHA, nil, &next); err != nil {
			return "", false, err
		}
		ref = next
	}
	if ref.Object.Type != "commit" {
		return "", false, fmt.Errorf("tag %q points at a %s, not a commit", tag, ref.Object.Type)
	}
	return ref.Object.SHA, true, nil
}

// onMain reports whether sha is reachable from main (main is ahead of or
// identical to it).
func (c *Client) onMain(ctx context.Context, repo, sha string) (bool, error) {
	var cmp struct {
		Status string `json:"status"`
	}
	err := c.get(ctx, "/repos/"+repo+"/compare/"+sha+"...main", nil, &cmp)
	if errors.Is(err, errNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return cmp.Status == "ahead" || cmp.Status == "identical", nil
}

type workflowRun struct {
	HeadSHA    string `json:"head_sha"`
	Event      string `json:"event"`
	HeadBranch string `json:"head_branch"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// mainCIPassed requires the newest main-push run of MainWorkflow for exactly
// sha to have completed successfully.
func (c *Client) mainCIPassed(ctx context.Context, repo, sha string) (bool, error) {
	var page struct {
		Runs []workflowRun `json:"workflow_runs"`
	}
	q := url.Values{"head_sha": {sha}, "event": {"push"}, "branch": {"main"}, "per_page": {"100"}}
	if err := c.get(ctx, "/repos/"+repo+"/actions/workflows/"+MainWorkflow+"/runs", q, &page); err != nil {
		return false, err
	}
	for _, r := range page.Runs { // newest first
		if r.HeadSHA != sha || r.Event != "push" || r.HeadBranch != "main" {
			continue
		}
		return r.Status == "completed" && r.Conclusion == "success", nil
	}
	return false, nil
}

func imagePath(owner, component string) string {
	return strings.ToLower(owner) + "/gk-cakes-" + component
}

// registryDigest returns the manifest digest for repo:tag in the registry, or
// "" when the tag does not exist. It tries an anonymous pull token first
// (public packages) and then the GitHub token.
func (c *Client) registryDigest(ctx context.Context, repo, tag string) (string, error) {
	base := strings.TrimRight(c.RegistryURL, "/")
	if base == "" {
		base = "https://ghcr.io"
	}
	token, err := c.registryToken(ctx, base, repo, false)
	if err != nil {
		token, err = c.registryToken(ctx, base, repo, true)
	}
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, base+"/v2/"+repo+"/manifests/"+tag, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
	}, ", "))
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("registry %s:%s: %w", repo, tag, err)
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", nil
	case resp.StatusCode/100 != 2:
		return "", fmt.Errorf("registry %s:%s: HTTP %d", repo, tag, resp.StatusCode)
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if !digestPattern.MatchString(digest) {
		return "", fmt.Errorf("registry %s:%s: unexpected digest %q", repo, tag, digest)
	}
	return digest, nil
}

func (c *Client) registryToken(ctx context.Context, base, repo string, authenticated bool) (string, error) {
	u := base + "/token?" + url.Values{"scope": {"repository:" + repo + ":pull"}, "service": {"ghcr.io"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	if authenticated {
		req.SetBasicAuth("x-access-token", c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("registry token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("registry token: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Token == "" {
		return "", errors.New("registry token: no token in response")
	}
	return body.Token, nil
}

// VerifyArtifact re-checks, outside a release event, that digest is the
// registry image built for commit sha of component, and that sha is on main
// with a successful main CI run. Staging uses it before deploying a
// release-path artifact.
func (c *Client) VerifyArtifact(ctx context.Context, repo, owner, component, sha, digest string) error {
	switch {
	case component != "api" && component != "ui":
		return fmt.Errorf("component %q must be api or ui", component)
	case !shaPattern.MatchString(sha):
		return fmt.Errorf("commit %q must be a 40-character SHA", sha)
	case !digestPattern.MatchString(digest):
		return fmt.Errorf("digest %q must be sha256:<64 hex>", digest)
	}
	onMain, err := c.onMain(ctx, repo, sha)
	if err != nil {
		return err
	}
	if !onMain {
		return fmt.Errorf("commit %s is not on main", sha)
	}
	passed, err := c.mainCIPassed(ctx, repo, sha)
	if err != nil {
		return err
	}
	if !passed {
		return fmt.Errorf("no successful main CI run for commit %s", sha)
	}
	got, err := c.registryDigest(ctx, imagePath(owner, component), "sha-"+sha)
	if err != nil {
		return err
	}
	if got != digest {
		return fmt.Errorf("registry digest for %s sha-%s is %q, not %q", imagePath(owner, component), sha, got, digest)
	}
	return nil
}

var migrationFile = regexp.MustCompile(`^([0-9]{4}_[a-z0-9_]+)\.sql$`)

// latestMigration returns the newest api/migrations/NNNN_name version at sha.
func (c *Client) latestMigration(ctx context.Context, repo, sha string) (string, error) {
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	err := c.get(ctx, "/repos/"+repo+"/contents/api/migrations", url.Values{"ref": {sha}}, &entries)
	if errors.Is(err, errNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	latest := ""
	for _, e := range entries {
		if m := migrationFile.FindStringSubmatch(e.Name); e.Type == "file" && m != nil && m[1] > latest {
			latest = m[1]
		}
	}
	return latest, nil
}

type deploymentPayload struct {
	ArtifactDigest string `json:"artifact_digest"`
	Version        string `json:"version"`
}

type deployment struct {
	ID      int64             `json:"id"`
	SHA     string            `json:"sha"`
	Payload deploymentPayload `json:"payload"`
}

// AlreadyReleased reports whether environment has a successful deployment
// record for this exact release (version tag and digest). A replayed release
// is therefore skipped even after later deploys, while a rollback must use a
// new release tag and is deployed.
func (c *Client) AlreadyReleased(ctx context.Context, repo, environment, version, digest string) (bool, error) {
	const perPage, maxPages = 100, 50
	for page := 1; page <= maxPages; page++ {
		var deployments []deployment // newest first
		q := url.Values{"environment": {environment}, "per_page": {fmt.Sprint(perPage)}, "page": {fmt.Sprint(page)}}
		if err := c.get(ctx, "/repos/"+repo+"/deployments", q, &deployments); err != nil {
			return false, err
		}
		for _, d := range deployments {
			if d.Payload.Version != version || d.Payload.ArtifactDigest != digest {
				continue
			}
			ok, err := c.succeeded(ctx, repo, d.ID)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		if len(deployments) < perPage {
			return false, nil
		}
	}
	return false, fmt.Errorf("environment %s: more than %d pages of deployments", environment, maxPages)
}

// semver is a parsed X.Y.Z release version.
type semver [3]int

func (v semver) less(o semver) bool {
	for i := range v {
		if v[i] != o[i] {
			return v[i] < o[i]
		}
	}
	return false
}

// parseTag splits a ui-vX.Y.Z or api-vX.Y.Z tag into component and version.
func parseTag(tag string) (string, semver, bool) {
	m := tagPattern.FindStringSubmatch(tag)
	if m == nil {
		return "", semver{}, false
	}
	var v semver
	for i := range v {
		n, err := strconv.Atoi(m[i+2])
		if err != nil {
			return "", semver{}, false // digits overflow int
		}
		v[i] = n
	}
	return m[1], v, true
}

// NewerReleased returns the highest release tag of tag's component with a
// successful deployment record in environment whose version is greater than
// tag's, or "" when there is none. Deploying tag would then be a downgrade.
// Failed or unfinished attempts and non-release (main-...) deploys do not
// count. Callers run it inside their serialized deploy section so concurrent
// releases are ordered; a deliberate rollback uses a new, higher tag.
func (c *Client) NewerReleased(ctx context.Context, repo, environment, tag string) (string, error) {
	component, version, ok := parseTag(tag)
	if !ok {
		return "", fmt.Errorf("tag %q is not ui-vX.Y.Z or api-vX.Y.Z", tag)
	}
	type candidate struct {
		tag string
		v   semver
		id  int64
	}
	var newer []candidate
	const perPage, maxPages = 100, 50
	for page := 1; ; page++ {
		if page > maxPages {
			return "", fmt.Errorf("environment %s: more than %d pages of deployments", environment, maxPages)
		}
		var deployments []deployment // newest first
		q := url.Values{"environment": {environment}, "per_page": {fmt.Sprint(perPage)}, "page": {fmt.Sprint(page)}}
		if err := c.get(ctx, "/repos/"+repo+"/deployments", q, &deployments); err != nil {
			return "", err
		}
		for _, d := range deployments {
			if comp, v, ok := parseTag(d.Payload.Version); ok && comp == component && version.less(v) {
				newer = append(newer, candidate{d.Payload.Version, v, d.ID})
			}
		}
		if len(deployments) < perPage {
			break
		}
	}
	// Highest version first; only its latest status decides, so stop at the
	// first successful one.
	sort.SliceStable(newer, func(i, j int) bool { return newer[j].v.less(newer[i].v) })
	for _, n := range newer {
		ok, err := c.succeeded(ctx, repo, n.id)
		if err != nil {
			return "", err
		}
		if ok {
			return n.tag, nil
		}
	}
	return "", nil
}

// succeeded reports whether a deployment's latest status is success.
func (c *Client) succeeded(ctx context.Context, repo string, id int64) (bool, error) {
	var statuses []struct {
		State string `json:"state"`
	}
	if err := c.get(ctx, fmt.Sprintf("/repos/%s/deployments/%d/statuses", repo, id), url.Values{"per_page": {"1"}}, &statuses); err != nil {
		return false, err
	}
	return len(statuses) > 0 && statuses[0].State == "success", nil
}
