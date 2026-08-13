package generate

import (
	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenController writes a REST controller file for the scaffolded resource.
func GenController(d ScaffoldData) error {
	return WriteTemplate(d.L().ControllerFile(d.SnakeName), "controller", templates.Controller, d)
}
