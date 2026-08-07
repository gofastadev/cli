package docs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// factsFixture builds a miniature command tree resembling the real CLI.
func factsFixture() Facts {
	return Facts{
		GlobalFlags: []Flag{
			{Name: "json", Persistent: true},
			{Name: "no-banner", Persistent: true},
		},
		Commands: []Command{
			{
				Name: "new", Path: "gofasta new",
				Flags: []Flag{{Name: "driver"}, {Name: "layout"}, {Name: "graphql"}},
			},
			{
				Name: "generate", Path: "gofasta generate", Aliases: []string{"g"},
				Flags: []Flag{{Name: "dry-run", Persistent: true}},
				Children: []Command{
					{Name: "scaffold", Path: "gofasta generate scaffold", Flags: []Flag{{Name: "graphql"}, {Name: "no-verify"}}},
				},
			},
			{
				Name: "dev", Path: "gofasta dev",
				Flags: []Flag{{Name: "services"}, {Name: "fresh"}},
			},
			{
				Name: "test", Path: "gofasta test",
				Flags: []Flag{{Name: "coverage", Shorthand: "c"}, {Name: "verbose", Shorthand: "v"}},
			},
		},
	}
}

func problems(tokens ...string) []string {
	return ValidateInvocations(factsFixture(), []Invocation{{File: "doc.md", Line: 7, Tokens: tokens}})
}

func TestValidateInvocations_HappyPaths(t *testing.T) {
	assert.Empty(t, problems("gofasta", "new", "myapp", "--driver", "mysql"))
	assert.Empty(t, problems("gofasta", "g", "scaffold", "Product", "name:string", "--graphql"))
	assert.Empty(t, problems("gofasta", "generate", "scaffold", "Product"))
	assert.Empty(t, problems("gofasta", "dev", "--services", "db,cache"))
	assert.Empty(t, problems("gofasta", "--json", "dev"))
	// Flags before a subcommand must not stop path descent.
	assert.Empty(t, problems("gofasta", "--json", "g", "scaffold", "Product", "--dry-run"))
	assert.Empty(t, problems("gofasta", "dev", "--services=all"))
	assert.Empty(t, problems("gofasta", "test", "-c"))
	assert.Empty(t, problems("gofasta", "test", "-cv"))
}

func TestValidateInvocations_UnknownRootCommand(t *testing.T) {
	got := problems("gofasta", "bogus")
	require.Len(t, got, 1)
	assert.Contains(t, got[0], `doc.md:7: unknown command "bogus"`)
}

func TestValidateInvocations_UnknownFlag(t *testing.T) {
	got := problems("gofasta", "dev", "--no-services")
	require.Len(t, got, 1)
	assert.Contains(t, got[0], "flag --no-services does not exist on `gofasta dev`")

	got = problems("gofasta", "test", "-x")
	require.Len(t, got, 1)
	assert.Contains(t, got[0], "shorthand -x does not exist")
}

func TestValidateInvocations_InheritedPersistentFlag(t *testing.T) {
	// --dry-run is persistent on generate, so scaffold accepts it.
	assert.Empty(t, problems("gofasta", "g", "scaffold", "Product", "--dry-run"))
}

func TestValidateInvocations_PositionalArgsBelowRootAllowed(t *testing.T) {
	// "Product" and "name:string" are positionals, not subcommands.
	assert.Empty(t, problems("gofasta", "g", "scaffold", "Product", "name:string", "price:float"))
}

func TestValidateInvocations_PlaceholdersSkipChecks(t *testing.T) {
	assert.Empty(t, problems("gofasta", "g", "scaffold", "<ResourceName>", "[field:type", "...]"))
	assert.Empty(t, problems("gofasta", "new", "<name>", "--<flag>"))
	assert.Empty(t, problems("gofasta", "<command>", "--anything"))
}

func TestValidateInvocations_DoubleDashStopsChecking(t *testing.T) {
	assert.Empty(t, problems("gofasta", "dev", "--", "--not-a-real-flag"))
}

func TestValidateInvocations_UniversalCobraFlags(t *testing.T) {
	assert.Empty(t, problems("gofasta", "--help"))
	assert.Empty(t, problems("gofasta", "-v"))
	assert.Empty(t, problems("gofasta", "dev", "-h"))
	// --version is root-only.
	got := problems("gofasta", "dev", "--version")
	require.Len(t, got, 1)
}

func TestCheckLintVersionParity_RealRepo(t *testing.T) {
	assert.Empty(t, CheckLintVersionParity("../.."))
}

func TestCheckGoreleaserPlatforms_RealRepo(t *testing.T) {
	assert.Empty(t, CheckGoreleaserPlatforms("../..",
		[]string{"linux", "darwin", "windows"}, []string{"amd64", "arm64"}))

	got := CheckGoreleaserPlatforms("../..",
		[]string{"linux", "darwin"}, []string{"amd64", "arm64"})
	require.Len(t, got, 1)
	assert.Contains(t, got[0], "goos")
}
