package generate

import (
	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenWireProvider writes a Wire provider file for the scaffolded
// resource. Layered puts it under app/di/providers/; feature puts it
// inside the per-resource directory at app/<resource>/wire.go.
func GenWireProvider(d ScaffoldData) error {
	return WriteTemplate(d.L().WireProviderFile(d.SnakeName), "wire_provider", templates.WireProvider, d)
}
