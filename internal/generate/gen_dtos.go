package generate

import (
	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenDTOs writes DTO structs for the scaffolded resource.
func GenDTOs(d ScaffoldData) error {
	return WriteTemplate(d.L().DTOsFile(d.SnakeName), "dtos", templates.DTOs, d)
}
