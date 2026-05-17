package generate

import (
	"fmt"
	"os"
	"strings"

	"github.com/gofastadev/cli/internal/cliout"
)

// GenJob generates a cron job file in app/jobs/ AND a sibling
// _test.go with executable behavior tests (Name(), Run happy path,
// Run respects ctx cancellation).
func GenJob(d ScaffoldData) error {
	subs := []tokenPair{
		{"__NAME__", d.Name},
		{"__LOWER_NAME__", d.LowerName},
		{"__SNAKE_NAME__", d.SnakeName},
		{"__MODULE_PATH__", d.ModulePath},
	}
	if err := renderAndEmit(fmt.Sprintf("app/jobs/%s.go", d.SnakeName), jobTemplate, subs); err != nil {
		return err
	}
	return renderAndEmit(fmt.Sprintf("app/jobs/%s_test.go", d.SnakeName), jobTestTemplate, subs)
}

// jobTestTemplate is the executable test file emitted alongside every
// generated job. No t.Skip — every test exercises real code paths
// (Name() value, Run happy path, Run respects ctx cancellation).
const jobTestTemplate = `package jobs_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"__MODULE_PATH__/app/jobs"
)

func test__NAME__JobDeps(t *testing.T) (*gorm.DB, *slog.Logger) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db, slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Test__NAME__Job_Name pins the registered name. Renaming this
// without also updating jobs config in config.yaml would silently
// disable the job in production.
func Test__NAME__Job_Name(t *testing.T) {
	db, logger := test__NAME__JobDeps(t)
	j := jobs.New__NAME__Job(db, logger)
	assert.Equal(t, "__SNAKE_NAME__", j.Name())
}

// Test__NAME__Job_Run_Success — happy path: Run returns nil.
func Test__NAME__Job_Run_Success(t *testing.T) {
	db, logger := test__NAME__JobDeps(t)
	j := jobs.New__NAME__Job(db, logger)
	require.NoError(t, j.Run(context.Background()))
}

// Test__NAME__Job_Run_RespectsContext — well-behaved jobs return
// promptly when ctx is canceled. The starter body is short and
// doesn't block on a non-ctx-aware sleep, so even a pre-canceled ctx
// should let Run finish.
func Test__NAME__Job_Run_RespectsContext(t *testing.T) {
	db, logger := test__NAME__JobDeps(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	j := jobs.New__NAME__Job(db, logger)
	done := make(chan error, 1)
	go func() { done <- j.Run(ctx) }()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return promptly when ctx was already canceled")
	}
}
`

// PatchJobRegistry adds the new job to the registry in cmd/serve.go.
func PatchJobRegistry(d ScaffoldData) error {
	path := "cmd/serve.go"
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(content)

	// Check for uncommented registry line (tab-indented, not //-prefixed)
	uncommentedRegistry := fmt.Sprintf("\t\t%q: jobs.New%sJob(", d.SnakeName, d.Name)
	if strings.Contains(s, uncommentedRegistry) {
		cliout.Skip(path, "already registered")
		return nil
	}

	// Add to registry map
	insertLine := fmt.Sprintf("\t\t%q: jobs.New%sJob(container.DB, logger),\n", d.SnakeName, d.Name)
	s = strings.Replace(s,
		"// \"cleanup-tokens\":",
		insertLine+"// \"cleanup-tokens\":",
		1,
	)

	// If the marker comment doesn't exist, try inserting before the closing brace of registry
	if !strings.Contains(s, insertLine) {
		s = strings.Replace(s,
			"\t}\n\n\tfor _, jobCfg",
			insertLine+"\t}\n\n\tfor _, jobCfg",
			1,
		)
	}

	return writeOrRecordPatch(path, "job registry", []byte(s))
}

// PatchJobConfig adds the job schedule to config.yaml.
func PatchJobConfig(d ScaffoldData) error {
	path := "config.yaml"
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(content)

	// Check for an active (uncommented) entry with this job name
	activeEntry := fmt.Sprintf("  - name: %s\n", d.SnakeName)
	if strings.Contains(s, activeEntry) {
		cliout.Skip(path, "already in config")
		return nil
	}

	schedule := d.Schedule
	if schedule == "" {
		schedule = "0 0 * * * *" // default: every hour
	}

	entry := fmt.Sprintf("  - name: %s\n    schedule: %q\n    enabled: true\n", d.SnakeName, schedule)

	// Append after the jobs: section
	if strings.Contains(s, "jobs:") {
		// Find the end of the first job entry and insert after
		s = strings.Replace(s, "jobs:\n", "jobs:\n"+entry, 1)
	} else {
		s += "\njobs:\n" + entry
	}

	return writeOrRecordPatch(path, "job schedule", []byte(s))
}

const jobTemplate = `package jobs

import (
	"context"
	"log/slog"

	"github.com/gofastadev/gofasta/pkg/scheduler"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

const __LOWER_NAME__JobTracerName = "app/jobs/__SNAKE_NAME__"

var _ scheduler.Job = (*__NAME__Job)(nil)

// __NAME__Job runs the __SNAKE_NAME__ cron job. Replace the body of
// Run with the business logic.
//
// Fields are unexported — Wire (or your own caller) injects them via
// the constructor. Public fields would invite callers to mutate
// dependencies after construction.
type __NAME__Job struct {
	db     *gorm.DB
	logger *slog.Logger
}

// New__NAME__Job constructs the job.
func New__NAME__Job(db *gorm.DB, logger *slog.Logger) *__NAME__Job {
	return &__NAME__Job{db: db, logger: logger}
}

// Name returns the scheduler-visible job name. The framework matches
// this to a ` + "`name`" + ` entry in the project's jobs config to look up
// the cron schedule.
func (j *__NAME__Job) Name() string { return "__SNAKE_NAME__" }

// Run executes the job once. The accepted ctx is canceled when the
// scheduler is stopped (via app shutdown). Long-running jobs should
// periodically check ctx.Done() so a graceful shutdown doesn't have
// to wait for them.
//
// Returning an error doesn't stop the scheduler — the next tick still
// fires — but the framework logs the error at ERROR level with the
// job name attached. Idempotency is the job's responsibility.
func (j *__NAME__Job) Run(ctx context.Context) error {
	ctx, span := otel.Tracer(__LOWER_NAME__JobTracerName).Start(ctx, "__NAME__Job.Run")
	defer span.End()

	// TODO: Implement your job logic here.
	j.logger.InfoContext(ctx, "__SNAKE_NAME__ job executed")
	return nil
}
`
