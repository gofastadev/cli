package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "version" {
			found = true
			break
		}
	}
	assert.True(t, found, "versionCmd should be registered on rootCmd")
}

func TestVersionCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, versionCmd.Short)
	assert.NotEmpty(t, versionCmd.Long)
}

func TestRunVersion_NoError(t *testing.T) {
	err := runVersion()
	assert.NoError(t, err)
}
