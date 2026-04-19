package commands

import (
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// TestNewRestEndpoint_RequiresResourceName — the build function must
// reject invocations with no positional argument.
func TestNewRestEndpoint_RequiresResourceName(t *testing.T) {
	wf := findWorkflow("new-rest-endpoint")
	require.NotNil(t, wf)
	_, err := wf.Build(nil)
	require.Error(t, err)
	ce, ok := clierr.As(err)
	require.True(t, ok)
	assert.Equal(t, string(clierr.CodeInvalidName), ce.Code)
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
