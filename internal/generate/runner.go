package generate

import (
	"fmt"
	"os"
	"os/exec"

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
