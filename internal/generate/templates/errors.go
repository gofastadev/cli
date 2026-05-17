package templates

// Errors is the Go template for generating per-resource sentinel
// errors in `app/services/<lower>_errors.go`.
//
// Sentinels live in the `services` package so any code that calls a
// service method (controllers, GraphQL resolvers, other services) can
// `errors.Is(err, services.ErrXVersionConflict)` to branch on a known
// outcome without leaking ORM-internal errors past the service layer.
var Errors = `package services

import "errors"

// Domain sentinel errors for the {{.LowerName}} service. Controllers
// (and other callers) match these with ` + "`errors.Is`" + ` to translate them
// into the right transport-level response — HTTP status codes,
// gqlerror extensions, etc. — instead of leaking ORM-internal errors
// like ` + "`gorm.ErrRecordNotFound`" + ` past the service boundary.
//
// Wrapping rule: services should ` + "`return nil, ErrX…`" + ` directly when the
// cause IS the domain condition, and ` + "`return nil, fmt.Errorf(\"…: %w\", err)`" + `
// when the cause is infrastructure that callers shouldn't branch on.
var (
	// Err{{.Name}}NotFound — no {{.LowerName}} matches the given identifier
	// (or the row is soft-deleted, which from the API's perspective is
	// the same thing).
	Err{{.Name}}NotFound = errors.New("{{.LowerName}} not found")

	// Err{{.Name}}VersionConflict — the caller's expected RecordVersion
	// doesn't match the persisted value. Another writer changed the row
	// between read and write; the caller should refetch and retry.
	Err{{.Name}}VersionConflict = errors.New("{{.LowerName}} record version mismatch")

	// Err{{.Name}}NotDeletable — the row's IsDeletable flag is false
	// (or the row doesn't exist). Some rows are explicitly protected
	// from archiving (system records, etc.).
	Err{{.Name}}NotDeletable = errors.New("{{.LowerName}} is not deletable")
)
`
