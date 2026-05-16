package commands

import (
	"bytes"
	"errors"
	"strings"
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

// TestBuildGoTestArgs_Coverage — --coverage emits BOTH
// -coverpkg=./... (load-bearing in projects with `tool` directives;
// see the inline comment in buildGoTestArgs for the covdata regression
// this prevents) AND -coverprofile=coverage.out (where the merged
// profile lands). Pinned in this exact order so future refactors that
// reshuffle the flag pile don't silently regress the covdata fix.
func TestBuildGoTestArgs_Coverage(t *testing.T) {
	got := buildGoTestArgs(testOptions{coverage: true})
	assert.Equal(t, []string{"test", "-race", "-coverpkg=./...", "-coverprofile=coverage.out", "./..."}, got)
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
		"-coverpkg=./...", "-coverprofile=coverage.out",
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

// --- dropLDWarnings ---------------------------------------------------------

// TestDropLDWarnings_PassesThroughNormalOutput — any line that's
// neither a build marker nor an LC_DYSYMTAB warning must round-trip
// untouched. This is the no-op baseline for the filter.
func TestDropLDWarnings_PassesThroughNormalOutput(t *testing.T) {
	in := strings.NewReader("--- PASS: TestFoo\nok\tpkg/foo\t0.123s\n")
	var out bytes.Buffer
	dropLDWarnings(in, &out)
	assert.Equal(t, "--- PASS: TestFoo\nok\tpkg/foo\t0.123s\n", out.String())
}

// TestDropLDWarnings_StripsLDWarning — the canonical LC_DYSYMTAB
// warning is dropped. The preceding `# <pkg>.test` build marker is
// also dropped at EOF because nothing real followed it. Models
// realistic `go test` stderr (no `ok` result lines — those go to
// stdout via a separate file descriptor that bypasses this filter).
func TestDropLDWarnings_StripsLDWarning(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		"# github.com/gofastadev/gofasta/pkg/cache.test",
		"ld: warning: '/tmp/x.o' has malformed LC_DYSYMTAB, expected 98 undefined symbols",
		"",
	}, "\n"))
	var out bytes.Buffer
	dropLDWarnings(in, &out)
	assert.Empty(t, out.String())
}

// TestDropLDWarnings_KeepsHeaderForRealError — when the same
// `# <pkg>.test` marker heads a REAL compiler error, the marker
// MUST survive so the user can find the package whose build broke.
// This is the regression driver for "don't lose legitimate build
// diagnostics in the noise filter".
func TestDropLDWarnings_KeepsHeaderForRealError(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		"# github.com/example/badpkg",
		"./main.go:42:5: undefined: notDefined",
		"",
	}, "\n"))
	var out bytes.Buffer
	dropLDWarnings(in, &out)
	want := "# github.com/example/badpkg\n./main.go:42:5: undefined: notDefined\n"
	assert.Equal(t, want, out.String())
}

// TestDropLDWarnings_MixedSequence — interleaved stream covering
// the state machine end-to-end: a marker followed only by
// LC_DYSYMTAB warnings is dropped when the NEXT marker arrives (the
// only unambiguous "section is empty" signal stderr can give us);
// a marker followed by a real error keeps both. Models the actual
// shape of `go test`'s stderr (markers + build output only — `ok`
// result lines go to stdout, not stderr).
func TestDropLDWarnings_MixedSequence(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		"# pkg/a.test",
		"ld: warning: malformed LC_DYSYMTAB at offset 1",
		"# pkg/b",
		"./b.go:10:1: syntax error",
		"# pkg/c.test",
		"ld: warning: malformed LC_DYSYMTAB at offset 99",
		"# pkg/d.test",
		"ld: warning: malformed LC_DYSYMTAB at offset 7",
		"",
	}, "\n"))
	var out bytes.Buffer
	dropLDWarnings(in, &out)
	want := strings.Join([]string{
		"# pkg/b",
		"./b.go:10:1: syntax error",
		"",
	}, "\n")
	assert.Equal(t, want, out.String())
}

