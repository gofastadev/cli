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
//  2. Filesystem detection — if config.yaml doesn't carry a value (a
//     project scaffolded before this feature shipped, or a config.yaml
//     that was hand-written), look for `app/models/`. Its presence is
//     the signature of the layered layout; absence implies feature.
//
//  3. Fall back to Layered as a safe default. Layered has always been
//     the only output and remains the default for `gofasta new` without
//     a flag.

package layout

import (
	"os"

	"github.com/gofastadev/cli/internal/commands/configutil"
)

// Detect resolves the layout for the project rooted at the current
// working directory. See the package comment for the resolution order.
func Detect() Layout {
	switch configutil.ReadLayout() {
	case "feature":
		return For(Feature)
	case "layered":
		return For(Layered)
	}
	// No explicit value in config.yaml — fall back to filesystem.
	if _, err := os.Stat("app/models"); err == nil {
		return For(Layered)
	}
	// app/models/ is absent but we have no positive feature signal
	// either. Default Layered — the historical behavior — so an empty or
	// unrelated directory doesn't suddenly route to a layout that won't
	// produce a working project.
	return For(Layered)
}
