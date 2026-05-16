package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/commands/gitdiff"
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
  --no-lint     Skip golangci-lint (useful on a machine without it)
  --no-race     Skip the race detector in ` + "`go test`" + `
  --keep-going  Continue after the first failure and report every result
  --since=<ref> Scope fmt/vet/lint/test/build to files changed since <ref>
                (Wire drift and routes are whole-project invariants, always full)
  --changed     Shortcut: scope to working-tree + staged + untracked changes
                (no committed comparison)

Use ` + "`--json`" + ` (inherited from the root command) to emit one JSON object
per check, suitable for agent consumption.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := verifyOptions{
			skipLint:  verifyNoLint,
			skipRace:  verifyNoRace,
			keepGoing: verifyKeepGoing,
			since:     verifySince,
			changed:   verifyChanged,
		}
		return runVerify(opts)
	},
}

var (
	verifyNoLint    bool
	verifyNoRace    bool
	verifyKeepGoing bool
	verifySince     string
	verifyChanged   bool
)

func init() {
	verifyCmd.Flags().BoolVar(&verifyNoLint, "no-lint", false,
		"Skip golangci-lint (use if not installed or to speed up)")
	verifyCmd.Flags().BoolVar(&verifyNoRace, "no-race", false,
		"Skip the race detector in go test")
	verifyCmd.Flags().BoolVar(&verifyKeepGoing, "keep-going", false,
		"Continue after the first failure and report every result")
	verifyCmd.Flags().StringVar(&verifySince, "since", "",
		"Scope fmt/vet/lint/test/build to files changed since <git-ref> (e.g. HEAD~1, main)")
	verifyCmd.Flags().BoolVar(&verifyChanged, "changed", false,
		"Scope to working-tree + staged + untracked changes (no committed comparison)")
	rootCmd.AddCommand(verifyCmd)
}

