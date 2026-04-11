package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeployCmd_IsRegistered(t *testing.T) {
	cmds := rootCmd.Commands()
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Name())
	}
	assert.Contains(t, names, "deploy", "rootCmd should have deploy subcommand")
}

func TestDeployCmd_HasSubcommands(t *testing.T) {
	cmds := deployCmd.Commands()
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Name())
	}

	expected := []string{"setup", "status", "logs", "rollback"}
	for _, exp := range expected {
		assert.Contains(t, names, exp, "deployCmd should have subcommand: %s", exp)
	}
}

func TestDeployCmd_HasFlags(t *testing.T) {
	flags := deployCmd.Flags()

	assert.NotNil(t, flags.Lookup("host"), "deploy should have --host flag")
	assert.NotNil(t, flags.Lookup("method"), "deploy should have --method flag")
	assert.NotNil(t, flags.Lookup("port"), "deploy should have --port flag")
	assert.NotNil(t, flags.Lookup("path"), "deploy should have --path flag")
	assert.NotNil(t, flags.Lookup("arch"), "deploy should have --arch flag")
	assert.NotNil(t, flags.Lookup("dry-run"), "deploy should have --dry-run flag")
}

func TestDeployCmd_HasDescriptions(t *testing.T) {
	assert.NotEmpty(t, deployCmd.Short)
	assert.NotEmpty(t, deployCmd.Long)
	assert.NotEmpty(t, deploySetupCmd.Short)
	assert.NotEmpty(t, deployStatusCmd.Short)
	assert.NotEmpty(t, deployLogsCmd.Short)
	assert.NotEmpty(t, deployRollbackCmd.Short)
}
