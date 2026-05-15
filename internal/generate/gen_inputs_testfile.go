package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenInputsTestFile writes the executable inputs test alongside every
// generated inputs file (AsMap negative-space + AsRepoFilter empty-map).
func GenInputsTestFile(d ScaffoldData) error {
	return WriteTemplate(
		fmt.Sprintf("app/services/%s_inputs_test.go", d.SnakeName),
		"inputs_test", templates.InputsTest, d,
	)
}
