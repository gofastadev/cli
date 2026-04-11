package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConsoleCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "console" {
			found = true
			break
		}
	}
	assert.True(t, found, "consoleCmd should be registered on rootCmd")
}

func TestConsoleCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, consoleCmd.Short)
	assert.NotEmpty(t, consoleCmd.Long)
}
