package templates

var DTOs = `package dtos

import (
	"time"

	"github.com/google/uuid"
)

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

type T{{.Name}}ResponseDto struct {
	Data   *{{.Name}}            ` + "`" + `json:"data,omitempty"` + "`" + `
	Errors []*TCommonAPIErrorDto ` + "`" + `json:"errors,omitempty"` + "`" + `
}

type T{{.PluralName}}ResponseDto struct {
	Data       []*{{.Name}}          ` + "`" + `json:"data"` + "`" + `
	Pagination *TPaginationObjectDto ` + "`" + `json:"pagination"` + "`" + `
}

type TCreate{{.Name}}Dto struct {
{{- range .Fields}}
	{{.Name}} {{.GoType}} ` + "`" + `json:"{{.JSONName}}" validate:"required"` + "`" + `
{{- end}}
}

type TUpdate{{.Name}}Dto struct {
	ID            uuid.UUID ` + "`" + `json:"id" validate:"required,uuid4_valid,does_record_exist_by_id_for_verification={{.PluralSnake}}"` + "`" + `
	RecordVersion int       ` + "`" + `json:"recordVersion" validate:"required,min=1"` + "`" + `
{{- range .Fields}}
	{{.Name}} *{{.GoType}} ` + "`" + `json:"{{.JSONName}},omitempty"` + "`" + `
{{- end}}
}

type TFind{{.Name}}ByIDDto struct {
	ID uuid.UUID ` + "`" + `json:"id" validate:"uuid4_valid,does_record_exist_by_id_for_verification={{.PluralSnake}}"` + "`" + `
}

type TArchive{{.Name}}Dto struct {
	ID uuid.UUID ` + "`" + `json:"id" validate:"uuid4_valid,does_record_exist_by_id_for_verification={{.PluralSnake}},is_record_deletable={{.PluralSnake}}"` + "`" + `
}

type {{.Name}}FiltersDto struct {
	Pagination *TPaginationInputDto ` + "`" + `json:"pagination,omitempty"` + "`" + `
	Sorting    *TSortingInputDto    ` + "`" + `json:"sorting,omitempty"` + "`" + `
}
`
