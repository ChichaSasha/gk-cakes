// Package railway deploys an exact container image digest to a Railway
// service through the public GraphQL API (fields checked against the live
// schema by introspection on 2026-11-02).
package railway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// DefaultEndpoint is Railway's public GraphQL API.
const DefaultEndpoint = "https://backboard.railway.com/graphql/v2"

var (
	digestRef    = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*@sha256:[0-9a-f]{64}$`)
	startCommand = regexp.MustCompile(`^/[a-z0-9/_.-]{1,200}$`)
)

// Request selects what to deploy where.
type Request struct {
	ServiceID       string
	EnvironmentID   string
	Image           string // must be pinned: name@sha256:<64 hex>
	HealthcheckPath string // optional; Railway marks SUCCESS only once it answers
	// StartCommand optionally overrides the image ENTRYPOINT (exec form), for
	// example /usr/local/bin/gk-worker for the worker service.
	StartCommand string
	// Settle keeps watching after SUCCESS and fails if the deployment crashes
	// within it: the check for a process with no health endpoint.
	Settle time.Duration
}

// Result reports the deployment Railway created.
type Result struct {
	DeploymentID string
	Status       string
	// Warnings note checks that could not be made (for example instance
	// executions the token cannot read during a settle period).
	Warnings []string
}

// Deployer talks to the Railway API with an environment-scoped project token.
type Deployer struct {
	Endpoint     string
	ProjectToken string
	HTTP         *http.Client
	PollInterval time.Duration
	Timeout      time.Duration
}

// maxResponseBytes bounds a GraphQL response (log pages are the largest).
const maxResponseBytes = 8 << 20

// healthcheckTimeoutSeconds bounds how long Railway waits for the health path.
const healthcheckTimeoutSeconds = 120

// maxPollErrors is how many consecutive status-query failures are tolerated
// before giving up; Railway keeps rolling out regardless.
const maxPollErrors = 5

const (
	updateMutation = `mutation gkUpdate($serviceId: String!, $environmentId: String!, $input: ServiceInstanceUpdateInput!) {
  serviceInstanceUpdate(serviceId: $serviceId, environmentId: $environmentId, input: $input)
}`
	deployMutation = `mutation gkDeploy($serviceId: String!, $environmentId: String!) {
  serviceInstanceDeployV2(serviceId: $serviceId, environmentId: $environmentId)
}`
	statusQuery = `query gkStatus($id: String!) {
  deployment(id: $id) { id status meta }
}`
	// environmentLogs is anchored: beforeDate/beforeLimit select the logs
	// between the oldest date and the anchor, afterDate/afterLimit those after
	// it (the Railway CLI reads a window the same way).
	logsQuery = `query gkLogs($environmentId: String!, $anchorDate: String!, $beforeDate: String!, $beforeLimit: Int!) {
  environmentLogs(environmentId: $environmentId, anchorDate: $anchorDate, beforeDate: $beforeDate,
    beforeLimit: $beforeLimit, afterDate: $anchorDate, afterLimit: 0) { timestamp message attributes { key value } }
}`
	executionsQuery = `query gkExecutions($input: DeploymentInstanceExecutionListInput!, $after: String) {
  deploymentInstanceExecutions(input: $input, first: 50, after: $after) {
    edges { node { deploymentId status } }
    pageInfo { hasNextPage endCursor }
  }
}`
)

// MaxLogLines is the most lines EnvironmentLogs requests (Railway's maximum).
const MaxLogLines = 5000

// EnvironmentLogs returns the environment's log lines between since and
// until, oldest first, each as its message followed by its attributes
// (key=value). complete is false when the limit was reached, so older lines
// in the window may be missing.
func (d *Deployer) EnvironmentLogs(ctx context.Context, environmentID string, since, until time.Time) (lines []string, complete bool, err error) {
	if d.ProjectToken == "" || environmentID == "" || !since.Before(until) {
		return nil, false, errors.New("railway: logs need a token, an environment ID and a time window")
	}
	var out struct {
		Logs []struct {
			Message    string `json:"message"`
			Attributes []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"attributes"`
		} `json:"environmentLogs"`
	}
	if err := d.call(ctx, logsQuery, map[string]any{
		"environmentId": environmentID, "anchorDate": until.UTC().Format(time.RFC3339Nano),
		"beforeDate": since.UTC().Format(time.RFC3339Nano), "beforeLimit": MaxLogLines,
	}, &out); err != nil {
		return nil, false, fmt.Errorf("railway: environment logs: %w", err)
	}
	lines = make([]string, 0, len(out.Logs))
	for _, l := range out.Logs {
		var b strings.Builder
		b.WriteString(l.Message)
		for _, a := range l.Attributes {
			b.WriteString(" " + a.Key + "=" + attributeValue(a.Value))
		}
		lines = append(lines, b.String())
	}
	return lines, len(out.Logs) < MaxLogLines, nil
}

