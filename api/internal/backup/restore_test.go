package backup

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/handlers"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
	"github.com/jackc/pgx/v5/pgconn"
)

// resealManifest replaces a stored backup's manifest.
func resealManifest(t *testing.T, store *memStore, sealer Sealer, m Manifest) {
	t.Helper()
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	name := manifestName(m.Environment, m.CreatedAt.UTC().Format(stampLayout))
	sealed, err := sealer.Seal(name, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), name, sealed); err != nil {
		t.Fatal(err)
	}
}

const sampleScript = `--
-- PostgreSQL database dump
--

\restrict CWzJuIGRaciPKwLNvBENXiSTM7plIjMiOTa0PSLRiGyKDzhYOSoQWh5Yp6g8vhD

-- Dumped from database version 17.11
SET statement_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET row_security = off;

--
-- Data for Name: job; Type: TABLE DATA; Schema: business; Owner: gk_owner
--

COPY business.job (id, operation_id, state, next_attempt_at, lease_owner, lease_until, generation, version) FROM stdin;
11111111-1111-4111-8111-111111111111	22222222-2222-4222-8222-222222222222	queued	2026-11-05 02:00:00+00	\N	\N	0	1
33333333-3333-4333-8333-333333333333	44444444-4444-4444-8444-444444444444	running	2026-11-05 02:00:00+00	55555555-5555-4555-8555-555555555555	2026-11-05 02:01:00+00	7	9
\.


COPY business.recipe_line (recipe_version_id, "position", component, quantity, unit) FROM stdin;
\.

COPY business.recovery_freeze (singleton, frozen, reason, changed_at) FROM stdin;
t	f	\N	2026-11-05 01:00:00+00
\.

COPY gk_meta.schema_migration (version, sha256, applied_at) FROM stdin;
0002_job_gateway	bbbb	2026-11-05 01:00:00+00
0001_roles_and_schema	aaaa	2026-11-05 01:00:00+00
\.

\unrestrict CWzJuIGRaciPKwLNvBENXiSTM7plIjMiOTa0PSLRiGyKDzhYOSoQWh5Yp6g8vhD
`

func TestSummarizeReadsCountsHistoryAndLeases(t *testing.T) {
	s, err := summarize(strings.NewReader(sampleScript))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Rows) != 3 || s.Rows["job"] != 2 || s.Rows["recipe_line"] != 0 || s.Rows["recovery_freeze"] != 1 {
		t.Fatalf("rows = %v", s.Rows)
	}
	if len(s.History) != 2 || s.History[0] != (MigrationRecord{"0001_roles_and_schema", "aaaa"}) || s.History[1].Version != "0002_job_gateway" {
		t.Fatalf("history = %v", s.History)
	}
	want := Lease{Job: "33333333-3333-4333-8333-333333333333", Worker: "55555555-5555-4555-8555-555555555555", Generation: 7}
	if len(s.Leases) != 1 || s.Leases[0] != want {
		t.Fatalf("leases = %v", s.Leases)
	}
}

func TestParseScriptRebuildsCopyStatements(t *testing.T) {
	var stmts []string
	var data strings.Builder
	err := parseScript(strings.NewReader(sampleScript), func(b copyBlock, rows io.Reader) error {
		stmts = append(stmts, b.Statement())
		if b.Table == "job" {
			_, err := io.Copy(&data, rows)
			return err
		}
		return nil // unread rows are skipped
	})
	if err != nil {
		t.Fatal(err)
	}
	if stmts[1] != `COPY "business"."recipe_line" ("recipe_version_id", "position", "component", "quantity", "unit") FROM STDIN` || len(stmts) != 4 {
		t.Fatalf("statements = %q", stmts)
	}
	if strings.Count(data.String(), "\n") != 2 || strings.Contains(data.String(), `\.`) {
		t.Fatalf("job rows = %q", data.String())
	}
}

// The script is accepted only as data: anything else is refused, naming a
// line number and never the line.
func TestParseScriptRefusesAnythingButData(t *testing.T) {
	const secret = "Synthetic Customer"
	for name, script := range map[string]string{
		"ddl":              "DROP TABLE business.job;\n",
		"function call":    "SELECT business.recovery_set_freeze(false, '" + secret + "');\n",
		"sequence reset":   "SELECT pg_catalog.setval('business.x_seq', 1, true);\n",
		"psql meta":        "\\! rm -rf /\n",
		"unknown setting":  "SET session_replication_role = origin;\n",
		"other encoding":   "SET client_encoding = 'LATIN1';\n",
		"other schema":     "COPY public.users (id) FROM stdin;\n\\.\n",
		"unterminated":     "COPY business.job (id) FROM stdin;\n" + secret + "\n",
		"twice":            "COPY business.job (id) FROM stdin;\n\\.\nCOPY business.job (id) FROM stdin;\n\\.\n",
		"bad column":       "COPY business.job (id; DROP TABLE x) FROM stdin;\n\\.\n",
		"half quoted":      "COPY business.job (\"id) FROM stdin;\n\\.\n",
		"copy to program":  "COPY business.job (id) FROM PROGRAM 'x';\n",
		"unknown meta tbl": "COPY gk_meta.other (id) FROM stdin;\n\\.\n",
	} {
		_, err := summarize(strings.NewReader(script))
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "DROP") {
			t.Errorf("%s: error quotes the script: %v", name, err)
		}
	}
}

