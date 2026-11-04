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
	// StartLine optionally names the log message the process writes once at
	// each start (for example "worker starting"); during Settle it is counted
	// like Railway's own container-start line, so a restart is caught even if
	// the platform line is missing.
	StartLine string
}

// Result reports the deployment Railway created.
type Result struct {
	DeploymentID string
	Status       string
	// Warnings note checks that could not be made (for example a deployment
	// log the token cannot read during a settle period).
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
	// startsQuery reads the newest lines of one deployment's runtime log.
	// Railway writes a "Starting Container" line when it starts the
	// deployment's container (one per start in the staging logs of
	// 2026-11-03; a line per restart-policy restart is assumed, not yet
	// observed), so a crash and restart under the restart policy shows a
	// second one while the status stays SUCCESS. Request.StartLine adds the
	// process's own start line as a second count. (Railway's
	// deploymentInstanceExecutions listed no execution for a running staging
	// worker, so it cannot show restarts.)
	startsQuery = `query gkStarts($deploymentId: String!, $limit: Int!) {
  deploymentLogs(deploymentId: $deploymentId, limit: $limit) { message }
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

// containerStart is the platform log line for one container start.
const containerStart = "Starting Container"

// Container-start check results.
const (
	startsSteady  = "steady"
	startsBad     = "bad"
	startsUnknown = "unknown" // no start line yet, or the log page was full
)

// starts counts the starts in deploymentID's log, as the larger of Railway's
// container-start lines and the process's own start lines (startLine, when
// set): one means the first container still runs (the service has one
// replica), more mean it was restarted. For an unknown result the reason says
// why.
func (d *Deployer) starts(ctx context.Context, deploymentID, startLine string) (string, string, error) {
	var out struct {
		Logs []struct {
			Message string `json:"message"`
		} `json:"deploymentLogs"`
	}
	if err := d.call(ctx, startsQuery, map[string]any{"deploymentId": deploymentID, "limit": MaxLogLines}, &out); err != nil {
		return "", "", err
	}
	platform, own := 0, 0
	for _, l := range out.Logs {
		msg := strings.TrimSpace(l.Message)
		switch {
		case strings.EqualFold(msg, containerStart):
			platform++
		case startLine != "" && (msg == startLine || jsonMessage(msg) == startLine):
			own++
		}
	}
	n := max(platform, own)
	switch {
	case n > 1:
		return startsBad, fmt.Sprintf("it started %d times (restarted)", n), nil
	case len(out.Logs) >= MaxLogLines:
		return startsUnknown, "the deployment log page was full, so earlier starts may be missing", nil
	case startLine != "" && own == 0:
		// A named start line that never matches must not leave the check
		// resting silently on Railway's line alone.
		return startsUnknown, fmt.Sprintf("the process start line %q did not appear in the deployment log", startLine), nil
	case n == 0:
		return startsUnknown, "no start line appeared in the deployment log", nil
	}
	return startsSteady, "", nil
}

// jsonMessage returns the "msg" of a structured (slog JSON) log line, for a
// log that arrives unparsed; "" otherwise.
func jsonMessage(line string) string {
	if !strings.HasPrefix(line, "{") {
		return ""
	}
	var v struct {
		Msg string `json:"msg"`
	}
	if json.Unmarshal([]byte(line), &v) != nil {
		return ""
	}
	return v.Msg
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
	startsChecked := req.Settle > 0
	startsSeen := false // the latest successful read counted exactly one start
	startsWhy := ""     // why the latest successful read was inconclusive
	startsRead := false // the deployment log was read at least once
	var startErr error  // the last failed read
	startErrors := 0    // consecutive failed reads
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
				if startsChecked {
					state, why, serr := d.starts(ctx, deployed.ID, req.StartLine)
					switch {
					case serr != nil:
						startErr = serr
						if startErrors++; startErrors >= maxPollErrors {
							startsChecked = false
							res.Warnings = append(res.Warnings, "deployment log not readable; settle checked the deployment status only: "+serr.Error())
						}
					case state == startsBad:
						return res, fmt.Errorf("railway: deployment %s is not running steadily within %s of SUCCESS: %s", deployed.ID, req.Settle, why)
					default:
						startErrors, startsRead = 0, true
						startsSeen, startsWhy = state == startsSteady, why
					}
				}
				if time.Now().After(settleUntil) {
					switch {
					case !startsChecked || startsSeen:
					case !startsRead && startErr != nil:
						res.Warnings = append(res.Warnings, "deployment log not readable; settle checked the deployment status only: "+startErr.Error())
					default:
						res.Warnings = append(res.Warnings, "restart check inconclusive: "+startsWhy+"; settle checked the deployment status only")
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
