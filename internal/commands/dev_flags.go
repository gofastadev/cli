package commands

import (
	"strings"
	"time"
)

// devFlags collects every CLI flag the dev command recognizes. Flags are
// defined once on the cobra command and then resolved into this struct
// at the top of runDev so the orchestration logic can treat them as a
// plain Go value, not a collection of package-level globals.
type devFlags struct {
	// What runs in Docker. Anything in servicesList runs in compose;
	// everything else runs on host (with Air). Empty list = host-only.
	// If servicesList contains "app", the app runs in a foreground
	// container instead of on host.
	servicesRaw  string   // raw `--services` value before parsing ("db,cache" or "all")
	servicesList []string // resolved list of compose services to start

	// Pipeline opt-outs that survived the host-first redesign. Removed
	// flags: --no-services, --no-db, --no-cache, --no-queue (replaced
	// by "default to empty servicesList"); the legacy --all-in-docker
	// is preserved as a deprecated alias that rewrites to
	// --services=all in runDev's prelude.
	noMigrate  bool // skip running migrate up
	noTeardown bool // leave compose services running on exit

	// noDB is set when the user picks menu option [3] from the
	// preflight. Migrations + seeds skip. The scaffold's ProvideDB
	// falls back to an in-memory SQLite stub so the app still boots;
	// DB-touching endpoints return 5xx (no schema, no persistence).
	noDB bool

	// Volume + profile + Air knobs (unchanged from previous design).
	keepVolumes   bool          // deprecated — default is already "keep", kept for discoverability
	fresh         bool          // drop + recreate volumes before starting
	profile       string        // docker compose --profile
	waitTimeout   time.Duration // healthcheck polling timeout
	envFile       string        // path to .env file to load
	port          string        // override PORT env var
	rebuild       bool          // force Air to do a rebuild cycle before serving
	seed          bool          // run seeders after migrations
	dryRun        bool          // print the plan and exit
	attachLogs    bool          // stream `docker compose logs -f` alongside Air
	dashboard     bool          // start the local dev dashboard on dashboardPort
	dashboardPort int           // debug port for the dev dashboard (default 9090)

	// Deprecated alias for `--services all`. Kept for one release with
	// a warning emitted on use; remove next release.
	allInDocker bool

	// Disable the interactive keyboard layer (r/q/h). Used when stdin
	// is needed elsewhere or in tests where raw mode is undesirable.
	noKeyboard bool
}

// parseServicesList splits a comma-separated string into a non-empty
// slice of trimmed service names. Returns nil for an empty input.
func parseServicesList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
