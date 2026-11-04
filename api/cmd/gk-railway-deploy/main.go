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
	"regexp"
	"strings"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/railway"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Getenv, os.Stdout))
}

// escapeData encodes a GitHub Actions workflow-command message, so a
// provider error containing a newline or percent sign can neither end the
// annotation early nor inject another workflow command.
func escapeData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
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
	startLine := fs.String("start-line", "", "log message the process writes once at each start, counted during -settle")
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
		StartCommand: *startCmd, Settle: *settle, StartLine: *startLine,
	})
	// On GitHub Actions every line carrying provider text is escaped, so it
	// stays one line and cannot be parsed as a workflow command.
	gha := getenv("GITHUB_ACTIONS") == "true"
	text := func(s string) string {
		if gha {
			return escapeData(s)
		}
		return s
	}
	fmt.Fprintf(stdout, "service %s image %s deployment %s status %s\n", text(*service), text(*image), text(res.DeploymentID), text(res.Status))
	for _, w := range res.Warnings {
		if gha {
			fmt.Fprintf(stdout, "::warning::%s\n", escapeData(w)) // visible on a green run
		} else {
			fmt.Fprintf(stdout, "warning: %s\n", w)
		}
	}
	if path := getenv("GITHUB_OUTPUT"); path != "" && outputValue.MatchString(res.DeploymentID) {
		if f, ferr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644); ferr == nil {
			fmt.Fprintf(f, "deployment_id=%s\n", res.DeploymentID)
			if outputValue.MatchString(res.Status) {
				fmt.Fprintf(f, "status=%s\n", res.Status)
			}
			_ = f.Close()
		}
	}
	if err != nil {
		if gha {
			fmt.Fprintf(stdout, "::error::%s\n", escapeData(err.Error()))
		} else {
			fmt.Fprintf(stdout, "error: %v\n", err)
		}
		return 1
	}
	return 0
}

// outputValue is what may be written to GITHUB_OUTPUT: a provider value with
// a line break could otherwise set further outputs.
var outputValue = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
