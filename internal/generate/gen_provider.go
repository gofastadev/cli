package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

func GenWireProvider(d ScaffoldData) error {
	return WriteTemplate(fmt.Sprintf("app/di/providers/%s.go", d.SnakeName), "wire_provider", templates.WireProvider, d)
}
