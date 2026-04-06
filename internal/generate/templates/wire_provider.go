package templates

var WireProvider = `package providers

import (
	"github.com/google/wire"
	"github.com/gofastadev/gofasta/app/repositories"
	repoInterfaces "github.com/gofastadev/gofasta/app/repositories/interfaces"
{{- if .IncludeController}}
	"github.com/gofastadev/gofasta/app/rest/controllers"
{{- end}}
	"github.com/gofastadev/gofasta/app/services"
	svcInterfaces "github.com/gofastadev/gofasta/app/services/interfaces"
)

var {{.Name}}Set = wire.NewSet(
	repositories.New{{.Name}}Repository,
	wire.Bind(new(repoInterfaces.{{.Name}}RepositoryInterface), new(*repositories.{{.Name}}Repository)),
	services.New{{.Name}}Service,
	wire.Bind(new(svcInterfaces.{{.Name}}ServiceInterface), new(*services.{{.Name}}Service)),
{{- if .IncludeController}}
	controllers.New{{.Name}}ControllerInstance,
{{- end}}
)
`
