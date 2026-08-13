package generate

import (
	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenInputsTestFile writes the executable inputs test alongside every
// generated inputs file (AsMap negative-space + AsRepoFilter empty-map).
func GenInputsTestFile(d ScaffoldData) error {
	return WriteTemplate(
		d.L().InputsTestFile(d.SnakeName),
		"inputs_test", templates.InputsTest, d,
	)
}
