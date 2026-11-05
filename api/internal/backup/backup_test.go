package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
)

// memStore is an in-memory Store that counts reads.
type memStore struct {
	mu    sync.Mutex
	objs  map[string][]byte
	reads map[string]int
	puts  []string // names in upload order
}

func newMemStore() *memStore { return &memStore{objs: map[string][]byte{}, reads: map[string]int{}} }

func (s *memStore) Put(_ context.Context, name string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objs[name] = append([]byte(nil), data...)
	s.puts = append(s.puts, name)
	return nil
}

func (s *memStore) Get(_ context.Context, name string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads[name]++
	d, ok := s.objs[name]
	if !ok {
		return nil, recovery.ErrNotFound
	}
	return d, nil
}

func (s *memStore) List(_ context.Context, prefix string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var names []string
	for n := range s.objs {
		if strings.HasPrefix(n, prefix) {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	return names, nil
}

func testSealer(t *testing.T) *recovery.Sealer {
	t.Helper()
	s, err := recovery.NewSealer("k1:AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// putBackup stores a sealed manifest (and optionally its dump) as Dump would.
func putBackup(t *testing.T, s *memStore, sealer Sealer, m Manifest, dump []byte) {
	t.Helper()
	stamp := m.CreatedAt.UTC().Format(stampLayout)
	if m.Dump == "" {
		m.Dump = Root(m.Environment) + stamp + "/dump.sealed"
	}
	if dump != nil {
		sealed, err := sealer.Seal(m.Dump, dump)
		if err != nil {
			t.Fatal(err)
		}
		_ = s.Put(context.Background(), m.Dump, sealed)
	}
	body, _ := json.Marshal(m)
	name := manifestName(m.Environment, stamp)
	sealed, err := sealer.Seal(name, body)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Put(context.Background(), name, sealed)
}

func manifestAt(env string, at time.Time) Manifest {
	return Manifest{V: 1, Environment: env, CreatedAt: at, SchemaVersion: "0008_x", Migrations: []string{"0001_a", "0008_x"}, Rows: map[string]int64{"job": 1}}
}

func TestLatestPicksTheNewestCompleteBackup(t *testing.T) {
	s, sealer := newMemStore(), testSealer(t)
	t0 := time.Date(2026, 11, 3, 2, 30, 0, 0, time.UTC)
	putBackup(t, s, sealer, manifestAt("staging", t0), []byte("old"))
	putBackup(t, s, sealer, manifestAt("staging", t0.Add(24*time.Hour)), []byte("new"))
	putBackup(t, s, sealer, manifestAt("production", t0.Add(48*time.Hour)), []byte("other environment"))
	// A newer dump without a manifest is an incomplete backup.
	_ = s.Put(context.Background(), Root("staging")+t0.Add(47*time.Hour).Format(stampLayout)+"/dump.sealed", []byte("x"))
	m, err := Latest(context.Background(), s, sealer, "staging")
	if err != nil || !m.CreatedAt.Equal(t0.Add(24*time.Hour)) || m.Dump != "database/staging/20261104-023000z/dump.sealed" {
		t.Fatalf("latest = %+v, %v", m, err)
	}
	if _, err := Latest(context.Background(), newMemStore(), sealer, "staging"); err == nil || !strings.Contains(err.Error(), "no complete backup") {
		t.Fatalf("no backups: %v", err)
	}
}

func TestLatestRefusesForeignOrTamperedManifests(t *testing.T) {
	sealer := testSealer(t)
	at := time.Date(2026, 11, 4, 2, 30, 0, 0, time.UTC)
	stamp := at.Format(stampLayout)
	name := manifestName("staging", stamp)
	dump := Root("staging") + stamp + "/dump.sealed"
	// Each case breaks exactly one binding; the others hold.
	put := func(s *memStore, m Manifest, sealedFor string) {
		body, _ := json.Marshal(m)
		sealed, _ := sealer.Seal(sealedFor, body)
		_ = s.Put(context.Background(), name, sealed)
	}
	cases := map[string]func(s *memStore){
		"another environment": func(s *memStore) {
			m := manifestAt("production", at)
			m.Dump = dump
			put(s, m, name)
		},
		"another time": func(s *memStore) {
			m := manifestAt("staging", at.Add(time.Hour))
			m.Dump = Root("staging") + at.Add(time.Hour).Format(stampLayout) + "/dump.sealed"
			put(s, m, name)
		},
		"a dump elsewhere": func(s *memStore) {
			m := manifestAt("staging", at)
			m.Dump = Root("staging") + "20260101-000000z/dump.sealed"
			put(s, m, name)
		},
		"sealed for another name (moved)": func(s *memStore) {
			m := manifestAt("staging", at)
			m.Dump = dump
			put(s, m, manifestName("staging", "20260101-000000z"))
		},
		"corrupted": func(s *memStore) { _ = s.Put(context.Background(), name, []byte(`{"v":1}`)) },
	}
	for label, setup := range cases {
		s := newMemStore()
		setup(s)
		if _, err := Latest(context.Background(), s, sealer, "staging"); err == nil {
			t.Errorf("%s: accepted", label)
		}
	}
	ok := newMemStore()
	m := manifestAt("staging", at)
	m.Dump = dump
	put(ok, m, name)
	if _, err := Latest(context.Background(), ok, sealer, "staging"); err != nil {
		t.Fatalf("the intact manifest: %v", err)
	}
}

func TestRestoreCheckFailsFastOnAStaleOrAlteredBackup(t *testing.T) {
	sealer := testSealer(t)
	now := time.Date(2026, 11, 4, 5, 0, 0, 0, time.UTC)
	// Stale: the newest backup is 27 hours old; the dump is never read.
	stale := newMemStore()
	m := manifestAt("staging", now.Add(-27*time.Hour))
	putBackup(t, stale, sealer, m, []byte("archive"))
	r := &Restorer{Env: "staging", Target: "postgres://u@127.0.0.1/x", Store: stale, Sealer: sealer, Now: func() time.Time { return now }, PgRestore: "/nonexistent"}
	if _, err := r.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "backups have stopped") {
		t.Fatalf("stale backup: %v", err)
	}
	if n := stale.reads[Root("staging")+m.CreatedAt.Format(stampLayout)+"/dump.sealed"]; n != 0 {
		t.Fatalf("a stale backup's dump was read %d times", n)
	}
	// Altered: the dump does not match the manifest's hash.
	altered := newMemStore()
	m = manifestAt("staging", now.Add(-2*time.Hour))
	m.DumpSHA256, m.DumpBytes = strings.Repeat("0", 64), 7
	putBackup(t, altered, sealer, m, []byte("archive"))
	r.Store = altered
	if _, err := r.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "does not match its manifest") {
		t.Fatalf("altered dump: %v", err)
	}
	// A backup dated in the future would be chosen forever: refused.
	future := newMemStore()
	putBackup(t, future, sealer, manifestAt("staging", now.Add(48*time.Hour)), []byte("archive"))
	r.Store = future
	if _, err := r.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "in the future") {
		t.Fatalf("future-dated backup: %v", err)
	}
	// A dump sealed with another key cannot be opened.
	other, _ := recovery.NewSealer("k2:" + strings.Repeat("A", 43) + "=")
	foreign := newMemStore()
	putBackup(t, foreign, sealer, manifestAt("staging", now.Add(-time.Hour)), nil)
	sealed, _ := other.Seal(Root("staging")+now.Add(-time.Hour).Format(stampLayout)+"/dump.sealed", []byte("archive"))
	_ = foreign.Put(context.Background(), Root("staging")+now.Add(-time.Hour).Format(stampLayout)+"/dump.sealed", sealed)
	r.Store = foreign
	if _, err := r.Check(context.Background()); err == nil || !errors.Is(err, recovery.ErrUnreadable) {
		t.Fatalf("foreign dump: %v", err)
	}
}

// The database password never appears on the tool's command line or in an
// error, and output beyond the limit is refused.
func TestRunKeepsThePasswordOffTheCommandLine(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	tool := filepath.Join(dir, "tool")
	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\necho \"tool: error: password was $PGPASSWORD; host env ${PGHOST:-none} b2 ${BACKUP_B2_KEY:-none}\" >&2\nexit 3\n"
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PGHOST", "elsewhere.example.com") // inherited PG* variables never reach the tool
	t.Setenv("BACKUP_B2_KEY", "b2-secret")
	_, err := run(context.Background(), tool, "postgres://gk_backup_staging:s3cr3t-pw@db.example.com:5432/postgres?sslmode=verify-full", 1024, "--format=custom")
	args, _ := os.ReadFile(argsFile)
	if strings.Contains(string(args), "s3cr3t-pw") || !strings.Contains(string(args), "gk_backup_staging@db.example.com") {
		t.Fatalf("command line %q", args)
	}
	if err == nil || strings.Contains(err.Error(), "s3cr3t-pw") || !strings.Contains(err.Error(), "[redacted]") || !strings.Contains(err.Error(), "host env none b2 none") {
		t.Fatalf("error %v; want the tool's message with the password redacted and no inherited PGHOST", err)
	}
	// Without a password nothing is "redacted" (no scrambled message).
	if _, err := run(context.Background(), tool, "postgres://u@127.0.0.1/db", 1024); err == nil || strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error without a password: %v", err)
	}
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, []byte("#!/bin/sh\nhead -c 4096 /dev/zero\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := run(context.Background(), big, "postgres://u:p@127.0.0.1/db", 1024); err == nil || !strings.Contains(err.Error(), "exceeds 1024 bytes") {
		t.Fatalf("oversized output: %v", err)
	}
}

// Tool errors keep one line: PostgreSQL's DETAIL, CONTEXT, LINE and "Command
// was" lines can quote row values and the workflow logs are public.
func TestToolErrorKeepsOneLine(t *testing.T) {
	stderr := "pg_restore: while PROCESSING TOC:\npg_restore: error: could not execute query: ERROR:  duplicate key value violates unique constraint \"x_pkey\"\nDETAIL:  Key (email)=(someone@example.com) already exists.\nCONTEXT:  COPY account, line 1: \"someone@example.com\"\nCommand was: COPY business.account (id) FROM stdin;\n"
	got := toolError(stderr, "")
	if !strings.HasPrefix(got, "pg_restore: error: could not execute query") || strings.Contains(got, "someone@example.com") {
		t.Fatalf("toolError = %q", got)
	}
	if got := toolError("pg_dump: error: connection failed: password pw1 rejected\n", "pw1"); strings.Contains(got, "pw1") {
		t.Fatalf("password not redacted: %q", got)
	}
	if got := toolError("plain noise\n", ""); got != "no error message" {
		t.Fatalf("no error line: %q", got)
	}
}
