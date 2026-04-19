package commands

import (
	"os"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "config" {
			found = true
			break
		}
	}
	assert.True(t, found, "configCmd should be registered on rootCmd")
}

func TestConfigSchemaCmd_Registered(t *testing.T) {
	found := false
	for _, c := range configCmd.Commands() {
		if c.Name() == "schema" {
			found = true
			break
		}
	}
	assert.True(t, found, "configSchemaCmd should be a subcommand of configCmd")
}

func TestConfigCmd_HasGroup(t *testing.T) {
	assert.Equal(t, groupWorkflow, configCmd.GroupID,
		"configCmd should be in the development-workflow group")
}

// TestRunConfigSchema_FailsWhenHelperMissing — outside a gofasta project
// the cmd/schema/ directory won't exist; the command must fail with a
// structured CodeNotGofastaProject error pointing the user at the root.
func TestRunConfigSchema_FailsWhenHelperMissing(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	err := runConfigSchema()
	require.Error(t, err)
	ce, ok := clierr.As(err)
	require.True(t, ok, "expected clierr.Error")
	assert.Equal(t, string(clierr.CodeNotGofastaProject), ce.Code)
	assert.Contains(t, ce.Hint, "gofasta project")
}
