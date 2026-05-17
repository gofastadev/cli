package templates

// DTOs is the Go template for generating DTO structs for a resource.
//
// Wire shapes only — no business logic. Mapping helpers
// (FromModel, ToCreateInput, ToPatch) bridge the wire layer and the
// domain layer so the service stays free of any transport awareness.
//
// Validation errors come back via 4xx (apperrors.NewValidation) — the
// success-shape envelope intentionally has no `Errors` field.
var DTOs = `package dtos

import (
	"time"

	"github.com/google/uuid"

	"{{.ModulePath}}/app/models"
	"{{.ModulePath}}/app/services"
)

// {{.Name}} is the public-facing representation of a {{.LowerName}} record.
type {{.Name}} struct {
	ID            uuid.UUID  ` + "`" + `json:"id"` + "`" + `
	RecordVersion int        ` + "`" + `json:"recordVersion"` + "`" + `
	CreatedAt     time.Time  ` + "`" + `json:"createdAt"` + "`" + `
	UpdatedAt     time.Time  ` + "`" + `json:"updatedAt"` + "`" + `
	IsActive      bool       ` + "`" + `json:"isActive"` + "`" + `
	IsDeletable   bool       ` + "`" + `json:"isDeletable"` + "`" + `
	DeletedAt     *time.Time ` + "`" + `json:"deletedAt,omitempty"` + "`" + `
{{- range .Fields}}
	{{.Name}} {{.GoType}} ` + "`" + `json:"{{.JSONName}}"` + "`" + `
{{- end}}
}

// {{.Name}}FromModel maps a persisted {{.Name}} onto its public DTO.
// DeletedAt is only emitted when the row was actually soft-deleted
// (gorm.DeletedAt.Valid == true) so JSON omits the field on live rows
// instead of rendering "0001-01-01T00:00:00Z".
func {{.Name}}FromModel(m *models.{{.Name}}) *{{.Name}} {
	out := &{{.Name}}{
		ID:            m.ID,
		RecordVersion: m.RecordVersion,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
		IsActive:      m.IsActive,
		IsDeletable:   m.IsDeletable,
{{- range .Fields}}
		{{.Name}}: m.{{.Name}},
{{- end}}
	}
	if m.DeletedAt.Valid {
		t := m.DeletedAt.Time
		out.DeletedAt = &t
	}
	return out
}

// {{.PluralName}}FromModels maps the slice form for list responses.
func {{.PluralName}}FromModels(ms []*models.{{.Name}}) []*{{.Name}} {
	out := make([]*{{.Name}}, 0, len(ms))
	for _, m := range ms {
		out = append(out, {{.Name}}FromModel(m))
	}
	return out
}

// T{{.Name}}ResponseDto is the success-shape envelope for endpoints
// returning a single {{.LowerName}}. Validation/error responses flow
// via HTTP status codes (4xx) carrying the framework's apperrors
// envelope — there is intentionally no ` + "`Errors`" + ` field here.
type T{{.Name}}ResponseDto struct {
	Data *{{.Name}} ` + "`" + `json:"data,omitempty"` + "`" + `
}

// T{{.PluralName}}ResponseDto is the success envelope for list endpoints,
// pairing the page of records with pagination metadata.
type T{{.PluralName}}ResponseDto struct {
	Data       []*{{.Name}}          ` + "`" + `json:"data"` + "`" + `
	Pagination *TPaginationObjectDto ` + "`" + `json:"pagination"` + "`" + `
}

// TCreate{{.Name}}Dto is the input for the create endpoint. validate
// tags drive the AppValidator at the controller boundary — invalid
// requests get HTTP 422 before reaching the service.
type TCreate{{.Name}}Dto struct {
{{- range .Fields}}
	{{.Name}} {{.GoType}} ` + "`" + `json:"{{.JSONName}}" validate:"required"` + "`" + `
{{- end}}
}

// ToCreateInput maps the wire DTO to the typed domain input the
// service expects. Keeps the service free of any wire-format knowledge.
func (d TCreate{{.Name}}Dto) ToCreateInput() services.Create{{.Name}}Input {
	return services.Create{{.Name}}Input{
{{- range .Fields}}
		{{.Name}}: d.{{.Name}},
{{- end}}
	}
}

// TUpdate{{.Name}}Dto is the REST input for partial updates. Optional
// fields are pointers so the controller can distinguish absent from
// empty.
//
// ` + "`id`" + ` and ` + "`recordVersion`" + ` are intentionally NOT here:
//   - id comes from the URL path (` + "`PUT /{{.PluralLower}}/{id}`" + `)
//   - recordVersion is a precondition, not data, so it travels in the
//     ` + "`If-Match`" + ` header per RFC 7232 (Azure / GCP / Kubernetes /
//     GitHub all use this — QuickBooks' SyncToken-in-body is the
//     outlier). The controller reads the header and passes the
//     parsed int to the service.
type TUpdate{{.Name}}Dto struct {
{{- range .Fields}}
	{{.Name}} *{{.GoType}} ` + "`" + `json:"{{.JSONName}},omitempty"` + "`" + `
{{- end}}
	IsActive    *bool ` + "`" + `json:"isActive,omitempty"` + "`" + `
	IsDeletable *bool ` + "`" + `json:"isDeletable,omitempty"` + "`" + `
}

// ToPatch maps the wire DTO to the typed domain patch.
func (d TUpdate{{.Name}}Dto) ToPatch() services.Update{{.Name}}Patch {
	return services.Update{{.Name}}Patch{
{{- range .Fields}}
		{{.Name}}: d.{{.Name}},
{{- end}}
		IsActive:    d.IsActive,
		IsDeletable: d.IsDeletable,
	}
}

// TFind{{.Name}}ByIDDto is the input for the get-by-id endpoint.
type TFind{{.Name}}ByIDDto struct {
	ID uuid.UUID ` + "`" + `json:"id" validate:"uuid4_valid,does_record_exist_by_id_for_verification={{.PluralSnake}}"` + "`" + `
}

// TArchive{{.Name}}Dto is the input for the soft-delete endpoint.
type TArchive{{.Name}}Dto struct {
	ID uuid.UUID ` + "`" + `json:"id" validate:"uuid4_valid,does_record_exist_by_id_for_verification={{.PluralSnake}},is_record_deletable={{.PluralSnake}}"` + "`" + `
}

// T{{.Name}}FiltersQueryParamsDto is the flat query-string shape
// decoded from REST list requests. Defaults applied at the boundary
// (page=1, limit=10, sort=createdAt DESC) via ToFilter.
type T{{.Name}}FiltersQueryParamsDto struct {
{{- range .Fields}}
	{{.Name}} *{{.GoType}} ` + "`" + `json:"{{.JSONName}},omitempty" schema:"{{.JSONName}}"` + "`" + `
{{- end}}
	Limit           *int             ` + "`" + `json:"limit,omitempty" schema:"limit" validate:"omitempty,gte=1"` + "`" + `
	Page            *int             ` + "`" + `json:"page,omitempty" schema:"page" validate:"omitempty,gte=1"` + "`" + `
	SortByField     *string          ` + "`" + `json:"sortByField,omitempty" schema:"sortByField"` + "`" + `
	SortOrientation *SortOrientation ` + "`" + `json:"sortOrientation,omitempty" schema:"sortOrientation" validate:"omitempty"` + "`" + `
}

// ToFilter maps the wire query DTO to the typed domain filter, with
// defaults applied (page=1, limit=10, SortField=created_at, SortDesc=true).
func (q T{{.Name}}FiltersQueryParamsDto) ToFilter() services.List{{.PluralName}}Filter {
	f := services.List{{.PluralName}}Filter{
{{- range .Fields}}
		{{.Name}}: q.{{.Name}},
{{- end}}
		Page:      1,
		Limit:     10,
		SortField: "created_at",
		SortDesc:  true,
	}
	if q.Page != nil && *q.Page > 0 {
		f.Page = *q.Page
	}
	if q.Limit != nil && *q.Limit > 0 {
		f.Limit = *q.Limit
	}
	if q.SortByField != nil && *q.SortByField != "" {
		f.SortField = *q.SortByField
	}
	if q.SortOrientation != nil && q.SortOrientation.IsValid() {
		f.SortDesc = (*q.SortOrientation == SortOrientationDesc)
	}
	return f
}
`
