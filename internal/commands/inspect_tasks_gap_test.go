package commands

import (
	"bytes"
	"go/ast"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInspectTasksCmd_RunE_NoArgs — wrapper with no arg.
func TestInspectTasksCmd_RunE_NoArgs(t *testing.T) {
	chdirTest(t, t.TempDir())
	err := inspectTasksCmd.RunE(inspectTasksCmd, nil)
	require.Error(t, err)
}

// TestInspectTasksCmd_RunE_OneArg — wrapper with one arg.
func TestInspectTasksCmd_RunE_OneArg(t *testing.T) {
	chdirTest(t, t.TempDir())
	err := inspectTasksCmd.RunE(inspectTasksCmd, []string{"some-filter"})
	require.Error(t, err)
}

// TestRunInspectTasks_ReadDirError — app/tasks is a regular file.
func TestRunInspectTasks_ReadDirError(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "app"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "app", "tasks"), []byte("not a dir"), 0o644))
	require.Error(t, runInspectTasks(""))
}

// TestRunInspectTasks_IgnoresSubdirAndBadFile — subdirectory, _test.go,
// non-task.go, and broken .task.go each exercise a continue branch.
func TestRunInspectTasks_IgnoresSubdirAndBadFile(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	tasksDir := filepath.Join(tmp, "app", "tasks")
	require.NoError(t, os.MkdirAll(tasksDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tasksDir, "subdir"), 0o755))
	// Non-task.go file (no .task suffix) — skipped by suffix filter.
	require.NoError(t, os.WriteFile(filepath.Join(tasksDir, "helper.go"), []byte("package tasks\n"), 0o644))
	// _test.go — skipped.
	require.NoError(t, os.WriteFile(filepath.Join(tasksDir, "foo_test.go"), []byte("package tasks\n"), 0o644))
	// Broken .task.go — scanTasksFile errors, then `continue`.
	require.NoError(t, os.WriteFile(filepath.Join(tasksDir, "broken.task.go"),
		[]byte("package tasks\n!!!syntax!!!\n"), 0o644))
	src := `package tasks
import (
	"context"
	"github.com/hibiken/asynq"
)
const TaskFoo = "task.foo"
type FooPayload struct{ X int }
func HandleFoo(ctx context.Context, t *asynq.Task) error { return nil }
func EnqueueFoo(ctx context.Context, q *asynq.Client, p FooPayload) (*asynq.TaskInfo, error) {
	return nil, nil
}
`
	require.NoError(t, os.WriteFile(filepath.Join(tasksDir, "foo.task.go"), []byte(src), 0o644))
	require.NoError(t, runInspectTasks(""))
}

// TestRunInspectTasks_FilterMismatch — filter excludes the only task.
func TestRunInspectTasks_FilterMismatch(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	tasksDir := filepath.Join(tmp, "app", "tasks")
	require.NoError(t, os.MkdirAll(tasksDir, 0o755))
	src := `package tasks
import (
	"context"
	"github.com/hibiken/asynq"
)
const TaskFoo = "task.foo"
type FooPayload struct{ X int }
func HandleFoo(ctx context.Context, t *asynq.Task) error { return nil }
func EnqueueFoo(ctx context.Context, q *asynq.Client, p FooPayload) (*asynq.TaskInfo, error) {
	return nil, nil
}
`
	require.NoError(t, os.WriteFile(filepath.Join(tasksDir, "foo.task.go"), []byte(src), 0o644))
	require.NoError(t, runInspectTasks("Bar"))
}

// TestRunInspectTasks_SortsTwoTasks — exercise the sort comparator.
func TestRunInspectTasks_SortsTwoTasks(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	tasksDir := filepath.Join(tmp, "app", "tasks")
	require.NoError(t, os.MkdirAll(tasksDir, 0o755))
	for _, p := range []struct{ file, src string }{
		{"a.task.go", `package tasks
import ("context"; "github.com/hibiken/asynq")
const TaskAlpha = "alpha"
type AlphaPayload struct{}
func HandleAlpha(ctx context.Context, t *asynq.Task) error { return nil }
func EnqueueAlpha(ctx context.Context, q *asynq.Client, p AlphaPayload) (*asynq.TaskInfo, error) { return nil, nil }
`},
		{"b.task.go", `package tasks
import ("context"; "github.com/hibiken/asynq")
const TaskBeta = "beta"
type BetaPayload struct{}
func HandleBeta(ctx context.Context, t *asynq.Task) error { return nil }
func EnqueueBeta(ctx context.Context, q *asynq.Client, p BetaPayload) (*asynq.TaskInfo, error) { return nil, nil }
`},
	} {
		require.NoError(t, os.WriteFile(filepath.Join(tasksDir, p.file), []byte(p.src), 0o644))
	}
	require.NoError(t, runInspectTasks(""))
}

// TestScanTasksFile_ParseError — bad source surfaces an error.
func TestScanTasksFile_ParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.task.go")
	require.NoError(t, os.WriteFile(path, []byte("not-go"), 0o644))
	_, err := scanTasksFile(path)
	require.Error(t, err)
}

