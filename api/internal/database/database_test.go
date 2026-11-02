package database

import (
	"context"
	"strings"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
)

// pgx redacts URL passwords in parse errors but echoes the rest of the URL,
// including query parameters that may carry credentials (for example options).
func TestInvalidURLErrorsDoNotEchoTheURL(t *testing.T) {
	const canary = "c4n4ry-s3cr3t"
	secret := config.NewSecret("postgres://u:" + canary + "@localhost/db?options=-c%20gk.key%3D" + canary + "&connect_timeout=bogus")
	if _, err := Connect(context.Background(), secret, "test"); err == nil || strings.Contains(err.Error(), canary) {
		t.Fatalf("Connect error = %v; want an error without the URL", err)
	}
	if _, err := OpenPool(context.Background(), secret, "test"); err == nil || strings.Contains(err.Error(), canary) {
		t.Fatalf("OpenPool error = %v; want an error without the URL", err)
	}
}
