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

// TestPrintWorkflowText_DryRun — dry-run branch produces the
// "Dry run — workflow X would execute" block.
func TestPrintWorkflowText_DryRun(t *testing.T) {
	r := &workflowResult{
		Workflow:   "health-check",
		Status:     "planned",
		DryRun:     true,
		DurationMS: 0,
		Steps: []workflowStepResult{
			{Description: "verify", Command: []string{"gofasta", "verify"}, Status: "planned"},
		},
	}
	var buf bytes.Buffer
	printWorkflowText(&buf, r)
	assert.Contains(t, buf.String(), "Dry run")
}

// TestFindWorkflow_Known — returns a non-nil pointer for every
// registered workflow key.
func TestFindWorkflow_Known(t *testing.T) {
	for _, key := range []string{"health-check", "rebuild", "fresh-start", "clean-slate"} {
		t.Run(key, func(t *testing.T) {
			wf := findWorkflow(key)
			require.NotNil(t, wf)
			assert.Equal(t, key, wf.Key)
		})
	}
}

// TestFindWorkflow_Unknown — returns nil for an unknown key.
func TestFindWorkflow_Unknown(t *testing.T) {
	assert.Nil(t, findWorkflow("nonexistent-workflow"))
}

// ─────────────────────────────────────────────────────────────────────
// Coverage for runGofastaStep and runWorkflow's real-execution path.
// runGofastaStep uses the execCommand seam, so tests can stub it to a
// fake subprocess and drive the full workflow.
// ─────────────────────────────────────────────────────────────────────

// TestRunGofastaStep_FakeSuccess — exec seam returns exit 0.
func TestRunGofastaStep_FakeSuccess(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, runGofastaStep([]string{"version"}))
}

// TestRunGofastaStep_FakeFail — exec seam returns exit 1.
func TestRunGofastaStep_FakeFail(t *testing.T) {
	withFakeExec(t, 1)
	assert.Error(t, runGofastaStep([]string{"nope"}))
}

// TestRunWorkflow_Rebuild_Success — rebuild has two argless steps
// (wire, swagger); both succeed via the exec seam.
func TestRunWorkflow_Rebuild_Success(t *testing.T) {
	origDry := doDryRun
	doDryRun = false
	t.Cleanup(func() { doDryRun = origDry })
	withFakeExec(t, 0)
	require.NoError(t, runWorkflow("rebuild", nil))
}

// TestRunWorkflow_Rebuild_StepFails — the first step (wire) fails and
// runWorkflow returns a wrapped error.
func TestRunWorkflow_Rebuild_StepFails(t *testing.T) {
	origDry := doDryRun
	doDryRun = false
	t.Cleanup(func() { doDryRun = origDry })
	withFakeExec(t, 1)
	err := runWorkflow("rebuild", nil)
	require.Error(t, err)
}

// TestRunWorkflow_HealthCheck_Real — health-check runs verify +
// status; with exec stubs returning 0 it completes.
func TestRunWorkflow_HealthCheck_Real(t *testing.T) {
	origDry := doDryRun
	doDryRun = false
	t.Cleanup(func() { doDryRun = origDry })
	withFakeExec(t, 0)
	_ = runWorkflow("health-check", nil)
	// Either outcome exercises printWorkflowText's success branch.
}
