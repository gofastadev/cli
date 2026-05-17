package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/require"
)

func TestRunInspectJobs_MissingDirFires(t *testing.T) {
	chdirTest(t, t.TempDir())
	err := runInspectJobs("")
	require.Error(t, err)
	require.Equal(t, string(clierr.CodeJobsDirMissing), codeOf(err))
}

func TestScanJobsFile_DetectsNameRunContract(t *testing.T) {
	dir := t.TempDir()
	src := `package jobs

import "context"

type CleanupTokensJob struct{}

func (j *CleanupTokensJob) Name() string                  { return "cleanup-tokens" }
func (j *CleanupTokensJob) Run(ctx context.Context) error { return nil }

// HelperType lacks Name(), should NOT be reported as a job.
type HelperType struct{}
func (h *HelperType) Run(ctx context.Context) error { return nil }
`
	path := filepath.Join(dir, "cleanup_tokens.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	entries, err := scanJobsFile(path)
	require.NoError(t, err)
	require.Equal(t, 1, len(entries))
	require.Equal(t, "cleanup-tokens", entries[0].Name)
	require.Equal(t, "CleanupTokensJob", entries[0].Type)
}

func TestScanJobsFile_FallsBackToTypeNameWhenNameComputed(t *testing.T) {
	dir := t.TempDir()
	src := `package jobs

import "context"

type DynamicJob struct{ tag string }

func (j *DynamicJob) Name() string                  { return j.tag } // not a literal
func (j *DynamicJob) Run(ctx context.Context) error { return nil }
`
	path := filepath.Join(dir, "dynamic.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	entries, err := scanJobsFile(path)
	require.NoError(t, err)
	require.Equal(t, 1, len(entries))
	require.Equal(t, "dynamicjob", entries[0].Name) // lowercased type name
}

func TestRunInspectJobs_EndToEnd(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)

	jobsDir := filepath.Join(tmp, "app", "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0o755))

	src := `package jobs

import "context"

type CleanupJob struct{}
func (j *CleanupJob) Name() string                  { return "cleanup" }
func (j *CleanupJob) Run(ctx context.Context) error { return nil }
`
	require.NoError(t, os.WriteFile(filepath.Join(jobsDir, "cleanup.go"), []byte(src), 0o644))
	// And a sibling _test.go file which must be ignored.
	require.NoError(t, os.WriteFile(filepath.Join(jobsDir, "cleanup_test.go"), []byte("package jobs\n"), 0o644))
	// Config with a schedule.
	cfg := "jobs:\n  cleanup:\n    schedule: \"0 0 * * *\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte(cfg), 0o644))

	require.NoError(t, runInspectJobs(""))
}
