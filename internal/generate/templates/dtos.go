package templates

// DTOs is the Go template for generating DTO structs for a resource.
var DTOs = `package dtos

import (
	"time"

	"github.com/google/uuid"
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

// T{{.Name}}ResponseDto is the response shape for endpoints that return
// a single {{.LowerName}} — Data on success, Errors on validation failure.
type T{{.Name}}ResponseDto struct {
	Data   *{{.Name}}            ` + "`" + `json:"data,omitempty"` + "`" + `
	Errors []*TCommonAPIErrorDto ` + "`" + `json:"errors,omitempty"` + "`" + `
}

// T{{.PluralName}}ResponseDto is the response shape for list endpoints,
// pairing the page of records with pagination metadata.
type T{{.PluralName}}ResponseDto struct {
	Data       []*{{.Name}}          ` + "`" + `json:"data"` + "`" + `
	Pagination *TPaginationObjectDto ` + "`" + `json:"pagination"` + "`" + `
}

// TCreate{{.Name}}Dto is the input shape for the create endpoint.
type TCreate{{.Name}}Dto struct {
{{- range .Fields}}
	{{.Name}} {{.GoType}} ` + "`" + `json:"{{.JSONName}}" validate:"required"` + "`" + `
{{- end}}
}

// TUpdate{{.Name}}Dto is the input for partial updates. Optional fields
// are pointers so the controller can distinguish absent from empty.
type TUpdate{{.Name}}Dto struct {
	ID            uuid.UUID ` + "`" + `json:"id" validate:"required,uuid4_valid,does_record_exist_by_id_for_verification={{.PluralSnake}}"` + "`" + `
	RecordVersion int       ` + "`" + `json:"recordVersion" validate:"required,min=1"` + "`" + `
{{- range .Fields}}
	{{.Name}} *{{.GoType}} ` + "`" + `json:"{{.JSONName}},omitempty"` + "`" + `
{{- end}}
}

// TFind{{.Name}}ByIDDto is the input for the get-by-id endpoint.
type TFind{{.Name}}ByIDDto struct {
	ID uuid.UUID ` + "`" + `json:"id" validate:"uuid4_valid,does_record_exist_by_id_for_verification={{.PluralSnake}}"` + "`" + `
}

// TArchive{{.Name}}Dto is the input for the soft-delete endpoint. The
// is_record_deletable validator gates rows whose IsDeletable flag is false.
type TArchive{{.Name}}Dto struct {
	ID uuid.UUID ` + "`" + `json:"id" validate:"uuid4_valid,does_record_exist_by_id_for_verification={{.PluralSnake}},is_record_deletable={{.PluralSnake}}"` + "`" + `
}

// {{.Name}}FiltersDto bundles pagination + sorting on list queries.
type {{.Name}}FiltersDto struct {
	Pagination *TPaginationInputDto ` + "`" + `json:"pagination,omitempty"` + "`" + `
	Sorting    *TSortingInputDto    ` + "`" + `json:"sorting,omitempty"` + "`" + `
}
`
