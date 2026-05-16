package commands

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply or rollback SQL schema migrations against the configured database",
	Long: `Run golang-migrate against the database defined in config.yaml, using the
SQL files under db/migrations/.

The CLI builds the database URL from the active config (driver, host, port,
credentials, dsn overrides) and shells out to the ` + "`migrate`" + ` binary — you must
have it installed and on your $PATH (` + "`gofasta doctor`" + ` will check).

Subcommands:
  up     Apply every migration whose version is newer than the current schema
  down   Rollback the most recently applied migration (single step)

Create migrations with ` + "`gofasta g migration <Resource>`" + ` or ` + "`gofasta g scaffold`" + `,
which both write paired .up.sql / .down.sql files into db/migrations/.`,
}

var migrateUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply every pending migration in db/migrations/ (idempotent)",
	Long: `Run all migration files whose version is newer than the last version recorded
in the schema_migrations table. Safe to re-run: already-applied migrations are
skipped. Fails fast on the first migration that errors, leaving the schema at
the last successful version.

Use --explain to preview the risk profile of every .up.sql file under
db/migrations/ without opening a database connection. Static SQL analysis
flags lock-impact, data-loss, and app-incompatibility patterns:

  ALTER TABLE ... DROP COLUMN       → data-loss
  ADD COLUMN ... NOT NULL (no DEFAULT) → lock-and-fill (table rewrite)
  CREATE INDEX without CONCURRENTLY → lock-table (Postgres)
  RENAME COLUMN / RENAME TABLE      → app-incompatibility
  ALTER COLUMN ... TYPE             → lock-and-rewrite

Combine with --strict to exit non-zero when any high-severity warning fires
(useful in CI). --explain never opens a DB connection — works offline.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if migrateExplainFlags.enabled {
			return runMigrateExplain()
		}
		return runMigrationUp()
	},
}

var migrateDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Rollback applied migrations (interactive menu by default; --all / --steps for scripts)",
	Long: `Rollback applied migrations. With no flags and an interactive terminal,
opens a menu offering single-step / all / specific count. With --all or --steps
N, runs unattended (CI/scripts).

Flags:
  --all       Roll back every applied migration (destructive — confirms unless --yes)
  --steps N   Roll back exactly N migrations (no prompt; default 1 in non-interactive mode)
  --yes       Skip the destructive-action confirmation prompt

Without flags, in a non-interactive context (CI, piped stdin, --json) the
default is single-step rollback so automation never blocks on a prompt.

Destructive operations in the .down.sql are not reversible, so review the
SQL before running against a shared database.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMigrationDown()
	},
}

var (
	downAll   bool
	downSteps int
	downYes   bool
)

func init() {
	migrateUpCmd.Flags().BoolVar(&migrateExplainFlags.enabled, "explain", false,
		"static analysis of every pending .up.sql (no DB connection) — flags risky DDL")
	migrateUpCmd.Flags().BoolVar(&migrateExplainFlags.strict, "strict", false,
		"with --explain: exit non-zero when any high-severity warning fires (CI gate)")

	migrateCmd.AddCommand(migrateUpCmd)
	migrateCmd.AddCommand(migrateDownCmd)
	migrateDownCmd.Flags().BoolVar(&downAll, "all", false,
		"Roll back ALL applied migrations (destructive — prompts for confirmation unless --yes)")
	migrateDownCmd.Flags().IntVar(&downSteps, "steps", 0,
		"Roll back exactly N migrations (skips the menu; default 1 in non-interactive mode)")
	migrateDownCmd.Flags().BoolVar(&downYes, "yes", false,
		"Skip the destructive-action confirmation prompt (use with --all in scripts)")
	rootCmd.AddCommand(migrateCmd)
}

// downStdin is a package-level seam over os.Stdin so tests can drive
// the interactive menu via a strings.Reader without wiring a real pipe.
var downStdin io.Reader = os.Stdin

// stdinStat is the inner seam over os.Stdin.Stat — defaults to the
// real os.Stdin but tests can swap it to inject a Stat-failure so the
// defensive `err != nil` branch of stdinIsTTY is exercisable. (Most
// tests skip the production stdinIsTTY entirely by swapping the
// outer var below, so without an inner seam the err-branch is
// unreachable in tests.)
var stdinStat = func() (os.FileInfo, error) { return os.Stdin.Stat() }

