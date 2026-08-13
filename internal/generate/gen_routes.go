package generate

import (
	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenRoutes writes the route-registration file for the scaffolded resource.
func GenRoutes(d ScaffoldData) error {
	return WriteTemplate(d.L().RoutesFile(d.SnakeName), "routes", templates.Routes, d)
}
