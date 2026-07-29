package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findingsFor filters a report's issues by check key.
func findingsFor(issues []preflightIssue, check string) []preflightIssue {
	var out []preflightIssue
	for _, i := range issues {
		if i.Check == check {
			out = append(out, i)
		}
	}
	return out
}

// withoutChecks strips the named checks — tests on non-git temp
// fixtures use it to ignore the expected no-git blocker and the
// static generated-hand-edits note.
func withoutChecks(issues []preflightIssue, checks ...string) []preflightIssue {
	drop := map[string]bool{}
	for _, c := range checks {
		drop[c] = true
	}
	var out []preflightIssue
	for _, i := range issues {
		if !drop[i.Check] {
			out = append(out, i)
		}
	}
	return out
}

// TestRefactorPreflight_PristineLayeredProjectIsClean pins the managed
// set and the expected-symbol surface against the real skeleton: a
// freshly rendered REST project must produce ZERO findings beyond the
// no-git blocker (the fixture is a temp dir, not a repo). Any new
// skeleton file or exported symbol that isn't classified fails here
// instead of producing false warnings for users.
func TestRefactorPreflight_PristineLayeredProjectIsClean(t *testing.T) {
	inRenderedProject(t)

	pf := refactorPreflight(preflightToFeature)

	assert.Empty(t, withoutChecks(pf.Blockers, "no-git"),
		"pristine project must have no blockers besides no-git")
	assert.Empty(t, pf.Warnings, "pristine project must have no warnings")
	require.Len(t, findingsFor(pf.Blockers, "no-git"), 1)
}

// TestRefactorPreflight_PristineGraphQLProjectIsClean is the GraphQL
// variant: same guarantee, plus the one static generated-hand-edits
// note that always accompanies gqlgen projects.
func TestRefactorPreflight_PristineGraphQLProjectIsClean(t *testing.T) {
	inRenderedGraphQLProject(t)

	pf := refactorPreflight(preflightToFeature)

	assert.Empty(t, withoutChecks(pf.Blockers, "no-git"))
	assert.Empty(t, withoutChecks(pf.Warnings, "generated-hand-edits"),
		"pristine GraphQL project must warn only about regenerated files")
	require.Len(t, findingsFor(pf.Warnings, "generated-hand-edits"), 1)
}

// TestRefactorPreflight_MigratedProjectIsCleanForReverse pins the
// feature-side managed set: a project taken forward by the real
// orchestrator must be clean for the reverse direction.
func TestRefactorPreflight_MigratedProjectIsCleanForReverse(t *testing.T) {
	migratedProject(t)

	pf := refactorPreflight(preflightToLayered)

	assert.Empty(t, withoutChecks(pf.Blockers, "no-git"))
	assert.Empty(t, pf.Warnings)
}

func TestRefactorPreflight_TornResourceBlocks(t *testing.T) {
	inRenderedProject(t)
	// User exists fully layered; planting one feature-side file makes
	// the resource torn — the one provably broken state.
	writeRefactorFile(t, "app/user/service.go", "package user\n")

	pf := refactorPreflight(preflightToFeature)

	torn := findingsFor(pf.Blockers, "layout-state")
	require.Len(t, torn, 1)
	assert.Contains(t, torn[0].Message, "BOTH layouts")
	assert.Contains(t, torn[0].Message, "User")
}

// TestRefactorPreflight_AlreadyMigratedResourceWarns: a resource fully
// on the target side is the legal per-resource workflow — warn (it
// will be skipped), never block.
func TestRefactorPreflight_AlreadyMigratedResourceWarns(t *testing.T) {
	migratedProject(t) // User fully feature-side

	pf := refactorPreflight(preflightToFeature)

	assert.Empty(t, findingsFor(pf.Blockers, "layout-state"))
	warns := findingsFor(pf.Warnings, "layout-state")
	require.Len(t, warns, 1)
	assert.Contains(t, warns[0].Message, "already in feature layout")
}

