// Command gk-email-proof runs the GK-024 AC3 sandbox proof against a deployed
// environment (manual workflow .github/workflows/email-proof.yml). It uses
// the migration credential to enqueue controlled notices as a domain
// transaction would, so it is not part of the runtime image.
//
// Environment: APP_ENV, MIGRATION_DATABASE_URL, API_URL (https://host),
// PROOF_LABEL ([a-z0-9]{1,24}, unique per run), optional
// PROOF_EXTRA_RECIPIENT (a controlled mailbox for the display-name check),
// optional PROOF_WAIT (default 10m), optional RAILWAY_TOKEN and
// RAILWAY_ENVIRONMENT_ID for the log scan, optional GITHUB_STEP_SUMMARY.
// Exit status: 0 pass, 1 fail, 2 configuration.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/ChichaSasha/gk-cakes/api/internal/emailproof"
	"github.com/ChichaSasha/gk-cakes/api/internal/railway"
)

func main() {
	os.Exit(run(context.Background(), os.Getenv, os.Stdout))
}

var apiURL = regexp.MustCompile(`^https://[a-z0-9.-]+$`)

type railwayLogs struct {
	d   *railway.Deployer
	env string
}

func (r railwayLogs) Since(ctx context.Context, since time.Time) ([]string, error) {
	return r.d.EnvironmentLogs(ctx, r.env, since, 5000)
}

func run(ctx context.Context, getenv func(string) string, stdout io.Writer) int {
	cfg, err := config.LoadMigrate(getenv)
	if err != nil {
		fmt.Fprintln(stdout, "invalid configuration:", err)
		return 2
	}
	if !apiURL.MatchString(getenv("API_URL")) {
		fmt.Fprintln(stdout, "invalid configuration: API_URL must be https://<host> with no path")
		return 2
	}
	wait := 10 * time.Minute
	if v := getenv("PROOF_WAIT"); v != "" {
		if wait, err = time.ParseDuration(v); err != nil || wait <= 0 || wait > 30*time.Minute {
			fmt.Fprintln(stdout, "invalid configuration: PROOF_WAIT must be a duration up to 30m")
			return 2
		}
	}
	conn, err := database.Connect(ctx, cfg.DatabaseURL, "gk-email-proof")
	if err != nil {
		fmt.Fprintln(stdout, "database:", err)
		return 1
	}
	defer conn.Close(context.Background())

	var logs emailproof.LogSearcher
	if token, env := getenv("RAILWAY_TOKEN"), getenv("RAILWAY_ENVIRONMENT_ID"); token != "" && env != "" {
		logs = railwayLogs{d: &railway.Deployer{ProjectToken: token, HTTP: &http.Client{Timeout: 60 * time.Second}}, env: env}
	}
	out := io.Writer(stdout)
	if path := getenv("GITHUB_STEP_SUMMARY"); path != "" {
		if f, ferr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644); ferr == nil {
			defer f.Close()
			out = io.MultiWriter(stdout, f)
		}
	}
	err = emailproof.Run(ctx, emailproof.Config{
		DB: conn, Environment: string(cfg.Env), APIURL: getenv("API_URL"), HTTP: &http.Client{Timeout: 30 * time.Second},
		Logs: logs, Label: getenv("PROOF_LABEL"), ExtraRecipient: getenv("PROOF_EXTRA_RECIPIENT"),
		Wait: wait, Poll: 10 * time.Second, Out: out,
	})
	if err != nil {
		fmt.Fprintln(stdout, err)
		return 1
	}
	return 0
}
