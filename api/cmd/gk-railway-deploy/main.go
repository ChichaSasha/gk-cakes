// Command gk-railway-deploy deploys one exact image digest to one Railway
// service and waits for the result. The project token comes from
// RAILWAY_TOKEN and is never printed. Exit status: 0 success, 1 failure.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/railway"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Getenv, os.Stdout))
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) int {
	fs := flag.NewFlagSet("gk-railway-deploy", flag.ContinueOnError)
	fs.SetOutput(stdout)
	service := fs.String("service", "", "Railway service ID")
	environment := fs.String("environment", "", "Railway environment ID")
	image := fs.String("image", "", "image pinned by digest: name@sha256:<64 hex>")
	healthcheck := fs.String("healthcheck", "", "HTTP path Railway must see healthy before SUCCESS")
	startCmd := fs.String("start-command", "", "absolute executable overriding the image entrypoint, e.g. /usr/local/bin/gk-worker")
	settle := fs.Duration("settle", 0, "after SUCCESS keep watching this long and fail if the deployment crashes")
	timeout := fs.Duration("timeout", 10*time.Minute, "how long to wait for the deployment")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	d := &railway.Deployer{
		Endpoint:     getenv("RAILWAY_API_URL"),
		ProjectToken: getenv("RAILWAY_TOKEN"),
		HTTP:         &http.Client{Timeout: 30 * time.Second},
		PollInterval: 5 * time.Second,
		Timeout:      *timeout,
	}
	res, err := d.Deploy(ctx, railway.Request{
		ServiceID: *service, EnvironmentID: *environment, Image: *image, HealthcheckPath: *healthcheck,
		StartCommand: *startCmd, Settle: *settle,
	})
	fmt.Fprintf(stdout, "service %s image %s deployment %s status %s\n", *service, *image, res.DeploymentID, res.Status)
	if path := getenv("GITHUB_OUTPUT"); path != "" && res.DeploymentID != "" {
		if f, ferr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644); ferr == nil {
			fmt.Fprintf(f, "deployment_id=%s\nstatus=%s\n", res.DeploymentID, res.Status)
			_ = f.Close()
		}
	}
	if err != nil {
		fmt.Fprintf(stdout, "error: %v\n", err)
		return 1
	}
	return 0
}
