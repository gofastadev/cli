package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenRoutes writes the route-registration file for the scaffolded resource.
func GenRoutes(d ScaffoldData) error {
	return WriteTemplate(fmt.Sprintf("app/rest/routes/%s.routes.go", d.SnakeName), "routes", templates.Routes, d)
}
