package generate

import (
	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenModel writes a GORM model file for the scaffolded resource.
func GenModel(d ScaffoldData) error {
	return WriteTemplate(d.L().ModelFile(d.SnakeName), "model", templates.Model, d)
}
