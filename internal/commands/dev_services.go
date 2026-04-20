package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────
// Service orchestration for `gofasta dev`.
//
// The dev command brings up the full local environment — database,
// cache, queue — via docker compose, waits for healthchecks, runs
// migrations, and then starts Air for hot reload. This file owns the
// "docker compose" side of that pipeline: detect services, resolve
// which ones to start, start them, poll for health, and tear them
// down on exit.
// ─────────────────────────────────────────────────────────────────────

// composeFile is the canonical scaffolded compose file. docker compose
// auto-discovers `compose.yaml` in the current directory, so we don't
// need to pass it explicitly — but we do need to check for its
// existence for the "no compose file" short-circuit.
const composeFile = "compose.yaml"

// appServiceName is the service name inside compose.yaml that represents
// the application binary. gofasta dev always runs the app on the host
// (for fast host-side Air hot reload), so this service is explicitly
// excluded from the orchestrated set.
const appServiceName = "app"

// defaultWaitTimeout is how long we poll compose healthchecks before
// giving up. Postgres typically reports healthy within 2–4 seconds;
// 30s is generous enough for slow laptops or Docker-starting-cold.
const defaultWaitTimeout = 30 * time.Second

// devServices holds resolved orchestration configuration for one run
// of `gofasta dev`. Built once in runDev and passed down; never mutated
// after construction.
type devServices struct {
	available []string        // every service in compose.yaml except the app
	selected  []string        // services we'll actually start (post-flag resolution)
	profile   string          // docker compose --profile value, empty if not set
	hasHealth map[string]bool // per-service: does compose.yaml define a healthcheck?
}

// composeAvailableFn is a package-level seam over composeAvailable so
// tests can simulate docker being absent without clobbering PATH.
var composeAvailableFn = composeAvailable

// composeAvailable returns true when `docker compose` is both on PATH
// and the daemon is reachable. Used by preflight to decide between the
// orchestrated path and the "just run Air" fallback.
func composeAvailable() bool {
	if _, err := execLookPath("docker"); err != nil {
		return false
	}
	// `docker info` is the canonical daemon-reachability probe. It fails
	// quickly (no retry loop, no long network timeouts) when the daemon
	// is down — ideal for preflight.
	cmd := execCommand("docker", "info")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// composeFileExists reports whether the canonical compose file is in the
// project root. Projects without one fall back to "just run Air".
func composeFileExists() bool {
	_, err := os.Stat(composeFile)
	return err == nil
}

// detectComposeServices returns every service name declared in
// compose.yaml (minus the app service), plus a per-service flag
// indicating whether it declares a healthcheck block. Uses
// `docker compose config --format json` so we get the fully-resolved
// configuration including merged overrides and applied profiles.
func detectComposeServices(profile string) (available []string, hasHealth map[string]bool, err error) {
	args := []string{"compose"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	args = append(args, "config", "--format", "json")

	cmd := execCommand("docker", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("docker compose config: %w", err)
	}

	var parsed struct {
		Services map[string]struct {
			Healthcheck *struct {
				Test any `json:"test"`
			} `json:"healthcheck"`
		} `json:"services"`
	}
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		return nil, nil, fmt.Errorf("parsing compose config: %w", err)
	}

	hasHealth = make(map[string]bool, len(parsed.Services))
	for name, svc := range parsed.Services {
		if name == appServiceName {
			continue
		}
		available = append(available, name)
		hasHealth[name] = svc.Healthcheck != nil
	}
	return available, hasHealth, nil
}

// resolveSelectedServices applies the flag rules to an available-services
// list and returns the services that should actually be started.
//
// Resolution order (highest priority first):
//  1. --no-services        → start nothing
//  2. --services=a,b,c     → start exactly these (overrides --no-db etc.)
//  3. default              → start everything in `available` minus --no-* filters
func resolveSelectedServices(available []string, flags devFlags) []string {
	if flags.noServices {
		return nil
	}
	if len(flags.servicesList) > 0 {
		// Trust the explicit list but still filter out `app` if the user
		// accidentally included it (dev always runs app on host).
		result := make([]string, 0, len(flags.servicesList))
		for _, s := range flags.servicesList {
			if s == appServiceName {
				continue
			}
			result = append(result, s)
		}
		return result
	}

	filtered := make([]string, 0, len(available))
	for _, s := range available {
		if flags.noDB && isDBLike(s) {
			continue
		}
		if flags.noCache && isCacheLike(s) {
			continue
		}
		if flags.noQueue && isQueueLike(s) {
			continue
		}
		filtered = append(filtered, s)
	}
	return filtered
}

// isDBLike / isCacheLike / isQueueLike apply simple name-based matching
// so the --no-db, --no-cache, --no-queue flags don't require the user
// to know the exact service names the scaffold used. The heuristics are
// intentionally narrow to avoid false positives in user-authored
// compose files.
func isDBLike(name string) bool {
	n := strings.ToLower(name)
	return n == "db" || n == "database" || n == "postgres" || n == "mysql" ||
		n == "mariadb" || n == "clickhouse" || strings.HasSuffix(n, "-db")
}

func isCacheLike(name string) bool {
	n := strings.ToLower(name)
	return n == "cache" || n == "redis" || n == "valkey" ||
		strings.HasSuffix(n, "-cache")
}

func isQueueLike(name string) bool {
	n := strings.ToLower(name)
	return n == "queue" || n == "asynq" || n == "nats" || n == "rabbitmq" ||
		strings.HasSuffix(n, "-queue")
}

// startServices runs `docker compose up -d <names>`. Returns the combined
// stderr output on failure so the caller can surface it to the user.
func startServices(names []string, profile string) error {
	if len(names) == 0 {
		return nil
	}
	args := []string{"compose"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	args = append(args, "up", "-d")
	args = append(args, names...)

	cmd := execCommand("docker", args...)
	var errBuf bytes.Buffer
	cmd.Stdout = nil
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose up: %w\n%s", err, errBuf.String())
	}
	return nil
}

// serviceState is the runtime state of a single compose service as
// reported by `docker compose ps --format json`. Only the fields we
// branch on are declared.
type serviceState struct {
	Name   string `json:"Service"`
	State  string `json:"State"`  // "running", "exited", etc.
	Health string `json:"Health"` // "healthy", "unhealthy", "starting", "" (no healthcheck)
}

// queryServiceStates returns the current runtime state of every service
// currently known to compose in this project. Used by waitHealthy to
// poll progress toward "healthy".
func queryServiceStates() ([]serviceState, error) {
	cmd := execCommand("docker", "compose", "ps", "--format", "json")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker compose ps: %w", err)
	}

	// `docker compose ps --format json` returns either a JSON array
	// (newer compose versions) or one JSON object per line (older ones).
	// Handle both by trying array first, then line-by-line.
	raw := bytes.TrimSpace(out.Bytes())
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] == '[' {
		var states []serviceState
		if err := json.Unmarshal(raw, &states); err != nil {
			return nil, fmt.Errorf("parsing compose ps (array): %w", err)
		}
		return states, nil
	}

	var states []serviceState
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var s serviceState
		if err := json.Unmarshal(line, &s); err != nil {
			return nil, fmt.Errorf("parsing compose ps (line): %w", err)
		}
		states = append(states, s)
	}
	return states, nil
}

