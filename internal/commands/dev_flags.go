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
	// Orchestration opt-outs.
	noServices    bool          // skip compose orchestration entirely
	noDB          bool          // skip DB-like services (postgres, mysql, …)
	noCache       bool          // skip cache-like services (redis, valkey, …)
	noQueue       bool          // skip queue-like services (asynq, nats, …)
	noMigrate     bool          // skip running migrate up
	noTeardown    bool          // leave compose services running on exit
	keepVolumes   bool          // deprecated — default is already "keep", kept for discoverability
	fresh         bool          // drop + recreate volumes before starting
	servicesList  []string      // explicit list of services to start (overrides detection)
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
