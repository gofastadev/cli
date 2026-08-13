package generate

import (
	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenDTOsTestFile writes the executable DTO mapper test alongside
// every generated DTOs file. Tests are real assertions, not TODO
// stubs — see templates.DTOsTest for the contract.
func GenDTOsTestFile(d ScaffoldData) error {
	return WriteTemplate(
		d.L().DTOsTestFile(d.SnakeName),
		"dtos_test", templates.DTOsTest, d,
	)
}
