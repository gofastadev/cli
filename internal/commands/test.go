package commands

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var (
	testShort       bool
	testIntegration bool
	testCoverage    bool
	testRunPattern  string
	testNoRace      bool
	testVerbose     bool
)

var testCmd = &cobra.Command{
	Use:   "test [path...]",
	Short: "Run go tests with sensible defaults (race detector on, all packages)",
	Long: `Run ` + "`go test`" + ` against the project with the gofasta defaults: race
detector on, every package under ` + "`./...`" + `, output streamed straight to
the terminal so you see live progress. Equivalent to typing
` + "`go test -race ./...`" + ` but shorter and consistent with the rest of the
gofasta surface.

Specify one or more package paths as positional arguments to scope the run:

  gofasta test                       # all packages, race detector on
  gofasta test ./app/services        # one package
  gofasta test ./app/...             # everything under app/

Flags compose so you can express common workflows succinctly:

  gofasta test --short               # skip long-running tests (-short)
  gofasta test --integration         # only Integration-named tests
  gofasta test --coverage            # write coverage.out + print total %
  gofasta test --run TestUserCreate  # filter by test name (regex)
  gofasta test --no-race             # skip the race detector (faster)
  gofasta test --verbose             # verbose output (-v)

Forward extra ` + "`go test`" + ` flags after a literal ` + "`--`" + `:

  gofasta test ./app/... -- -count=1 -tags=integration

Use the global ` + "`--json`" + ` flag for machine-readable output:

  gofasta test --json                # forwards -json to go test → NDJSON events

In JSON mode the banner, the "▶ go test ..." progress line, and the
coverage summary are suppressed so stdout stays a strict newline-
delimited JSON stream that downstream tools (gotestsum, GitHub Actions
test annotators, etc.) can parse directly.

Loads .env so child processes (testcontainers, fixture configs reading
project-prefixed env vars) inherit the same environment ` + "`gofasta dev`" + ` and
` + "`gofasta serve`" + ` use.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, _ = loadDotEnv(".env")

		// Cobra puts everything (positional + post-`--`) into args.
		// ArgsLenAtDash returns the index where `--` appeared so we
		// can split paths from raw forwarded flags.
		dashAt := cmd.ArgsLenAtDash()
		var paths, extra []string
		switch dashAt {
		case -1:
			paths = args
		default:
			paths = args[:dashAt]
			extra = args[dashAt:]
		}

		return runTests(testOptions{
			paths:       paths,
			short:       testShort,
			integration: testIntegration,
			coverage:    testCoverage,
			runPattern:  testRunPattern,
			noRace:      testNoRace,
			verbose:     testVerbose,
			jsonMode:    cliout.JSON(),
			extraArgs:   extra,
		})
	},
}

// testOptions is the typed flag bundle so tests can invoke runTests
// without a Cobra round-trip.
type testOptions struct {
	paths       []string
	short       bool
	integration bool
	coverage    bool
	runPattern  string
	noRace      bool
	verbose     bool
	// jsonMode is mirrored from cliout.JSON() at the cobra layer. When
	// true, `-json` is forwarded to `go test` so it emits newline-
	// delimited JSON events (one per test action) — the format
	// downstream tools (gotestsum, GitHub Actions test annotators, etc.)
	// expect. Verbose-mode and the coverage summary line are suppressed
	// in this mode so the stdout stream stays strictly NDJSON.
	jsonMode  bool
	extraArgs []string
}

// runTests builds the `go test` command line, streams the output to
// the user's terminal, and surfaces failures as CodeGoTestFailed so
// the root error handler exits non-zero.
func runTests(opts testOptions) error {
	if opts.integration && opts.runPattern != "" {
		return errors.New("--integration and --run are mutually exclusive — pick one")
	}

	args := buildGoTestArgs(opts)

	if !opts.jsonMode {
		cliout.Step("go %s", strings.Join(args, " "))
	}

	c := execCommand("go", args...)
	c.Stdout = os.Stdout
	c.Stdin = os.Stdin

	// In text mode, route the child's stderr through dropLDWarnings so
	// macOS ld's harmless LC_DYSYMTAB noise (golang/go#61229 — Apple's
	// new linker emits a warning for every cgo object Go produces with
	// -race) doesn't bury real diagnostics. JSON mode bypasses the
	// filter because `go test -json` consumers expect raw stderr if
	// they read it at all.
	if opts.jsonMode {
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return clierr.Newf(clierr.CodeGoTestFailed, "tests failed: %v", err)
		}
	} else {
		pr, pw := io.Pipe()
		c.Stderr = pw
		filterDone := make(chan struct{})
		var filterRes filterResult
		go func() {
			defer close(filterDone)
			filterRes = dropLDWarnings(pr, os.Stderr)
		}()
		runErr := c.Run()
		// Close the writer so the filter sees EOF and finishes draining
		// any held-back header line; then wait for it before returning.
		_ = pw.Close()
		<-filterDone
		if runErr != nil {
			// Go's per-package coverage merge invokes `go tool covdata`,
			// which the project's go.mod tool list shadows in scaffolded
			// projects (wire/air/swag/gqlgen — every gofasta scaffold).
			// Each shadowing emits `go: no such tool "covdata"` and
			// makes go test exit 1 even when every test binary ran ok.
			// If the filter saw only covdata warnings (no real
			// diagnostics), trust the test result and clear the exit.
			if !filterRes.exitClearedByCovdata() {
				return clierr.Newf(clierr.CodeGoTestFailed, "tests failed: %v", runErr)
			}
		}
	}

	// In JSON mode the coverage summary line would corrupt the NDJSON
	// stream; agents that want a coverage value can parse the profile
	// themselves or run `go tool cover -func=coverage.out` after.
	if opts.coverage && !opts.jsonMode {
		printCoverageTotal()
	}
	return nil
}

// buildGoTestArgs assembles the argument list passed to `go test`.
// Extracted so tests can assert the exact flags emitted for each
// option combination without spawning a subprocess.
//
// Order matters for readability: race/short/verbose come first as the
// "behavior" flags, then -coverprofile, then -run, then user-supplied
// extras (which may include their own -count, -tags, etc.), then the
// package paths last so they're easy to spot at the end of the line.
func buildGoTestArgs(opts testOptions) []string {
	args := []string{"test"}
	if !opts.noRace {
		args = append(args, "-race")
	}
	if opts.short {
		args = append(args, "-short")
	}
	// `-json` already streams every test action; layering `-v` on top
	// just duplicates verbose lines inside the Output field. Skip -v
	// in JSON mode and let the consumer's NDJSON parser do the work.
	if opts.verbose && !opts.jsonMode {
		args = append(args, "-v")
	}
	if opts.jsonMode {
		args = append(args, "-json")
	}
	if opts.coverage {
		// -coverpkg=./... is load-bearing alongside -coverprofile in
		// projects whose go.mod has `tool` directives (Wire / gqlgen /
		// air / swag — every scaffolded gofasta project does). Without
		// -coverpkg, `go test` instruments per-package and tries to
		// invoke `go tool covdata` to merge profiles for packages with
		// no test files; the lookup hits the project's tool list,
		// doesn't find covdata, and prints "go: no such tool covdata"
		// per package + non-zero exit. -coverpkg consolidates coverage
		// at the meta level so the merge happens in-process and the
		// tool lookup is never triggered.
		args = append(args, "-coverpkg=./...", "-coverprofile=coverage.out")
	}
	switch {
	case opts.integration:
		args = append(args, "-run", "Integration")
	case opts.runPattern != "":
		args = append(args, "-run", opts.runPattern)
	}
	args = append(args, opts.extraArgs...)

	paths := opts.paths
	if len(paths) == 0 {
		paths = []string{"./..."}
	}
	args = append(args, paths...)
	return args
}

// printCoverageTotal extracts the `total:` line from
// `go tool cover -func=coverage.out` and prints it as a one-line
// summary. Failure to read the profile is non-fatal — the test run
// itself succeeded; we just don't print a percentage.
func printCoverageTotal() {
	out, err := runShellFn("go", "tool", "cover", "-func=coverage.out")
	if err != nil {
		return
	}
	for line := range strings.SplitSeq(out, "\n") {
		if !strings.HasPrefix(line, "total:") {
			continue
		}
		if !cliout.JSON() {
			cliout.Plainln("  " + termcolor.Success("%s", line))
		}
		return
	}
}

// ldDYSYMTABWarning matches Apple ld's LC_DYSYMTAB warning. The exact
// text format is stable enough to anchor on: every variant carries
// "ld: warning:" and "LC_DYSYMTAB" with the same wording. See
// golang/go#61229 for the upstream tracking issue.
var ldDYSYMTABWarning = regexp.MustCompile(`^ld: warning:.*LC_DYSYMTAB`)

// covdataNoSuchTool matches the `go: no such tool "covdata"` line Go's
// per-package coverage merge emits in projects whose go.mod has any
// `tool` directive (wire / air / swag / gqlgen all qualify — every
// gofasta scaffold does). Go resolves the covdata sub-tool by walking
// the project's tool list first, which doesn't contain stdlib's
// covdata, and reports the bogus "no such tool" error per package
// without test files. The test binaries succeed; only the merge
// invocation fails. The lines are noise, and the non-zero exit they
// produce is misleading — see filterResult.exitClearedByCovdata.
var covdataNoSuchTool = regexp.MustCompile(`^go: no such tool "covdata"$`)

// goBuildMarker matches the `# <package>[.test]` line `go test` /
// `go build` emit just before any stderr output produced while
// building that package. Used to identify lines we may need to drop
// alongside the warnings they head.
var goBuildMarker = regexp.MustCompile(`^# \S+`)

// filterResult is what dropLDWarnings reports back. exitClearedByCovdata
// signals that the filter dropped ONLY covdata-tool warnings and no
// other content — runTests uses this to override a non-zero `go test`
// exit code when the only "failure" was Go's per-package merge tripping
// over the tool directive. realDiagnostics flips true the moment a
// non-filtered line arrives, so any genuine build error or test failure
// keeps the original exit code intact.
type filterResult struct {
	covdataWarnings int
	realDiagnostics bool
}

// exitClearedByCovdata reports whether a non-zero `go test` exit code
// can be safely treated as success. True when at least one covdata
// warning was dropped AND no real diagnostics survived the filter.
func (f filterResult) exitClearedByCovdata() bool {
	return f.covdataWarnings > 0 && !f.realDiagnostics
}

// dropLDWarnings copies r to w line-by-line, dropping each
// LC_DYSYMTAB / covdata-tool warning and any preceding `# <pkg>` build
// marker that turns out to head only such warnings. Real build errors
// keep their marker because the marker is flushed as soon as a
// non-filtered line arrives. Returns a filterResult so the caller can
// decide whether a non-zero exit was caused only by the dropped
// warnings.
func dropLDWarnings(r io.Reader, w io.Writer) filterResult {
	sc := bufio.NewScanner(r)
	// go test build errors can be long (full compiler diagnostics);
	// bump beyond the default 64KiB so we don't truncate them.
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	var pendingHeader string
	res := filterResult{}
	flushPending := func() {
		if pendingHeader != "" {
			_, _ = fmt.Fprintln(w, pendingHeader)
			pendingHeader = ""
		}
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case goBuildMarker.MatchString(line):
			// A new build marker arrived while another was still
			// pending — that earlier marker had only warnings, so
			// drop it and queue the new one.
			pendingHeader = line
		case ldDYSYMTABWarning.MatchString(line):
			// Drop. Header stays pending in case more arrive.
		case covdataNoSuchTool.MatchString(line):
			// Drop AND track — runTests uses the count to decide
			// whether to override a non-zero exit code.
			res.covdataWarnings++
		default:
			flushPending()
			res.realDiagnostics = true
			_, _ = fmt.Fprintln(w, line)
		}
	}
	// EOF: any still-pending header headed only warnings — drop it
	// by not flushing.
	return res
}

func init() {
	testCmd.Flags().BoolVarP(&testShort, "short", "s", false,
		"Skip long-running tests (passes -short to go test)")
	testCmd.Flags().BoolVarP(&testIntegration, "integration", "i", false,
		"Run only Integration-named tests (-run Integration)")
	testCmd.Flags().BoolVarP(&testCoverage, "coverage", "c", false,
		"Write coverage profile to coverage.out and print total %")
	testCmd.Flags().StringVarP(&testRunPattern, "run", "r", "",
		"Run only tests matching the regex pattern (-run <pattern>)")
	testCmd.Flags().BoolVar(&testNoRace, "no-race", false,
		"Skip the race detector (faster, less safe)")
	testCmd.Flags().BoolVarP(&testVerbose, "verbose", "v", false,
		"Verbose output (passes -v to go test)")
	rootCmd.AddCommand(testCmd)
}