func TestCheckSummaryComparesManifestAndBuild(t *testing.T) {
	r := &Restore{Migrations: migrations.FS}
	s, err := summarize(strings.NewReader(sampleScript))
	if err != nil {
		t.Fatal(err)
	}
	m := Manifest{Rows: map[string]int64{"job": 2, "recipe_line": 0, "recovery_freeze": 1}, Migrations: []string{"0001_roles_and_schema", "0002_job_gateway"}}
	if err := r.checkSummary(m, s); err == nil || !strings.Contains(err.Error(), "differs from this build's file") {
		t.Fatalf("checksums unlike the build: %v", err)
	}
	m.Rows["job"] = 3
	if err := r.checkSummary(m, s); err == nil || !strings.Contains(err.Error(), "2 rows of job, the manifest 3") {
		t.Fatalf("count unlike the manifest: %v", err)
	}
	m.Rows["job"] = 2
	m.Migrations = m.Migrations[:1]
	if err := r.checkSummary(m, s); err == nil || !strings.Contains(err.Error(), "differs from its manifest") {
		t.Fatalf("history unlike the manifest: %v", err)
	}
	s.History = []MigrationRecord{{"0999_future", "x"}}
	m.Migrations = []string{"0999_future"}
	if err := r.checkSummary(m, s); err == nil || !strings.Contains(err.Error(), "unknown to this build") {
		t.Fatalf("history newer than the build: %v", err)
	}
}

func TestFindSelectsANamedBackup(t *testing.T) {
	ctx := context.Background()
	store, sealer := newMemStore(), testSealer(t)
	older := manifestAt("staging", time.Date(2026, 11, 4, 2, 30, 0, 0, time.UTC))
	newer := manifestAt("staging", time.Date(2026, 11, 5, 2, 30, 0, 0, time.UTC))
	putBackup(t, store, sealer, older, []byte("a"))
	putBackup(t, store, sealer, newer, []byte("b"))
	if m, err := Find(ctx, store, sealer, "staging", ""); err != nil || !m.CreatedAt.Equal(newer.CreatedAt) {
		t.Fatalf("newest: %v %v", m.CreatedAt, err)
	}
	if m, err := Find(ctx, store, sealer, "staging", "20261104-023000z"); err != nil || !m.CreatedAt.Equal(older.CreatedAt) {
		t.Fatalf("named: %v %v", m.CreatedAt, err)
	}
	if _, err := Find(ctx, store, sealer, "staging", "20261103-023000z"); err == nil || !strings.Contains(err.Error(), "no complete backup 20261103") {
		t.Fatalf("missing named backup: %v", err)
	}
}

func TestLoginURLKeepsThePoolerTenant(t *testing.T) {
	for target, want := range map[string]string{
		"postgres://postgres.abcref:pw@pooler.example.com:5432/postgres?sslmode=verify-full": "postgres://gk_restore_check_1.abcref:secret@pooler.example.com:5432/postgres?sslmode=verify-full",
		"postgres://postgres:pw@127.0.0.1:55432/db?sslmode=disable":                          "postgres://gk_restore_check_1:secret@127.0.0.1:55432/db?sslmode=disable",
	} {
		if got, err := loginURL(target, "gk_restore_check_1", "secret"); err != nil || got != want {
			t.Errorf("loginURL(%s) = %s, %v", target, got, err)
		}
	}
}

func TestSCRAMVerifierShape(t *testing.T) {
	v, err := scramVerifier("password")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^SCRAM-SHA-256\$4096:[A-Za-z0-9+/]{22}==\$[A-Za-z0-9+/]{43}=:[A-Za-z0-9+/]{43}=$`).MatchString(v) || strings.Contains(v, "password") {
		t.Fatalf("verifier %q", v)
	}
	if w, _ := scramVerifier("password"); w == v {
		t.Fatal("verifiers share a salt")
	}
}

func TestSafeErrorDropsDataMessages(t *testing.T) {
	data := &pgconn.PgError{Code: "22P02", Message: `invalid input syntax for type uuid: "Synthetic Customer"`, TableName: "job"}
	if got := safeError(data).Error(); strings.Contains(got, "Synthetic") || !strings.Contains(got, "22P02") || !strings.Contains(got, "job") {
		t.Fatalf("data error: %q", got)
	}
	access := &pgconn.PgError{Code: "42501", Message: "permission denied to set parameter"}
	if got := safeError(access); !errors.Is(got, access) {
		t.Fatalf("access error: %v", got)
	}
}

// The synthetic check runs the worker's own chain (internal/handlers), with
// recording providers in place of Stripe and Resend.
func TestSyntheticHandlersAreTheWorkerChain(t *testing.T) {
	hs := syntheticHandlers(nil, nil, &recorder{}, nil, "test", nil)
	kinds := make([]string, len(hs))
	for i, h := range hs {
		kinds[i] = h.Kind
	}
	if !slices.Equal(kinds, handlers.Kinds) {
		t.Fatalf("synthetic kinds %v, worker kinds %v", kinds, handlers.Kinds)
	}
}
