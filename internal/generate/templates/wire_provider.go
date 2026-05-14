package templates

// WireProvider is the Go template for generating a Wire provider.
var WireProvider = `package providers

import (
	"github.com/google/wire"

	"{{.ModulePath}}/app/repositories"
	repoInterfaces "{{.ModulePath}}/app/repositories/interfaces"
{{- if .IncludeController}}
	"{{.ModulePath}}/app/rest/controllers"
{{- end}}
	"{{.ModulePath}}/app/services"
	svcInterfaces "{{.ModulePath}}/app/services/interfaces"
)

// {{.Name}}Set wires the {{.Name}} resource: repository, service{{if .IncludeController}}, and controller{{end}}.
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
