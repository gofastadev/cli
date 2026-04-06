package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

func GenModel(d ScaffoldData) error {
	return WriteTemplate(fmt.Sprintf("app/models/%s.model.go", d.SnakeName), "model", templates.Model, d)
}
