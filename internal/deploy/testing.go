package deploy

import (
	"os/exec"
)

// SetLookPathForTest overrides the exec.LookPath seam used by preflight
// checks. Intended for use from other packages' tests.
func SetLookPathForTest(fn func(name string) (string, error)) {
	execLookPath = fn
}

// ResetLookPathForTest restores the original exec.LookPath.
func ResetLookPathForTest() {
	execLookPath = exec.LookPath
}

// SetExecCommandForTest overrides the exec.Command seam used by ssh/scp/etc.
func SetExecCommandForTest(fn func(name string, args ...string) *exec.Cmd) {
	execCommand = fn
}

// ResetExecCommandForTest restores the original exec.Command.
func ResetExecCommandForTest() {
	execCommand = exec.Command
}
