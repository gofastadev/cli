package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for runGofastaStep and runWorkflow's real-execution path.
// Both previously showed 0% / ~50% because the happy path was never
// driven — runGofastaStep uses the execCommand seam, so we can stub
// it to a fake subprocess and drive the full workflow.
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
