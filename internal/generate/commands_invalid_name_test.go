package generate

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratorCommands_RejectInvalidResourceName covers the buildFromArgs
// error return in every generator subcommand.
//
// Each of these names flows into file paths, package names, and SQL identifiers,
// so validateIdentifier is the single gate that stops a hostile or malformed
// argument before any of that happens. The check exists once in buildFromArgs,
// but every subcommand has to actually propagate its error — a command that
// swallowed it would carry on and generate files from an unvalidated name.
func TestGeneratorCommands_RejectInvalidResourceName(t *testing.T) {
	commands := map[string]*cobra.Command{
		"scaffold":       scaffoldCmd,
		"model":          modelCmd,
		"repository":     repositoryCmd,
		"service":        serviceCmd,
		"controller":     controllerCmd,
		"dto":            dtoCmd,
		"migration":      migrationCmd,
		"route":          routeCmd,
		"resolver":       resolverCmd,
		"job":            jobCmd,
		"email-template": emailTemplateCmd,
		"task":           taskCmd,
		"provider":       providerCmd,
	}

	// Names that must never reach a generator: path traversal, shell
	// metacharacters, and shapes that are not Go identifiers.
	badNames := []string{
		"../escape",
		"9leading-digit",
		"has space",
		"semi;colon",
		"",
	}

	for name, cmd := range commands {
		t.Run(name, func(t *testing.T) {
			for _, bad := range badNames {
				t.Run(bad, func(t *testing.T) {
					setupTempProject(t)
					err := cmd.RunE(cmd, []string{bad})
					require.Error(t, err, "generator %q accepted invalid name %q", name, bad)
					assert.Contains(t, err.Error(), "invalid name")
				})
			}
		})
	}
}
