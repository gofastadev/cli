package templates

var Model = `package models

import "github.com/gofastadev/gofasta/pkg/models"

// {{.Name}} represents the {{.LowerName}} domain entity.
type {{.Name}} struct {
	models.BaseModelImpl
{{- range .Fields}}
	{{.Name}} {{.GoType}} ` + "`" + `{{.GormType}} json:"{{.JSONName}}"` + "`" + `
{{- end}}
}
`
