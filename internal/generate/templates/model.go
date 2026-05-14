package templates

// Model is the Go template for generating a GORM model.
//
// The import block is conditional on the resource's field types:
//   - Always: pkg/models (for BaseModelImpl).
//   - "time" — only when at least one field is time.Time. Without the
//     guard, models with no time field break gofmt/goimports for an
//     unused import; with the guard always emitted, models that DO
//     have a time field would compile-fail.
var Model = `package models

{{if .HasTimeField -}}
import (
	"time"

	"github.com/gofastadev/gofasta/pkg/models"
)
{{- else -}}
import "github.com/gofastadev/gofasta/pkg/models"
{{- end}}

// {{.Name}} represents the {{.LowerName}} domain entity.
type {{.Name}} struct {
	models.BaseModelImpl
{{- range .Fields}}
	{{.Name}} {{.GoType}} ` + "`" + `{{.GormType}} json:"{{.JSONName}}"` + "`" + `
{{- end}}
}
`
