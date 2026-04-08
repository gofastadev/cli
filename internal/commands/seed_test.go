package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSeedCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "seed" {
			found = true
			break
		}
	}
	assert.True(t, found, "seedCmd should be registered on rootCmd")
}

func TestSeedCmd_HasFreshFlag(t *testing.T) {
	f := seedCmd.Flags().Lookup("fresh")
	assert.NotNil(t, f, "seedCmd should have --fresh flag")
	assert.Equal(t, "false", f.DefValue)
}
