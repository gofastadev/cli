package generate

import (
	"fmt"
	"os"
	"strings"

	"github.com/gofastadev/cli/internal/termcolor"
)

// GenTask generates an async task handler file in app/tasks/.
func GenTask(d ScaffoldData) error {
	path := fmt.Sprintf("app/tasks/%s.task.go", d.SnakeName)
	if _, err := os.Stat(path); err == nil {
		termcolor.PrintSkip(path, "exists")
		return nil
	}

	content := taskTemplate
	content = strings.ReplaceAll(content, "__NAME__", d.Name)
	content = strings.ReplaceAll(content, "__LOWER_NAME__", d.LowerName)
	content = strings.ReplaceAll(content, "__SNAKE_NAME__", d.SnakeName)

	// writeOrRecordCreate handles MkdirAll + format.Source for .go files,
	// so the emitted task file is preflight-clean by construction.
	return writeOrRecordCreate(path, []byte(content))
}

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
