package migrations

import "embed"

// Files contains the plain SQL migrations for the Postgres database.
//
//go:embed sql/*.sql
var Files embed.FS
