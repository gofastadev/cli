package commands

import (
	"fmt"
	"io"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/commands/symbolresolve"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

// impactCmd is `gofasta impact <file-or-package>` — return the
// transitive closure of packages that depend on the target.
var impactCmd = &cobra.Command{
	Use:   "impact <file-or-package>",
	Short: "Show every package that depends (directly + transitively) on the target",
	Long: `Reverse-dependency analysis at the package level. Loads every
package in the module and walks the import graph in reverse from the
target to produce:

  • direct_importers      — packages that import the target directly
  • transitive_importers  — every package that depends on it indirectly
  • impacted_files        — every Go source file in those packages

Target can be:

  • a file path  (e.g. app/services/order.service.go)
  • an import path (e.g. irodata/app/services)

Useful before a signature change ("what breaks if I change this?"),
when scoping a code review, or when deciding which packages a CI run
must re-test.

Examples:

  gofasta impact app/services/order.service.go
  gofasta impact irodata/app/services --json | jq '.direct_importers'
  gofasta impact app/dtos/order.dtos.go`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		report, err := impactGraphFn(args[0])
		if err != nil {
			return err
		}
		cliout.Print(report, func(w io.Writer) { printImpactText(w, report) })
		return nil
	},
}

// impactGraphFn is a package-level seam over symbolresolve.ImpactGraph
// so tests can return a canned ImpactReport without standing up a
// loadable Go module.
var impactGraphFn = symbolresolve.ImpactGraph

func init() {
	rootCmd.AddCommand(impactCmd)
}

func printImpactText(w io.Writer, r symbolresolve.ImpactReport) {
	_, _ = fmt.Fprintf(w, "%s %s\n", termcolor.CBrand("Target:"), r.Target)
	if r.Package != "" {
		_, _ = fmt.Fprintf(w, "Package: %s\n\n", r.Package)
	}
	if len(r.DirectImporters) == 0 && len(r.TransitiveImporters) == 0 {
		_, _ = fmt.Fprintln(w, "No packages depend on this target.")
		return
	}
	if len(r.DirectImporters) > 0 {
		_, _ = fmt.Fprintf(w, "Direct importers (%d):\n", len(r.DirectImporters))
		for _, p := range r.DirectImporters {
			_, _ = fmt.Fprintf(w, "  · %s\n", p)
		}
		_, _ = fmt.Fprintln(w)
	}
	if len(r.TransitiveImporters) > 0 {
		_, _ = fmt.Fprintf(w, "Transitive importers (%d):\n", len(r.TransitiveImporters))
		for _, p := range r.TransitiveImporters {
			_, _ = fmt.Fprintf(w, "  · %s\n", p)
		}
		_, _ = fmt.Fprintln(w)
	}
	if len(r.ImpactedFiles) > 0 {
		_, _ = fmt.Fprintf(w, "Impacted files (%d) — pass to `gofasta verify --since=<ref>` for a scoped check.\n",
			len(r.ImpactedFiles))
	}
}
