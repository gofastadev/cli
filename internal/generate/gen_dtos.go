package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

func GenDTOs(d ScaffoldData) error {
	return WriteTemplate(fmt.Sprintf("app/dtos/%s.dtos.go", d.SnakeName), "dtos", templates.DTOs, d)
}
