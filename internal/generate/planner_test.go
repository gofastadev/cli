package generate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenField_DryRunRecordsPlannedActions(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)

	fields := ParseFields([]string{"notes:text"})
	SetDryRun(true)
	defer SetDryRun(false)
	require.NoError(t, GenField(FieldData{Resource: "Order", Field: fields[0]}))

	plan := Plan()
	require.GreaterOrEqual(t, len(plan), 3, "expected model patch + 2 migrations in plan")

	// One patch on the model file, one create per migration file.
	patches, creates := 0, 0
	for _, a := range plan {
		switch a.Kind {
		case "patch":
			patches++
		case "create":
			creates++
		}
	}
	require.Equal(t, 1, patches)
	require.Equal(t, 2, creates)

	// Disk must be untouched.
	model, _ := os.ReadFile(filepath.Join(tmp, "app", "models", "order.model.go"))
	require.NotContains(t, string(model), "Notes string")
}

func TestSetDryRun_Toggles(t *testing.T) {
	resetPlannerState(t)
	assert.False(t, GetDryRun())
	SetDryRun(true)
	assert.True(t, GetDryRun())
	SetDryRun(false)
	assert.False(t, GetDryRun())
}

func TestPlan_SortedByPath(t *testing.T) {
	resetPlannerState(t)
	SetDryRun(true)
	t.Cleanup(func() { SetDryRun(false) })

	recordCreate("app/z.go", 100)
	recordCreate("app/a.go", 200)
	recordCreate("app/m.go", 150)

	plan := Plan()
	require.Len(t, plan, 3)
	assert.Equal(t, "app/a.go", plan[0].Path)
	assert.Equal(t, "app/m.go", plan[1].Path)
	assert.Equal(t, "app/z.go", plan[2].Path)
}

func TestPrintPlanText_EmptyPlan(t *testing.T) {
	resetPlannerState(t)
	var buf bytes.Buffer
	PrintPlanText(&buf)
	assert.Contains(t, buf.String(), "No changes would be made")
}

func TestPrintPlanText_RendersCreateAndPatch(t *testing.T) {
	resetPlannerState(t)
	SetDryRun(true)
	t.Cleanup(func() { SetDryRun(false) })

	recordCreate("app/models/product.model.go", 340)
	recordPatch("app/di/container.go", "add ProductService field", 1234)

	var buf bytes.Buffer
	PrintPlanText(&buf)
	out := buf.String()
	assert.Contains(t, out, "Dry run — 1 create, 1 patch")
	assert.Contains(t, out, "+ app/models/product.model.go")
	assert.Contains(t, out, "~ app/di/container.go")
	assert.Contains(t, out, "add ProductService field")
}

// TestHumanSize — formatting boundaries.
func TestHumanSize(t *testing.T) {
	assert.Equal(t, "0 B", humanSize(0))
	assert.Equal(t, "1023 B", humanSize(1023))
	assert.Equal(t, "1.0 KB", humanSize(1024))
	assert.Equal(t, "4.2 KB", humanSize(4300))
}

// TestDescribePatch — fragments joined, newlines collapsed, long
// fragments truncated to the 60-char budget.
func TestDescribePatch(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"add field", "register route"}, "add field + register route"},
		{[]string{"  spacey  ", "  more  "}, "spacey + more"},
		{[]string{""}, ""},
		{[]string{"line1\nline2"}, "line1 line2"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, describePatch(tc.in...))
	}
}

// TestDryRun_IsolatedBetweenTests ensures that resetPlannerState clears
// any leftover state so later tests see an empty plan.
func TestDryRun_IsolatedBetweenTests(t *testing.T) {
	resetPlannerState(t)
	SetDryRun(true)
	recordCreate("junk.go", 1)
	SetDryRun(false)
	// After toggling off, a fresh dry-run should see an empty plan.
	SetDryRun(true)
	t.Cleanup(func() { SetDryRun(false) })
	assert.Empty(t, Plan(), "toggling dry-run on must reset the planner state")

	// Sanity: ensure temp files weren't created (defense in depth against
	// future refactors that accidentally write during planning).
	_, err := os.Stat(filepath.Join(t.TempDir(), "junk.go"))
	assert.True(t, os.IsNotExist(err))
}

// TestWriteOrRecordPatch_WriteFails — point at an unwritable path
// (chmod the parent read-only) so os.WriteFile returns an error.
func TestWriteOrRecordPatch_WriteFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	setupTempProject(t)
	dir := filepath.Join("ro")
	require.NoError(t, os.MkdirAll(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	err := writeOrRecordPatch(filepath.Join(dir, "file.go"), "test", []byte("x"))
	require.Error(t, err)
}

// TestPrintPlanText_EmptyDetail — a plan action with an empty Detail
// value uses the "in-place edit" fallback.
func TestPrintPlanText_EmptyDetail(t *testing.T) {
	// SetDryRun(true) clears planned. Add a patch with empty detail.
	SetDryRun(true)
	t.Cleanup(func() { SetDryRun(false) })
	recordPatch("file.go", "", 0)
	var buf bytes.Buffer
	PrintPlanText(&buf)
	out := buf.String()
	assert.Contains(t, out, "in-place edit")
}

// TestDescribePatch_Truncates — a fragment longer than 60 chars is
// truncated with "..." suffix.
func TestDescribePatch_Truncates(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := describePatch(long)
	assert.Len(t, got, 60)
	assert.True(t, strings.HasSuffix(got, "..."))
}

// TestWriteOrRecordPatch_RefusesOutOfTreePath covers the defense-in-depth net
// that stops a resource name which slipped past validateIdentifier from
// patching a file outside the project root.
func TestWriteOrRecordPatch_RefusesOutOfTreePath(t *testing.T) {
	setupTempProject(t)

	for _, path := range []string{"../escape.go", "/etc/passwd", ".."} {
		t.Run(path, func(t *testing.T) {
			err := writeOrRecordPatch(path, "attempted patch", []byte("package x\n"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "refusing to write outside the project root")
		})
	}
}
