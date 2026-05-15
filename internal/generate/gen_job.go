package generate

import (
	"fmt"
	"os"
	"strings"

	"github.com/gofastadev/cli/internal/termcolor"
)

// GenJob generates a cron job file in app/jobs/.
func GenJob(d ScaffoldData) error {
	path := fmt.Sprintf("app/jobs/%s.go", d.SnakeName)
	if _, err := os.Stat(path); err == nil {
		termcolor.PrintSkip(path, "exists")
		return nil
	}

	content := jobTemplate
	content = strings.ReplaceAll(content, "__NAME__", d.Name)
	content = strings.ReplaceAll(content, "__LOWER_NAME__", d.LowerName)
	content = strings.ReplaceAll(content, "__SNAKE_NAME__", d.SnakeName)

	// writeOrRecordCreate handles MkdirAll + format.Source for .go files,
	// so the emitted job file is preflight-clean by construction.
	return writeOrRecordCreate(path, []byte(content))
}

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
		termcolor.PrintSkip(path, "already registered")
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
		termcolor.PrintSkip(path, "already in config")
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
