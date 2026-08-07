package commands

import (
	"embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/docs"
	"github.com/gofastadev/cli/internal/skeleton"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildFacts_Deterministic(t *testing.T) {
	first, err := buildFacts()
	require.NoError(t, err)
	second, err := buildFacts()
	require.NoError(t, err)

	a, err := json.Marshal(first)
	require.NoError(t, err)
	b, err := json.Marshal(second)
	require.NoError(t, err)
	assert.Equal(t, string(a), string(b))
}

func TestBuildFacts_EveryTopLevelCommandGrouped(t *testing.T) {
	facts, err := buildFacts()
	require.NoError(t, err)
	for _, c := range facts.Commands {
		assert.NotEmpty(t, c.Group, "top-level command %q has no help group — add it to commandGroupAssignments (or set GroupID) so help, README, and facts agree", c.Name)
	}
}

func TestBuildFacts_HiddenCommandsExcluded(t *testing.T) {
	facts, err := buildFacts()
	require.NoError(t, err)
	for _, c := range facts.Commands {
		assert.NotEqual(t, "__complete", c.Name)
		assert.NotEqual(t, "help", c.Name)
	}
}

func TestBuildFacts_CoreShape(t *testing.T) {
	facts, err := buildFacts()
	require.NoError(t, err)

	assert.Equal(t, 1, facts.SchemaVersion)
	assert.Equal(t, 18, facts.Scaffold.CreatedCount)
	assert.Equal(t, 4, facts.Scaffold.PatchedCount)
	assert.Len(t, facts.Scaffold.GraphQLExtra.Created, 2)
	assert.Len(t, facts.Scaffold.GraphQLExtra.Patched, 2)
	assert.Equal(t, 78, facts.Skeleton.FileCount)
	assert.Equal(t, []string{"postgres", "mysql", "sqlite", "sqlserver", "clickhouse"}, facts.Drivers)
	assert.Equal(t, []string{"layered", "feature"}, facts.Layouts)
	assert.Len(t, facts.FieldTypes, 7)
	assert.Len(t, facts.Workflows, 5)
	assert.Len(t, facts.AIAgents, 5)
	assert.Equal(t, []string{"linux", "darwin", "windows"}, facts.ReleasePlatforms.Goos)

	// Every driver ships at least one foundational migration set.
	require.Len(t, facts.Skeleton.Migrations, len(facts.Drivers))
	for _, m := range facts.Skeleton.Migrations {
		assert.Positive(t, m.Count, "driver %s has no foundational migrations", m.Driver)
	}

	// The `g` alias must be published — docs use it everywhere.
	var generateCmd *string
	for _, c := range facts.Commands {
		if c.Name == "generate" {
			generateCmd = &c.Aliases[0]
		}
	}
	require.NotNil(t, generateCmd, "generate command missing from facts")
	assert.Equal(t, "g", *generateCmd)
}

func TestWorkflowFacts_RendersSteps(t *testing.T) {
	wfs := workflowFacts()
	require.Len(t, wfs, 5)
	byKey := map[string][]string{}
	for _, wf := range wfs {
		byKey[wf.Key] = wf.Steps
	}
	assert.Equal(t,
		[]string{"gofasta g scaffold <ResourceName>", "gofasta migrate up", "gofasta swagger"},
		byKey["new-rest-endpoint"])
	assert.Equal(t, []string{"gofasta wire", "gofasta swagger"}, byKey["rebuild"])
}

// ---------- facts / sync / check runners ----------

// cliRepoRoot is the CLI repository root relative to this package's
// directory — the tree that carries README.md, Makefile and
// .goreleaser.yaml. The tests that run against it assert the same
// invariant `make docs-check` gates on: the checked-in docs match the
// facts document of the current build.
const cliRepoRoot = "../.."

// withFactsRepo points the facts sync/check machinery at dir for the
// duration of the test.
func withFactsRepo(t *testing.T, dir string) {
	t.Helper()
	orig := factsRepo
	factsRepo = dir
	t.Cleanup(func() { factsRepo = orig })
}

// withBogusDriver appends a driver with no embedded migrations, which is
// the one input that makes buildFacts fail through the public surface.
func withBogusDriver(t *testing.T) {
	t.Helper()
	orig := supportedDrivers
	supportedDrivers = append(append([]string(nil), orig...), "bogusdb")
	t.Cleanup(func() { supportedDrivers = orig })
}

// realREADME returns the checked-in README.md contents.
func realREADME(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(cliRepoRoot, "README.md"))
	require.NoError(t, err)
	return string(body)
}

