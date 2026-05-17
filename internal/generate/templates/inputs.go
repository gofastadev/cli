package templates

// Inputs is the Go template for generating per-resource domain inputs
// in `app/services/<lower>_inputs.go`.
//
// These are the typed shapes the service layer accepts and exposes —
// distinct from the wire DTOs in `app/dtos/`. The DTO is what the
// HTTP/GraphQL surface accepts; this is what the service operates on.
// The controller is the translator (DTO → input) so the service stays
// unaware of any specific transport.
var Inputs = `package services

// Create{{.Name}}Input is the data required to create a new {{.LowerName}}.
// Field order mirrors the resource definition.
type Create{{.Name}}Input struct {
{{- range .Fields}}
	{{.Name}} {{.GoType}}
{{- end}}
}

// Update{{.Name}}Patch is a partial update — every field is a pointer
// so ` + "`nil`" + ` means "don't touch this column". AsMap returns the GORM
// ` + "`Updates(...)`" + ` payload containing only the fields the caller set.
//
// This replaces the older ` + "`utils.ConvertStructToMap(input)`" + ` pattern,
// which would silently include ID and RecordVersion in the SET
// clause and rewrite them on every update.
type Update{{.Name}}Patch struct {
{{- range .Fields}}
	{{.Name}} *{{.GoType}}
{{- end}}
	IsActive    *bool
	IsDeletable *bool
}

// AsMap builds the partial-update map for the repository. Only fields
// the caller set (non-nil pointers) end up in the SET clause — every
// other column is left untouched in the UPDATE statement.
func (p Update{{.Name}}Patch) AsMap() map[string]any {
	out := map[string]any{}
{{- range .Fields}}
	if p.{{.Name}} != nil {
		out["{{.SnakeName}}"] = *p.{{.Name}}
	}
{{- end}}
	if p.IsActive != nil {
		out["is_active"] = *p.IsActive
	}
	if p.IsDeletable != nil {
		out["is_deletable"] = *p.IsDeletable
	}
	return out
}

// List{{.PluralName}}Filter is the typed filter the service accepts for
// list queries. AsRepoFilter turns this into the per-column
// ` + "`map[string]any`" + ` that ` + "`utils.BuildQueryForAnyModel`" + ` consumes.
type List{{.PluralName}}Filter struct {
{{- range .Fields}}
	{{.Name}} *{{.GoType}}
{{- end}}

	Page  int
	Limit int

	SortField string // already-sanitized column name; empty → default
	SortDesc  bool   // true → DESC, false → ASC
}

// AsRepoFilter returns the per-column filter map for the repository.
func (f List{{.PluralName}}Filter) AsRepoFilter() map[string]any {
	out := map[string]any{}
{{- range .Fields}}
	if f.{{.Name}} != nil {
		out["{{.SnakeName}}"] = *f.{{.Name}}
	}
{{- end}}
	return out
}
`
