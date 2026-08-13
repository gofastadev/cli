// detect.go — runtime layout resolution for generators.
//
// Generators call Detect() once per invocation to learn which layout the
// current project uses. Priority order:
//
//  1. configutil.ReadLayout() — the authoritative source. `gofasta new`
//     writes `project.layout: layered|feature` to config.yaml at scaffold
//     time, so any project created with a layout-aware CLI version has
//     this field set.
//
//  2. Filesystem fallback — if config.yaml doesn't carry a value (a
//     project scaffolded before this feature shipped, or a config.yaml
//     that was hand-written), we default to Layered. There is only a
//     positive signal for the layered layout (`app/models/`); without a
//     recognized signal we stay on Layered — the historical behavior — so
//     an empty or unrelated directory never routes to a layout that
//     wouldn't produce a working project.

package layout

import (
	"os"

	"github.com/gofastadev/cli/internal/commands/configutil"
)

// Detect resolves the layout for the project rooted at the current
// working directory. See the package comment for the resolution order.
func Detect() Layout {
	// config.yaml is authoritative when it carries an explicit value;
	// ParseKind owns the string→Kind mapping so it lives in exactly one place.
	if s := configutil.ReadLayout(); s != "" {
		return For(ParseKind(s))
	}
	// No explicit value in config.yaml — fall back to the filesystem. We only
	// recognize a positive signal for the layered layout (app/models/); any
	// other shape defaults to Layered so an empty or unrelated directory never
	// routes to a layout that wouldn't produce a working project.
	if _, err := os.Stat("app/models"); err == nil {
		return For(Layered)
	}
	return For(Layered)
}

// HasGraphQLArtifacts reports whether the project rooted at the current
// working directory has GraphQL enabled: a gqlgen.yml or an
// app/graphql/resolvers/ directory. REST-only projects have neither.
//
// Shared by `gofasta refactor` (to decide whether the GraphQL phase
// applies) and the generators (to default --graphql on/off from
// project state rather than requiring the flag on every command).
func HasGraphQLArtifacts() bool {
	if _, err := os.Stat("gqlgen.yml"); err == nil {
		return true
	}
	if fi, err := os.Stat("app/graphql/resolvers"); err == nil && fi.IsDir() {
		return true
	}
	return false
}
