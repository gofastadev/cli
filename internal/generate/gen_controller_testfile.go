package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenControllerTestFile writes a starter _test.go file alongside the
// generated controller. The file compiles out of the box (so the
// scaffold's post-gen `go build` / `go test` passes) and contains
// smoke tests plus a skipped TODO placeholder. Developers and AI
// agents fill in real behavior tests on top of the stubs.
//
// Separate from GenController so callers can include or exclude it —
// we currently add it to every flow that produces a controller.
func GenControllerTestFile(d ScaffoldData) error {
	return WriteTemplate(
		fmt.Sprintf("app/rest/controllers/%s.controller_test.go", d.SnakeName),
		"controller_test",
		templates.ControllerTest,
		d,
	)
}
