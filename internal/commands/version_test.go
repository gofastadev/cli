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

func TestDisplayVersion(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"v0.1.4", "v0.1.4"}, // already prefixed — pass through
		{"0.1.4", "v0.1.4"},  // bare semver — add leading v
		{"v1.2.3-rc.1", "v1.2.3-rc.1"},
		{"dev", "dev"},         // unversioned dev build — no v
		{"(devel)", "(devel)"}, // go build output — pass through
		{"", ""},               // empty — pass through
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, displayVersion(tc.in), "input=%q", tc.in)
	}
}