// verifyOptions is the typed flag bundle so tests can invoke runVerify
// directly without going through Cobra.
type verifyOptions struct {
	skipLint  bool
	skipRace  bool
	keepGoing bool
	since     string // git ref to diff against ("" = no committed comparison)
	changed   bool   // include working-tree + staged + untracked
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
// Scoped fields are populated only when --since / --changed is in effect.
type verifyResult struct {
	Checks         []verifyCheck `json:"checks"`
	Passed         int           `json:"passed"`
	Failed         int           `json:"failed"`
	Skipped        int           `json:"skipped"`
	Duration       int64         `json:"duration_ms"`
	Scoped         bool          `json:"scoped,omitempty"`
	Since          string        `json:"since,omitempty"`
	ChangedFiles   []string      `json:"changed_files,omitempty"`
	ScopedPackages []string      `json:"scoped_packages,omitempty"`
}

// verifyScopeData is the resolved file/package scope when --since or
// --changed is in effect. Step functions read currentVerifyScope to
// decide whether to scope their work. nil = full project (default).
type verifyScopeData struct {
	Since     string
	Files     []string // every changed file (any extension), repo-relative
	GoFiles   []string // subset that are *.go
	Dirs      []string // unique parent dirs of GoFiles (relative)
	Packages  []string // import paths derived from Dirs
	TestSet   []string // packages to test = Packages + reverse-deps
	NonGoOnly bool     // true when files changed but none are .go
}

// currentVerifyScope is the active scope for the duration of one
// runVerify call. Step functions read it. Reset to nil after the run.
var currentVerifyScope *verifyScopeData

// verifyStepDef describes one step in the verify pipeline.
type verifyStepDef struct {
	name string
	fn   func() (string, string, error) // message, output, err
}

// extraVerifySteps is a test-only seam that lets tests inject
// additional steps into runVerify — used to exercise defensive
// branches without shelling out.
var extraVerifySteps []verifyStepDef

// wireDriftInfoErr is a test-only seam that forces the d.Info err
// branch inside stepWireDrift. Nil in production.
var wireDriftInfoErr error

// runVerify executes every verification step and emits the result. If any
// step failed (unless keep-going was passed), returns a CodeVerifyFailed
// error so the root command's error handler exits non-zero.
func runVerify(opts verifyOptions) error {
	// Load .env so the spawned `go test ./...` child inherits the
	// project's env vars. Integration tests that read config.yaml +
	// project-prefixed env vars (e.g. for testcontainers ports or DB
	// fixture configuration) need these visible in os.Environ(). See
	// migrate.go for the full rationale.
	_, _ = loadDotEnv(".env")

	start := time.Now()

	// Compute scope first if --since or --changed was passed. Errors
	// (no git, bad ref) surface as clierr immediately — no partial run.
	if opts.since != "" || opts.changed {
		scope, err := resolveVerifyScope(opts)
		if err != nil {
			return err
		}
		currentVerifyScope = scope
		defer func() { currentVerifyScope = nil }()
	}

	// Each step is {Name, Runner}. Runners return a verifyCheck with
	// status/message/output already filled in — runVerify only times
	// the run and aggregates results.
	type stepDef = verifyStepDef
	steps := []stepDef{
		{"gofmt", stepGofmt},
		{"go vet", stepGoVet},
	}
	if extraVerifySteps != nil {
		steps = append(steps, extraVerifySteps...)
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

	if s := currentVerifyScope; s != nil {
		result.Scoped = true
		result.Since = s.Since
		result.ChangedFiles = s.Files
		result.ScopedPackages = s.Packages
	}

	// JSON mode: emit the aggregated result. Text mode: summary footer.
	cliout.Print(result, func(w io.Writer) {
		_, _ = fmt.Fprintln(w)
		if result.Scoped {
			_, _ = fmt.Fprintf(w, "Scoped: %d changed file(s), %d package(s)\n",
				len(result.ChangedFiles), len(result.ScopedPackages))
		}
		_, _ = fmt.Fprintf(w, "%d passed · %d failed · %d skipped · %dms\n",
			result.Passed, result.Failed, result.Skipped, result.Duration)
	})

	if result.Failed > 0 {
		return clierr.Newf(clierr.CodeVerifyFailed,
			"%d verify check(s) failed", result.Failed)
	}
	return nil
}

// printVerifyStep prints one check's outcome using the canonical
// termcolor vocabulary (✓/✗/—). Pass/fail/skip line up with what other
// commands emit so users see one visual style across the CLI.
func printVerifyStep(c verifyCheck) {
	switch c.Status {
	case "pass":
		cliout.Plainln("  " + termcolor.Success("%-16s (%dms)", c.Name, c.Duration))
	case "fail":
		suffix := ""
		if c.Message != "" {
			suffix = ": " + c.Message
		}
		cliout.Plainln("  " + termcolor.Fail("%-16s (%dms)%s", c.Name, c.Duration, suffix))
		if c.Output != "" {
			for line := range strings.SplitSeq(strings.TrimRight(c.Output, "\n"), "\n") {
				cliout.Plainln("    " + termcolor.CDim(line))
			}
		}
	case "skip":
		cliout.Plainln("  " + termcolor.CDim(fmt.Sprintf("- %-16s (%dms): skip", c.Name, c.Duration)))
	}
}

// --- individual step runners -------------------------------------------------

// runShellFn is a package-level seam over runShell so tests can
// exercise the step functions without spawning real processes.
var runShellFn = runShell

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
// we check the output rather than the exit code. Under --since, scopes
// to changed *.go files only.
func stepGofmt() (message, output string, err error) {
	args := []string{"-s", "-l"}
	if s := currentVerifyScope; s != nil {
		if len(s.GoFiles) == 0 {
			return "skip", "", nil
		}
		args = append(args, s.GoFiles...)
	} else {
		args = append(args, ".")
	}
	out, runErr := runShellFn("gofmt", args...)
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
	args := []string{"vet"}
	if s := currentVerifyScope; s != nil && !s.NonGoOnly {
		if len(s.Packages) == 0 {
			return "skip", "", nil
		}
		for _, p := range s.Packages {
			args = append(args, p)
		}
	} else {
		args = append(args, "./...")
	}
	out, err := runShellFn("go", args...)
	if err != nil {
		return "vet reported issues", out, err
	}
	return "", "", nil
}

// golangciLintLookPath is a seam over exec.LookPath for the linter so
// tests can simulate "installed" vs "missing".
var golangciLintLookPath = func() (string, error) { return exec.LookPath("golangci-lint") }

// stepGolangciLint tries to run golangci-lint. If the binary is not on
// $PATH it returns ("skip", "", nil) which the aggregator treats as
// skipped, not failed — agents that lack the linter should not be
// blocked by its absence. Under --since, uses golangci-lint's native
// `--new-from-rev=<ref>` flag.
func stepGolangciLint() (message, output string, err error) {
	if _, err := golangciLintLookPath(); err != nil {
		return "skip", "", nil
	}
	args := []string{"run"}
	if s := currentVerifyScope; s != nil && s.Since != "" {
		args = append(args, "--new-from-rev="+s.Since)
	}
	out, err := runShellFn("golangci-lint", args...)
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
	if s := currentVerifyScope; s != nil && !s.NonGoOnly {
		if len(s.TestSet) == 0 {
			return "skip", "", nil
		}
		args = append(args, s.TestSet...)
	} else {
		args = append(args, "./...")
	}
	out, err := runShellFn("go", args...)
	if err != nil {
		return "tests failed", out, err
	}
	return "", "", nil
}

func stepGoBuild() (message, output string, err error) {
	args := []string{"build"}
	if s := currentVerifyScope; s != nil && !s.NonGoOnly {
		if len(s.Packages) == 0 {
			return "skip", "", nil
		}
		args = append(args, s.Packages...)
	} else {
		args = append(args, "./...")
	}
	out, err := runShellFn("go", args...)
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
		if wireDriftInfoErr != nil {
			err = wireDriftInfoErr
		}
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

// --- scope resolution (-- since / --changed) --------------------------------

// resolveVerifyScopeFn is a package-level seam so tests can stub the
// (expensive, side-effectful) git + go list calls.
var resolveVerifyScopeFn = resolveVerifyScopeImpl

// resolveVerifyScope is the wrapper indirected via the seam.
func resolveVerifyScope(opts verifyOptions) (*verifyScopeData, error) {
	return resolveVerifyScopeFn(opts)
}

// resolveVerifyScopeImpl computes the changed-file + package set for a
// scoped verify run. Returns a clierr (CodeGit* or CodeGoBuildFailed) on
// failure so the caller can short-circuit without producing a partial
// verify report.
func resolveVerifyScopeImpl(opts verifyOptions) (*verifyScopeData, error) {
	ctx := context.Background()
	files, err := gitdiff.ChangedFiles(ctx, opts.since, gitdiff.Options{})
	if err != nil {
		return nil, err
	}

	goFiles := gitdiff.FilterGoFiles(files)
	scope := &verifyScopeData{
		Since:   opts.since,
		Files:   files,
		GoFiles: goFiles,
	}
	if len(files) > 0 && len(goFiles) == 0 {
		scope.NonGoOnly = true
		return scope, nil
	}
	if len(goFiles) == 0 {
		return scope, nil
	}

	scope.Dirs = gitdiff.UniqueDirs(goFiles)
	pkgs, err := gitdiff.PackagesForDirs(ctx, scope.Dirs)
	if err != nil {
		return nil, err
	}
	scope.Packages = pkgs

	// Reverse-dep walk for the test set; fall back to scoped packages
	// when the walk errors so we still produce useful output.
	if rev, err := gitdiff.ReverseDeps(ctx, pkgs); err == nil {
		scope.TestSet = rev
	} else {
		scope.TestSet = pkgs
	}
	return scope, nil
}
