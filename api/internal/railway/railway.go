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

var digestRef = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*@sha256:[0-9a-f]{64}$`)

// Request selects what to deploy where.
type Request struct {
	ServiceID       string
	EnvironmentID   string
	Image           string // must be pinned: name@sha256:<64 hex>
	HealthcheckPath string // optional; Railway marks SUCCESS only once it answers
}

// Result reports the deployment Railway created.
type Result struct {
	DeploymentID string
	Status       string
}

// Deployer talks to the Railway API with an environment-scoped project token.
type Deployer struct {
	Endpoint     string
	ProjectToken string
	HTTP         *http.Client
	PollInterval time.Duration
	Timeout      time.Duration
}

// healthcheckTimeoutSeconds bounds how long Railway waits for the health path.
const healthcheckTimeoutSeconds = 120

const (
	updateMutation = `mutation gkUpdate($serviceId: String!, $environmentId: String!, $input: ServiceInstanceUpdateInput!) {
  serviceInstanceUpdate(serviceId: $serviceId, environmentId: $environmentId, input: $input)
}`
	deployMutation = `mutation gkDeploy($serviceId: String!, $environmentId: String!) {
  serviceInstanceDeployV2(serviceId: $serviceId, environmentId: $environmentId)
}`
	statusQuery = `query gkStatus($id: String!) {
  deployment(id: $id) { id status }
}`
)

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
	for {
		var st struct {
			Deployment struct {
				Status string `json:"status"`
			} `json:"deployment"`
		}
		if err := d.call(ctx, statusQuery, map[string]any{"id": deployed.ID}, &st); err != nil {
			return res, fmt.Errorf("railway: deployment %s status: %w", deployed.ID, err)
		}
		res.Status = st.Deployment.Status
		switch {
		case res.Status == "SUCCESS":
			return res, nil
		case failed[res.Status]:
			return res, fmt.Errorf("railway: deployment %s ended %s", deployed.ID, res.Status)
		case time.Now().After(deadline):
			return res, fmt.Errorf("railway: deployment %s timed out in status %s (left running)", deployed.ID, res.Status)
		}
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case <-time.After(d.PollInterval):
		}
	}
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message    string `json:"message"`
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
			if e.Extensions.TraceID != "" {
				msg += " (trace " + e.Extensions.TraceID + ")"
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
