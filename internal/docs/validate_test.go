package docs

import (
	"os"
	"path/filepath"
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

func TestValidateInvocations_BareDashIgnored(t *testing.T) {
	// A lone "-" trims to an empty flag name, which is skipped.
	assert.Empty(t, problems("gofasta", "dev", "-"))
}

func TestValidateInvocations_InheritedShorthand(t *testing.T) {
	f := factsFixture()
	f.GlobalFlags = append(f.GlobalFlags, Flag{Name: "output", Shorthand: "o", Persistent: true})
	got := ValidateInvocations(f, []Invocation{{File: "doc.md", Line: 1, Tokens: []string{"gofasta", "dev", "-o"}}})
	assert.Empty(t, got, "shorthand declared on an inherited flag must resolve")
}

func TestCheckGoreleaserPlatforms_MissingAndMalformedFile(t *testing.T) {
	dir := t.TempDir()

	got := CheckGoreleaserPlatforms(dir, []string{"linux"}, []string{"amd64"})
	require.Len(t, got, 1)
	assert.Contains(t, got[0], "reading .goreleaser.yaml")

	require.NoError(t, os.WriteFile(filepath.Join(dir, ".goreleaser.yaml"),
		[]byte("builds:\n  - main: ./cmd/gofasta\n"), 0o644))
	got = CheckGoreleaserPlatforms(dir, []string{"linux"}, []string{"amd64"})
	require.Len(t, got, 2, "both goos and goarch lists are unlocatable")
	for _, p := range got {
		assert.Contains(t, p, "could not locate")
	}
}

func TestYamlListItems_KeyWithoutItems(t *testing.T) {
	_, err := yamlListItems("goos:\nnot a dash list\n", "goos")
	assert.ErrorContains(t, err, "could not locate list items")
}

func TestEqualStringSets(t *testing.T) {
	assert.True(t, equalStringSets([]string{"linux", "darwin"}, []string{"darwin", "linux"}))
	assert.False(t, equalStringSets([]string{"linux"}, []string{"linux", "darwin"}), "length mismatch")
	assert.False(t, equalStringSets([]string{"linux", "darwin"}, []string{"linux", "freebsd"}), "same length, different members")
}

func TestCheckLintVersionParity_Fixtures(t *testing.T) {
	writeRepo := func(t *testing.T, makefile, ci string) string {
		t.Helper()
		dir := t.TempDir()
		if makefile != "" {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makefile), 0o644))
		}
		if ci != "" {
			require.NoError(t, os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".github", "workflows", "ci.yml"), []byte(ci), 0o644))
		}
		return dir
	}

	got := CheckLintVersionParity(writeRepo(t, "", ""))
	require.Len(t, got, 1)
	assert.Contains(t, got[0], "reading Makefile")

	got = CheckLintVersionParity(writeRepo(t, "GOLANGCI_LINT_VERSION := v2.1.0\n", ""))
	require.Len(t, got, 1)
	assert.Contains(t, got[0], "reading ci.yml")

	got = CheckLintVersionParity(writeRepo(t, "all: build\n", "jobs:\n  lint:\n    with:\n      version: v2.1.0\n"))
	require.Len(t, got, 1)
	assert.Contains(t, got[0], "could not locate GOLANGCI_LINT_VERSION in Makefile")

	got = CheckLintVersionParity(writeRepo(t, "GOLANGCI_LINT_VERSION := v2.1.0\n", "jobs:\n  lint:\n    steps: []\n"))
	require.Len(t, got, 1)
	assert.Contains(t, got[0], "could not locate golangci-lint version in ci.yml")

	got = CheckLintVersionParity(writeRepo(t, "GOLANGCI_LINT_VERSION := v2.1.0\n", "jobs:\n  lint:\n    with:\n      version: v2.2.0\n"))
	require.Len(t, got, 1)
	assert.Contains(t, got[0], "golangci-lint version drift")

	assert.Empty(t, CheckLintVersionParity(writeRepo(t,
		"GOLANGCI_LINT_VERSION := v2.1.0\n", "jobs:\n  lint:\n    with:\n      version: v2.1.0\n")))
}
