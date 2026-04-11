package commands

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDbCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "db" {
			found = true
			break
		}
	}
	assert.True(t, found, "dbCmd should be registered on rootCmd")
}

func TestDbResetCmd_Registered(t *testing.T) {
	subCmds := dbCmd.Commands()
	names := make([]string, 0, len(subCmds))
	for _, c := range subCmds {
		names = append(names, c.Name())
	}
	assert.Contains(t, names, "reset")
}

func TestDbResetCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, dbResetCmd.Short)
	assert.NotEmpty(t, dbResetCmd.Long)
}

func TestDbResetCmd_HasSkipSeedFlag(t *testing.T) {
	flag := dbResetCmd.Flags().Lookup("skip-seed")
	assert.NotNil(t, flag)
	assert.Equal(t, "false", flag.DefValue)
}

func TestRunDBReset_NoConfig(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	err := runDBReset(false)
	// Should fail because migrate binary is not available or DB unreachable
	assert.Error(t, err)
}
