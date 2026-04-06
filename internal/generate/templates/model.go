package templates

var Model = `package models

// {{.Name}} represents the {{.LowerName}} domain entity.
type {{.Name}} struct {
	BaseModelImpl
{{- range .Fields}}
	{{.Name}} {{.GoType}} ` + "`" + `{{.GormType}} json:"{{.JSONName}}"` + "`" + `
{{- end}}
}
`
