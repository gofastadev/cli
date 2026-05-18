package generate

import (
	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenInputs writes the per-resource domain-inputs file for the
// scaffolded resource. Contains CreateXInput, UpdateXPatch (with
// AsMap), and ListXFilter. Path is layout-dependent.
func GenInputs(d ScaffoldData) error {
	return WriteTemplate(
		d.L().InputsFile(d.SnakeName),
		"inputs", templates.Inputs, d,
	)
}