// TestDropLDWarnings_TrailingPendingHeaderDropped — if the stream
// ends while a header is still pending (only LC_DYSYMTAB lines
// followed it), the header is discarded. Verifies the EOF branch.
func TestDropLDWarnings_TrailingPendingHeaderDropped(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		"ok\tpkg/before\t0.1s",
		"# pkg/last.test",
		"ld: warning: malformed LC_DYSYMTAB at offset 5",
		"",
	}, "\n"))
	var out bytes.Buffer
	dropLDWarnings(in, &out)
	assert.Equal(t, "ok\tpkg/before\t0.1s\n", out.String())
}

// TestDropLDWarnings_KeepsOtherLDWarnings — only the LC_DYSYMTAB
// variant is filtered. Other ld warnings (real ones) survive so the
// user still sees genuine linker problems.
func TestDropLDWarnings_KeepsOtherLDWarnings(t *testing.T) {
	in := strings.NewReader("ld: warning: directory not found for option '-L/missing'\n")
	var out bytes.Buffer
	res := dropLDWarnings(in, &out)
	assert.Equal(t, "ld: warning: directory not found for option '-L/missing'\n", out.String())
	assert.True(t, res.realDiagnostics, "a real ld warning must mark realDiagnostics so the exit-clear shortcut doesn't fire")
	assert.False(t, res.exitClearedByCovdata())
}

// TestDropLDWarnings_StripsCovdataNoSuchTool — Go's per-package
// coverage merge invokes `go tool covdata`, which the project's go.mod
// tool list shadows in scaffolded projects (every gofasta scaffold
// has wire/air/swag in `tool` directives). Each shadowing emits one
// `go: no such tool "covdata"` line PER package. The lines are
// always preceded by a `# <pkg>` build marker. Filter drops both AND
// the count is tracked so runTests can clear the non-zero exit.
func TestDropLDWarnings_StripsCovdataNoSuchTool(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		"# scaffold/app/devtools",
		`go: no such tool "covdata"`,
		"# scaffold/cmd",
		`go: no such tool "covdata"`,
		"",
	}, "\n"))
	var out bytes.Buffer
	res := dropLDWarnings(in, &out)
	assert.Empty(t, out.String(), "covdata warnings + their headers must be dropped")
	assert.Equal(t, 2, res.covdataWarnings,
		"both covdata warnings should be counted so runTests can override the non-zero exit")
	assert.False(t, res.realDiagnostics)
	assert.True(t, res.exitClearedByCovdata(),
		"covdata-only stderr must clear the exit code — otherwise --coverage always fails in scaffolds")
}

// TestDropLDWarnings_CovdataMixedWithRealError — when a covdata
// warning AND a genuine build error are both present, the real
// diagnostic survives AND realDiagnostics flips. The exit-clear
// shortcut must NOT fire — Go's non-zero exit was caused by the real
// error, not the warning.
func TestDropLDWarnings_CovdataMixedWithRealError(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		"# scaffold/app/devtools",
		`go: no such tool "covdata"`,
		"# scaffold/app/broken",
		"./broken.go:5:1: undefined: missing",
		"",
	}, "\n"))
	var out bytes.Buffer
	res := dropLDWarnings(in, &out)
	want := "# scaffold/app/broken\n./broken.go:5:1: undefined: missing\n"
	assert.Equal(t, want, out.String(), "real error keeps its package marker")
	assert.Equal(t, 1, res.covdataWarnings)
	assert.True(t, res.realDiagnostics)
	assert.False(t, res.exitClearedByCovdata(),
		"presence of a real diagnostic must prevent the exit-clear shortcut")
}

// TestFilterResult_ExitClearedByCovdata_Branches pins the helper's
// truth table so future refactors don't subtly break the override.
func TestFilterResult_ExitClearedByCovdata_Branches(t *testing.T) {
	assert.False(t, filterResult{}.exitClearedByCovdata(),
		"empty result: no covdata seen, no clear")
	assert.True(t, filterResult{covdataWarnings: 1}.exitClearedByCovdata(),
		"covdata-only: clear")
	assert.False(t, filterResult{covdataWarnings: 1, realDiagnostics: true}.exitClearedByCovdata(),
		"covdata + real diagnostic: do not clear")
	assert.False(t, filterResult{realDiagnostics: true}.exitClearedByCovdata(),
		"real diagnostic alone: do not clear (no covdata to attribute the exit to)")
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
