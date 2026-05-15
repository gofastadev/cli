package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenErrors writes the per-resource sentinel-errors file (
// `app/services/<lower>_errors.go`) for the scaffolded resource.
func GenErrors(d ScaffoldData) error {
	return WriteTemplate(
		fmt.Sprintf("app/services/%s_errors.go", d.SnakeName),
		"errors", templates.Errors, d,
	)
}
