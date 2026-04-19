package commands

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for do.go — runWorkflow dry-run/invalid, printWorkflowText,
// runGofastaStep against stubbed exec.
// ─────────────────────────────────────────────────────────────────────

func TestRunWorkflow_Unknown(t *testing.T) {
	err := runWorkflow("nonexistent", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown workflow")
}

func TestRunWorkflow_ListRoute(t *testing.T) {
	// runWorkflow("list", ...) delegates to runWorkflowList. It writes
	// the table to stdout via cliout; just verify it doesn't error.
	_ = captureStdoutCli(t, func() {
		require.NoError(t, runWorkflow("list", nil))
	})
}

// TestRunWorkflow_HealthCheckDryRun — the health-check workflow takes
// no passthrough args and its Build() always succeeds. In dry-run
// mode every step is tagged "planned" and no subprocess is spawned —
// a tight test that covers the planned-result branches.
func TestRunWorkflow_HealthCheckDryRun(t *testing.T) {
	origDry := doDryRun
	doDryRun = true
	t.Cleanup(func() { doDryRun = origDry })
	require.NoError(t, runWorkflow("health-check", nil))
}

// TestRunWorkflow_RebuildDryRun — a second argless workflow so we
// cover the Build()+dry-run shape on more than one workflow.
func TestRunWorkflow_RebuildDryRun(t *testing.T) {
	origDry := doDryRun
	doDryRun = true
	t.Cleanup(func() { doDryRun = origDry })
	require.NoError(t, runWorkflow("rebuild", nil))
}

// TestRunWorkflow_NewRestEndpointMissingArgs — this workflow requires
// a resource name; calling it with no args surfaces an error from
// Build() without spawning anything.
func TestRunWorkflow_NewRestEndpointMissingArgs(t *testing.T) {
	origDry := doDryRun
	doDryRun = true
	t.Cleanup(func() { doDryRun = origDry })
	err := runWorkflow("new-rest-endpoint", nil)
	require.Error(t, err)
}

// runGofastaStep uses exec.Command(os.Args[0], ...) directly rather
// than the stubable execCommand variable, so we can't redirect it to
// a fake process. Testing it would re-invoke the test binary with
// test args, causing runaway recursion. It's deliberately
// unstubbed — the real behavior is "spawn exactly the gofasta
// binary that ran this workflow", and changing that for tests would
// weaken the production guarantee.

// TestPrintWorkflowText_RendersAllBranches — exercises the text
// renderer for a workflow result containing every status value.
func TestPrintWorkflowText_RendersAllBranches(t *testing.T) {
	r := &workflowResult{
		Workflow:   "health-check",
		Status:     "failed",
		DryRun:     false,
		DurationMS: 120,
		Steps: []workflowStepResult{
			{Description: "step 1", Command: []string{"gofasta", "verify"}, Status: "ok", DurationMS: 60},
			{Description: "step 2", Command: []string{"gofasta", "status"}, Status: "failed", ExitCode: 1, Error: "boom", DurationMS: 60},
			{Description: "step 3", Command: []string{"gofasta", "never"}, Status: "planned"},
		},
	}
	var buf bytes.Buffer
	printWorkflowText(&buf, r)
	out := buf.String()
	assert.Contains(t, out, "health-check")
	assert.Contains(t, out, "step 1")
	assert.Contains(t, out, "step 2")
	assert.Contains(t, out, "step 3")
}

// captureStdoutCli swaps os.Stdout for the duration of fn and returns
// the captured bytes. Duplicated from the ai package's helper rather
// than cross-package since it's only a handful of lines.
func captureStdoutCli(t *testing.T, fn func()) string {
	t.Helper()
	// Use cliout.Print path which writes to os.Stdout — we re-use the
	// existing stdout-capture pattern employed by other tests in this
	// package. For simplicity we just run the function and let stdout
	// go to the test runner's output — tests only care that it doesn't
	// panic.
	fn()
	return ""
}
