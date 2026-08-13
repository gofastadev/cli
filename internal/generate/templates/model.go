package templates

// Model is the Go template for generating a GORM model.
//
// The import block is conditional on the resource's field types:
//   - Always: pkg/models (for BaseModelImpl).
//   - "time" / "github.com/google/uuid" — only when at least one field
//     is time.Time / uuid.UUID. Without the guard, models without such
//     fields break gofmt for an unused import; with the guard always
//     emitted, models that DO have one would compile-fail.
var Model = `package models

{{if .HasTimeField -}}
import (
	"time"

	"github.com/gofastadev/gofasta/pkg/models"
{{- if .HasUUIDField}}
	"github.com/google/uuid"
{{- end}}
)
{{- else if .HasUUIDField -}}
import (
	"github.com/gofastadev/gofasta/pkg/models"
	"github.com/google/uuid"
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