// isServiceReady reports whether a service's current state counts as
// "ready to accept traffic" for our purposes:
//
//   - healthy             → ready (explicit healthcheck passing)
//   - running + no check  → ready (nothing to wait on)
//   - starting            → not ready yet (keep polling)
//   - anything else       → not ready
//
// Services without a healthcheck block rely entirely on the "running"
// state. This is a weaker guarantee than a real healthcheck but better
// than blocking indefinitely on a service the compose file never
// declared health for.
func isServiceReady(st serviceState, declaredHealth bool) bool {
	if declaredHealth {
		return st.Health == "healthy"
	}
	return st.State == "running"
}

// waitHealthy polls queryServiceStates until every named service
// returns true from isServiceReady, or the timeout elapses. progress is
// called once per service state transition so callers can stream human
// or JSON output as each service comes up.
//
//nolint:gocognit,gocyclo // One cohesive polling loop; splitting would obscure the timeout/deadline invariants.
func waitHealthy(
	names []string,
	hasHealth map[string]bool,
	timeout time.Duration,
	progress func(name, state string, elapsed time.Duration),
) error {
	if len(names) == 0 {
		return nil
	}

	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}

	start := time.Now()
	deadline := start.Add(timeout)
	lastState := make(map[string]string, len(names))

	for {
		states, err := queryServiceStates()
		if err != nil {
			return err
		}

		allReady := true
		seen := make(map[string]bool, len(names))
		for _, st := range states {
			if !wanted[st.Name] {
				continue
			}
			seen[st.Name] = true
			key := st.State + "/" + st.Health
			if lastState[st.Name] != key && progress != nil {
				progress(st.Name, key, time.Since(start))
				lastState[st.Name] = key
			}
			if !isServiceReady(st, hasHealth[st.Name]) {
				allReady = false
			}
		}
		// A service that hasn't shown up in `ps` yet counts as not-ready.
		for name := range wanted {
			if !seen[name] {
				allReady = false
			}
		}

		if allReady {
			return nil
		}
		if time.Now().After(deadline) {
			var stuck []string
			for name := range wanted {
				if lastState[name] != "running/healthy" && lastState[name] != "running/" {
					stuck = append(stuck, name)
				}
			}
			return fmt.Errorf("services did not become healthy within %s: %s",
				timeout, strings.Join(stuck, ", "))
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// stopServices runs `docker compose stop <names>`. Preserves volumes
// (so the next `gofasta dev` reuses the already-primed database). For
// full destruction use resetVolumes followed by startServices.
func stopServices(names []string) error {
	if len(names) == 0 {
		return nil
	}
	args := append([]string{"compose", "stop"}, names...)
	cmd := execCommand("docker", args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// resetVolumes runs `docker compose down -v` to delete all named
// volumes attached to the project. Called only when `--fresh` is set —
// it wipes the DB contents and forces the next startup to re-run every
// migration from scratch.
func resetVolumes() error {
	cmd := execCommand("docker", "compose", "down", "-v")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}
