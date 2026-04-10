package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenDTOs writes DTO structs for the scaffolded resource.
func GenDTOs(d ScaffoldData) error {
	return WriteTemplate(fmt.Sprintf("app/dtos/%s.dtos.go", d.SnakeName), "dtos", templates.DTOs, d)
}