// stdinIsTTY reports whether os.Stdin is an interactive terminal.
// Wrapped in a package-level seam so tests can deterministically
// simulate either side without depending on how the test runner wires
// /dev/stdin (which differs between `go test`, IDE runners, and CI).
var stdinIsTTY = func() bool {
	info, err := stdinStat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// buildMigrationURL is a package-level seam over
// configutil.BuildMigrationURL so tests can drive the empty-URL
// defensive branch.
var buildMigrationURL = configutil.BuildMigrationURL

// migrateResult is the structured payload emitted by runMigrate —
// suitable for `--json` consumers (CI, agents) and rendered by the
// human textFn for interactive use.
type migrateResult struct {
	Direction  string `json:"direction"`
	Status     string `json:"status"` // "ok" | "fail"
	Message    string `json:"message,omitempty"`
	Output     string `json:"output,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

// runMigrationUp applies every pending migration. Thin wrapper over
// runMigrate so the up path stays trivial (no menu, no flags).
func runMigrationUp() error {
	return runMigrate("up", []string{"up"}, nil)
}

// runMigrationDown resolves the rollback scope (single / all / N steps)
// from --all / --steps flags or an interactive menu, gates destructive
// choices behind a y/N confirm, then delegates to runMigrate.
//
// When the plan is "all" (steps == 0), the migrate tool's own
// "Are you sure you want to apply all down migrations? [y/N]" prompt
// will fire — but we've already confirmed via our menu, so we feed
// "y\n" to the child's stdin to suppress the duplicate prompt.
func runMigrationDown() error {
	plan, err := resolveDownPlan()
	if err != nil {
		return err
	}
	if plan.confirm && !confirmDestructive(plan) {
		return fmt.Errorf("rollback canceled")
	}

	args := []string{"down"}
	var stdinOverride io.Reader
	if plan.steps > 0 {
		args = append(args, strconv.Itoa(plan.steps))
	} else {
		// Bare `migrate down` triggers the tool's own y/N prompt.
		// We've already confirmed at our layer; auto-answer "y" so the
		// user doesn't see the same question twice.
		stdinOverride = strings.NewReader("y\n")
	}
	return runMigrate("down", args, stdinOverride)
}

// runMigration is a backward-compat dispatcher used by older tests.
// New callers should use runMigrationUp / runMigrationDown directly.
func runMigration(direction string) error {
	if direction == "down" {
		return runMigrationDown()
	}
	return runMigrationUp()
}

// migrateDownPlan describes a resolved rollback request. steps=0 means
// "all" (which is what passing no count to `migrate down` does).
type migrateDownPlan struct {
	steps   int
	confirm bool
}

// resolveDownPlan reads flags first (so scripts win deterministically),
// then falls back to either an interactive menu (TTY + non-JSON) or the
// safe single-step default (CI / piped / --json).
func resolveDownPlan() (migrateDownPlan, error) {
	switch {
	case downAll:
		return migrateDownPlan{steps: 0, confirm: !downYes}, nil
	case downSteps > 0:
		return migrateDownPlan{steps: downSteps, confirm: false}, nil
	case downSteps < 0:
		return migrateDownPlan{}, fmt.Errorf("--steps must be > 0")
	case stdinIsTTY() && !cliout.JSON():
		return promptDownPlan()
	default:
		// Non-interactive default = single step. Matches the docs' old
		// promise and never blocks automation on a prompt.
		return migrateDownPlan{steps: 1, confirm: false}, nil
	}
}

// promptDownPlan shows the interactive menu and parses the answer.
// Reading via `downStdin` (not os.Stdin directly) lets tests drive
// the menu with a strings.Reader.
func promptDownPlan() (migrateDownPlan, error) {
	cliout.Plainln(termcolor.Step("Roll back how many migrations?"))
	cliout.Plainln("  1 — latest only (default)")
	cliout.Plainln("  a — all migrations (destructive)")
	cliout.Plainln("  n — specific number")
	cliout.Plainln("  q — cancel")
	cliout.Plain("Choice [1]: ")

	reader := bufio.NewReader(downStdin)
	line, _ := reader.ReadString('\n')
	choice := strings.ToLower(strings.TrimSpace(line))

	switch choice {
	case "", "1":
		return migrateDownPlan{steps: 1, confirm: false}, nil
	case "a", "all":
		return migrateDownPlan{steps: 0, confirm: true}, nil
	case "n":
		cliout.Plain("How many steps? ")
		nLine, _ := reader.ReadString('\n')
		n, perr := strconv.Atoi(strings.TrimSpace(nLine))
		if perr != nil || n <= 0 {
			return migrateDownPlan{}, fmt.Errorf("invalid step count: %s", strings.TrimSpace(nLine))
		}
		return migrateDownPlan{steps: n, confirm: n > 1}, nil
	case "q", "quit", "cancel":
		return migrateDownPlan{}, fmt.Errorf("rollback canceled")
	default:
		// Accept a bare number too — quality-of-life so users who type
		// "3" instead of "n" then "3" still get what they want.
		if n, perr := strconv.Atoi(choice); perr == nil && n > 0 {
			return migrateDownPlan{steps: n, confirm: n > 1}, nil
		}
		return migrateDownPlan{}, fmt.Errorf("invalid choice: %q", choice)
	}
}

// confirmDestructive prompts y/N for a destructive plan. Reads from
// downStdin so tests can drive the response.
func confirmDestructive(plan migrateDownPlan) bool {
	var msg string
	if plan.steps == 0 {
		msg = "This will roll back ALL migrations. Confirm? [y/N]: "
	} else {
		msg = fmt.Sprintf("This will roll back %d migrations. Confirm? [y/N]: ", plan.steps)
	}
	cliout.Plain("%s", termcolor.Warn("%s", msg))

	reader := bufio.NewReader(downStdin)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// runMigrate is the shared core: load .env, build URL, exec migrate,
// emit a structured cliout payload. migrateArgs is the tail passed to
// migrate (e.g. ["up"] or ["down", "1"]).
//
// stdinOverride, when non-nil, is used as the child's stdin instead of
// os.Stdin — needed when the migrate tool would prompt for a y/N that
// we've already confirmed at our layer (see runMigrationDown for "all").
func runMigrate(direction string, migrateArgs []string, stdinOverride io.Reader) error {
	// Load .env BEFORE building the migration URL. config.yaml in
	// scaffolded projects ships with in-container defaults (host:
	// localhost, port: 5432 — what the app sees from inside the
	// compose network); the .env file is where the host-side
	// overrides live (e.g. PORT=5433 because docker maps host
	// 5433 → container 5432, plus DATABASE_USER/PASSWORD/NAME).
	// Without this, migrate builds a URL that points at port 5432
	// on the host (where nothing is listening) and fails with
	// "connection refused" even when the DB is fully healthy.
	// `gofasta dev` and `gofasta doctor` both load .env this way;
	// migrate must do the same.
	_, _ = loadDotEnv(".env")

	dbURL := buildMigrationURL()
	if dbURL == "" {
		return fmt.Errorf("failed to load config — ensure config.yaml exists")
	}

	// In text mode we want progress AS the migrate tool runs, but in
	// --json mode the consumer wants exactly one machine-readable
	// payload at the end. So we capture the migrate output, then route
	// it through cliout.Print at the end with the right shape.
	var captured bytes.Buffer
	if !cliout.JSON() {
		cliout.Plainln(termcolor.Step("Applying migrations (%s)", direction))
	}

	args := append([]string{"-path", "db/migrations", "-database", dbURL}, migrateArgs...)
	cmd := execCommand("migrate", args...)
	if cliout.JSON() {
		cmd.Stdout = &captured
		cmd.Stderr = &captured
		// In JSON mode any prompt would block forever — feed the
		// override (or an empty reader) so the migrate tool sees EOF
		// rather than hanging on Read.
		if stdinOverride != nil {
			cmd.Stdin = stdinOverride
		}
	} else {
		// Stream output live in human mode AND tee into the buffer so
		// the final payload (used for the final ✓/✗ summary line) has
		// access to it if needed. Wire stdin so any interactive prompt
		// the migrate tool itself shows (rare now that we always pass
		// an explicit count) reaches the user — unless the caller
		// supplied an override (used by `down --all` to auto-answer
		// the migrate tool's redundant confirmation).
		cmd.Stdout = io.MultiWriter(os.Stdout, &captured)
		cmd.Stderr = io.MultiWriter(os.Stderr, &captured)
		if stdinOverride != nil {
			cmd.Stdin = stdinOverride
		} else {
			cmd.Stdin = os.Stdin
		}
	}

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start).Milliseconds()

	result := migrateResult{
		Direction:  direction,
		Status:     "ok",
		Output:     captured.String(),
		DurationMs: elapsed,
	}
	if runErr != nil {
		result.Status = "fail"
		result.Message = runErr.Error()
	}

	cliout.Print(result, func(w io.Writer) {
		if runErr == nil {
			_, _ = fmt.Fprintln(w, termcolor.Success("Migration %s complete (%dms)", direction, elapsed))
		} else {
			_, _ = fmt.Fprintln(w, termcolor.Fail("Migration %s failed: %s", direction, runErr.Error()))
		}
	})

	if runErr != nil {
		return fmt.Errorf("migration %s failed: %w", direction, runErr)
	}
	return nil
}