// TestCollectTaskConsts_IgnoresVarBlock — `var` decls and `Task` (bare)
// must be skipped.
func TestCollectTaskConsts_IgnoresVarBlock(t *testing.T) {
	src := `package tasks
var TaskNotAConst = "x"
const Task = "bare"
const (
	TaskFoo = "foo"
	NotATask = "skip"
)
`
	dir := t.TempDir()
	path := filepath.Join(dir, "x.task.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	f, err := parseGoFile(path)
	require.NoError(t, err)
	got := collectTaskConsts(f)
	require.Equal(t, 1, len(got))
	_, hasFoo := got["Foo"]
	require.True(t, hasFoo)
}

// TestCollectTaskConsts_ImportSpecSkipped — an ImportSpec inside a
// const-shaped decl can't happen normally, but a non-ValueSpec spec is
// possible inside other GenDecls; we cover the !ok branch by using a
// `type` GenDecl (which is filtered by gd.Tok.String() != "const" but
// still walks if a future bug ever loosens that check).
func TestCollectTaskConsts_TypeBlockIgnored(t *testing.T) {
	src := `package tasks
type SomeType struct{}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "x.task.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	f, err := parseGoFile(path)
	require.NoError(t, err)
	require.Empty(t, collectTaskConsts(f))
}

// TestStringLitValue_Variants — exercise both fall-through branches:
// non-BasicLit (a CompositeLit) and a BasicLit that isn't a quoted
// string (raw-string-literal or numeric).
func TestStringLitValue_Variants(t *testing.T) {
	// non-BasicLit
	require.Equal(t, "", stringLitValue(&ast.CompositeLit{}))
	// BasicLit but raw string (backtick) — not double-quoted, so the
	// strip-quotes branch is skipped.
	require.Equal(t, "", stringLitValue(&ast.BasicLit{Kind: 9 /* token.STRING */, Value: "`raw`"}))
	// BasicLit numeric — same path.
	require.Equal(t, "", stringLitValue(&ast.BasicLit{Kind: 5 /* token.INT */, Value: "42"}))
}

// TestCollectTaskPayloads_NotStructTypeIsSkipped — `type FooPayload =
// int` is a TypeSpec but its Type isn't a StructType.
func TestCollectTaskPayloads_NotStructTypeIsSkipped(t *testing.T) {
	src := `package tasks
const TaskFoo = "foo"
type FooPayload = int
`
	dir := t.TempDir()
	path := filepath.Join(dir, "x.task.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	entries, err := scanTasksFile(path)
	require.NoError(t, err)
	require.Equal(t, 1, len(entries))
	require.Empty(t, entries[0].Payload)
}

// TestCollectTaskPayloads_PayloadForUnknownTaskSkipped — payload with
// no matching Task<Name> const must be ignored.
func TestCollectTaskPayloads_PayloadForUnknownTaskSkipped(t *testing.T) {
	src := `package tasks
const TaskFoo = "foo"
type FooPayload struct{ X int }
type OrphanPayload struct{ Y int } // no matching TaskOrphan
`
	dir := t.TempDir()
	path := filepath.Join(dir, "x.task.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	entries, err := scanTasksFile(path)
	require.NoError(t, err)
	require.Equal(t, 1, len(entries))
	require.Equal(t, "Foo", entries[0].Name)
}

// TestCollectTaskFuncs_MethodsSkipped — methods (Recv != nil) must not
// register as handlers/enqueuers.
func TestCollectTaskFuncs_MethodsSkipped(t *testing.T) {
	src := `package tasks
import (
	"context"
	"github.com/hibiken/asynq"
)
const TaskFoo = "foo"
type T struct{}
func (t T) HandleFoo(ctx context.Context, x *asynq.Task) error { return nil }
`
	dir := t.TempDir()
	path := filepath.Join(dir, "x.task.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	entries, err := scanTasksFile(path)
	require.NoError(t, err)
	require.Equal(t, 1, len(entries))
	require.False(t, entries[0].HasHandler, "methods named HandleX must not satisfy the contract")
}

// TestPrintInspectTasksText_Empty — count-0 branch.
func TestPrintInspectTasksText_Empty(t *testing.T) {
	var buf bytes.Buffer
	printInspectTasksText(&buf, inspectTasksResult{TasksDir: "app/tasks"})
	require.Contains(t, buf.String(), "No tasks registered under app/tasks.")
}

// TestPrintInspectTasksText_AllHandlerCombinations — exercise the
// HasHandler/HasEnqueue switch's four branches.
func TestPrintInspectTasksText_AllHandlerCombinations(t *testing.T) {
	cases := []struct {
		handler, enqueue bool
		needle           string
	}{
		{true, true, "Handle"},
		{true, false, "missing Enqueue"},
		{false, true, "missing Handle"},
		{false, false, "both missing"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		printInspectTasksText(&buf, inspectTasksResult{
			TasksDir: "app/tasks",
			Count:    1,
			Tasks: []inspectTaskEntry{{
				Name:       "Foo",
				TypeName:   "TaskFoo",
				WireName:   "foo",
				File:       "f",
				HasHandler: c.handler,
				HasEnqueue: c.enqueue,
				Payload:    []fieldEntry{{Name: "X", Type: "int"}},
			}},
		})
		require.Contains(t, buf.String(), c.needle)
	}
}

// TestPrintInspectTasksText_NoWireName — WireName empty branch.
func TestPrintInspectTasksText_NoWireName(t *testing.T) {
	var buf bytes.Buffer
	printInspectTasksText(&buf, inspectTasksResult{
		TasksDir: "app/tasks",
		Count:    1,
		Tasks: []inspectTaskEntry{{
			Name: "Foo", TypeName: "TaskFoo", File: "f",
			HasHandler: true, HasEnqueue: true,
		}},
	})
	require.NotContains(t, buf.String(), "wire_name:")
}