// factsFixtureRepo creates a temp repo root holding the given README
// body and points factsRepo at it.
func factsFixtureRepo(t *testing.T, readme string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644))
	withFactsRepo(t, dir)
	return dir
}

// staleREADME returns the real README with a junk line injected inside a
// generated block, so sync must rewrite and check must complain.
func staleREADME(t *testing.T) string {
	t.Helper()
	const begin = "<!-- gofasta:begin command-index -->"
	body := realREADME(t)
	require.Contains(t, body, begin)
	return strings.Replace(body, begin, begin+"\nJUNK LINE FROM TEST", 1)
}

func TestRunFacts_TextOutput(t *testing.T) {
	out := captureStdout(t, func() {
		require.NoError(t, runFacts())
	})
	assert.Contains(t, out, "facts schema v")
	assert.Contains(t, out, "Run with --json for the full document.")
}

func TestRunFacts_BuildFailure(t *testing.T) {
	withBogusDriver(t)
	err := runFacts()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "counting bogusdb migrations")
}

func TestBuildFacts_SkeletonCountFailure(t *testing.T) {
	orig := skeleton.ProjectFS
	skeleton.ProjectFS = embed.FS{}
	t.Cleanup(func() { skeleton.ProjectFS = orig })

	_, err := buildFacts()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "counting skeleton files")
}

func TestBuildFacts_MigrationCountFailure(t *testing.T) {
	withBogusDriver(t)
	_, err := buildFacts()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "counting bogusdb migrations")
}

func TestCountFiles_PropagatesWalkErrors(t *testing.T) {
	_, err := countFiles(skeleton.MigrationsFS, "no-such-root")
	require.Error(t, err)
}

func TestRunFactsSync_RealREADMEAlreadyInSync(t *testing.T) {
	body := realREADME(t)
	dir := factsFixtureRepo(t, body)

	require.NoError(t, runFactsSync())

	after, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	assert.Equal(t, body, string(after),
		"the checked-in README must already match the facts document — run `make docs-sync` if this fails")
}

func TestRunFactsSync_RewritesStaleBlocks(t *testing.T) {
	dir := factsFixtureRepo(t, staleREADME(t))

	require.NoError(t, runFactsSync())

	after, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(after), "JUNK LINE FROM TEST",
		"sync must rewrite the generated block wholesale")
}

func TestRunFactsSync_MissingREADME(t *testing.T) {
	withFactsRepo(t, t.TempDir())
	err := runFactsSync()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "README.md")
}

func TestRunFactsSync_MissingMarkers(t *testing.T) {
	factsFixtureRepo(t, "# no markers here\n")
	err := runFactsSync()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing marker")
}

func TestRunFactsSync_WriteFailure(t *testing.T) {
	requireNonRoot(t)
	dir := factsFixtureRepo(t, staleREADME(t))
	readmePath := filepath.Join(dir, "README.md")
	require.NoError(t, os.Chmod(readmePath, 0o444))
	t.Cleanup(func() { _ = os.Chmod(readmePath, 0o644) })

	require.Error(t, runFactsSync())
}

func TestRunFactsSync_BuildFactsFailure(t *testing.T) {
	factsFixtureRepo(t, realREADME(t))
	withBogusDriver(t)
	require.Error(t, runFactsSync())
}

