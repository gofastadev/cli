package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenInputs writes the per-resource domain-inputs file (
// `app/services/<lower>_inputs.go`) for the scaffolded resource.
// Contains CreateXInput, UpdateXPatch (with AsMap), and ListXFilter.
func GenInputs(d ScaffoldData) error {
	return WriteTemplate(
		fmt.Sprintf("app/services/%s_inputs.go", d.SnakeName),
		"inputs", templates.Inputs, d,
	)
}
