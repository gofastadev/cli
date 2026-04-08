package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDevCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "dev" {
			found = true
			break
		}
	}
	assert.True(t, found, "devCmd should be registered on rootCmd")
}

func TestDevCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, devCmd.Short)
	assert.NotEmpty(t, devCmd.Long)
}
