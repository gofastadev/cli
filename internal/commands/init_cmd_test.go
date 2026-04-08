package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInitCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "init" {
			found = true
			break
		}
	}
	assert.True(t, found, "initCmd should be registered on rootCmd")
}

func TestInitCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, initCmd.Short)
	assert.NotEmpty(t, initCmd.Long)
}
