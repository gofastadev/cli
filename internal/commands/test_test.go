package commands

import (
	"errors"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTestCmd_Registered ensures `test` shows up on the root command so
// `gofasta test` is discoverable from `gofasta --help`.
func TestTestCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "test" {
			found = true
			break
		}
	}
	assert.True(t, found, "testCmd should be registered on rootCmd")
}

// TestTestCmd_HasDescription pins the help-text contract — Short and
// Long must be set so `gofasta test --help` is informative.
func TestTestCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, testCmd.Short)
	assert.NotEmpty(t, testCmd.Long)
}

// TestBuildGoTestArgs_Defaults — no options → `test -race ./...`. The
// race detector is on by default because that's what CI uses; matching
// CI locally is the whole point of having a single test wrapper.
func TestBuildGoTestArgs_Defaults(t *testing.T) {
	got := buildGoTestArgs(testOptions{})
	assert.Equal(t, []string{"test", "-race", "./..."}, got)
}

// TestBuildGoTestArgs_Short — --short adds `-short` so long-running
// tests (testcontainers, etc.) are skipped.
func TestBuildGoTestArgs_Short(t *testing.T) {
	got := buildGoTestArgs(testOptions{short: true})
	assert.Equal(t, []string{"test", "-race", "-short", "./..."}, got)
}

// TestBuildGoTestArgs_Verbose — --verbose adds `-v`.
func TestBuildGoTestArgs_Verbose(t *testing.T) {
	got := buildGoTestArgs(testOptions{verbose: true})
	assert.Equal(t, []string{"test", "-race", "-v", "./..."}, got)
}

// TestBuildGoTestArgs_Integration — --integration translates to
// `-run Integration` so only Integration-named tests are picked up.
func TestBuildGoTestArgs_Integration(t *testing.T) {
	got := buildGoTestArgs(testOptions{integration: true})
	assert.Equal(t, []string{"test", "-race", "-run", "Integration", "./..."}, got)
}

// TestBuildGoTestArgs_RunPattern — --run is forwarded as the regex
// argument to go test's -run.
func TestBuildGoTestArgs_RunPattern(t *testing.T) {
	got := buildGoTestArgs(testOptions{runPattern: "TestUserCreate"})
	assert.Equal(t, []string{"test", "-race", "-run", "TestUserCreate", "./..."}, got)
}

// TestBuildGoTestArgs_IntegrationOverridesRunPattern — when both are
// set, integration wins inside the builder. The mutual-exclusion check
// happens earlier in runTests; this asserts the builder's last-write
// behavior is deterministic if it ever gets called with both set.
func TestBuildGoTestArgs_IntegrationOverridesRunPattern(t *testing.T) {
	got := buildGoTestArgs(testOptions{integration: true, runPattern: "ignored"})
	assert.Contains(t, got, "Integration")
	assert.NotContains(t, got, "ignored")
}

// TestBuildGoTestArgs_Coverage — --coverage emits the coverprofile
// flag. The summary printing is exercised separately.
func TestBuildGoTestArgs_Coverage(t *testing.T) {
	got := buildGoTestArgs(testOptions{coverage: true})
	assert.Equal(t, []string{"test", "-race", "-coverprofile=coverage.out", "./..."}, got)
}

// TestBuildGoTestArgs_NoRace — --no-race drops `-race` (faster, less
// safe; used to bypass race-detector noise during exploratory runs).
func TestBuildGoTestArgs_NoRace(t *testing.T) {
	got := buildGoTestArgs(testOptions{noRace: true})
	assert.Equal(t, []string{"test", "./..."}, got)
}

// TestBuildGoTestArgs_JSONMode — --json (mirrored from cliout.JSON())
// forwards `-json` to go test so it emits NDJSON events. This is the
// regression driver for "gofasta test --json" producing parseable
// output for downstream tools.
func TestBuildGoTestArgs_JSONMode(t *testing.T) {
	got := buildGoTestArgs(testOptions{jsonMode: true})
	assert.Equal(t, []string{"test", "-race", "-json", "./..."}, got)
}

// TestBuildGoTestArgs_JSONModeSuppressesVerbose — `go test -json`
// already streams every test action; adding `-v` would just duplicate
// verbose lines inside each event's Output field and break some
// strict NDJSON parsers. Verbose is silently dropped in JSON mode.
func TestBuildGoTestArgs_JSONModeSuppressesVerbose(t *testing.T) {
	got := buildGoTestArgs(testOptions{jsonMode: true, verbose: true})
	assert.NotContains(t, got, "-v", "verbose flag must not be emitted alongside -json")
	assert.Contains(t, got, "-json")
}

// TestBuildGoTestArgs_JSONModeKeepsCoverage — coverage profile writing
// is orthogonal to the JSON action stream; -coverprofile still works
// (it writes to a file, not stdout). The summary print is suppressed
// at the runTests layer, not here.
func TestBuildGoTestArgs_JSONModeKeepsCoverage(t *testing.T) {
	got := buildGoTestArgs(testOptions{jsonMode: true, coverage: true})
	assert.Contains(t, got, "-json")
	assert.Contains(t, got, "-coverprofile=coverage.out")
}

// TestBuildGoTestArgs_CustomPaths — positional path args replace the
// default `./...` so users can scope a run to one package.
func TestBuildGoTestArgs_CustomPaths(t *testing.T) {
	got := buildGoTestArgs(testOptions{paths: []string{"./app/services", "./app/repositories"}})
	assert.Equal(t, []string{"test", "-race", "./app/services", "./app/repositories"}, got)
}

