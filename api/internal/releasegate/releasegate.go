// Package releasegate decides whether a GitHub release event may deploy a
// component, and which exact artifact it deploys. It implements the delivery
// contract: only a published ui-vX.Y.Z or api-vX.Y.Z release whose tag points
// at a commit on main with a successful main CI run, and an image built from
// that commit, may deploy; a repeated event for an already deployed artifact
// is skipped. Full releases target production-<component>; prereleases target
// staging-<component> only. Artifact digests are read from the container
// registry (ghcr.io), the same source the publish step resolves, and the image
// must carry its commit in the org.opencontainers.image.revision label. A
// release older than one already deployed to the same environment is denied.
// Any GitHub or registry API failure is returned as an error so callers fail
// closed.
package releasegate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
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
	revision, err := gh.imageRevision(ctx, imagePath(ev.Repository.Owner.Login, component), digest)
	if err != nil {
		return Decision{}, err
	}
	if revision != sha {
		return deny("artifact %s@%s was built from commit %q (%s), not %s", pkg, digest, revision, RevisionLabel, sha), nil
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
		return deny("release %s is older than %s, already recorded in %s; a rollback needs a new, higher tag", tag, newer, manifest.Environment), nil
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
// "" when the tag does not exist.
func (c *Client) registryDigest(ctx context.Context, repo, tag string) (string, error) {
	base, token, err := c.registryAuth(ctx, repo)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, base+"/v2/"+repo+"/manifests/"+tag, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", manifestAccept)
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

const (
	// RevisionLabel is the image label the publish step sets to the source
	// commit SHA.
	RevisionLabel = "org.opencontainers.image.revision"

	dockerManifestType = "application/vnd.docker.distribution.manifest.v2+json"
	ociManifestType    = "application/vnd.oci.image.manifest.v1+json"
	maxRegistryObject  = 4 << 20
)

var manifestAccept = strings.Join([]string{
	ociManifestType,
	"application/vnd.oci.image.index.v1+json",
	dockerManifestType,
	"application/vnd.docker.distribution.manifest.list.v2+json",
}, ", ")

// imageRevision returns the RevisionLabel of the single-platform image
// repo@digest ("" when absent). The manifest and config bytes are checked
// against their digests, so the label provably belongs to that digest.
func (c *Client) imageRevision(ctx context.Context, repo, digest string) (string, error) {
	base, token, err := c.registryAuth(ctx, repo)
	if err != nil {
		return "", err
	}
	body, contentType, err := c.registryGet(ctx, base+"/v2/"+repo+"/manifests/"+digest, token, manifestAccept)
	if err != nil {
		return "", err
	}
	if got := sha256Of(body); got != digest {
		return "", fmt.Errorf("registry %s@%s: manifest does not match its digest (%s)", repo, digest, got)
	}
	var manifest struct {
		MediaType string `json:"mediaType"`
		Config    struct {
			Digest string `json:"digest"`
		} `json:"config"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return "", fmt.Errorf("registry %s@%s: decode manifest: %w", repo, digest, err)
	}
	mediaType := manifest.MediaType
	if mediaType == "" {
		mediaType, _, _ = strings.Cut(contentType, ";")
	}
	if mediaType != dockerManifestType && mediaType != ociManifestType {
		return "", fmt.Errorf("registry %s@%s: %q is not a single-platform image manifest", repo, digest, mediaType)
	}
	if !digestPattern.MatchString(manifest.Config.Digest) {
		return "", fmt.Errorf("registry %s@%s: unexpected config digest %q", repo, digest, manifest.Config.Digest)
	}
	config, _, err := c.registryGet(ctx, base+"/v2/"+repo+"/blobs/"+manifest.Config.Digest, token, "")
	if err != nil {
		return "", err
	}
	if got := sha256Of(config); got != manifest.Config.Digest {
		return "", fmt.Errorf("registry %s@%s: config does not match its digest %s (%s)", repo, digest, manifest.Config.Digest, got)
	}
	var image struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if err := json.Unmarshal(config, &image); err != nil {
		return "", fmt.Errorf("registry %s@%s: decode config: %w", repo, digest, err)
	}
	return image.Config.Labels[RevisionLabel], nil
}

func sha256Of(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// registryGet fetches a registry object (bounded size) and its content type.
// Blob requests may redirect to storage; the client drops the token there.
func (c *Client) registryGet(ctx context.Context, u, token, accept string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("registry GET: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, "", fmt.Errorf("registry GET %s: HTTP %d", req.URL.Path, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRegistryObject+1))
	if err != nil {
		return nil, "", fmt.Errorf("registry GET %s: %w", req.URL.Path, err)
	}
	if len(body) > maxRegistryObject {
		return nil, "", fmt.Errorf("registry GET %s: object larger than %d bytes", req.URL.Path, maxRegistryObject)
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// registryAuth returns the registry base URL and a pull token for repo. It
// tries an anonymous token first (public packages), then the GitHub token.
func (c *Client) registryAuth(ctx context.Context, repo string) (string, string, error) {
	base := strings.TrimRight(c.RegistryURL, "/")
	if base == "" {
		base = "https://ghcr.io"
	}
	token, err := c.registryToken(ctx, base, repo, false)
	if err != nil {
		token, err = c.registryToken(ctx, base, repo, true)
	}
	if err != nil {
		return "", "", err
	}
	return base, token, nil
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
// registry image tagged and labelled for commit sha of component, and that sha
// is on main with a successful main CI run. Staging uses it before deploying a
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
	revision, err := c.imageRevision(ctx, imagePath(owner, component), digest)
	if err != nil {
		return err
	}
	if revision != sha {
		return fmt.Errorf("artifact %s@%s was built from commit %q (%s), not %s", imagePath(owner, component), digest, revision, RevisionLabel, sha)
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

// ReleaseTask is the GitHub deployment task of release-path records (tag
// versions such as api-v1.2.3). Main-path records use the default "deploy",
// so replay and downgrade lookups list only release records. Records made
// before this task existed (the api-v0.0.1 and ui-v0.0.1 staging test
// prereleases) are not considered.
const ReleaseTask = "deploy:release"

type deploymentPayload struct {
	ArtifactDigest string `json:"artifact_digest"`
	Version        string `json:"version"`
}

type deployment struct {
	ID      int64             `json:"id"`
	SHA     string            `json:"sha"`
	Task    string            `json:"task"`
	Payload deploymentPayload `json:"payload"`
}

const (
	deploymentsPerPage = 100
	maxDeploymentPages = 50
)

// releaseRecords calls visit for each release-path deployment record of
// environment, newest first, until visit returns true. It fails closed when
// the history exceeds maxDeploymentPages pages.
func (c *Client) releaseRecords(ctx context.Context, repo, environment string, visit func(deployment) (bool, error)) error {
	for page := 1; page <= maxDeploymentPages; page++ {
		var deployments []deployment
		q := url.Values{
			"environment": {environment},
			"task":        {ReleaseTask},
			"per_page":    {strconv.Itoa(deploymentsPerPage)},
			"page":        {strconv.Itoa(page)},
		}
		if err := c.get(ctx, "/repos/"+repo+"/deployments", q, &deployments); err != nil {
			return err
		}
		for _, d := range deployments {
			if d.Task != ReleaseTask {
				continue // defensive: the filter is the API's
			}
			stop, err := visit(d)
			if err != nil || stop {
				return err
			}
		}
		if len(deployments) < deploymentsPerPage {
			return nil
		}
	}
	return fmt.Errorf("environment %s: more than %d pages of release deployments", environment, maxDeploymentPages)
}

// AlreadyReleased reports whether environment has a successful release record
// for this exact release (version tag and digest). A replayed release is
// therefore skipped even after later deploys, while a failed attempt may be
// retried.
func (c *Client) AlreadyReleased(ctx context.Context, repo, environment, version, digest string) (bool, error) {
	released := false
	err := c.releaseRecords(ctx, repo, environment, func(d deployment) (bool, error) {
		if d.Payload.Version != version || d.Payload.ArtifactDigest != digest {
			return false, nil
		}
		ok, err := c.succeeded(ctx, repo, d.ID)
		released = ok
		return ok, err
	})
	return released, err
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

// NewerReleased returns the highest release tag of tag's component that has a
// release record in environment with a version greater than tag's, or "" when
// there is none; deploying tag would then be a downgrade. Every recorded
// attempt counts, whatever its status: a record marked failed after Railway
// already switched traffic (for example a failed smoke check) may be what is
// live. A deliberate rollback therefore uses a new, higher tag. The release
// gate calls this early; the staging verify-release job repeats it inside the
// deploy-staging concurrency group, where concurrent releases are ordered. A
// production deploy job must likewise re-check inside its serialized section.
func (c *Client) NewerReleased(ctx context.Context, repo, environment, tag string) (string, error) {
	component, version, ok := parseTag(tag)
	if !ok {
		return "", fmt.Errorf("tag %q is not ui-vX.Y.Z or api-vX.Y.Z", tag)
	}
	highest, newest := "", version
	err := c.releaseRecords(ctx, repo, environment, func(d deployment) (bool, error) {
		if comp, v, ok := parseTag(d.Payload.Version); ok && comp == component && newest.less(v) {
			highest, newest = d.Payload.Version, v
		}
		return false, nil
	})
	if err != nil {
		return "", err
	}
	return highest, nil
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

// VerifyOutcome is the deploy job's verdict on a release artifact.
type VerifyOutcome string

// Verify outcomes.
const (
	VerifyDeploy   VerifyOutcome = "deploy"   // verified: deploy it
	VerifyReplay   VerifyOutcome = "replay"   // this release is already deployed: skip
	VerifyRejected VerifyOutcome = "rejected" // not verified, inconsistent request or a downgrade
)

// VerifyRequest identifies the artifact handed to a deploy job. Environment
// and Tag enable the replay and downgrade checks and must be given together.
type VerifyRequest struct {
	Repository  string // owner/name
	Owner       string // registry namespace owner
	Component   string // api or ui
	CommitSHA   string
	Digest      string
	Environment string // staging-<component> or production-<component>
	Tag         string // <component>-vX.Y.Z
}

// VerifyResult is the outcome and a human-readable reason.
type VerifyResult struct {
	Outcome VerifyOutcome
	Reason  string
}

func rejected(format string, args ...any) VerifyResult {
	return VerifyResult{Outcome: VerifyRejected, Reason: fmt.Sprintf(format, args...)}
}

// Verify re-checks a release artifact inside the deploy job's serialized
// section: the artifact (VerifyArtifact), then replay (skip), then downgrade
// (reject). Errors from the replay and downgrade lookups are returned so the
// caller fails closed; artifact verification failures are rejections.
func (c *Client) Verify(ctx context.Context, r VerifyRequest) (VerifyResult, error) {
	if (r.Environment == "") != (r.Tag == "") {
		return rejected("environment and tag must be given together"), nil
	}
	if r.Tag != "" {
		component, _, ok := parseTag(r.Tag)
		switch {
		case !ok:
			return rejected("tag %q is not ui-vX.Y.Z or api-vX.Y.Z", r.Tag), nil
		case component != r.Component:
			return rejected("tag %q is not a %s release", r.Tag, r.Component), nil
		case r.Environment != "staging-"+r.Component && r.Environment != "production-"+r.Component:
			return rejected("environment %q is not staging-%s or production-%s", r.Environment, r.Component, r.Component), nil
		}
	}
	if err := c.VerifyArtifact(ctx, r.Repository, r.Owner, r.Component, r.CommitSHA, r.Digest); err != nil {
		return rejected("%v", err), nil
	}
	if r.Tag == "" {
		return VerifyResult{Outcome: VerifyDeploy, Reason: "artifact verified"}, nil
	}
	released, err := c.AlreadyReleased(ctx, r.Repository, r.Environment, r.Tag, r.Digest)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("replay check: %w", err)
	}
	if released {
		return VerifyResult{Outcome: VerifyReplay, Reason: fmt.Sprintf("%s (%s) is already deployed to %s", r.Tag, r.Digest, r.Environment)}, nil
	}
	newer, err := c.NewerReleased(ctx, r.Repository, r.Environment, r.Tag)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("downgrade check: %w", err)
	}
	if newer != "" {
		return rejected("%s is older than %s, already recorded in %s; a rollback needs a new, higher tag", r.Tag, newer, r.Environment), nil
	}
	return VerifyResult{Outcome: VerifyDeploy, Reason: "artifact verified, not yet deployed"}, nil
}
