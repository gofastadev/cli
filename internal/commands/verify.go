package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Run the full preflight gauntlet (fmt, vet, lint, test, build, wire, routes)",
	Long: `Run every quality gate that CI runs, in order, and fail fast on the
first failure. Acts as the single "am I done?" check for both humans and
AI agents — one command, structured JSON output, non-zero exit on any
check failure.

Steps, in order:
  1. gofmt            — formatting
  2. go vet           — compiler static checks
  3. golangci-lint    — aggregate linter (skipped if not installed)
  4. go test -race    — tests with the race detector
  5. go build         — every package compiles
  6. wire drift       — app/di/wire_gen.go is in sync with its inputs
  7. routes           — app/rest/routes/ parses and has at least one entry

Flags:
  --no-lint    Skip golangci-lint (useful on a machine without it)
  --no-race    Skip the race detector in ` + "`go test`" + `
  --keep-going Continue after the first failure and report every result

Use ` + "`--json`" + ` (inherited from the root command) to emit one JSON object
per check, suitable for agent consumption.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := verifyOptions{
			skipLint:  verifyNoLint,
			skipRace:  verifyNoRace,
			keepGoing: verifyKeepGoing,
		}
		return runVerify(opts)
	},
}

var (
	verifyNoLint    bool
	verifyNoRace    bool
	verifyKeepGoing bool
)

func init() {
	verifyCmd.Flags().BoolVar(&verifyNoLint, "no-lint", false,
		"Skip golangci-lint (use if not installed or to speed up)")
	verifyCmd.Flags().BoolVar(&verifyNoRace, "no-race", false,
		"Skip the race detector in go test")
	verifyCmd.Flags().BoolVar(&verifyKeepGoing, "keep-going", false,
		"Continue after the first failure and report every result")
	rootCmd.AddCommand(verifyCmd)
}

// verifyOptions is the typed flag bundle so tests can invoke runVerify
// directly without going through Cobra.
type verifyOptions struct {
	skipLint  bool
	skipRace  bool
	keepGoing bool
}

// verifyCheck is one step's result. The JSON tags are the stable API.
type verifyCheck struct {
	Name     string `json:"name"`
	Status   string `json:"status"` // "pass" | "fail" | "skip"
	Message  string `json:"message,omitempty"`
	Output   string `json:"output,omitempty"`
	Duration int64  `json:"duration_ms"`
}

// verifyResult aggregates every check into a single structured payload.
type verifyResult struct {
	Checks   []verifyCheck `json:"checks"`
	Passed   int           `json:"passed"`
	Failed   int           `json:"failed"`
	Skipped  int           `json:"skipped"`
	Duration int64         `json:"duration_ms"`
}

// runVerify executes every verification step and emits the result. If any
// step failed (unless keep-going was passed), returns a CodeVerifyFailed
// error so the root command's error handler exits non-zero.
func runVerify(opts verifyOptions) error {
	start := time.Now()

	// Each step is {Name, Runner}. Runners return a verifyCheck with
	// status/message/output already filled in — runVerify only times
	// the run and aggregates results.
	type stepDef struct {
		name string
		fn   func() (string, string, error) // message, output, err
	}
	steps := []stepDef{
		{"gofmt", stepGofmt},
		{"go vet", stepGoVet},
	}
	if !opts.skipLint {
		steps = append(steps, stepDef{"golangci-lint", stepGolangciLint})
	}
	steps = append(steps,
		stepDef{"go test", func() (string, string, error) { return stepGoTest(opts.skipRace) }},
		stepDef{"go build", stepGoBuild},
		stepDef{"wire drift", stepWireDrift},
		stepDef{"routes", stepRoutes},
	)

	result := verifyResult{Checks: make([]verifyCheck, 0, len(steps))}

	for _, step := range steps {
		t := time.Now()
		message, output, err := step.fn()
		check := verifyCheck{
			Name:     step.name,
			Message:  message,
			Output:   output,
			Duration: time.Since(t).Milliseconds(),
		}
		switch {
		case err == nil && message == "skip":
			check.Status = "skip"
			result.Skipped++
		case err == nil:
			check.Status = "pass"
			result.Passed++
		default:
			check.Status = "fail"
			if check.Message == "" {
				check.Message = err.Error()
			}
			result.Failed++
		}
		result.Checks = append(result.Checks, check)

		// In text mode, print each step as it completes — agents parsing
		// JSON see the aggregate payload at the end, but humans want a
		// live progress indicator.
		if !cliout.JSON() {
			printVerifyStep(check)
		}

		if check.Status == "fail" && !opts.keepGoing {
			break
		}
	}

	result.Duration = time.Since(start).Milliseconds()

	// JSON mode: emit the aggregated result. Text mode: summary footer.
	cliout.Print(result, func(w io.Writer) {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintf(w, "%d passed · %d failed · %d skipped · %dms\n",
			result.Passed, result.Failed, result.Skipped, result.Duration)
	})

	if result.Failed > 0 {
		return clierr.Newf(clierr.CodeVerifyFailed,
			"%d verify check(s) failed", result.Failed)
	}
	return nil
}

func printVerifyStep(c verifyCheck) {
	var mark string
	switch c.Status {
	case "pass":
		mark = termcolor.CGreen("✓")
	case "fail":
		mark = termcolor.CRed("✗")
	case "skip":
		mark = termcolor.CDim("-")
	}
	suffix := ""
	if c.Message != "" && c.Status != "pass" {
		suffix = ": " + c.Message
	}
	fmt.Printf("  %s %-16s (%dms)%s\n", mark, c.Name, c.Duration, suffix)
	if c.Status == "fail" && c.Output != "" {
		for line := range strings.SplitSeq(strings.TrimRight(c.Output, "\n"), "\n") {
			fmt.Printf("    %s\n", line)
		}
	}
}

// --- individual step runners -------------------------------------------------

// runShell runs name args... and returns (stdout+stderr, err). The combined
// output is captured as a single stream because most Go tools write errors
// to stderr but warnings to stdout — splitting makes the output harder to
// read without adding information.
func runShell(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// stepGofmt runs `gofmt -s -l .` and fails if any file would be reformatted.
// gofmt prints the list of non-conforming files to stdout with exit 0, so
// we check the output rather than the exit code.
func stepGofmt() (message, output string, err error) {
	out, runErr := runShell("gofmt", "-s", "-l", ".")
	if runErr != nil {
		return "", out, runErr
	}
	out = strings.TrimSpace(out)
	if out != "" {
		return "files need reformatting", out, fmt.Errorf("gofmt: %s", out)
	}
	return "", "", nil
}

func stepGoVet() (message, output string, err error) {
	out, err := runShell("go", "vet", "./...")
	if err != nil {
		return "vet reported issues", out, err
	}
	return "", "", nil
}

// stepGolangciLint tries to run golangci-lint. If the binary is not on
// $PATH it returns ("skip", "", nil) which the aggregator treats as
// skipped, not failed — agents that lack the linter should not be
// blocked by its absence.
func stepGolangciLint() (message, output string, err error) {
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		return "skip", "", nil
	}
	out, err := runShell("golangci-lint", "run")
	if err != nil {
		return "lint reported issues", out, err
	}
	return "", "", nil
}

func stepGoTest(skipRace bool) (message, output string, err error) {
	args := []string{"test"}
	if !skipRace {
		args = append(args, "-race")
	}
	args = append(args, "./...")
	out, err := runShell("go", args...)
	if err != nil {
		return "tests failed", out, err
	}
	return "", "", nil
}

func stepGoBuild() (message, output string, err error) {
	out, err := runShell("go", "build", "./...")
	if err != nil {
		return "build failed", out, err
	}
	return "", "", nil
}

// stepWireDrift checks whether app/di/wire_gen.go is out of date relative
// to its input files. "Out of date" means one of the input files has a
// newer modification time than wire_gen.go itself. Skipped when the
// project does not use Wire (no wire_gen.go present).
func stepWireDrift() (message, output string, err error) {
	wireGen := filepath.Join("app", "di", "wire_gen.go")
	info, err := os.Stat(wireGen)
	if err != nil {
		// No wire_gen.go — not a wire-using project. Skip.
		return "skip", "", nil
	}
	wireGenModTime := info.ModTime()

	// Scan every Go file under app/di/ (excluding wire_gen.go itself).
	diDir := filepath.Join("app", "di")
	var stalest string
	var stalestTime time.Time
	walkErr := filepath.WalkDir(diDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if filepath.Base(path) == "wire_gen.go" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(wireGenModTime) && info.ModTime().After(stalestTime) {
			stalest = path
			stalestTime = info.ModTime()
		}
		return nil
	})
	if walkErr != nil {
		return "could not inspect app/di/", "", walkErr
	}
	if stalest != "" {
		return fmt.Sprintf("wire_gen.go is older than %s — run `gofasta wire`", stalest),
			"", fmt.Errorf("wire drift")
	}
	return "", "", nil
}

// stepRoutes verifies that app/rest/routes/ exists and at least one route
// file parses to at least one route entry. Catches layout corruption and
// import regressions that slip through the compiler.
func stepRoutes() (message, output string, err error) {
	if _, err := os.Stat("app/rest/routes"); err != nil {
		// Projects without a REST layer are valid (e.g., pure GraphQL).
		// Skip rather than fail.
		return "skip", "", nil
	}
	if err := runRoutes(); err != nil {
		return "routes command failed", "", err
	}
	return "", "", nil
}