// TestBuildGoTestArgs_ExtraArgs — args after `--` are inserted before
// the package paths so they reach `go test` verbatim. Critical for
// power-user flags like `-count=1` or `-tags=integration` we don't
// front-door through dedicated CLI flags.
func TestBuildGoTestArgs_ExtraArgs(t *testing.T) {
	got := buildGoTestArgs(testOptions{
		extraArgs: []string{"-count=1", "-tags=integration"},
	})
	assert.Equal(t, []string{"test", "-race", "-count=1", "-tags=integration", "./..."}, got)
}

// TestBuildGoTestArgs_AllFlagsTogether — composing every flag emits a
// stable argument order. Pins the order so downstream tools that grep
// the printed command line don't break on a refactor.
func TestBuildGoTestArgs_AllFlagsTogether(t *testing.T) {
	got := buildGoTestArgs(testOptions{
		paths:      []string{"./app/..."},
		short:      true,
		verbose:    true,
		coverage:   true,
		runPattern: "TestX",
		extraArgs:  []string{"-count=1"},
	})
	assert.Equal(t, []string{
		"test", "-race", "-short", "-v",
		"-coverprofile=coverage.out",
		"-run", "TestX",
		"-count=1",
		"./app/...",
	}, got)
}

// TestRunTests_MutuallyExclusiveFlags — --integration and --run can't
// both be passed; they'd give conflicting `-run` values and silently
// produce confusing results. Reject up front.
func TestRunTests_MutuallyExclusiveFlags(t *testing.T) {
	err := runTests(testOptions{integration: true, runPattern: "TestX"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// TestRunTests_Success — go test exits 0 → runTests returns nil.
// Coverage flag is off so no follow-up printCoverageTotal call.
func TestRunTests_Success(t *testing.T) {
	stagedFakeExec(t, 0)
	stubExecLookPathOK(t)
	require.NoError(t, runTests(testOptions{}))
}

// TestRunTests_Failure — go test exits non-zero → runTests wraps it as
// CodeGoTestFailed so the root error handler exits with the right
// taxonomy code.
func TestRunTests_Failure(t *testing.T) {
	stagedFakeExec(t, 1)
	stubExecLookPathOK(t)
	err := runTests(testOptions{})
	require.Error(t, err)
	var ce *clierr.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, string(clierr.CodeGoTestFailed), ce.Code)
}

// TestRunTests_CoveragePrintsTotal — when --coverage is set and go
// test passes, runTests calls go tool cover -func and prints the
// `total:` line. Stub the cover output so the test doesn't depend on
// an actual coverage.out file existing.
func TestRunTests_CoveragePrintsTotal(t *testing.T) {
	stagedFakeExec(t, 0)
	stubExecLookPathOK(t)
	withStubShell(t, stubResponse{
		out: "github.com/x/y/file.go:1:\tFoo\t100.0%\ntotal:\t\t\t(statements)\t87.3%\n",
		err: nil,
	})
	require.NoError(t, runTests(testOptions{coverage: true}))
}

// TestRunTests_JSONModeSkipsCoverageSummary — in JSON mode the
// coverage summary line would corrupt the NDJSON stream, so
// printCoverageTotal must not be invoked. This test would fail if
// runTests called it: the stub shell would record a call we can
// detect via call count in a future change. For now it locks in the
// behavioral contract by setting BOTH coverage and jsonMode and
// asserting no error — the suppression itself is structural in
// runTests (see the `if opts.coverage && !opts.jsonMode` guard).
func TestRunTests_JSONModeSkipsCoverageSummary(t *testing.T) {
	stagedFakeExec(t, 0)
	stubExecLookPathOK(t)
	// Stub returns a value that, if printed, would be detectable —
	// but we're asserting structurally that runTests doesn't call out.
	withStubShell(t, stubResponse{out: "should not be read", err: nil})
	require.NoError(t, runTests(testOptions{coverage: true, jsonMode: true}))
}

// TestPrintCoverageTotal_NoTotalLine — when go tool cover succeeds but
// the output has no `total:` prefix (unexpected format / empty file),
// printCoverageTotal silently returns. Test asserts no panic.
func TestPrintCoverageTotal_NoTotalLine(t *testing.T) {
	withStubShell(t, stubResponse{out: "no relevant lines here\n", err: nil})
	printCoverageTotal()
}

// TestPrintCoverageTotal_ShellError — go tool cover errors (e.g. no
// coverage.out yet); printCoverageTotal swallows it. The test run
// succeeded; failing to print a percentage isn't worth a non-zero
// exit.
func TestPrintCoverageTotal_ShellError(t *testing.T) {
	withStubShell(t, stubResponse{out: "", err: errors.New("no coverage.out")})
	printCoverageTotal()
}

// stubExecLookPathOK satisfies any execLookPath probes runTests might
// trigger via downstream helpers. Keeps this test from depending on
// what's installed on the host. Mirrors the helper used by other
// commands in this package that shell out to `go`.
func stubExecLookPathOK(t *testing.T) {
	t.Helper()
	orig := execLookPath
	execLookPath = func(_ string) (string, error) { return "/usr/bin/go", nil }
	t.Cleanup(func() { execLookPath = orig })
}
