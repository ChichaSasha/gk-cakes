package migrate

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ChichaSasha/gk-cakes/api/migrations"
)

func TestLoadOrdersEmbeddedMigrations(t *testing.T) {
	ms, err := Load(migrations.FS)
	if err != nil {
		t.Fatalf("Load(embedded): %v", err)
	}
	if len(ms) == 0 || ms[0].Version != "0001_roles_and_schema" {
		t.Fatalf("first migration = %v, want 0001_roles_and_schema", ms)
	}
	for i := 1; i < len(ms); i++ {
		if ms[i-1].Version >= ms[i].Version {
			t.Errorf("migrations out of order: %s before %s", ms[i-1].Version, ms[i].Version)
		}
	}
	for _, m := range ms {
		if len(m.SHA256) != 64 {
			t.Errorf("%s checksum %q is not a sha256 hex digest", m.Version, m.SHA256)
		}
	}
}

func TestLoadRejectsInvalidSets(t *testing.T) {
	cases := map[string]fstest.MapFS{
		"must be named": {"1_bad.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}},
		"share a number": {
			"0001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
			"0001_b.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
		},
		"no migrations": {"README.md": &fstest.MapFile{Data: []byte("x")}},
	}
	for want, fsys := range cases {
		if _, err := Load(fsys); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Load: err = %v, want error containing %q", err, want)
		}
	}
}
