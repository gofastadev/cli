package commands

import (
	"net"
	"sync"
	"time"

	"github.com/gofastadev/cli/internal/commands/configutil"
)

// ─────────────────────────────────────────────────────────────────────
// Preflight — dep connectivity probes for `gofasta dev`.
//
// Three probes, all TCP-based:
//
//   - probeDatabase — TCP-connect to database.host:port. Skipped
//     entirely when database.driver is "sqlite"/"sqlite3" (file-
//     backed, no network endpoint).
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
//
// Why TCP-only for the DB: an earlier version used `migrate version`
// which double-checks "DSN parses AND schema_migrations table
// exists". That false-positive'd as "unreachable" the first time a
// fresh Postgres container came up via menu option [2], because the
// table doesn't exist yet — looping the menu forever. Real schema
// validation belongs in Stage 6's `migrate up`, not the preflight
// probe.
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

	// tcpDialFn is the seam over net.DialTimeout. Tests use httptest
	// listeners or canned errors to drive the three probe outcomes.
	tcpDialFn = net.DialTimeout

	// configBuildDatabaseEndpointFn / configBuildCacheEndpointFn /
	// configBuildQueueEndpointFn / configBuildMigrationURLFn — seams
	// over the configutil endpoint builders. configutil's real
	// implementations never return ("", true) for the *enabled* case
	// (host/port always fall back to localhost defaults), but the
	// probe functions defensively check for that shape. Tests use
	// these seams to drive the defensive branches.
	configBuildDatabaseEndpointFn = configutil.BuildDatabaseEndpoint
	configBuildCacheEndpointFn    = configutil.BuildCacheEndpoint
	configBuildQueueEndpointFn    = configutil.BuildQueueEndpoint
	configBuildMigrationURLFn     = configutil.BuildMigrationURL
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

// probeDatabase TCP-connects to the DB backend at the host:port
// resolved from config.yaml + env vars. File-backed drivers
// (sqlite/sqlite3) are reported as probeNotConfigured because they
// have no network endpoint — a TCP probe makes no sense for them.
//
// We intentionally do NOT use `migrate version` as the probe. That
// command requires the target database to exist AND a
// schema_migrations table to be present, both of which are FALSE
// the first time a Postgres container comes up via menu option [2].
// Using it as a reachability check caused the menu to loop forever
// after option [2] succeeded: the DB was up, but `migrate version`
// returned exit 1 because the schema_migrations table didn't exist
// yet, so the menu re-classified the DB as unreachable.
//
// A plain TCP dial answers the question the menu actually asks: "is
// the DB accepting connections?". Stage 6 of the pipeline then runs
// the real `migrate up`, which creates the database/schema as
// needed.
//
// The displayed Endpoint stays the full migration URL so error
// messages remain copy-pasteable and the developer immediately sees
// user/database/driver. The reachability decision is independent of
// that string.
func probeDatabase() probeResult {
	endpoint, enabled := configBuildDatabaseEndpointFn()
	if !enabled {
		return probeResult{Dep: "database", Status: probeNotConfigured}
	}
	if endpoint == "" {
		return probeResult{
			Dep:    "database",
			Status: probeUnreachable,
			Reason: "database configuration is incomplete",
		}
	}
	display := configBuildMigrationURLFn()
	if display == "" {
		display = endpoint
	}
	if err := tcpProbe(endpoint); err != nil {
		return probeResult{
			Dep:      "database",
			Status:   probeUnreachable,
			Endpoint: display,
			Reason:   err.Error(),
		}
	}
	return probeResult{Dep: "database", Status: probeOK, Endpoint: display}
}

// probeCache TCP-connects to the cache backend (Redis) at the host:port
// resolved from config.yaml. Skipped via probeNotConfigured when the
// app uses an in-memory cache.
func probeCache() probeResult {
	endpoint, enabled := configBuildCacheEndpointFn()
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
	endpoint, enabled := configBuildQueueEndpointFn()
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
