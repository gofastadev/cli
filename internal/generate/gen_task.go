package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GenTask generates an async task handler file in app/tasks/.
func GenTask(d ScaffoldData) error {
	path := fmt.Sprintf("app/tasks/%s.task.go", d.SnakeName)
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("  skip (exists): %s\n", path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	content := taskTemplate
	content = strings.ReplaceAll(content, "__NAME__", d.Name)
	content = strings.ReplaceAll(content, "__SNAKE_NAME__", d.SnakeName)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("  create: %s\n", path)
	return nil
}

const taskTemplate = `package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"
)

const Task__NAME__ = "__SNAKE_NAME__"

// __NAME__Payload is the data passed to the __SNAKE_NAME__ task.
type __NAME__Payload struct {
	// TODO: Add payload fields
	ID string ` + "`" + `json:"id"` + "`" + `
}

// Handle__NAME__ processes the __SNAKE_NAME__ task.
func Handle__NAME__(ctx context.Context, t *asynq.Task) error {
	var payload __NAME__Payload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal %s payload: %w", Task__NAME__, err)
	}

	slog.Info("processing task", "task", Task__NAME__, "payload", payload)

	// TODO: Implement task logic here

	return nil
}

// Enqueue__NAME__ creates and enqueues a __SNAKE_NAME__ task.
// Usage: tasks.Enqueue__NAME__(ctx, queueService, __NAME__Payload{ID: "123"})
func Enqueue__NAME__(ctx context.Context, queue interface{ Enqueue(context.Context, string, []byte, ...asynq.Option) (*asynq.TaskInfo, error) }, payload __NAME__Payload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = queue.Enqueue(ctx, Task__NAME__, data)
	return err
}
`
