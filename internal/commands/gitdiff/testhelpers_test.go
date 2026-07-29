package gitdiff

import (
	"context"
	"os/exec"
	"testing"
)

// stagedExecCommand returns a fake execCommand seam that hands out one
// fake command per call from a pre-baked queue. Lets a test choreograph
// successes followed by a single failure on the Nth call.
func stagedExecCommand(t *testing.T, plans []func() *exec.Cmd) func(ctx context.Context, name string, args ...string) *exec.Cmd {
	t.Helper()
	var i int
	return func(_ context.Context, _ string, _ ...string) *exec.Cmd {
		if i >= len(plans) {
			t.Fatalf("execCommand called %d time(s); only %d staged", i+1, len(plans))
		}
		c := plans[i]()
		i++
		return c
	}
}

func okCmd(out string) func() *exec.Cmd {
	return func() *exec.Cmd { return exec.Command("printf", out) }
}

func failCmd() func() *exec.Cmd { return func() *exec.Cmd { return exec.Command("false") } }
