package commands

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
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

The cache and queue compose profiles are auto-activated by default;
pass --no-cache / --no-queue to skip them. Pass --all-in-docker to
run Air inside the app container instead of on the host — supporting
services run detached and the app's stdout streams to the foreground.

While the pipeline is running, the terminal accepts these single-key
shortcuts (disable with --no-keyboard, auto-disabled when stdin is
not a TTY):
  r, R    restart the entire pipeline from scratch (re-runs every stage)
  q, Q    quit gofasta dev (same as Ctrl+C)
  h, H, ? print the keybinding help

Pipeline (each stage can be opted out independently):
  1. Preflight        — verify docker + docker compose availability
  2. Fresh volumes    — optional; drops every compose volume (--fresh)
  3. Service start    — docker compose up -d <resolved services>
  4. Health-wait      — poll each service until healthy (timeout 30s)
  5. Migrate          — migrate up against the now-healthy database
                        (skipped under --all-in-docker — the dev container
                        runs migrate itself before Air starts)
  6. Seed             — optional; runs ` + "`gofasta seed`" + ` after migrations
  7. Air              — exec ` + "`go tool air`" + ` against .air.toml on the host,
                        OR (under --all-in-docker) tail the app container's
                        stdout to the foreground via ` + "`docker compose logs -f`" + `
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
		"skip the cache service (default: cache compose profile is auto-activated)")
	f.BoolVar(&devFlagValues.noQueue, "no-queue", false,
		"skip the queue service (default: queue compose profile is auto-activated)")
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
		"additional docker compose profile to activate (cache and queue are auto-on unless --no-cache / --no-queue)")
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
	f.BoolVar(&devFlagValues.allInDocker, "all-in-docker", false,
		"run the entire stack inside docker compose, including the app + Air; supporting services run detached and the app container's stdout streams to the foreground for live hot-reload logs")
	f.BoolVar(&devFlagValues.noKeyboard, "no-keyboard", false,
		"disable the interactive keyboard layer (r=restart, q=quit, h=help); use when stdin is piped or for non-interactive sessions")
}

// runDev is the orchestration entrypoint. It owns three top-level
// concerns:
//
//   - the keyboard listener (raw-mode stdin → restart/quit signals),
//     which must persist across pipeline iterations so the user can
//     press R again immediately after a restart completes;
//   - the restart loop, which re-runs runDevPipeline from scratch each
//     time the pipeline returns restart=true (i.e. the user pressed R);
//   - exit-time terminal restoration (deferred), so a panic does not
//     leave the user's terminal in raw mode.
//
// Tests that exercise the pipeline shape call runDev directly — under
// `go test`, os.Stdin is a pipe (not a TTY) so the keyboard layer
// auto-skips and runDev behaves exactly like the old single-pass body.
func runDev(flags devFlags) error {
	keySignals, restoreKB, _ := startKeyboardListener(os.Stdin, flags.noKeyboard)
	defer restoreKB()

	emitter := newDevEmitter(jsonOutput)

	for iter := 1; ; iter++ {
		if iter > 1 {
			emitter.Info(fmt.Sprintf("⟳ restarting from scratch (iteration %d)", iter))
		}
		restart, err := runDevPipeline(flags, keySignals, emitter)
		if err != nil {
			return err
		}
		if !restart {
			return nil
		}
	}
}

