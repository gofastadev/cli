package generate

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/termcolor"
)

// execCommand is a package-level seam so tests can inject a fake.
var execCommand = exec.Command

// RunSteps executes a sequence of steps, stopping at the first error.
func RunSteps(d ScaffoldData, steps []Step) error {
	for _, s := range steps {
		if err := s.Fn(d); err != nil {
			return fmt.Errorf("failed at %s: %w", s.Label, err)
		}
	}
	return nil
}

// AutoVerify runs `go build ./...` in the project root to confirm the
// just-generated code compiles. Intended as a post-hook after a generator
// that produces a full compilable unit (scaffold, service, controller).
// Kept small and shell-based so it is cheap to run; callers that need
// the full preflight gauntlet should invoke `gofasta verify` instead.
//
// When the build succeeds, returns nil silently. When it fails, returns
// a structured clierr.Error whose Hint points at common causes agents
// can act on programmatically (template regression, missing Wire rerun,
// outdated deps).
func AutoVerify() error {
	fmt.Printf("  %s go build ./...\n", termcolor.CBrand("verifying:"))
	cmd := execCommand("go", "build", "./...")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		if output := buf.String(); output != "" {
			_, _ = os.Stderr.WriteString(output)
		}
		return clierr.Wrap(clierr.CodeGoBuildFailed, err,
			"the generated code does not compile")
	}
	return nil
}

// RunWire regenerates the Wire dependency injection code.
func RunWire(_ ScaffoldData) error {
	fmt.Printf("  %s go tool wire ./app/di/\n", termcolor.CBrand("running:"))
	cmd := execCommand("go", "tool", "wire", "./app/di/")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunGqlgen regenerates the GraphQL code from schema files.
func RunGqlgen(_ ScaffoldData) error {
	fmt.Printf("  %s go tool gqlgen generate\n", termcolor.CBrand("running:"))
	cmd := execCommand("go", "tool", "gqlgen", "generate")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
