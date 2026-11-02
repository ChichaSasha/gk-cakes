// Package releasegate decides whether a GitHub release event may deploy a
// component, and which exact artifact it deploys. It implements the delivery
// contract: only a published ui-vX.Y.Z or api-vX.Y.Z release whose tag points
// at a commit on main with a successful main CI run, and an image built from
// that commit, may deploy; a repeated event for an already deployed artifact
// is skipped. Full releases target production-<component>; prereleases target
// staging only. Any GitHub API failure is returned as an error so callers fail
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

// Client calls the GitHub REST API.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

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
	digest, err := gh.imageDigest(ctx, ev.Repository.Owner.Login, ev.Repository.Owner.Type, pkg, sha)
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
		Image:          "ghcr.io/" + strings.ToLower(ev.Repository.Owner.Login) + "/" + pkg,
		ArtifactDigest: digest,
		Environment:    "production-" + component,
		APIContract:    "v1",
	}
	if ev.Release.Prerelease {
		// Explicit routing: prereleases verify the release path on staging and
		// never reach production.
		manifest.Environment = "staging"
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
	deployed, err := gh.alreadyDeployed(ctx, in.Repository, manifest.Environment, sha, digest)
	if err != nil {
		return Decision{}, err
	}
	if deployed {
		return Decision{Action: ActionSkip, Reason: fmt.Sprintf("%s is already deployed to %s", digest, manifest.Environment), Manifest: manifest}, nil
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

type pkgContainer struct {
	Tags []string `json:"tags"`
}

type pkgMetadata struct {
	Container pkgContainer `json:"container"`
}

type pkgVersion struct {
	Name     string      `json:"name"`
	Metadata pkgMetadata `json:"metadata"`
}

// imageDigest returns the digest of the container package version tagged
// sha-<sha>, or "" when there is none.
func (c *Client) imageDigest(ctx context.Context, owner, ownerType, pkg, sha string) (string, error) {
	scope := "/users/"
	if ownerType == "Organization" {
		scope = "/orgs/"
	}
	want := "sha-" + sha
	const perPage, maxPages = 100, 20
	for page := 1; page <= maxPages; page++ {
		var versions []pkgVersion
		q := url.Values{"per_page": {fmt.Sprint(perPage)}, "page": {fmt.Sprint(page)}}
		err := c.get(ctx, scope+owner+"/packages/container/"+pkg+"/versions", q, &versions)
		if errors.Is(err, errNotFound) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		for _, v := range versions {
			for _, t := range v.Metadata.Container.Tags {
				if t == want && strings.HasPrefix(v.Name, "sha256:") {
					return v.Name, nil
				}
			}
		}
		if len(versions) < perPage {
			return "", nil
		}
	}
	return "", fmt.Errorf("package %s: more than %d pages of versions", pkg, maxPages)
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
}

type deployment struct {
	ID      int64             `json:"id"`
	SHA     string            `json:"sha"`
	Payload deploymentPayload `json:"payload"`
}

// alreadyDeployed reports whether a deployment of digest for sha to
// environment has a latest status of success.
func (c *Client) alreadyDeployed(ctx context.Context, repo, environment, sha, digest string) (bool, error) {
	var deployments []deployment
	q := url.Values{"environment": {environment}, "sha": {sha}, "per_page": {"100"}}
	if err := c.get(ctx, "/repos/"+repo+"/deployments", q, &deployments); err != nil {
		return false, err
	}
	for _, d := range deployments {
		if d.Payload.ArtifactDigest != digest {
			continue
		}
		var statuses []struct {
			State string `json:"state"`
		}
		if err := c.get(ctx, fmt.Sprintf("/repos/%s/deployments/%d/statuses", repo, d.ID), url.Values{"per_page": {"1"}}, &statuses); err != nil {
			return false, err
		}
		if len(statuses) > 0 && statuses[0].State == "success" {
			return true, nil
		}
	}
	return false, nil
}
