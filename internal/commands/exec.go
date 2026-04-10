package commands

import "os/exec"

// execCommand is a package-level seam for os/exec.Command so tests can inject
// a fake command runner. Production code always uses the real exec.Command.
var execCommand = exec.Command

// execLookPath is a package-level seam for os/exec.LookPath.
var execLookPath = exec.LookPath
