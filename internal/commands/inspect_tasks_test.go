package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/require"
)

func TestRunInspectTasks_MissingDirFires(t *testing.T) {
	chdirTest(t, t.TempDir())
	err := runInspectTasks("")
	require.Error(t, err)
	require.Equal(t, string(clierr.CodeTasksDirMissing), codeOf(err))
}

func TestScanTasksFile_DetectsFullContract(t *testing.T) {
	dir := t.TempDir()
	src := `package tasks

import (
	"context"
	"github.com/hibiken/asynq"
)

const TaskSendWelcomeEmail = "task.send.welcome.email"

type SendWelcomeEmailPayload struct {
	UserID string
	Email  string
}

func HandleSendWelcomeEmail(ctx context.Context, t *asynq.Task) error { return nil }
func EnqueueSendWelcomeEmail(ctx context.Context, q *asynq.Client, p SendWelcomeEmailPayload) (*asynq.TaskInfo, error) {
	return nil, nil
}
`
	path := filepath.Join(dir, "send_welcome_email.task.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	entries, err := scanTasksFile(path)
	require.NoError(t, err)
	require.Equal(t, 1, len(entries))
	e := entries[0]
	require.Equal(t, "SendWelcomeEmail", e.Name)
	require.Equal(t, "TaskSendWelcomeEmail", e.TypeName)
	require.Equal(t, "task.send.welcome.email", e.WireName)
	require.True(t, e.HasHandler)
	require.True(t, e.HasEnqueue)
	require.Equal(t, 2, len(e.Payload))
}

func TestScanTasksFile_PartialContractStillReported(t *testing.T) {
	dir := t.TempDir()
	// Missing Enqueue helper.
	src := `package tasks
import (
	"context"
	"github.com/hibiken/asynq"
)
const TaskCleanup = "task.cleanup"
type CleanupPayload struct{ ID string }
func HandleCleanup(ctx context.Context, t *asynq.Task) error { return nil }
`
	path := filepath.Join(dir, "cleanup.task.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	entries, err := scanTasksFile(path)
	require.NoError(t, err)
	require.Equal(t, 1, len(entries))
	require.True(t, entries[0].HasHandler)
	require.False(t, entries[0].HasEnqueue, "missing Enqueue helper must surface as false, not hide the entry")
}

func TestRunInspectTasks_EndToEnd(t *testing.T) {
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
func EnqueueFoo(ctx context.Context, q *asynq.Client, p FooPayload) (*asynq.TaskInfo, error) { return nil, nil }
`
	require.NoError(t, os.WriteFile(filepath.Join(tasksDir, "foo.task.go"), []byte(src), 0o644))

	require.NoError(t, runInspectTasks(""))
}
