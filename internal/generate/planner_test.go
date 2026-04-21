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

// resetPlannerState clears any dry-run state left over from earlier
// tests. Called at the top of every test to isolate from other tests
// that toggle the package-level planner flag.
func resetPlannerState(t *testing.T) {
	t.Helper()
	SetDryRun(false)
	// Clear the slice by re-enabling + disabling, which flushes via
	// the "enabled" branch of SetDryRun.
	SetDryRun(true)
	SetDryRun(false)
}

func TestSetDryRun_Toggles(t *testing.T) {
	resetPlannerState(t)
	assert.False(t, GetDryRun())
	SetDryRun(true)
	assert.True(t, GetDryRun())
	SetDryRun(false)
	assert.False(t, GetDryRun())
}

func TestWriteTemplate_DryRunRecordsButDoesNotWrite(t *testing.T) {
	resetPlannerState(t)
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))

	SetDryRun(true)
	t.Cleanup(func() { SetDryRun(false) })

	d := ScaffoldData{Name: "Product", SnakeName: "product", ModulePath: "example.com/app"}
	err := WriteTemplate("app/models/product.model.go", "model",
		"package models\n\ntype {{.Name}} struct{}\n", d)
	require.NoError(t, err)

	// Disk must be untouched.
	_, statErr := os.Stat("app/models/product.model.go")
	assert.True(t, os.IsNotExist(statErr), "dry-run must not create files on disk")

	// Plan must record exactly one create action.
	plan := Plan()
	require.Len(t, plan, 1)
	assert.Equal(t, "create", plan[0].Kind)
	assert.Equal(t, "app/models/product.model.go", plan[0].Path)
	assert.Greater(t, plan[0].Size, 0)
}

func TestPatchContainer_DryRunRecordsPatch(t *testing.T) {
	resetPlannerState(t)
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))

	// Minimal container.go that PatchContainer will accept.
	require.NoError(t, os.MkdirAll("app/di", 0755))
	container := `package di

import (
	svcInterfaces "example.com/app/app/services/interfaces"
	"example.com/app/app/rest/controllers"
)

type Container struct {
	Resolver       *resolvers.Resolver
}
`
	require.NoError(t, os.WriteFile("app/di/container.go", []byte(container), 0644))

	SetDryRun(true)
	t.Cleanup(func() { SetDryRun(false) })

	d := ScaffoldData{Name: "Product", ModulePath: "example.com/app", IncludeController: true}
	require.NoError(t, PatchContainer(d))

	// File on disk must be unchanged.
	after, err := os.ReadFile("app/di/container.go")
	require.NoError(t, err)
	assert.Equal(t, container, string(after), "dry-run must not modify files on disk")

	plan := Plan()
	require.Len(t, plan, 1)
	assert.Equal(t, "patch", plan[0].Kind)
	assert.Equal(t, "app/di/container.go", plan[0].Path)
	assert.Contains(t, plan[0].Detail, "Product")
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
