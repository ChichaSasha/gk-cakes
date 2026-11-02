// Package migrations embeds the versioned SQL migrations applied by gk-migrate.
// Applied files are immutable: change the schema with a new numbered file.
package migrations

import "embed"

// FS holds every NNNN_name.sql migration.
//
//go:embed *.sql
var FS embed.FS
