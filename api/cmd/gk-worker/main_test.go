package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/ChichaSasha/gk-cakes/api/internal/handlers"
	"github.com/jackc/pgx/v5"
)

// loginRow answers the login query: a login holding groups, or an error.
type loginRow struct {
	groups []string
	err    error
}

func (r loginRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*string), *dest[1].(*string), *dest[2].(*bool) = "w", "w", false
	*dest[3].(*[]string), *dest[4].(*[]string), *dest[5].(*bool) = r.groups, r.groups, false
	return nil
}

type loginDB struct{ row loginRow }

func (d loginDB) QueryRow(context.Context, string, ...any) pgx.Row { return d.row }

func TestVerifyLoginExitCodes(t *testing.T) {
	for name, c := range map[string]struct {
		row  loginRow
		code int
	}{
		"worker login":   {loginRow{groups: []string{"gk_worker"}}, 0},
		"api login":      {loginRow{groups: []string{"gk_api"}}, 2},
		"unreachable db": {loginRow{err: errors.New("connection refused")}, 1},
	} {
		code, err := verifyLogin(context.Background(), loginDB{c.row}, 50*time.Millisecond, time.Millisecond)
		if code != c.code || (code == 0) != (err == nil) {
			t.Errorf("%s: exit %d, %v; want exit %d", name, code, err, c.code)
		}
	}
}

// countingDB records how often the login was queried.
type countingDB struct {
	row   loginRow
	calls *int
}

func (d countingDB) QueryRow(context.Context, string, ...any) pgx.Row { *d.calls++; return d.row }

// Only local development may skip the login check; every other environment,
// including test, refuses another service's login before running jobs.
func TestStartupLoginIsRequiredOutsideLocal(t *testing.T) {
	for _, env := range []config.Env{config.EnvLocal, config.EnvTest, config.EnvStaging, config.EnvProduction} {
		calls := 0
		checked, code, err := startupLogin(context.Background(), env, countingDB{loginRow{groups: []string{"gk_api"}}, &calls}, 50*time.Millisecond, time.Millisecond)
		if env == config.EnvLocal {
			if checked || code != 0 || err != nil || calls != 0 {
				t.Errorf("local: checked=%v exit %d, %v after %d queries; want no check", checked, code, err, calls)
			}
			continue
		}
		if !checked || code != 2 || !errors.Is(err, database.ErrLogin) || calls == 0 {
			t.Errorf("%s: checked=%v exit %d, %v after %d queries; want the api login refused (exit 2)", env, checked, code, err, calls)
		}
	}
	calls := 0
	if checked, code, err := startupLogin(context.Background(), config.EnvStaging, countingDB{loginRow{groups: []string{workerGroup}}, &calls}, 50*time.Millisecond, time.Millisecond); !checked || code != 0 || err != nil {
		t.Fatalf("staging worker login: checked=%v exit %d, %v; want it verified", checked, code, err)
	}
}

// A shutdown signal while the database is still unreachable is a normal stop.
func TestVerifyLoginStopsQuietlyOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	code, err := verifyLogin(ctx, loginDB{loginRow{err: errors.New("connection refused")}}, 5*time.Second, time.Millisecond)
	if code != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown while waiting: exit %d, %v; want exit 0 with the cancellation", code, err)
	}
}

func TestStartLoggerLogsTheStartLineFirst(t *testing.T) {
	var buf bytes.Buffer
	startLogger(&buf).Info("later")
	first, _, _ := strings.Cut(buf.String(), "\n")
	var line struct {
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(first), &line); err != nil || line.Msg != startLine {
		t.Fatalf("first log line %q (%v); want msg %q", first, err, startLine)
	}
}

// The staging deploy counts this exact line; a rename on either side would
// silently degrade restart detection to a warning.
func TestStagingDeployNamesTheStartLine(t *testing.T) {
	wf, err := os.ReadFile("../../../.github/workflows/staging.yml")
	if err != nil {
		t.Fatal(err)
	}
	if want := `-start-line "` + startLine + `"`; !strings.Contains(string(wf), want) {
		t.Fatalf("staging.yml does not pass %s to the worker deploy", want)
	}
}

// The worker builds its chain with the constructor the restore's synthetic
// dispatch check uses (internal/handlers).
func TestWorkerHandlersAreTheSharedChain(t *testing.T) {
	hs := workerHandlers(nil, nil, nil, nil, nil, "test", "", nil)
	kinds := make([]string, len(hs))
	for i, h := range hs {
		kinds[i] = h.Kind
	}
	if !slices.Equal(kinds, handlers.Kinds) {
		t.Fatalf("worker kinds %v, shared kinds %v", kinds, handlers.Kinds)
	}
}