// runDevPipeline is the single-pass dev pipeline. Each call goes
// through every stage from .env load to Stage 8, then either exits
// (restart=false) or signals the outer loop to re-run (restart=true).
//
// Broken into clearly-named stages so the pipeline reads top-to-bottom.
// Each stage consults flags to decide whether to execute; each emits
// one or more events via the devEmitter so humans see status lines and
// agents see JSON events.
//
//nolint:gocognit,gocyclo // Linear pipeline; breaking it up would obscure the ordering invariants.
func runDevPipeline(flags devFlags, keySignals <-chan keyboardSignal, emitter devEmitter) (bool, error) {
	// --- Stage 0: load .env ------------------------------------------------
	// Re-loaded each iteration so editing .env and pressing R picks
	// up the new values without re-invoking gofasta dev from outside.
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
		return false, err
	}

	if flags.dryRun {
		printDevPlan(plan, emitter)
		return false, nil
	}

	// --- Stage 2: preflight ------------------------------------------------
	if plan.orchestrate {
		if !composeAvailableFn() {
			return false, clierr.New(clierr.CodeDevDockerUnavailable,
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
		if err := startServices(plan.services.selected, plan.profiles); err != nil {
			return false, clierr.Wrap(clierr.CodeDevServiceUnhealthy, err,
				"failed to start compose services")
		}

		if err := waitHealthy(plan.services.selected, plan.services.hasHealth, flags.waitTimeout,
			func(name, state string, elapsed time.Duration) {
				if strings.HasPrefix(state, "running/healthy") || state == "running/" {
					emitter.ServiceHealthy(name, elapsed)
				}
			}); err != nil {
			return false, clierr.Wrap(clierr.CodeDevServiceUnhealthy, err, err.Error())
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
	// Under --all-in-docker the app container's CMD already runs
	// migrate against db:5432 before starting Air (see
	// deployments/docker/dev.dockerfile.tmpl), so a host-side run would
	// be a wasteful double-attempt and would force the user to have
	// `migrate` on $PATH. Skip explicitly and emit a MigrateSkipped
	// event so the JSON consumer still sees the decision.
	if flags.allInDocker {
		emitter.MigrateSkipped("running inside the app container")
	} else if !flags.noMigrate {
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
	//
	// Under --all-in-docker the foreground stream IS the app container's
	// log stream (set up below in Stage 8), so an explicit --attach-logs
	// here would just duplicate work; warn and let Stage 8 own it.
	var sideCancels []func()
	if flags.attachLogs && plan.orchestrate && len(plan.services.selected) > 0 && !flags.allInDocker {
		sideCancels = append(sideCancels, startLogStreamer(plan.services.selected))
	}
	if flags.attachLogs && flags.allInDocker {
		emitter.Warn("--attach-logs is implicit under --all-in-docker (the app's logs are already in the foreground); pass --attach-logs to multiplex db/cache/queue alongside")
	}

	// --- Stage 8: Air ------------------------------------------------------
	port := configutil.GetPort()
	if flags.port != "" {
		port = flags.port
	}
	portInt, _ := strconv.Atoi(port)
	urls := airURLs(port)

	if flags.dashboard {
		sideCancels = append(sideCancels, startDashboard(flags.dashboardPort, portInt, &plan.services, emitter))
	}

	if plan.inDocker {
		// Containerized mode: the app container is already running
		// (Stage 4 brought it up with `compose up -d`), and Air is
		// running inside it. Tail its stdout so the developer sees
		// live hot-reload output exactly as they would on the host.
		// When --attach-logs is also set, multiplex every selected
		// service's logs (compose prefixes each line with the service
		// name) so the foreground shows the full picture.
		emitter.AirInDocker(portInt, urls)
		streamServices := []string{appServiceName}
		if flags.attachLogs {
			streamServices = plan.services.selected
		}
		streamerCancel := startLogStreamer(streamServices)
		sideCancels = append(sideCancels, streamerCancel)
		restart := runInDockerSupervisor(runTeardown, keySignals)
		for _, c := range sideCancels {
			c()
		}
		return restart, nil
	}

	emitter.Air(portInt, urls)
	restart, err := runAir(flags, runTeardown, keySignals)
	for _, c := range sideCancels {
		c()
	}
	return restart, err
}

// runInDockerSupervisor blocks until one of:
//
//   - SIGINT/SIGTERM (Ctrl+C from the terminal or `kill`)
//   - sigKeyboardQuit on keySignals (the user pressed Q)
//   - sigKeyboardRestart on keySignals (the user pressed R)
//
// Then calls teardown to stop the compose stack. Used by
// --all-in-docker mode in place of runAir — there's no host-side Air
// subprocess to babysit, so the supervisor just waits for the
// developer's signal and forwards shutdown to compose. Returns true
// only when the user pressed R, in which case the outer pipeline loop
// re-runs from scratch.
//
// Extracted into a named function so tests can drive it without racing
// the OS signal subsystem.
func runInDockerSupervisor(teardown func(string), keySignals <-chan keyboardSignal) bool {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	select {
	case <-sigChan:
		teardown("interrupted")
		return false
	case sig := <-keySignals:
		if sig == sigKeyboardRestart {
			teardown("restart")
			return true
		}
		teardown("quit")
		return false
	}
}

// devPlan is what resolveDevPlan builds before any side effect runs.
// Passed to both the dry-run printer and the real execution path, so
// both paths see an identical picture of what's about to happen.
type devPlan struct {
	orchestrate bool        // run the compose pipeline at all
	inDocker    bool        // --all-in-docker: app + Air run inside the app container
	profiles    []string    // compose profiles to activate (cache + queue auto-on, plus --profile)
	services    devServices // resolved service set (may be empty)
}

// resolveProfiles builds the list of compose profiles to activate for
// this run. cache + queue are auto-on so the existing --no-cache /
// --no-queue opt-outs do real work; the user-supplied --profile (if
// any) is merged in. Order is stable for deterministic test assertions
// and the resulting slice has duplicates removed.
func resolveProfiles(flags devFlags) []string {
	candidates := make([]string, 0, 3)
	if flags.profile != "" {
		candidates = append(candidates, flags.profile)
	}
	if !flags.noCache {
		candidates = append(candidates, "cache")
	}
	if !flags.noQueue {
		candidates = append(candidates, "queue")
	}
	seen := make(map[string]bool, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, p := range candidates {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func resolveDevPlan(flags devFlags) (devPlan, error) {
	// --all-in-docker is incompatible with several opt-outs; surface a
	// clean error before we touch the filesystem so the user sees the
	// conflict in the dry-run output too.
	if flags.allInDocker {
		if flags.noServices {
			return devPlan{}, clierr.New(clierr.CodeDevFlagConflict,
				"--all-in-docker and --no-services are mutually exclusive")
		}
		if flags.noDB {
			return devPlan{}, clierr.New(clierr.CodeDevFlagConflict,
				"--all-in-docker and --no-db are mutually exclusive (the in-container app needs the database)")
		}
		if !composeFileExists() {
			return devPlan{}, clierr.New(clierr.CodeDevComposeNotFound,
				"--all-in-docker requires a compose.yaml in the project root")
		}
	}

	// If the user opts out of orchestration entirely, or there's no
	// compose.yaml in sight, fall straight through to the Air-only path.
	if flags.noServices || !composeFileExists() {
		if len(flags.servicesList) > 0 && !composeFileExists() {
			return devPlan{}, clierr.New(clierr.CodeDevComposeNotFound,
				"no compose.yaml found but --services was set")
		}
		return devPlan{orchestrate: false}, nil
	}

	profiles := resolveProfiles(flags)
	available, hasHealth, err := detectComposeServices(profiles, flags.allInDocker)
	if err != nil {
		return devPlan{}, clierr.Wrap(clierr.CodeDevComposeNotFound, err,
			"could not read compose configuration")
	}

	if flags.allInDocker && !slices.Contains(available, appServiceName) {
		return devPlan{}, clierr.New(clierr.CodeDevFlagConflict,
			"--all-in-docker requires an `app` service in compose.yaml")
	}

	selected := resolveSelectedServices(available, flags)
	return devPlan{
		orchestrate: true,
		inDocker:    flags.allInDocker,
		profiles:    profiles,
		services: devServices{
			available: available,
			selected:  selected,
			profiles:  profiles,
			hasHealth: hasHealth,
		},
	}, nil
}

func printDevPlan(plan devPlan, emitter devEmitter) {
	if plan.orchestrate {
		emitter.Info(fmt.Sprintf("orchestrate=true in_docker=%t profiles=%v selected=%v",
			plan.inDocker, plan.profiles, plan.services.selected))
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
// removeAllFn is a package-level seam over os.RemoveAll so tests can
// exercise the post-RemoveAll error branch (in practice RemoveAll("tmp")
// rarely fails).
var removeAllFn = os.RemoveAll

func runAir(flags devFlags, teardown func(string), keySignals <-chan keyboardSignal) (bool, error) {
	if flags.rebuild {
		// tmp/ is Air's default build directory; projects that
		// customize .air.toml may use a different path, but clearing
		// the default is a safe best-effort.
		if err := removeAllFn("tmp"); err != nil {
			// A missing tmp dir is the expected state on first run.
			// Any other failure is non-fatal: Air will still run; the
			// developer just won't get the forced-rebuild guarantee.
			_ = err
		}
	}
	args := []string{"tool", "air"}

	if _, err := execLookPath("go"); err != nil {
		return false, clierr.New(clierr.CodeDevAirNotInstalled,
			"Go toolchain not on $PATH")
	}

	airCmd := execCommand("go", args...)
	airCmd.Stdout = os.Stdout
	airCmd.Stderr = os.Stderr
	// Deliberately do NOT pipe os.Stdin to Air. The keyboard listener
	// in dev_keyboard.go owns stdin (in raw mode) for r/q/h shortcuts,
	// and Air does not read stdin for any interactive feature — it
	// reloads on filesystem events. Letting Air keep stdin would either
	// fight the listener for bytes or, depending on terminal mode,
	// cause Air to burn CPU on EOF.

	// Inject GOFLAGS=-tags=devtools so Air's internal `go build`
	// compiles the scaffold's app/devtools/devtools_enabled.go file
	// (and excludes devtools_stub.go). Merges with any existing GOFLAGS
	// value in the environment so projects that rely on custom GOFLAGS
	// for other purposes aren't clobbered.
	//
	// Append to whatever Env the caller already set on the command
	// (tests use a fake exec helper that populates Env with subprocess
	// markers); starting from os.Environ() when no Env was set
	// preserves the regular pass-through behavior.
	if airCmd.Env == nil {
		airCmd.Env = os.Environ()
	}
	airCmd.Env = append(airCmd.Env, appendTag(os.Getenv("GOFLAGS"), "devtools"))

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// restartFlag is set by the signal-handler goroutine when the
	// caller pressed R. Read by the post-Run block to decide whether
	// to return restart=true and let the outer loop re-run the
	// pipeline.
	var restartFlag atomicBool
	go airSignalHandler(sigChan, keySignals, airCmd, teardown, &restartFlag)

	err := airCmd.Run()
	restart := restartFlag.Load()
	// Air exits non-zero when it receives SIGINT. Treat a signal-triggered
	// exit as a successful shutdown rather than a pipeline failure.
	if err != nil && airCmd.ProcessState != nil && airCmd.ProcessState.Exited() {
		if isSignaledExit(airCmd.ProcessState) {
			return restart, nil
		}
	}
	if err != nil {
		return restart, clierr.Wrap(clierr.CodeDevAirNotInstalled, err,
			"air exited with error")
	}
	return restart, nil
}

// isSignaledExit reports whether a process exited due to a signal.
// Extracted from runAir so tests can stub it.
var isSignaledExit = func(ps *os.ProcessState) bool {
	if ps == nil {
		return false
	}
	ws, ok := ps.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled()
}

// airSignalHandler is the goroutine body from runAir. Extracted into a
// named function so tests can drive it directly without racing the
// real air process.
//
// Multiplexes three kinds of input:
//
//   - sigChan (OS SIGINT/SIGTERM): SIGINT the Air process and tear
//     down. Pipeline exits cleanly.
//   - keySignals = sigKeyboardRestart: same as Ctrl+C semantics but
//     sets restartFlag so the outer loop re-runs the pipeline.
//   - keySignals = sigKeyboardQuit: same as Ctrl+C semantics, no
//     restart.
//
// restartFlag is the seam through which runAir learns whether the
// child's SIGINT was caused by the user pressing R; it is the cleanest
// way to communicate from a goroutine without wrapping the return path
// in extra channels.
func airSignalHandler(
	sigChan <-chan os.Signal,
	keySignals <-chan keyboardSignal,
	airCmd *exec.Cmd,
	teardown func(string),
	restartFlag *atomicBool,
) {
	select {
	case <-sigChan:
		if airCmd.Process != nil {
			_ = airCmd.Process.Signal(os.Interrupt)
		}
		teardown("interrupted")
	case sig := <-keySignals:
		if sig == sigKeyboardRestart {
			restartFlag.Store(true)
			if airCmd.Process != nil {
				_ = airCmd.Process.Signal(os.Interrupt)
			}
			teardown("restart")
			return
		}
		if airCmd.Process != nil {
			_ = airCmd.Process.Signal(os.Interrupt)
		}
		teardown("quit")
	}
}

// atomicBool is a tiny sync/atomic.Bool stand-in scoped to the
// runAir/airSignalHandler exchange. We keep our own type rather than
// pulling sync/atomic across the call sites so the goroutine
// interaction stays obvious in this file (and so runAir's signature
// does not pollute call sites with sync/atomic.Bool pointers).
type atomicBool struct {
	v atomic.Bool
}

// Store atomically writes b.
func (a *atomicBool) Store(b bool) { a.v.Store(b) }

// Load atomically returns the current value.
func (a *atomicBool) Load() bool { return a.v.Load() }

// appendTag merges a build tag into an existing GOFLAGS string. If the
// existing value already contains a -tags= fragment, we splice the new
// tag into it (comma-separated, no dupes). Otherwise we append a fresh
// -tags=<name> fragment. The returned string has the GOFLAGS= prefix
// and is suitable for dropping into os.Environ(). Kept generic (rather
// than hard-coded to "devtools") so future stages can layer on other
// tags — for instance, an observability-heavy `-tags=profiling` mode.
func appendTag(existing, tag string) string {
	// Normalize the incoming value. Both "GOFLAGS=..." and just "..."
	// variants come through the test helpers; we always return a
	// "GOFLAGS=..." string.
	val := strings.TrimPrefix(existing, "GOFLAGS=")

	if !strings.Contains(val, "-tags=") {
		if val == "" {
			return "GOFLAGS=-tags=" + tag
		}
		return "GOFLAGS=" + val + " -tags=" + tag
	}

	// Splice the tag into the existing -tags= fragment.
	parts := strings.Fields(val)
	for i, p := range parts {
		if !strings.HasPrefix(p, "-tags=") {
			continue
		}
		existingTags := strings.TrimPrefix(p, "-tags=")
		for _, t := range strings.Split(existingTags, ",") {
			if t == tag {
				return "GOFLAGS=" + val // already present
			}
		}
		parts[i] = "-tags=" + existingTags + "," + tag
		break
	}
	return "GOFLAGS=" + strings.Join(parts, " ")
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
