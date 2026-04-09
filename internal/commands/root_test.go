package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRootCmd_HasSubcommands(t *testing.T) {
	cmds := rootCmd.Commands()
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Name())
	}

	expectedCmds := []string{"new", "dev", "init", "migrate", "seed", "serve", "swagger", "generate", "wire", "upgrade", "version", "db", "doctor", "routes", "console"}
	for _, expected := range expectedCmds {
		assert.Contains(t, names, expected, "rootCmd should have subcommand: %s", expected)
	}
}

func TestRootCmd_UseIsGofasta(t *testing.T) {
	assert.Equal(t, "gofasta", rootCmd.Use)
}

func TestRootCmd_HasShortDescription(t *testing.T) {
	assert.NotEmpty(t, rootCmd.Short)
}

func TestExecute_UnknownCommand(t *testing.T) {
	// rootCmd.Execute() calls os.Exit on error, which we can't test directly.
	// Instead, test that rootCmd returns an error for unknown subcommands.
	rootCmd.SetArgs([]string{"nonexistent-command"})
	err := rootCmd.Execute()
	assert.Error(t, err)
	// Reset args
	rootCmd.SetArgs(nil)
}

func TestRootCmd_HasLongDescription(t *testing.T) {
	assert.NotEmpty(t, rootCmd.Long)
}
