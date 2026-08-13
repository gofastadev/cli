package generate

import (
	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenErrors writes the per-resource sentinel-errors file for the
// scaffolded resource. Path is layout-dependent — layered puts it under
// app/services/, feature puts it under app/<resource>/.
func GenErrors(d ScaffoldData) error {
	return WriteTemplate(
		d.L().ErrorsFile(d.SnakeName),
		"errors", templates.Errors, d,
	)
}
