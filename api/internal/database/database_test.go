package database

import (
	"context"
	"strings"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
)

func TestInvalidURLErrorsDoNotEchoTheURL(t *testing.T) {
	const canary = "c4n4ry-s3cr3t"
	cfg, err := config.LoadMigrate(func(k string) string {
		return map[string]string{
			"APP_ENV":                "local",
			"MIGRATION_DATABASE_URL": "postgres://u:" + canary + "@localhost/db?sslmode=bogus",
		}[k]
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if _, err := Connect(context.Background(), cfg.DatabaseURL, "test"); err == nil || strings.Contains(err.Error(), canary) {
		t.Fatalf("Connect error = %v; want an error without the secret", err)
	}
	if _, err := OpenPool(context.Background(), cfg.DatabaseURL, "test"); err == nil || strings.Contains(err.Error(), canary) {
		t.Fatalf("OpenPool error = %v; want an error without the secret", err)
	}
}
