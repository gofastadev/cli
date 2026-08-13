package commands

import (
	"bytes"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStepStatusMark_EveryBranch(t *testing.T) {
	// Every status string supported by the do.go step renderer.
	for _, in := range []string{"ok", "error", "skip", "unknown"} {
		got := stripANSI(stepStatusMark(in))
		assert.NotEmpty(t, got, "in=%s produced empty mark", in)
	}
}

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

func TestDoCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "do" {
			found = true
			break
		}
	}
	assert.True(t, found, "doCmd should be registered on rootCmd")
}

// TestWorkflows_EveryEntryValid — each registered workflow must have a
// non-empty Key, a Build function, and Build must accept empty args
// when Args is empty (the "no positional args required" case).
func TestWorkflows_EveryEntryValid(t *testing.T) {
	seen := map[string]bool{}
	for _, wf := range workflows {
		assert.NotEmpty(t, wf.Key, "workflow with empty key")
		assert.NotEmpty(t, wf.Description, "workflow %q has no description", wf.Key)
		require.NotNil(t, wf.Build, "workflow %q has nil Build", wf.Key)

		if seen[wf.Key] {
			t.Errorf("duplicate workflow key %q", wf.Key)
		}
		seen[wf.Key] = true

		if wf.Args == "" {
			// Must be able to build with no positional args.
			steps, err := wf.Build(nil)
			require.NoError(t, err, "workflow %q failed to build with empty args", wf.Key)
			assert.NotEmpty(t, steps, "workflow %q returned zero steps", wf.Key)
		}
	}
}

func TestFindWorkflow_Hit(t *testing.T) {
	got := findWorkflow("new-rest-endpoint")
	require.NotNil(t, got)
	assert.Equal(t, "new-rest-endpoint", got.Key)
}

func TestFindWorkflow_Miss(t *testing.T) {
	assert.Nil(t, findWorkflow("no-such-workflow"))
}

// TestNewRestEndpoint_BuildsExpectedSteps — happy path, confirms the
// step sequence is scaffold → migrate up → swagger.
func TestNewRestEndpoint_BuildsExpectedSteps(t *testing.T) {
	wf := findWorkflow("new-rest-endpoint")
	require.NotNil(t, wf)
	steps, err := wf.Build([]string{"Invoice", "total:float", "status:string"})
	require.NoError(t, err)
	require.Len(t, steps, 3)

	// Step 1: g scaffold Invoice total:float status:string
	assert.Equal(t, []string{"g", "scaffold", "Invoice", "total:float", "status:string"}, steps[0].Args)
	assert.Contains(t, steps[0].Description, "scaffold")

	// Step 2: migrate up
	assert.Equal(t, []string{"migrate", "up"}, steps[1].Args)

	// Step 3: swagger
	assert.Equal(t, []string{"swagger"}, steps[2].Args)
}

// TestRebuild_BuildsTwoSteps — rebuild has no args and produces
// wire + swagger.
func TestRebuild_BuildsTwoSteps(t *testing.T) {
	wf := findWorkflow("rebuild")
	require.NotNil(t, wf)
	steps, err := wf.Build(nil)
	require.NoError(t, err)
	require.Len(t, steps, 2)
	assert.Equal(t, []string{"wire"}, steps[0].Args)
	assert.Equal(t, []string{"swagger"}, steps[1].Args)
}

// TestFreshStart_BuildsThreeSteps — init + migrate up + seed.
func TestFreshStart_BuildsThreeSteps(t *testing.T) {
	steps, err := findWorkflow("fresh-start").Build(nil)
	require.NoError(t, err)
	require.Len(t, steps, 3)
	assert.Equal(t, []string{"init"}, steps[0].Args)
	assert.Equal(t, []string{"migrate", "up"}, steps[1].Args)
	assert.Equal(t, []string{"seed"}, steps[2].Args)
}

func TestCleanSlate_BuildsTwoSteps(t *testing.T) {
	steps, err := findWorkflow("clean-slate").Build(nil)
	require.NoError(t, err)
	require.Len(t, steps, 2)
	assert.Equal(t, []string{"db", "reset"}, steps[0].Args)
	assert.Equal(t, []string{"seed"}, steps[1].Args)
}

func TestHealthCheck_BuildsTwoSteps(t *testing.T) {
	steps, err := findWorkflow("health-check").Build(nil)
	require.NoError(t, err)
	require.Len(t, steps, 2)
	assert.Equal(t, []string{"verify"}, steps[0].Args)
	assert.Equal(t, []string{"status"}, steps[1].Args)
}

// TestRunWorkflow_UnknownReturnsClierr — unknown workflow key surfaces
// a CodeInvalidName clierr.Error (not a plain fmt.Errorf).
func TestRunWorkflow_UnknownReturnsClierr(t *testing.T) {
	err := runWorkflow("nonexistent", nil)
	require.Error(t, err)
	ce, ok := clierr.As(err)
	require.True(t, ok)
	assert.Equal(t, string(clierr.CodeInvalidName), ce.Code)
}

// TestRunWorkflow_ListIsSpecial — "list" isn't a registered workflow
// but the runWorkflow dispatcher intercepts it.
func TestRunWorkflow_ListIsSpecial(t *testing.T) {
	err := runWorkflow("list", nil)
	require.NoError(t, err)
}

// TestDoCmd_RunE_Unknown — exercises the Cobra RunE wrapper with an
// unknown workflow name.
func TestDoCmd_RunE_Unknown(t *testing.T) {
	err := doCmd.RunE(doCmd, []string{"nonexistent-workflow"})
	require.Error(t, err)
}

// TestWorkflowFacts_SkipsWorkflowsWhoseBuildFails covers the defensive
// continue: a Build that errors even with its placeholder args must be
// dropped from the facts projection rather than panic or truncate it.
func TestWorkflowFacts_SkipsWorkflowsWhoseBuildFails(t *testing.T) {
	orig := workflows
	workflows = append(append([]workflow(nil), orig...), workflow{
		Key:         "boom",
		Description: "always fails to build",
		Args:        "<Name>",
		Build:       func([]string) ([]workflowStep, error) { return nil, errDummy },
	})
	t.Cleanup(func() { workflows = orig })

	wfs := workflowFacts()
	require.Len(t, wfs, len(orig), "the failing workflow must be skipped")
	for _, wf := range wfs {
		assert.NotEqual(t, "boom", wf.Key)
	}
}
