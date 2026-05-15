package generate

import (
	"fmt"
	"os"
	"strings"

	"github.com/gofastadev/cli/internal/termcolor"
)

// GenTask generates an async task handler file in app/tasks/ AND a
// sibling _test.go with executable behavior tests (Handle valid +
// invalid payloads; Enqueue round-trips through a captured queue).
func GenTask(d ScaffoldData) error {
	path := fmt.Sprintf("app/tasks/%s.task.go", d.SnakeName)
	testPath := fmt.Sprintf("app/tasks/%s.task_test.go", d.SnakeName)

	if _, err := os.Stat(path); err == nil {
		termcolor.PrintSkip(path, "exists")
	} else {
		content := taskTemplate
		content = strings.ReplaceAll(content, "__NAME__", d.Name)
		content = strings.ReplaceAll(content, "__LOWER_NAME__", d.LowerName)
		content = strings.ReplaceAll(content, "__SNAKE_NAME__", d.SnakeName)
		if err := writeOrRecordCreate(path, []byte(content)); err != nil {
			return err
		}
	}

	if _, err := os.Stat(testPath); err == nil {
		termcolor.PrintSkip(testPath, "exists")
		return nil
	}
	test := taskTestTemplate
	test = strings.ReplaceAll(test, "__NAME__", d.Name)
	test = strings.ReplaceAll(test, "__SNAKE_NAME__", d.SnakeName)
	test = strings.ReplaceAll(test, "__MODULE_PATH__", d.ModulePath)
	return writeOrRecordCreate(testPath, []byte(test))
}

// taskTestTemplate is the executable test file emitted alongside
// every generated task. Covers Handle (valid + invalid payload) and
// Enqueue (round-trip via an inline mock queue).
const taskTestTemplate = `package tasks_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"__MODULE_PATH__/app/tasks"
)

// captureQueue is an inline queue.QueueService stand-in. It records
// the bytes the task emits so the test can decode and inspect them.
type captureQueue struct {
	lastTask    string
	lastPayload []byte
	enqueueErr  error
}

func (c *captureQueue) Enqueue(_ context.Context, taskName string, payload []byte, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	c.lastTask = taskName
	c.lastPayload = payload
	if c.enqueueErr != nil {
		return nil, c.enqueueErr
	}
	return &asynq.TaskInfo{}, nil
}

// TestEnqueue__NAME___RoundTripsPayload — payload marshaled and
// passed to Enqueue with the right task name.
func TestEnqueue__NAME___RoundTripsPayload(t *testing.T) {
	q := &captureQueue{}
	in := tasks.__NAME__Payload{ID: "abc-123"}
	require.NoError(t, tasks.Enqueue__NAME__(context.Background(), q, in))

	assert.Equal(t, tasks.Task__NAME__, q.lastTask)
	var got tasks.__NAME__Payload
	require.NoError(t, json.Unmarshal(q.lastPayload, &got))
	assert.Equal(t, "abc-123", got.ID)
}

// TestEnqueue__NAME___PropagatesQueueError — queue errors are wrapped
// (not swallowed).
func TestEnqueue__NAME___PropagatesQueueError(t *testing.T) {
	q := &captureQueue{enqueueErr: errors.New("redis down")}
	err := tasks.Enqueue__NAME__(context.Background(), q, tasks.__NAME__Payload{ID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Enqueue__NAME__")
	assert.Contains(t, err.Error(), "redis down")
}

// TestHandle__NAME___ValidPayload — happy path returns nil.
func TestHandle__NAME___ValidPayload(t *testing.T) {
	payload, err := json.Marshal(tasks.__NAME__Payload{ID: "abc"})
	require.NoError(t, err)
	task := asynq.NewTask(tasks.Task__NAME__, payload)
	require.NoError(t, tasks.Handle__NAME__(context.Background(), task))
}

// TestHandle__NAME___InvalidPayload — malformed JSON returns a
// wrapped error so asynq schedules a retry.
func TestHandle__NAME___InvalidPayload(t *testing.T) {
	task := asynq.NewTask(tasks.Task__NAME__, []byte("not-json"))
	err := tasks.Handle__NAME__(context.Background(), task)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Handle__NAME__")
}
`

const taskTemplate = `package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/gofastadev/gofasta/pkg/queue"
	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel"
)

const __LOWER_NAME__TaskTracerName = "app/tasks/__SNAKE_NAME__"

// Task__NAME__ is the asynq task type name. Workers route inbound
// payloads to Handle__NAME__ by matching on this string.
const Task__NAME__ = "__SNAKE_NAME__"

// __NAME__Payload is the data passed to the __SNAKE_NAME__ task.
// Replace the placeholder fields with the real payload schema.
type __NAME__Payload struct {
	// TODO: replace with real payload fields
	ID string ` + "`" + `json:"id"` + "`" + `
}

// Handle__NAME__ processes the __SNAKE_NAME__ task.
//
// Idempotency: asynq retries failed tasks with backoff (default 25
// retries). Make this handler safe to invoke multiple times for the
// same payload — use upserts, deduplicated keys, or a processed-set
// to avoid double-applying side effects.
func Handle__NAME__(ctx context.Context, t *asynq.Task) error {
	ctx, span := otel.Tracer(__LOWER_NAME__TaskTracerName).Start(ctx, "Handle__NAME__")
	defer span.End()

	var payload __NAME__Payload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("Handle__NAME__: unmarshal: %w", err)
	}

	slog.InfoContext(ctx, "processing task", "task", Task__NAME__, "payload", payload)
	// TODO: Implement task logic here.
	return nil
}

// Enqueue__NAME__ creates and enqueues a __SNAKE_NAME__ task on the
// supplied queue service. Usage:
//
//	tasks.Enqueue__NAME__(ctx, container.QueueService, __NAME__Payload{ID: "123"})
func Enqueue__NAME__(ctx context.Context, qs queue.QueueService, payload __NAME__Payload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("Enqueue__NAME__: marshal: %w", err)
	}
	if _, err := qs.Enqueue(ctx, Task__NAME__, data); err != nil {
		return fmt.Errorf("Enqueue__NAME__: enqueue: %w", err)
	}
	return nil
}
`