func TestRefactorPreflight_GitRepoSilencesNoGit(t *testing.T) {
	inRenderedProject(t)
	gitInitOrSkip(t)

	pf := refactorPreflight(preflightToFeature)
	assert.Empty(t, findingsFor(pf.Blockers, "no-git"))
}

func TestRefactorPreflight_ParseErrors(t *testing.T) {
	inRenderedProject(t)
	// Transformed file → blocker.
	writeRefactorFile(t, "app/services/user.service.go", "package services\n\nfunc Broken( {\n")
	// Allowed-dir file → ignored entirely.
	writeRefactorFile(t, "app/jobs/broken_job.go", "package jobs\n\nfunc Broken( {\n")
	// Unmanaged file outside drained dirs → parse warning.
	writeRefactorFile(t, "app/rest/broken_extra.go", "package rest\n\nfunc Broken( {\n")

	pf := refactorPreflight(preflightToFeature)

	blockers := findingsFor(pf.Blockers, "parse-error")
	require.Len(t, blockers, 1)
	assert.Equal(t, "app/services/user.service.go", blockers[0].Path)

	warns := findingsFor(pf.Warnings, "parse-error")
	require.Len(t, warns, 1)
	assert.Equal(t, "app/rest/broken_extra.go", warns[0].Path)
}

func TestRefactorPreflight_UnmanagedFileInDrainedDirWarns(t *testing.T) {
	inRenderedProject(t)
	writeRefactorFile(t, "app/services/custom_cache.go", "package services\n\nfunc CacheWarm() {}\n")
	// Allowed dirs take anything without comment.
	writeRefactorFile(t, "app/models/extra_helpers.go", "package models\n\nfunc Helper() {}\n")

	pf := refactorPreflight(preflightToFeature)

	warns := findingsFor(pf.Warnings, "unmanaged-file")
	require.Len(t, warns, 1)
	assert.Equal(t, "app/services/custom_cache.go", warns[0].Path)
	assert.Contains(t, warns[0].Message, "stay behind")
}

func TestRefactorPreflight_UnmanagedFeatureFileWarnsOnReverse(t *testing.T) {
	migratedProject(t)
	writeRefactorFile(t, "app/user/repository_cache.go", "package user\n\nfunc CacheWarm() {}\n")

	pf := refactorPreflight(preflightToLayered)

	warns := findingsFor(pf.Warnings, "unmanaged-file")
	require.Len(t, warns, 1)
	assert.Equal(t, "app/user/repository_cache.go", warns[0].Path)
}