// attributeValue undoes the JSON encoding Railway may apply to attribute
// values (a string arrives as "value", quotes included); anything else is
// kept as sent.
func attributeValue(v string) string {
	if strings.HasPrefix(v, `"`) {
		var s string
		if json.Unmarshal([]byte(v), &s) == nil {
			return s
		}
	}
	return v
}

var badExecution = map[string]bool{"CRASHED": true, "EXITED": true, "RESTARTING": true, "REMOVED": true, "STOPPED": true}

// maxExecutionPages bounds the instance-execution pages read per check.
const maxExecutionPages = 10

// Instance-execution check results.
const (
	executionsSteady  = "steady"
	executionsBad     = "bad"
	executionsMissing = "missing" // the deployment is not (yet) listed
)

// executions checks whether deploymentID still runs its first and only
// instance execution: a crash and restart under Railway's restart policy keeps
// the deployment SUCCESS, so the status alone cannot show a crash loop. (With
// one replica, two running executions mean a restart.)
func (d *Deployer) executions(ctx context.Context, req Request, deploymentID string) (string, string, error) {
	var mine []string
	var after any
	for page := 0; page < maxExecutionPages; page++ {
		var out struct {
			Executions struct {
				Edges []struct {
					Node struct {
						DeploymentID string `json:"deploymentId"`
						Status       string `json:"status"`
					} `json:"node"`
				} `json:"edges"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"deploymentInstanceExecutions"`
		}
		if err := d.call(ctx, executionsQuery, map[string]any{
			"input": map[string]any{"environmentId": req.EnvironmentID, "serviceId": req.ServiceID}, "after": after,
		}, &out); err != nil {
			return "", "", err
		}
		for _, e := range out.Executions.Edges {
			if e.Node.DeploymentID == deploymentID {
				mine = append(mine, e.Node.Status)
			}
		}
		if !out.Executions.PageInfo.HasNextPage || out.Executions.PageInfo.EndCursor == "" {
			break
		}
		after = out.Executions.PageInfo.EndCursor
	}
	for _, st := range mine {
		if badExecution[st] {
			return executionsBad, fmt.Sprintf("an instance execution is %s", st), nil
		}
	}
	switch {
	case len(mine) > 1:
		return executionsBad, fmt.Sprintf("%d instance executions (restarted)", len(mine)), nil
	case len(mine) == 0:
		return executionsMissing, "", nil
	}
	return executionsSteady, "", nil
}

var failed = map[string]bool{"FAILED": true, "CRASHED": true, "REMOVED": true, "REMOVING": true, "SKIPPED": true}

// Deploy points the service at req.Image, starts a deployment and waits for
// it to succeed or fail. A timeout reports the deployment without cancelling
// it, so a slow but healthy rollout is never interrupted.
func (d *Deployer) Deploy(ctx context.Context, req Request) (Result, error) {
	switch {
	case d.ProjectToken == "":
		return Result{}, errors.New("railway: project token is required")
	case req.ServiceID == "" || req.EnvironmentID == "":
		return Result{}, errors.New("railway: service and environment IDs are required")
	case !digestRef.MatchString(req.Image):
		return Result{}, fmt.Errorf("railway: image %q must be pinned by sha256 digest (name@sha256:<64 hex>)", req.Image)
	}

	input := map[string]any{"source": map[string]any{"image": req.Image}}
	if req.StartCommand != "" {
		if !startCommand.MatchString(req.StartCommand) {
			return Result{}, fmt.Errorf("railway: start command %q must be an absolute executable path", req.StartCommand)
		}
		input["startCommand"] = req.StartCommand
	}
	if req.HealthcheckPath != "" {
		input["healthcheckPath"] = req.HealthcheckPath
		input["healthcheckTimeout"] = healthcheckTimeoutSeconds
	}
	var updated struct {
		OK bool `json:"serviceInstanceUpdate"`
	}
	if err := d.call(ctx, updateMutation, map[string]any{
		"serviceId": req.ServiceID, "environmentId": req.EnvironmentID, "input": input,
	}, &updated); err != nil {
		return Result{}, fmt.Errorf("railway: set image: %w", err)
	}
	if !updated.OK {
		return Result{}, errors.New("railway: set image: serviceInstanceUpdate returned false")
	}

	var deployed struct {
		ID string `json:"serviceInstanceDeployV2"`
	}
	if err := d.call(ctx, deployMutation, map[string]any{
		"serviceId": req.ServiceID, "environmentId": req.EnvironmentID,
	}, &deployed); err != nil {
		return Result{}, fmt.Errorf("railway: start deployment: %w", err)
	}
	if deployed.ID == "" {
		return Result{}, errors.New("railway: start deployment: no deployment ID returned")
	}

	res := Result{DeploymentID: deployed.ID}
	deadline := time.Now().Add(d.Timeout)
	var settleUntil time.Time // set once SUCCESS is first seen
	executionsChecked := req.Settle > 0
	executionsSeen := false // the deployment appeared among the executions
	executionErrors := 0
	pollErrors := 0
	for {
		var st struct {
			Deployment struct {
				Status string          `json:"status"`
				Meta   json.RawMessage `json:"meta"`
			} `json:"deployment"`
		}
		err := d.call(ctx, statusQuery, map[string]any{"id": deployed.ID}, &st)
		switch {
		case err != nil:
			pollErrors++
			if pollErrors >= maxPollErrors {
				return res, fmt.Errorf("railway: deployment %s status (after %d attempts): %w", deployed.ID, pollErrors, err)
			}
		default:
			pollErrors = 0
			res.Status = st.Deployment.Status
			if img := metaImage(st.Deployment.Meta); digestOf(img) != "" && digestOf(img) != digestOf(req.Image) {
				return res, fmt.Errorf("railway: deployment %s runs image %q, not the requested %q", deployed.ID, img, req.Image)
			}
			switch {
			case failed[res.Status] && !settleUntil.IsZero():
				return res, fmt.Errorf("railway: deployment %s ended %s within %s of SUCCESS", deployed.ID, res.Status, req.Settle)
			case failed[res.Status]:
				return res, fmt.Errorf("railway: deployment %s ended %s", deployed.ID, res.Status)
			case res.Status == "SUCCESS" && req.Settle <= 0:
				return res, nil
			case res.Status == "SUCCESS":
				if settleUntil.IsZero() {
					settleUntil = time.Now().Add(req.Settle)
					if end := settleUntil.Add(d.PollInterval); end.After(deadline) {
						deadline = end // a late SUCCESS still gets its full settle period
					}
				}
				if executionsChecked {
					state, why, xerr := d.executions(ctx, req, deployed.ID)
					switch {
					case xerr != nil:
						if executionErrors++; executionErrors >= maxPollErrors {
							executionsChecked = false
							res.Warnings = append(res.Warnings, "instance executions not readable; settle checked the deployment status only: "+xerr.Error())
						}
					case state == executionsBad:
						return res, fmt.Errorf("railway: deployment %s is not running steadily within %s of SUCCESS: %s", deployed.ID, req.Settle, why)
					case state == executionsSteady:
						executionErrors, executionsSeen = 0, true
					default:
						executionErrors = 0
					}
				}
				if time.Now().After(settleUntil) {
					if executionsChecked && !executionsSeen {
						res.Warnings = append(res.Warnings, "the deployment never appeared among the service's instance executions; settle checked the deployment status only")
					}
					return res, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return res, fmt.Errorf("railway: deployment %s timed out in status %s (left running)", deployed.ID, res.Status)
		}
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case <-time.After(d.PollInterval):
		}
	}
}

// digestOf returns the @sha256:... part of an image reference, or "". Only
// digests are compared, so registry-name normalization cannot cause a false
// mismatch.
func digestOf(ref string) string {
	if i := strings.LastIndex(ref, "@sha256:"); i >= 0 {
		return ref[i+1:]
	}
	return ""
}

// metaImage extracts the image Railway reports for a deployment, when its
// metadata includes one. The metadata shape is not part of the published
// examples, so absence is tolerated.
func metaImage(raw json.RawMessage) string {
	var meta struct {
		Image string `json:"image"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &meta) != nil {
		return ""
	}
	return meta.Image
}

func (d *Deployer) call(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	endpoint := d.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Project-Access-Token", d.ProjectToken)
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message    string `json:"message"`
			TraceID    string `json:"traceId"`
			Extensions struct {
				Code    string `json:"code"`
				TraceID string `json:"traceId"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("HTTP %d: unreadable response", resp.StatusCode)
	}
	if len(envelope.Errors) > 0 {
		var msgs []string
		for _, e := range envelope.Errors {
			msg := e.Message
			if e.Extensions.Code != "" {
				msg += " [" + e.Extensions.Code + "]"
			}
			trace := e.TraceID
			if trace == "" {
				trace = e.Extensions.TraceID
			}
			if trace != "" {
				msg += " (trace " + trace + ")"
			}
			msgs = append(msgs, msg)
		}
		return errors.New(strings.Join(msgs, "; "))
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(envelope.Data, out)
}
