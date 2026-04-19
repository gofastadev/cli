package commands

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var (
	devFlagValues  devFlags
	devServicesRaw string
)

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Run the full local dev environment (services + migrations + Air hot reload)",
	Long: `Bring the project's full development environment up with one
command: start the compose services (database, cache, queue),
health-check each one, apply pending migrations, then launch Air
for hot reload of the host-side app.

Pipeline (each stage can be opted out independently):
  1. Preflight        — verify docker + docker compose availability
  2. Fresh volumes    — optional; drops every compose volume (--fresh)
  3. Service start    — docker compose up -d <resolved services>
  4. Health-wait      — poll each service until healthy (timeout 30s)
  5. Migrate          — migrate up against the now-healthy database
  6. Seed             — optional; runs ` + "`gofasta seed`" + ` after migrations
  7. Air              — exec ` + "`go tool air`" + ` against .air.toml
  8. Teardown         — on SIGINT/SIGTERM, stop services (volumes preserved)

Projects without compose.yaml get steps 1–4 short-circuited and fall
straight through to Air — preserving the "I brought my own DB"
workflow.

Every step emits a structured event when --json is set, so agents and
CI tooling can branch on facts instead of log strings.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		devFlagValues.servicesList = parseServicesList(devServicesRaw)
		return runDev(devFlagValues)
	},
}

func init() {
	rootCmd.AddCommand(devCmd)

	f := devCmd.Flags()
	f.BoolVar(&devFlagValues.noServices, "no-services", false,
		"skip all compose orchestration; just run Air (honors an externally-managed database)")
	f.BoolVar(&devFlagValues.noDB, "no-db", false,
		"skip DB-like services (postgres, mysql, clickhouse, …)")
	f.BoolVar(&devFlagValues.noCache, "no-cache", false,
		"skip cache-like services (redis, valkey, …)")
	f.BoolVar(&devFlagValues.noQueue, "no-queue", false,
		"skip queue-like services (asynq, nats, rabbitmq, …)")
	f.BoolVar(&devFlagValues.noMigrate, "no-migrate", false,
		"skip running migrate up after services become healthy")
	f.BoolVar(&devFlagValues.noTeardown, "no-teardown", false,
		"leave compose services running on exit (default: stop them)")
	f.BoolVar(&devFlagValues.keepVolumes, "keep-volumes", true,
		"preserve named volumes on teardown (default: true)")
	f.BoolVar(&devFlagValues.fresh, "fresh", false,
		"drop every compose volume before starting — forces a clean DB state")
	f.StringVar(&devServicesRaw, "services", "",
		"comma-separated list of compose services to start (overrides --no-* flags)")
	f.StringVar(&devFlagValues.profile, "profile", "",
		"docker compose profile to activate (e.g. cache, queue)")
	f.DurationVar(&devFlagValues.waitTimeout, "wait-timeout", defaultWaitTimeout,
		"how long to wait for compose services to report healthy")
	f.StringVar(&devFlagValues.envFile, "env-file", ".env",
		"path to the .env file to load before starting Air")
	f.StringVar(&devFlagValues.port, "port", "",
		"override the PORT env var passed to Air / the app binary")
	f.BoolVar(&devFlagValues.rebuild, "rebuild", false,
		"force Air to do a rebuild cycle before first serve")
	f.BoolVar(&devFlagValues.seed, "seed", false,
		"run seeders after migrations (equivalent to running `gofasta seed` post-start)")
	f.BoolVar(&devFlagValues.dryRun, "dry-run", false,
		"print the resolved plan and exit without touching anything")
	f.BoolVar(&devFlagValues.attachLogs, "attach-logs", false,
		"stream `docker compose logs -f` alongside Air (service-prefixed)")
	f.BoolVar(&devFlagValues.dashboard, "dashboard", false,
		"start the local dev dashboard — an HTML debug page with routes, health, and live service state")
	f.IntVar(&devFlagValues.dashboardPort, "dashboard-port", 9090,
		"port for the dev dashboard HTTP server")
}

// runDev is the orchestration entrypoint. Broken into clearly-named
// stages so the pipeline reads top-to-bottom. Each stage consults flags
// to decide whether to execute; each emits one or more events via the
// devEmitter so humans see status lines and agents see JSON events.
//
//nolint:gocognit,gocyclo // Linear pipeline; breaking it up would obscure the ordering invariants.
func runDev(flags devFlags) error {
	emitter := newDevEmitter(jsonOutput)

	// --- Stage 0: load .env ------------------------------------------------
	// Keep the existing behavior: load .env first so subsequent stages see
	// the developer's local overrides for DATABASE_HOST / PORT / etc.
	if loaded, err := loadDotEnv(flags.envFile); err != nil {
		emitter.Warn(fmt.Sprintf("%s present but could not be loaded: %v", flags.envFile, err))
	} else if loaded > 0 {
		emitter.Info(fmt.Sprintf("loaded %d variables from %s", loaded, flags.envFile))
	}
	if flags.port != "" {
		_ = os.Setenv("PORT", flags.port)
	}

	// --- Stage 1: resolve services -----------------------------------------
	// Decide what we'd do — even in non-dry-run mode, we build the plan
	// before touching anything so a failure here surfaces without side
	// effects.
	plan, err := resolveDevPlan(flags)
	if err != nil {
		return err
	}

	if flags.dryRun {
		printDevPlan(plan, emitter)
		return nil
	}

	// --- Stage 2: preflight ------------------------------------------------
	if plan.orchestrate {
		if !composeAvailable() {
			return clierr.New(clierr.CodeDevDockerUnavailable,
				"docker or docker compose is not available")
		}
		docker, compose := detectVersions()
		emitter.Preflight(docker, compose)
	}

	// --- Stage 3: fresh volumes (optional) ---------------------------------
	if plan.orchestrate && flags.fresh {
		emitter.Info("dropping compose volumes (--fresh)")
		if err := resetVolumes(); err != nil {
			emitter.Warn(fmt.Sprintf("could not drop volumes: %v — continuing", err))
		}
	}

	// --- Stage 4: start services -------------------------------------------
	if plan.orchestrate && len(plan.services.selected) > 0 {
		for _, name := range plan.services.selected {
			emitter.ServiceStart(name)
		}
		if err := startServices(plan.services.selected, flags.profile); err != nil {
			return clierr.Wrap(clierr.CodeDevServiceUnhealthy, err,
				"failed to start compose services")
		}

		if err := waitHealthy(plan.services.selected, plan.services.hasHealth, flags.waitTimeout,
			func(name, state string, elapsed time.Duration) {
				if strings.HasPrefix(state, "running/healthy") || state == "running/" {
					emitter.ServiceHealthy(name, elapsed)
				}
			}); err != nil {
			return clierr.Wrap(clierr.CodeDevServiceUnhealthy, err, err.Error())
		}
	}

	// Teardown runs exactly once on exit, unless --no-teardown is set.
	// `--keep-volumes=false` upgrades the teardown from `stop` (preserve
	// containers + volumes) to `down -v` (destroy both). The default keeps
	// volumes so the next `gofasta dev` reuses the primed database.
	var teardownDone bool
	runTeardown := func(reason string) {
		if teardownDone || flags.noTeardown {
			return
		}
		teardownDone = true
		if plan.orchestrate && len(plan.services.selected) > 0 {
			var err error
			var mode string
			if flags.keepVolumes {
				err = stopServices(plan.services.selected)
				mode = "stopped"
			} else {
				err = resetVolumes()
				mode = "destroyed"
			}
			if err == nil {
				emitter.Shutdown(mode, 0)
			} else {
				emitter.Shutdown(mode+"-failed", 1)
			}
		} else {
			emitter.Shutdown(reason, 0)
		}
	}
	defer runTeardown("clean")

	// --- Stage 5: migrations -----------------------------------------------
	if !flags.noMigrate {
		if applied, err := runMigrationsWithCount(); err != nil {
			emitter.MigrateSkipped(err.Error())
		} else {
			emitter.MigrateOK(applied)
		}
	}

	// --- Stage 6: seed (optional) ------------------------------------------
	if flags.seed {
		if err := runSeedDelegation(); err != nil {
			emitter.Warn(fmt.Sprintf("seed failed: %v", err))
		} else {
			emitter.Info("seeders completed")
		}
	}

	// --- Stage 7: optional side-processes ----------------------------------
	// --attach-logs: stream docker compose logs alongside Air output.
	// --dashboard:   spin up the debug HTTP server on dashboardPort.
	// Both register shutdown hooks so they stop cleanly with the pipeline.
	var sideCancels []func()
	if flags.attachLogs && plan.orchestrate && len(plan.services.selected) > 0 {
		sideCancels = append(sideCancels, startLogStreamer(plan.services.selected))
	}

	// --- Stage 8: Air ------------------------------------------------------
	port := configutil.GetPort()
	if flags.port != "" {
		port = flags.port
	}
	portInt, _ := strconv.Atoi(port)
	urls := airURLs(port)
	emitter.Air(portInt, urls)

	if flags.dashboard {
		sideCancels = append(sideCancels, startDashboard(flags.dashboardPort, portInt, &plan.services, emitter))
	}

	err = runAir(flags, runTeardown)
	for _, c := range sideCancels {
		c()
	}
	return err
}

// devPlan is what resolveDevPlan builds before any side effect runs.
// Passed to both the dry-run printer and the real execution path, so
// both paths see an identical picture of what's about to happen.
type devPlan struct {
	orchestrate bool        // run the compose pipeline at all
	services    devServices // resolved service set (may be empty)
}

func resolveDevPlan(flags devFlags) (devPlan, error) {
	// If the user opts out of orchestration entirely, or there's no
	// compose.yaml in sight, fall straight through to the Air-only path.
	if flags.noServices || !composeFileExists() {
		if len(flags.servicesList) > 0 && !composeFileExists() {
			return devPlan{}, clierr.New(clierr.CodeDevComposeNotFound,
				"no compose.yaml found but --services was set")
		}
		return devPlan{orchestrate: false}, nil
	}

	available, hasHealth, err := detectComposeServices(flags.profile)
	if err != nil {
		return devPlan{}, clierr.Wrap(clierr.CodeDevComposeNotFound, err,
			"could not read compose configuration")
	}
	selected := resolveSelectedServices(available, flags)
	return devPlan{
		orchestrate: true,
		services: devServices{
			available: available,
			selected:  selected,
			profile:   flags.profile,
			hasHealth: hasHealth,
		},
	}, nil
}

func printDevPlan(plan devPlan, emitter devEmitter) {
	if plan.orchestrate {
		emitter.Info(fmt.Sprintf("orchestrate=true profile=%q services=%v",
			plan.services.profile, plan.services.selected))
	} else {
		emitter.Info("orchestrate=false (no compose.yaml or --no-services)")
	}
}

// detectVersions returns best-effort version strings for docker and
// docker compose. Used for the preflight event — non-critical, so any
// detection failure just returns "unknown".
func detectVersions() (docker, compose string) {
	docker = captureVersionLine(execCommand("docker", "version", "--format", "{{.Client.Version}}"))
	if docker == "" {
		docker = "unknown"
	}
	compose = captureVersionLine(execCommand("docker", "compose", "version", "--short"))
	if compose == "" {
		compose = "unknown"
	}
	return docker, compose
}

// captureVersionLine runs a prepared *exec.Cmd and returns the first
// non-empty line of stdout trimmed. Returns "" on any failure so the
// preflight event can fall back to "unknown" without a panic.
func captureVersionLine(cmd *exec.Cmd) string {
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return ""
	}
	line := strings.SplitN(strings.TrimSpace(out.String()), "\n", 2)[0]
	return strings.TrimSpace(line)
}

// runMigrationsWithCount re-uses the existing runMigrations but also
// tries to extract a count of applied migrations from the migrate CLI
// output. The golang-migrate CLI prints one line per applied step to
// stderr in the form "N/u migration_name (duration)" — counting those
// is a good-enough approximation of "how many ran".
func runMigrationsWithCount() (int, error) {
	if _, err := execLookPath("migrate"); err != nil {
		return 0, errors.New("migrate CLI not found on $PATH")
	}
	dbURL := configutil.BuildMigrationURL()

	var buf bytes.Buffer
	cmd := execCommand("migrate", "-path", "db/migrations", "-database", dbURL, "up")
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		// If the output says "no change", treat it as a zero-applied success
		// rather than an error — migrate exits 0 in that case anyway, but
		// we want this branch to be explicit.
		return 0, clierr.Wrapf(clierr.CodeDevMigrationFailed, err,
			"migrate up failed:\n%s", strings.TrimSpace(buf.String()))
	}

	applied := strings.Count(buf.String(), "/u ")
	return applied, nil
}

// runSeedDelegation shells out to the project's own seed command. The
// seed code path lives in the scaffolded project (not the CLI), so we
// invoke it the same way `gofasta seed` does: via the project's main
// binary with the `seed` subcommand.
func runSeedDelegation() error {
	cmd := execCommand("go", "run", "./app/main", "seed")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// airURLs builds the per-transport URL set for the running project.
// GraphQL / swagger endpoints are included only if the project actually
// exposes them (detected via filesystem markers), so the URL set never
// lies about what's live.
func airURLs(port string) map[string]string {
	urls := map[string]string{"rest": "http://localhost:" + port}
	if _, err := os.Stat("gqlgen.yml"); err == nil {
		urls["graphql"] = "http://localhost:" + port + "/graphql"
		urls["playground"] = "http://localhost:" + port + "/graphql-playground"
	}
	if _, err := os.Stat("docs/swagger.json"); err == nil {
		urls["swagger"] = "http://localhost:" + port + "/swagger/index.html"
	}
	urls["metrics"] = "http://localhost:" + port + "/metrics"
	urls["health"] = "http://localhost:" + port + "/health"
	return urls
}

// runAir execs `go tool air` and wires SIGINT/SIGTERM through so Ctrl+C
// tears down services before the process exits.
//
// If --rebuild is set, the Air build cache directory (tmp/ by default,
// configured via .air.toml) is deleted first so the next `go tool air`
// invocation rebuilds from scratch rather than reusing a stale binary.
// Air has no "force rebuild" flag of its own — deleting the tmp dir is
// the officially-documented way.
func runAir(flags devFlags, teardown func(string)) error {
	if flags.rebuild {
		// tmp/ is Air's default build directory; projects that
		// customize .air.toml may use a different path, but clearing
		// the default is a safe best-effort.
		if err := os.RemoveAll("tmp"); err != nil {
			// A missing tmp dir is the expected state on first run.
			// Any other failure is non-fatal: Air will still run; the
			// developer just won't get the forced-rebuild guarantee.
			_ = err
		}
	}
	args := []string{"tool", "air"}

	if _, err := execLookPath("go"); err != nil {
		return clierr.New(clierr.CodeDevAirNotInstalled,
			"Go toolchain not on $PATH")
	}

	airCmd := execCommand("go", args...)
	airCmd.Stdout = os.Stdout
	airCmd.Stderr = os.Stderr
	airCmd.Stdin = os.Stdin

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		if airCmd.Process != nil {
			_ = airCmd.Process.Signal(os.Interrupt)
		}
		teardown("interrupted")
	}()

	err := airCmd.Run()
	// Air exits non-zero when it receives SIGINT. Treat a signal-triggered
	// exit as a successful shutdown rather than a pipeline failure.
	if err != nil && airCmd.ProcessState != nil && airCmd.ProcessState.Exited() {
		if ws, ok := airCmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return nil
		}
	}
	if err != nil {
		return clierr.Wrap(clierr.CodeDevAirNotInstalled, err,
			"air exited with error")
	}
	return nil
}

// Legacy helpers kept for backward-compat with other files that still
// reference them. runMigrations is the original best-effort entrypoint
// used elsewhere in the codebase; leaving it here avoids churning
// callers outside the dev command.
func runMigrations() error {
	if _, err := execLookPath("migrate"); err != nil {
		return fmt.Errorf("migrate CLI not found on $PATH — install with:\n" +
			"  go install -tags 'postgres mysql sqlite3 sqlserver clickhouse' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1")
	}
	dbURL := configutil.BuildMigrationURL()
	if err := runMigrateUp(dbURL); err == nil {
		return nil
	}
	termcolor.PrintHint("Database not ready, retrying in 2 seconds...")
	time.Sleep(2 * time.Second)
	return runMigrateUp(dbURL)
}

func runMigrateUp(dbURL string) error {
	cmd := execCommand("migrate", "-path", "db/migrations", "-database", dbURL, "up")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
