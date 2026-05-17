// Package skeleton provides embedded project template files for gofasta new.
package skeleton

import "embed"

// ProjectFS holds the embedded skeleton project used by `gofasta new`.
//
// The project tree intentionally ships NO db/migrations directory of
// its own; the per-driver foundational migrations live in MigrationsFS
// below and are copied in at scaffold time based on the --driver flag.
//
//go:embed all:project
var ProjectFS embed.FS

// MigrationsFS holds the per-driver foundational migration sets that
// `gofasta new --driver X` copies into the new project's db/migrations
// directory. One subdirectory per supported driver:
//
//	migrations/postgres   — 5-step set: citext, 3 plpgsql functions, users
//	migrations/mysql      — single users migration with inlined triggers
//	migrations/sqlite     — single users migration with inlined triggers
//	migrations/sqlserver  — single users migration with INSTEAD OF DELETE
//	migrations/clickhouse — single users migration, no triggers (engine
//	                        limitation; invariants enforced app-side only)
//
// Per-driver isolation keeps each set independently reviewable and
// testable; a single conditioned .sql.tmpl would be unreadable across
// PL/pgSQL vs T-SQL vs SQLite trigger syntaxes.
//
//go:embed all:migrations
var MigrationsFS embed.FS