func TestRunFactsSync_RenderFailure(t *testing.T) {
	factsFixtureRepo(t, realREADME(t))
	orig := renderBlocksFn
	renderBlocksFn = func(docs.Facts) (map[string]string, error) { return nil, errDummy }
	t.Cleanup(func() { renderBlocksFn = orig })

	assert.ErrorIs(t, runFactsSync(), errDummy)
}

func TestFactsDocFiles_IncludesWorkspaceDocs(t *testing.T) {
	// A repo root whose PARENT carries .claude/docs/*.md — the workspace
	// shape factsDocFiles globs for.
	parent := t.TempDir()
	repo := filepath.Join(parent, "cli")
	require.NoError(t, os.MkdirAll(repo, 0o755))
	docsDir := filepath.Join(parent, ".claude", "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "cli.md"), []byte("# doc\n"), 0o644))
	withFactsRepo(t, repo)

	files := factsDocFiles()
	assert.Contains(t, files, "README.md")
	assert.Contains(t, files, "internal/skeleton/project/README.md.tmpl")
	assert.Contains(t, files, filepath.Join("..", ".claude", "docs", "cli.md"))
}

func TestRunFactsCheck_GreenOnRealRepo(t *testing.T) {
	withFactsRepo(t, cliRepoRoot)
	assert.NoError(t, runFactsCheck(),
		"docs drifted from the facts document — run `make docs-check` for details")
}

func TestRunFactsCheck_MissingREADME(t *testing.T) {
	withFactsRepo(t, t.TempDir())
	err := runFactsCheck()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "README.md")
}

func TestRunFactsCheck_StaleBlocksReported(t *testing.T) {
	factsFixtureRepo(t, staleREADME(t))
	out := captureStdout(t, func() {
		err := runFactsCheck()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "docs-check found")
	})
	assert.Contains(t, out, "command-index")
}

func TestRunFactsCheck_UnreadableDocFileFails(t *testing.T) {
	requireNonRoot(t)
	dir := factsFixtureRepo(t, realREADME(t))
	tmpl := filepath.Join(dir, "internal", "skeleton", "project", "README.md.tmpl")
	require.NoError(t, os.MkdirAll(filepath.Dir(tmpl), 0o755))
	require.NoError(t, os.WriteFile(tmpl, []byte("# tmpl\n"), 0o644))
	require.NoError(t, os.Chmod(tmpl, 0o000))
	t.Cleanup(func() { _ = os.Chmod(tmpl, 0o644) })

	err := runFactsCheck()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestRunFactsCheck_BuildFactsFailure(t *testing.T) {
	withBogusDriver(t)
	require.Error(t, runFactsCheck())
}

func TestRunFactsCheck_RenderFailure(t *testing.T) {
	orig := renderBlocksFn
	renderBlocksFn = func(docs.Facts) (map[string]string, error) { return nil, errDummy }
	t.Cleanup(func() { renderBlocksFn = orig })

	assert.ErrorIs(t, runFactsCheck(), errDummy)
}

// ---------- the cobra RunE closures ----------

func TestFactsCmd_ExecutesThroughRoot(t *testing.T) {
	rootCmd.SetArgs([]string{"facts"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	out := captureStdout(t, func() {
		require.NoError(t, rootCmd.Execute())
	})
	assert.Contains(t, out, "facts schema v")
}

func TestFactsSyncCmd_ExecutesThroughRoot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte(realREADME(t)), 0o644))
	rootCmd.SetArgs([]string{"facts", "sync", "--repo", dir})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		factsRepo = "."
	})

	out := captureStdout(t, func() {
		require.NoError(t, rootCmd.Execute())
	})
	assert.Contains(t, out, "already in sync")
}

func TestFactsCheckCmd_ExecutesThroughRoot(t *testing.T) {
	rootCmd.SetArgs([]string{"facts", "check", "--repo", cliRepoRoot})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		factsRepo = "."
	})

	out := captureStdout(t, func() {
		require.NoError(t, rootCmd.Execute())
	})
	assert.Contains(t, out, "docs-check green")
}
