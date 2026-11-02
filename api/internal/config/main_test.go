package config

import (
	"os"
	"strings"
	"testing"
)

// TestMain clears libpq PG* variables, which pgx applies as connection
// defaults, so results do not depend on the developer's shell.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "PG") {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}
