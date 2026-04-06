package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

func GenController(d ScaffoldData) error {
	return WriteTemplate(fmt.Sprintf("app/rest/controllers/%s.controller.go", d.SnakeName), "controller", templates.Controller, d)
}
