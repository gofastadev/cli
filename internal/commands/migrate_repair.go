package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

// repairResult is the JSON payload `gofasta migrate repair --json`
// emits. Captures what was found and what was done so automation can
// branch on the outcome.
type repairResult struct {
	WasDirty        bool   `json:"was_dirty"`
	DirtyVersion    int    `json:"dirty_version,omitempty"`
	Action          string `json:"action"` // "none" | "force" | "canceled"
	ForcedToVersion int    `json:"forced_to_version,omitempty"`
	Message         string `json:"message,omitempty"`
}

var (
	repairForce    int
	repairRevert   bool
	repairComplete bool
	repairYes      bool
)

var migrateRepairCmd = &cobra.Command{
	Use:   "repair",
	Short: "Fix a dirty schema_migrations row (interactive by default)",
	Long: `Detect and clear the "dirty" flag on schema_migrations. A version is
marked dirty when a migration starts but doesn't complete cleanly (SQL
error mid-statement, killed process, network drop). While dirty, migrate
refuses to apply or reverse migrations because the DB state is unknown.

` + "`" + `gofasta migrate repair` + "`" + ` does NOT run any of your migration SQL — it
only updates schema_migrations to match a state YOU have manually
reconciled. The two recovery paths:

  • REVERT — you've manually undone what the failing migration did, so
            the DB is at version N-1. Run with --revert (or pick "r" in
            the menu) to mark N-1 as the current version.
  • COMPLETE — you've manually finished what the failing migration started,
              so the DB is at version N. Run with --complete (or "c" in
              the menu) to mark N as the current version.

For full manual control, --force <V> sets the version to <V> directly.

Flags:
  --revert       Mark version N-1 as current (you reverted the dirty migration)
  --complete     Mark version N as current (you finished the dirty migration)
  --force N      Mark version N as current (skips the dirty check; use carefully)
  --yes          Skip the destructive-action confirmation prompt

Without flags, in a non-interactive context (CI / piped / --json) the
command refuses to act and exits non-zero so automation never silently
guesses your intent.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMigrateRepair()
	},
}

func init() {
	migrateRepairCmd.Flags().IntVar(&repairForce, "force", -1,
		"Mark schema_migrations.version = N directly (skips the dirty-state inspection)")
	migrateRepairCmd.Flags().BoolVar(&repairRevert, "revert", false,
		"You've manually undone the dirty migration; mark version N-1 as current")
	migrateRepairCmd.Flags().BoolVar(&repairComplete, "complete", false,
		"You've manually finished the dirty migration; mark version N as current")
	migrateRepairCmd.Flags().BoolVar(&repairYes, "yes", false,
		"Skip the destructive-action confirmation prompt (use in scripts)")
	migrateCmd.AddCommand(migrateRepairCmd)
}

// repairStdin is a package-level seam over os.Stdin so tests can drive
// the interactive menu via a strings.Reader.
var repairStdin io.Reader = os.Stdin

func runMigrateRepair() error {
	// Same .env-then-build-URL dance as every other DB-touching command.
	_, _ = loadDotEnv(".env")
	dbURL := buildMigrationURL()
	if dbURL == "" {
		return fmt.Errorf("failed to load config — ensure config.yaml exists")
	}

	migrationsDir := "db/migrations"

	// --force <N> short-circuits all inspection. Dangerous, so still
	// require --yes (or interactive confirm) before applying.
	if repairForce >= 0 {
		return repairForceTo(repairForce, dbURL, migrationsDir)
	}

	current, dirty, err := readAppliedMigrationVersion(migrationsDir, dbURL)
	if err != nil {
		return clierr.Wrap(clierr.CodeAIInstallFailed, err,
			"could not read schema_migrations — is the DB reachable?")
	}

	result := repairResult{
		WasDirty:     dirty,
		DirtyVersion: current,
	}

	if !dirty {
		result.Action = "none"
		result.Message = fmt.Sprintf("schema is clean at version %d — nothing to repair", current)
		cliout.Print(result, func(w io.Writer) {
			_, _ = fmt.Fprintln(w, termcolor.Success("%s", result.Message))
		})
		return nil
	}

	// Dirty. Resolve the target version from flags first, then menu.
	target, err := resolveRepairTarget(current, migrationsDir)
	if err != nil {
		result.Action = "canceled"
		result.Message = err.Error()
		cliout.Print(result, func(w io.Writer) {
			_, _ = fmt.Fprintln(w, termcolor.Warn("%s", result.Message))
		})
		return err
	}

	return repairForceTo(target, dbURL, migrationsDir)
}

// resolveRepairTarget chooses the version to force schema_migrations
// to. Flags win deterministically; falls back to interactive menu in a
// TTY; refuses in non-interactive mode (CI shouldn't guess).
func resolveRepairTarget(dirtyVersion int, migrationsDir string) (int, error) {
	switch {
	case repairRevert && repairComplete:
		return 0, fmt.Errorf("--revert and --complete are mutually exclusive")
	case repairRevert:
		return dirtyVersion - 1, nil
	case repairComplete:
		return dirtyVersion, nil
	case stdinIsTTY() && !cliout.JSON():
		return promptRepairTarget(dirtyVersion, migrationsDir)
	default:
		return 0, fmt.Errorf(
			"non-interactive context: pass --revert, --complete, or --force <N> to choose a recovery path " +
				"(see `gofasta migrate repair --help`)")
	}
}

// promptRepairTarget walks the user through choosing revert vs complete,
// showing the failing migration's SQL on request so they can decide.
//
// Reuses ONE bufio.Reader across iterations — creating a new reader
// each loop would discard the previous reader's internal buffer, so
// a multi-line strings.Reader (used by tests) would lose subsequent
// lines after the first ReadString call.
func promptRepairTarget(dirtyVersion int, migrationsDir string) (int, error) {
	reader := bufio.NewReader(repairStdin)
	for {
		fmt.Println(termcolor.Warn("Schema is dirty at version %d", dirtyVersion))
		fmt.Println("A migration started but didn't complete cleanly. The DB might be in")
		fmt.Println("any in-between state. You need to manually reconcile the DB before")
		fmt.Println("clearing the dirty flag. Choose how:")
		fmt.Println()
		fmt.Println("  r — REVERT: I manually undid migration", dirtyVersion,
			"→ mark version", dirtyVersion-1, "as current")
		fmt.Println("  c — COMPLETE: I manually finished migration", dirtyVersion,
			"→ mark version", dirtyVersion, "as current")
		fmt.Println("  s — SHOW the .up.sql and .down.sql so I can decide")
		fmt.Println("  q — QUIT without changing anything")
		fmt.Print("Choice: ")

		line, _ := reader.ReadString('\n')
		choice := strings.ToLower(strings.TrimSpace(line))

		switch choice {
		case "r", "revert":
			return dirtyVersion - 1, nil
		case "c", "complete":
			return dirtyVersion, nil
		case "s", "show":
			showMigrationSQL(migrationsDir, dirtyVersion)
			fmt.Println()
			continue
		case "q", "quit", "cancel", "":
			return 0, fmt.Errorf("repair canceled")
		default:
			fmt.Println(termcolor.Warn("Invalid choice %q — please enter r, c, s, or q", choice))
			fmt.Println()
		}
	}
}

// showMigrationSQL prints the .up.sql and .down.sql for the given
// version so the user can decide which recovery path to take.
func showMigrationSQL(migrationsDir string, version int) {
	upPath := findMigrationFile(migrationsDir, version, "up.sql")
	downPath := findMigrationFile(migrationsDir, version, "down.sql")

	if upPath != "" {
		fmt.Println(termcolor.Step("Up migration: %s", upPath))
		printFileWithIndent(upPath, "    ")
	} else {
		fmt.Println(termcolor.Warn("No .up.sql found for version %d", version))
	}
	fmt.Println()
	if downPath != "" {
		fmt.Println(termcolor.Step("Down migration: %s", downPath))
		printFileWithIndent(downPath, "    ")
	} else {
		fmt.Println(termcolor.Warn("No .down.sql found for version %d", version))
	}
}

// findMigrationFile looks for a file in migrationsDir matching
// <version>_*.<suffix>. Returns "" if not found.
func findMigrationFile(migrationsDir string, version int, suffix string) string {
	prefix := fmt.Sprintf("%06d_", version)
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, "."+suffix) {
			return filepath.Join(migrationsDir, name)
		}
	}
	// Try without zero-padding as a fallback (some projects use 1_, 2_, …).
	prefix = strconv.Itoa(version) + "_"
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, "."+suffix) {
			return filepath.Join(migrationsDir, name)
		}
	}
	return ""
}

// printFileWithIndent reads path and prints each line indented. Errors
// are surfaced as a warning line so the user knows something's off
// without halting the whole repair flow.
func printFileWithIndent(path, indent string) {
	body, err := os.ReadFile(path)
	if err != nil {
		fmt.Println(indent + termcolor.Warn("could not read %s: %s", path, err.Error()))
		return
	}
	for line := range strings.SplitSeq(strings.TrimRight(string(body), "\n"), "\n") {
		fmt.Println(indent + termcolor.CDim(line))
	}
}

// repairForceTo runs `migrate force <target>` with destructive-action
// confirmation when not --yes / not in JSON mode. Emits a structured
// repairResult either way.
func repairForceTo(target int, dbURL, migrationsDir string) error {
	if target < 0 {
		return fmt.Errorf("target version must be ≥ 0 (got %d)", target)
	}

	result := repairResult{Action: "force", ForcedToVersion: target}

	// Confirm unless --yes or non-TTY (the resolveRepairTarget path
	// already errored in non-TTY non-flag cases, so we only reach here
	// non-TTY when explicit flags were passed — trust the caller).
	if !repairYes && stdinIsTTY() && !cliout.JSON() {
		fmt.Print(termcolor.Warn("This will set schema_migrations.version=%d, dirty=false. Continue? [y/N]: ", target))
		reader := bufio.NewReader(repairStdin)
		line, _ := reader.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer != "y" && answer != "yes" {
			result.Action = "canceled"
			result.Message = "repair canceled"
			cliout.Print(result, func(w io.Writer) {
				_, _ = fmt.Fprintln(w, termcolor.Warn("repair canceled"))
			})
			return fmt.Errorf("repair canceled")
		}
	}

	if !cliout.JSON() {
		fmt.Println(termcolor.Step("Running `migrate force %d`", target))
	}

	cmd := execCommand("migrate",
		"-path", migrationsDir,
		"-database", dbURL,
		"force", strconv.Itoa(target),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if runErr := cmd.Run(); runErr != nil {
		result.Action = "force"
		result.Message = runErr.Error()
		cliout.Print(result, func(w io.Writer) {
			_, _ = fmt.Fprintln(w, termcolor.Fail("migrate force %d failed: %s", target, runErr.Error()))
		})
		return fmt.Errorf("migrate force %d failed: %w", target, runErr)
	}

	result.Message = fmt.Sprintf("schema_migrations.version = %d, dirty = false", target)
	cliout.Print(result, func(w io.Writer) {
		_, _ = fmt.Fprintln(w, termcolor.Success("%s", result.Message))
		_, _ = fmt.Fprintln(w, termcolor.Hint("verify with `gofasta status` or `gofasta migrate up`"))
	})
	return nil
}
