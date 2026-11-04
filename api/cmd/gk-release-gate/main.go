// Command gk-release-gate evaluates the GitHub event of the current workflow
// run and decides whether a release may deploy. It writes the release manifest
// and step outputs. With -verify it instead re-checks one release artifact
// (-component, -commit, -digest; -environment and -version are required)
// before a deploy: the artifact, then whether that release is already deployed
// there (replay), then whether a higher release is already recorded there
// (downgrade).
// Exit status: 0 deploy, skip or verified; 3 denied, not verified or a
// downgrade; 4 verified but already deployed; 1 error.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/releasegate"
)

const (
	exitOK       = 0
	exitError    = 1
	exitDenied   = 3
	exitReleased = 4
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Getenv, os.Stdout))
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) int {
	fs := flag.NewFlagSet("gk-release-gate", flag.ContinueOnError)
	manifestPath := fs.String("manifest", "release-manifest.json", "where to write the manifest for deploy/skip decisions")
	verify := fs.Bool("verify", false, "verify one artifact instead of evaluating the event")
	component := fs.String("component", "", "with -verify: api or ui")
	commit := fs.String("commit", "", "with -verify: 40-character commit SHA")
	digest := fs.String("digest", "", "with -verify: sha256 digest")
	environment := fs.String("environment", "", "with -verify: deployment record environment for the replay and downgrade checks")
	version := fs.String("version", "", "with -verify: release tag for the replay and downgrade checks")
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	token := getenv("GITHUB_TOKEN")
	if token == "" {
		token = getenv("GH_TOKEN")
	}
	if token == "" {
		fmt.Fprintln(stdout, "error: GITHUB_TOKEN (or GH_TOKEN) is required")
		return exitError
	}
	var event []byte
	if path := getenv("GITHUB_EVENT_PATH"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(stdout, "error: read event: %v\n", err)
			return exitError
		}
		event = b
	}
	baseURL := getenv("GITHUB_API_URL")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	client := &releasegate.Client{
		BaseURL:     baseURL,
		RegistryURL: getenv("GK_REGISTRY_URL"),
		Token:       token,
		HTTP:        &http.Client{Timeout: 20 * time.Second},
	}
	if *verify {
		res, err := client.Verify(ctx, releasegate.VerifyRequest{
			Repository:  getenv("GITHUB_REPOSITORY"),
			Owner:       getenv("GITHUB_REPOSITORY_OWNER"),
			Component:   *component,
			CommitSHA:   *commit,
			Digest:      *digest,
			Environment: *environment,
			Tag:         *version,
		})
		code, line := verifyExit(res, err)
		fmt.Fprintln(stdout, line)
		return code
	}
	d, err := releasegate.Evaluate(ctx, releasegate.Input{
		EventName:  getenv("GITHUB_EVENT_NAME"),
		Event:      event,
		Repository: getenv("GITHUB_REPOSITORY"),
	}, client)
	if err != nil {
		fmt.Fprintf(stdout, "error: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "decision: %s (%s)\n", d.Action, d.Reason)

	outputs := map[string]string{"decision": d.Action}
	if d.Manifest != nil {
		b, err := json.MarshalIndent(d.Manifest, "", "  ")
		if err != nil {
			fmt.Fprintf(stdout, "error: encode manifest: %v\n", err)
			return exitError
		}
		if err := os.WriteFile(*manifestPath, append(b, '\n'), 0o644); err != nil {
			fmt.Fprintf(stdout, "error: write manifest: %v\n", err)
			return exitError
		}
		fmt.Fprintf(stdout, "%s\n", b)
		outputs["component"] = d.Manifest.Component
		outputs["environment"] = d.Manifest.Environment
		outputs["image"] = d.Manifest.Image
		outputs["digest"] = d.Manifest.ArtifactDigest
		outputs["commit_sha"] = d.Manifest.CommitSHA
		outputs["version"] = d.Manifest.Version
	}
	if path := getenv("GITHUB_OUTPUT"); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			fmt.Fprintf(stdout, "error: open GITHUB_OUTPUT: %v\n", err)
			return exitError
		}
		for _, k := range []string{"decision", "component", "environment", "image", "digest", "commit_sha", "version"} {
			if v, ok := outputs[k]; ok {
				fmt.Fprintf(f, "%s=%s\n", k, v)
			}
		}
		if err := f.Close(); err != nil {
			fmt.Fprintf(stdout, "error: write GITHUB_OUTPUT: %v\n", err)
			return exitError
		}
	}
	if d.Action == releasegate.ActionDeny {
		return exitDenied
	}
	return exitOK
}

// verifyExit maps a -verify result to the exit status and output line.
func verifyExit(res releasegate.VerifyResult, err error) (int, string) {
	if err != nil {
		return exitError, "error: " + err.Error()
	}
	switch res.Outcome {
	case releasegate.VerifyDeploy:
		return exitOK, "verified: " + res.Reason
	case releasegate.VerifyReplay:
		return exitReleased, "replay: " + res.Reason
	case releasegate.VerifyRejected:
		return exitDenied, "not verified: " + res.Reason
	default:
		return exitError, fmt.Sprintf("error: unknown verify outcome %q", res.Outcome)
	}
}
