package generate

import (
	"fmt"
	"os"
	"os/exec"
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
	fmt.Println("  running: go tool wire ./app/di/")
	cmd := execCommand("go", "tool", "wire", "./app/di/")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunGqlgen regenerates the GraphQL code from schema files.
func RunGqlgen(_ ScaffoldData) error {
	fmt.Println("  running: go tool gqlgen generate")
	cmd := execCommand("go", "tool", "gqlgen", "generate")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
