package commands

import (
	"fmt"
	"io"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/commands/symbolresolve"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

// xrefsCmd is `gofasta xrefs <symbol>` — find every reference to a Go
// symbol across the current module. Type-aware (uses go/packages), so
// it survives renames, aliasing, and method-set resolution that pure
// grep can't.
var xrefsCmd = &cobra.Command{
	Use:   "xrefs <symbol>",
	Short: "Find every reference to a Go symbol across the current module",
	Long: `Type-aware reverse lookup of a Go symbol. Loads every package in
the module with full type info, then collects every use-site of the
target symbol — far more accurate than a string grep, since it respects
imports, aliasing, and method-set resolution.

Symbol syntax (the most specific form that disambiguates):

  Pkg.Func             — package-level func / var / const / type
  Pkg.Type.Method      — method on a type
  Name                 — unqualified (ambiguous matches return
                         AMBIGUOUS_SYMBOL with the list of packages)

Examples:

  gofasta xrefs irodata/app/services/interfaces.UserServiceInterface.Create
  gofasta xrefs UserController                       # bare name
  gofasta xrefs irodata/app/services.UserService     # fully qualified
  gofasta xrefs UserService --json | jq '.references[] | .file'

JSON output:

  {
    "symbol": "...",
    "package": "irodata/app/services/interfaces",
    "kind": "method",
    "definition": { "file": "...", "line": 42, "column": 2, "kind": "decl" },
    "references": [ { "file": "...", "line": 117, "column": 18, "in_func": "...", "kind": "call" } ],
    "count": 17
  }`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		report, err := symbolresolve.LookupReferences(args[0])
		if err != nil {
			return err
		}
		cliout.Print(report, func(w io.Writer) { printXrefsText(w, report) })
		return nil
	},
}

func init() {
	rootCmd.AddCommand(xrefsCmd)
}

func printXrefsText(w io.Writer, r symbolresolve.SymbolReport) {
	_, _ = fmt.Fprintf(w, "%s %s (%s)\n",
		termcolor.CBrand(r.Kind), r.Symbol, r.Package)
	if r.Definition != nil {
		_, _ = fmt.Fprintf(w, "  defined at %s:%d:%d\n",
			r.Definition.File, r.Definition.Line, r.Definition.Column)
	}
	if r.Count == 0 {
		_, _ = fmt.Fprintln(w, "  no references found")
		return
	}
	_, _ = fmt.Fprintf(w, "  %d reference(s):\n", r.Count)
	for _, ref := range r.References {
		fn := ""
		if ref.InFunc != "" {
			fn = " — in " + ref.InFunc
		}
		_, _ = fmt.Fprintf(w, "    %s:%d:%d (%s)%s\n",
			ref.File, ref.Line, ref.Column, ref.Kind, fn)
	}
}
