package commands

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gofastadev/cli/internal/commands/configutil"
)

// ─────────────────────────────────────────────────────────────────────
// Preflight — dep connectivity probes for `gofasta dev`.
//
// Three probes:
//
//   - probeDatabase — runs `migrate -database <url> version` (same
//     shape `gofasta doctor` uses). Verifies the DSN parses, the host
//     is reachable, the credentials work, and the schema_migrations
//     table is accessible. Slower than a TCP-only probe but catches
//     real config errors (wrong password, wrong db name) that TCP
//     can't see.
//
//   - probeCache — TCP-connect to cache.redis.host:port. Skipped
//     entirely when cache.driver is "memory" or empty.
//
//   - probeQueue — TCP-connect to queue.redis.host:port. Skipped
//     entirely when queue.enabled is false.
//
// Each probe returns a probeResult; the three are aggregated by
// runPreflight and handed to the interactive menu when any probe
// reports unreachable.
// ─────────────────────────────────────────────────────────────────────

// probeStatus is the verdict of a single preflight probe. Three states:
//
//   - probeOK: the dep is reachable and ready to use.
//   - probeUnreachable: the dep is configured but cannot be contacted.
//     This is the case that triggers the interactive recovery menu.
//   - probeNotConfigured: the app explicitly opted out of this dep
//     (e.g. cache.driver=memory). Silently skipped — no menu, no warning.
type probeStatus int

const (
	probeOK probeStatus = iota
	probeUnreachable
	probeNotConfigured
)

// probeResult is one preflight probe's outcome. Plain struct so JSON
// emitters and the interactive menu can both consume it without an
// extra adapter layer.
type probeResult struct {
	Dep      string      // "database" | "cache" | "queue" — stable identifiers used by the menu
	Status   probeStatus // see probeStatus constants
	Endpoint string      // host:port or DSN being probed; empty for probeNotConfigured
	Reason   string      // human-readable cause for unreachable; empty otherwise
}

// preflightTCPTimeout is the per-TCP-probe timeout. Short enough to
// not noticeably slow down `gofasta dev` startup; long enough that a
// slow DNS resolution doesn't false-positive as unreachable.
const preflightTCPTimeout = 2 * time.Second

// ── Package-level seams ─────────────────────────────────────────────
//
// Each probe goes through a function pointer so tests can substitute
// deterministic implementations without a real DB, Redis, or
// network. Production assigns the real probe at init; _test.go
// reassigns and restores via t.Cleanup.

var (
	probeDatabaseFn = probeDatabase
	probeCacheFn    = probeCache
	probeQueueFn    = probeQueue

	// migrateProbeRunner is the seam over the actual `migrate version`
	// invocation. Production uses execCommand("migrate", ...) under the
	// hood; tests swap to a function returning canned outcomes.
	migrateProbeRunner = runMigrateVersionProbe

	// tcpDialFn is the seam over net.DialTimeout. Tests use httptest
	// listeners or canned errors to drive the three probe outcomes.
	tcpDialFn = net.DialTimeout
)

// runPreflight runs the three probes in parallel and returns their
// aggregated results in stable Dep-name order: database, cache, queue.
// Parallelism keeps total preflight latency bounded by the slowest
// probe (typically the migrate version call, ~50–200ms).
func runPreflight() []probeResult {
	results := make([]probeResult, 3)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); results[0] = probeDatabaseFn() }()
	go func() { defer wg.Done(); results[1] = probeCacheFn() }()
	go func() { defer wg.Done(); results[2] = probeQueueFn() }()
	wg.Wait()
	return results
}

// probeDatabase resolves the DSN via configutil.BuildMigrationURL()
// (already used by doctor + dev's migrate step) and runs `migrate
// version` against it. An empty DSN is treated as not-configured
// because the scaffold always sets one; if it's empty the user has
// explicitly stripped the config.
func probeDatabase() probeResult {
	dsn := configutil.BuildMigrationURL()
	if dsn == "" {
		return probeResult{Dep: "database", Status: probeNotConfigured}
	}
	if err := migrateProbeRunner(dsn); err != nil {
		return probeResult{
			Dep:      "database",
			Status:   probeUnreachable,
			Endpoint: dsn,
			Reason:   err.Error(),
		}
	}
	return probeResult{Dep: "database", Status: probeOK, Endpoint: dsn}
}

// runMigrateVersionProbe shells out to `migrate -database <dsn>
// version`. Returns nil on success; the wrapped error on failure
// includes migrate's stderr so the menu can surface "connection
// refused" / "auth failed" / etc.
func runMigrateVersionProbe(dsn string) error {
	cmd := execCommand("migrate", "-path", "db/migrations", "-database", dsn, "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("migrate version: %w", err)
	}
	return nil
}

// probeCache TCP-connects to the cache backend (Redis) at the host:port
// resolved from config.yaml. Skipped via probeNotConfigured when the
// app uses an in-memory cache.
func probeCache() probeResult {
	endpoint, enabled := configutil.BuildCacheEndpoint()
	if !enabled {
		return probeResult{Dep: "cache", Status: probeNotConfigured}
	}
	if endpoint == "" {
		return probeResult{
			Dep:    "cache",
			Status: probeUnreachable,
			Reason: "cache configuration is incomplete",
		}
	}
	if err := tcpProbe(endpoint); err != nil {
		return probeResult{
			Dep:      "cache",
			Status:   probeUnreachable,
			Endpoint: endpoint,
			Reason:   err.Error(),
		}
	}
	return probeResult{Dep: "cache", Status: probeOK, Endpoint: endpoint}
}

// probeQueue TCP-connects to the queue's Redis backend. Skipped via
// probeNotConfigured when queue.enabled is false.
func probeQueue() probeResult {
	endpoint, enabled := configutil.BuildQueueEndpoint()
	if !enabled {
		return probeResult{Dep: "queue", Status: probeNotConfigured}
	}
	if endpoint == "" {
		return probeResult{
			Dep:    "queue",
			Status: probeUnreachable,
			Reason: "queue configuration is incomplete",
		}
	}
	if err := tcpProbe(endpoint); err != nil {
		return probeResult{
			Dep:      "queue",
			Status:   probeUnreachable,
			Endpoint: endpoint,
			Reason:   err.Error(),
		}
	}
	return probeResult{Dep: "queue", Status: probeOK, Endpoint: endpoint}
}

// tcpProbe is the shared TCP-dial primitive for cache + queue. We
// don't issue a Redis PING because a TCP-only check answers the
// question the preflight cares about ("is anything listening on this
// port?") without taking on the redis client dependency, and Redis
// rejects unknown protocols cleanly so a misconfigured port produces
// the right error class either way.
func tcpProbe(endpoint string) error {
	conn, err := tcpDialFn("tcp", endpoint, preflightTCPTimeout)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

// hasUnreachable reports whether any probe came back as unreachable.
// Convenience for the pipeline — if false, preflight passes silently
// and Air proceeds; if true, the interactive menu runs.
func hasUnreachable(results []probeResult) bool {
	for _, r := range results {
		if r.Status == probeUnreachable {
			return true
		}
	}
	return false
}
