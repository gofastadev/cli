package generate

import (
	"fmt"
	"os"
	"path/filepath"
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	content := jobTemplate
	content = strings.ReplaceAll(content, "__NAME__", d.Name)
	content = strings.ReplaceAll(content, "__LOWER_NAME__", d.LowerName)
	content = strings.ReplaceAll(content, "__SNAKE_NAME__", d.SnakeName)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	termcolor.PrintCreate(path)
	return nil
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

	termcolor.PrintPatch(path, "job registry")
	return os.WriteFile(path, []byte(s), 0o644)
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

	termcolor.PrintPatch(path, "job schedule")
	return os.WriteFile(path, []byte(s), 0o644)
}

const jobTemplate = `package jobs

import (
	"log/slog"

	"github.com/gofastadev/gofasta/pkg/scheduler"
	"gorm.io/gorm"
)

var _ scheduler.Job = (*__NAME__Job)(nil)

// __NAME__Job runs the __SNAKE_NAME__ cron job.
// Customize the Run() method with your business logic.
type __NAME__Job struct {
	DB     *gorm.DB
	Logger *slog.Logger
}

// New__NAME__Job constructs the job.
func New__NAME__Job(db *gorm.DB, logger *slog.Logger) *__NAME__Job {
	return &__NAME__Job{DB: db, Logger: logger}
}

// Name returns the scheduler-visible job name.
func (j *__NAME__Job) Name() string {
	return "__SNAKE_NAME__"
}

// Run executes the job.
func (j *__NAME__Job) Run() {
	// TODO: Implement your job logic here
	j.Logger.Info("__SNAKE_NAME__ job executed")
}
`