func TestRefactorPreflight_RenamedSymbols(t *testing.T) {
	inRenderedProject(t)

	// Fully renamed interface: zero recognized symbols in the file.
	content, err := os.ReadFile("app/services/interfaces/user_service.go")
	require.NoError(t, err)
	renamed := strings.ReplaceAll(string(content), "UserServiceInterface", "UserSvc")
	require.NoError(t, os.WriteFile("app/services/interfaces/user_service.go", []byte(renamed), 0o644))

	// Extra exported symbol alongside recognized ones.
	f, err := os.OpenFile("app/services/user.service.go", os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString("\nfunc TotallyCustomExport() {}\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	pf := refactorPreflight(preflightToFeature)

	warns := findingsFor(pf.Warnings, "renamed-symbols")
	require.Len(t, warns, 2)
	byPath := map[string]string{}
	for _, w := range warns {
		byPath[w.Path] = w.Message
	}
	assert.Contains(t, byPath["app/services/interfaces/user_service.go"], "appear renamed")
	assert.Contains(t, byPath["app/services/user.service.go"], "TotallyCustomExport")
}

func TestRefactorPreflight_GqlgenAnchors(t *testing.T) {
	inRenderedGraphQLProject(t)

	content, err := os.ReadFile("gqlgen.yml")
	require.NoError(t, err)
	sabotaged := strings.Replace(string(content),
		`- "`+fixtureModulePath+`/app/dtos"`,
		`- "`+fixtureModulePath+`/app/api/dto"`, 1)
	require.NoError(t, os.WriteFile("gqlgen.yml", []byte(sabotaged), 0o644))

	pf := refactorPreflight(preflightToFeature)

	blockers := findingsFor(pf.Blockers, "gqlgen-anchors")
	require.Len(t, blockers, 1)
	assert.Contains(t, blockers[0].Message, "autobind")
}

func TestRefactorPreflight_GqlgenModelFilenameAnchor(t *testing.T) {
	inRenderedGraphQLProject(t)

	content, err := os.ReadFile("gqlgen.yml")
	require.NoError(t, err)
	sabotaged := strings.Replace(string(content),
		"filename: app/dtos/generated-types.dtos.go",
		"filename: app/models_gen/types.go", 1)
	require.NoError(t, os.WriteFile("gqlgen.yml", []byte(sabotaged), 0o644))

	pf := refactorPreflight(preflightToFeature)

	blockers := findingsFor(pf.Blockers, "gqlgen-anchors")
	require.Len(t, blockers, 1)
	assert.Contains(t, blockers[0].Message, "model.filename")
}

func TestRefactorPreflight_GqlgenCustomizationsWarn(t *testing.T) {
	inRenderedGraphQLProject(t)

	content, err := os.ReadFile("gqlgen.yml")
	require.NoError(t, err)
	custom := strings.Replace(string(content),
		"# federation:", "federation:", 1)
	custom = strings.Replace(custom,
		"dir: app/graphql/resolvers", "dir: internal/resolvers", 1)
	require.NoError(t, os.WriteFile("gqlgen.yml", []byte(custom), 0o644))

	pf := refactorPreflight(preflightToFeature)

	warns := findingsFor(pf.Warnings, "gqlgen-custom")
	require.Len(t, warns, 2)
}

func TestRefactorPreflight_MissingGeneratorMarkerWarns(t *testing.T) {
	inRenderedProject(t)

	content, err := os.ReadFile("app/di/container.go")
	require.NoError(t, err)
	// Drop the whole marker LINE — the scaffold's marker comment
	// carries trailing text, so substring removal would leave an
	// unparseable dangling comment and hit the parse-error blocker
	// instead of the marker warning.
	var kept []string
	for line := range strings.SplitSeq(string(content), "\n") {
		if strings.Contains(line, "gofasta:scaffold:container-fields") {
			continue
		}
		kept = append(kept, line)
	}
	require.NoError(t, os.WriteFile("app/di/container.go", []byte(strings.Join(kept, "\n")), 0o644))

	pf := refactorPreflight(preflightToFeature)

	assert.Empty(t, findingsFor(pf.Blockers, "parse-error"),
		"removing a marker line must not corrupt the file")
	warns := findingsFor(pf.Warnings, "generator-markers")
	require.Len(t, warns, 1)
	assert.Equal(t, "app/di/container.go", warns[0].Path)
	assert.Contains(t, warns[0].Message, "container-fields")
}

func TestPreflightReport_DowngradeNoGit(t *testing.T) {
	var pf preflightReport
	pf.block("no-git", "", "no repo")
	pf.block("layout-state", "app/user", "torn")

	pf.downgradeNoGit()

	require.Len(t, pf.Blockers, 1)
	assert.Equal(t, "layout-state", pf.Blockers[0].Check)
	require.Len(t, findingsFor(pf.Warnings, "no-git"), 1)
	assert.Contains(t, findingsFor(pf.Warnings, "no-git")[0].Message, "--force")
	assert.False(t, pf.eligible())

	// Downgrading the only blocker makes the report eligible.
	var only preflightReport
	only.block("no-git", "", "no repo")
	only.downgradeNoGit()
	assert.True(t, only.eligible())
}
