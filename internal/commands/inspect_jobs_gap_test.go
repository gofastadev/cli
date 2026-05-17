package commands

import (
	"bytes"
	"go/ast"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInspectJobsCmd_RunE_NoArgs — calls the cobra RunE wrapper with
// zero args; filter remains "" and the call fails because there's no
// app/jobs dir in tempdir.
func TestInspectJobsCmd_RunE_NoArgs(t *testing.T) {
	chdirTest(t, t.TempDir())
	err := inspectJobsCmd.RunE(inspectJobsCmd, nil)
	require.Error(t, err)
}

// TestInspectJobsCmd_RunE_OneArg — calls the RunE wrapper with one
// positional arg so the `filter = args[0]` branch fires.
func TestInspectJobsCmd_RunE_OneArg(t *testing.T) {
	chdirTest(t, t.TempDir())
	err := inspectJobsCmd.RunE(inspectJobsCmd, []string{"some-filter"})
	require.Error(t, err)
}

// TestRunInspectJobs_ReadDirError — app/jobs is a regular file, not a
// dir; os.ReadDir then returns an error.
func TestRunInspectJobs_ReadDirError(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "app"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "app", "jobs"), []byte("not a dir"), 0o644))
	err := runInspectJobs("")
	require.Error(t, err)
}

// TestRunInspectJobs_IgnoresSubdirAndBadFile — a subdirectory, a
// _test.go file, and an unparseable .go each exercise a different
// skip/continue branch.
func TestRunInspectJobs_IgnoresSubdirAndBadFile(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	jobsDir := filepath.Join(tmp, "app", "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(jobsDir, "subdir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(jobsDir, "broken.go"),
		[]byte("package jobs\n!!!syntax error!!!\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(jobsDir, "x_test.go"),
		[]byte("package jobs\n"), 0o644))
	src := `package jobs
import "context"
type J struct{}
func (j *J) Name() string                  { return "j" }
func (j *J) Run(ctx context.Context) error { return nil }
`
	require.NoError(t, os.WriteFile(filepath.Join(jobsDir, "j.go"), []byte(src), 0o644))
	require.NoError(t, runInspectJobs(""))
}

// TestRunInspectJobs_FilterMismatch — filter excludes the only job so
// the `continue` branch fires.
func TestRunInspectJobs_FilterMismatch(t *testing.T) {
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
	require.NoError(t, runInspectJobs("other-name"))
}

// TestScanJobsFile_ParseError — bad Go source surfaces an error.
func TestScanJobsFile_ParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.go")
	require.NoError(t, os.WriteFile(path, []byte("not-a-go-file"), 0o644))
	_, err := scanJobsFile(path)
	require.Error(t, err)
}

// TestCollectJobMethods_SkipsNonStructReceiver — a method whose
// receiver type is not declared as a struct in this file is skipped.
func TestCollectJobMethods_SkipsNonStructReceiver(t *testing.T) {
	src := `package jobs
import "context"
type RealJob struct{}
func (r *RealJob) Name() string                  { return "r" }
func (r *RealJob) Run(ctx context.Context) error { return nil }
// Method whose receiver isn't a struct declared above — must skip.
func (s SomeOtherType) Name() string { return "x" }
`
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	entries, err := scanJobsFile(path)
	require.NoError(t, err)
	require.Equal(t, 1, len(entries))
	require.Equal(t, "RealJob", entries[0].Type)
}

// TestExtractSingleReturnString_NonTrivialBodies — exercise the
// fall-through return-"" branches.
func TestExtractSingleReturnString_NonTrivialBodies(t *testing.T) {
	src := `package x
func MultiStmt() string {
	_ = 1
	return "x"
}
func NotReturn() string { _ = "x"; return "x" }
func MultiReturn() (string, error) { return "x", nil }
func UnquotedLit() string { return ` + "`" + `raw-bt-string` + "`" + ` }
func NonBasicLit() string { return "y" + "z" }
`
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	f, err := parseGoFile(path)
	require.NoError(t, err)
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch fd.Name.Name {
		case "MultiStmt", "NotReturn", "MultiReturn", "NonBasicLit":
			require.Equal(t, "", extractSingleReturnString(fd))
		case "UnquotedLit":
			// Backtick string — BasicLit but not double-quoted, so the
			// strip-quotes branch is skipped and the raw value is
			// returned.
			require.NotEqual(t, "", extractSingleReturnString(fd))
		}
	}
}

// TestExtractSingleReturnString_NilBody — fd.Body == nil branch.
func TestExtractSingleReturnString_NilBody(t *testing.T) {
	// Construct a FuncDecl with nil body — forwarder-style stub.
	fd := &ast.FuncDecl{Name: ast.NewIdent("X")}
	require.Equal(t, "", extractSingleReturnString(fd))
}

// TestLoadJobSchedules_NoConfigYaml — file missing → empty map.
func TestLoadJobSchedules_NoConfigYaml(t *testing.T) {
	chdirTest(t, t.TempDir())
	require.Equal(t, map[string]string{}, loadJobSchedules())
}

// TestLoadJobSchedules_KoanfLoadError — malformed yaml → empty map.
func TestLoadJobSchedules_KoanfLoadError(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "config.yaml"),
		[]byte(":not yaml:::\n!!!"), 0o644))
	require.Equal(t, map[string]string{}, loadJobSchedules())
}

// TestLoadJobSchedules_NonScheduleKeysIgnored — keys not under
// jobs.<name>.schedule are skipped.
func TestLoadJobSchedules_NonScheduleKeysIgnored(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	cfg := "other:\n  k: v\njobs:\n  cleanup:\n    schedule: \"0 0 * * *\"\n    other: ignored\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte(cfg), 0o644))
	got := loadJobSchedules()
	require.Equal(t, "0 0 * * *", got["cleanup"])
	require.Equal(t, 1, len(got))
}

// TestRunInspectJobs_SortsTwoJobs — two jobs in the result force
// sort.Slice's comparator to fire (single-element slices skip it).
func TestRunInspectJobs_SortsTwoJobs(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	jobsDir := filepath.Join(tmp, "app", "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0o755))
	for _, j := range []struct{ file, src string }{
		{"a.go", `package jobs
import "context"
type AJob struct{}
func (j *AJob) Name() string                  { return "a" }
func (j *AJob) Run(ctx context.Context) error { return nil }
`},
		{"b.go", `package jobs
import "context"
type BJob struct{}
func (j *BJob) Name() string                  { return "b" }
func (j *BJob) Run(ctx context.Context) error { return nil }
`},
	} {
		require.NoError(t, os.WriteFile(filepath.Join(jobsDir, j.file), []byte(j.src), 0o644))
	}
	require.NoError(t, runInspectJobs(""))
}

// TestPrintInspectJobsText_Empty — count-0 branch.
func TestPrintInspectJobsText_Empty(t *testing.T) {
	var buf bytes.Buffer
	printInspectJobsText(&buf, inspectJobsResult{JobsDir: "app/jobs"})
	require.Contains(t, buf.String(), "No jobs registered under app/jobs.")
}

// TestPrintInspectJobsText_WithoutSchedule — schedule == "" branch.
func TestPrintInspectJobsText_WithoutSchedule(t *testing.T) {
	var buf bytes.Buffer
	printInspectJobsText(&buf, inspectJobsResult{
		JobsDir: "app/jobs",
		Count:   1,
		Jobs:    []inspectJobEntry{{Name: "j", Type: "J", File: "f"}},
	})
	require.Contains(t, buf.String(), "(not set in config.yaml)")
}
