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
}

func newMemStore() *memStore { return &memStore{objs: map[string][]byte{}, reads: map[string]int{}} }

func (s *memStore) Put(_ context.Context, name string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objs[name] = append([]byte(nil), data...)
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
	name := manifestName("staging", at.Format(stampLayout))
	cases := map[string]func(s *memStore){
		"another environment's manifest under this name": func(s *memStore) {
			body, _ := json.Marshal(manifestAt("production", at))
			sealed, _ := sealer.Seal(name, body)
			_ = s.Put(context.Background(), name, sealed)
		},
		"a manifest claiming another time": func(s *memStore) {
			body, _ := json.Marshal(manifestAt("staging", at.Add(time.Hour)))
			sealed, _ := sealer.Seal(name, body)
			_ = s.Put(context.Background(), name, sealed)
		},
		"a manifest sealed for another name (moved)": func(s *memStore) {
			body, _ := json.Marshal(manifestAt("staging", at))
			sealed, _ := sealer.Seal(manifestName("staging", "20260101-000000z"), body)
			_ = s.Put(context.Background(), name, sealed)
		},
		"a corrupted manifest": func(s *memStore) { _ = s.Put(context.Background(), name, []byte(`{"v":1}`)) },
	}
	for label, setup := range cases {
		s := newMemStore()
		setup(s)
		if _, err := Latest(context.Background(), s, sealer, "staging"); err == nil {
			t.Errorf("%s: accepted", label)
		}
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
	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\necho \"password was $PGPASSWORD\" >&2\nexit 3\n"
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := run(context.Background(), tool, "postgres://gk_backup_staging:s3cr3t-pw@db.example.com:5432/postgres?sslmode=verify-full", 1024, "--format=custom")
	args, _ := os.ReadFile(argsFile)
	if strings.Contains(string(args), "s3cr3t-pw") || !strings.Contains(string(args), "gk_backup_staging@db.example.com") {
		t.Fatalf("command line %q", args)
	}
	if err == nil || strings.Contains(err.Error(), "s3cr3t-pw") || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error %v; want the tool's message with the password redacted", err)
	}
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, []byte("#!/bin/sh\nhead -c 4096 /dev/zero\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := run(context.Background(), big, "postgres://u:p@127.0.0.1/db", 1024); err == nil || !strings.Contains(err.Error(), "exceeds 1024 bytes") {
		t.Fatalf("oversized output: %v", err)
	}
}
