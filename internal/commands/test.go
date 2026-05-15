package commands

import (
	"errors"
	"fmt"
	"os"
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
		termcolor.PrintStep("go %s", strings.Join(args, " "))
	}

	c := execCommand("go", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	if err := c.Run(); err != nil {
		return clierr.Newf(clierr.CodeGoTestFailed, "tests failed: %v", err)
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
		args = append(args, "-coverprofile=coverage.out")
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
			fmt.Println("  " + termcolor.Success("%s", line))
		}
		return
	}
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
