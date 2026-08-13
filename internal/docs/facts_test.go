package docs

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCommandFacts(t *testing.T) {
	root := &cobra.Command{Use: "gofasta"}
	root.PersistentFlags().Bool("json", false, "machine output")

	child := &cobra.Command{Use: "dev [flags]", Short: "Run dev", Aliases: []string{"d"}, GroupID: "workflow"}
	child.Flags().String("services", "", "compose services\nacross lines")
	child.PersistentFlags().Bool("verbose", false, "chatty")
	grandchild := &cobra.Command{Use: "dashboard", Short: "Dash"}
	child.AddCommand(grandchild)

	hidden := &cobra.Command{Use: "secret", Hidden: true}
	root.AddGroup(&cobra.Group{ID: "workflow", Title: "Workflow:"})
	root.AddCommand(child, hidden)

	cmds := BuildCommandFacts(root)
	require.Len(t, cmds, 1, "hidden commands must be excluded")

	dev := cmds[0]
	assert.Equal(t, "dev", dev.Name)
	assert.Equal(t, "gofasta dev", dev.Path)
	assert.Equal(t, []string{"d"}, dev.Aliases)
	assert.Equal(t, "workflow", dev.Group)
	require.Len(t, dev.Children, 1)
	assert.Equal(t, "gofasta dev dashboard", dev.Children[0].Path)
	assert.Empty(t, dev.Children[0].Group, "group is top-level only")

	byName := map[string]Flag{}
	for _, f := range dev.Flags {
		byName[f.Name] = f
	}
	require.Contains(t, byName, "services")
	require.Contains(t, byName, "verbose")
	assert.False(t, byName["services"].Persistent)
	assert.True(t, byName["verbose"].Persistent)
	assert.Equal(t, "compose services across lines", byName["services"].Usage, "usage must be one line")
	assert.NotContains(t, byName, "help")
}

func TestGlobalFlagFacts(t *testing.T) {
	root := &cobra.Command{Use: "gofasta"}
	root.PersistentFlags().BoolP("json", "j", false, "machine output")

	flags := GlobalFlagFacts(root)
	require.Len(t, flags, 1)
	assert.Equal(t, "json", flags[0].Name)
	assert.Equal(t, "j", flags[0].Shorthand)
	assert.Equal(t, "bool", flags[0].Type)
	assert.True(t, flags[0].Persistent)
}

func TestCommandFlags_HelpFlagExcluded(t *testing.T) {
	c := &cobra.Command{Use: "thing", Short: "Thing"}
	c.Flags().Bool("verbose", false, "chatty")
	// Execute lazily registers --help on every command; simulate that so the
	// skip branch in commandFlags is exercised.
	c.InitDefaultHelpFlag()

	flags := commandFlags(c)
	names := make([]string, 0, len(flags))
	for _, f := range flags {
		names = append(names, f.Name)
	}
	assert.Contains(t, names, "verbose")
	assert.NotContains(t, names, "help", "the implicit --help flag must be excluded")
}
