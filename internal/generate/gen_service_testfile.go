package generate

import (
	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenSvcTestFile writes the executable service test alongside every
// generated service file. Inline mock repository; covers Get,
// Update, Archive sentinel paths + happy paths.
func GenSvcTestFile(d ScaffoldData) error {
	return WriteTemplate(
		d.L().SvcTestFile(d.SnakeName),
		"svc_test", templates.SvcTest, d,
	)
}
