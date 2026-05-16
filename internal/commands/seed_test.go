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

// TestSeedCmd_RunE_Success — `go run ./app/main seed` exits 0; the
// success line fires in text mode. Without --fresh.
func TestSeedCmd_RunE_Success(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	assert.NoError(t, seedCmd.RunE(seedCmd, nil))
}

// TestSeedCmd_RunE_Failure — child exits non-zero; the fail line
// fires in text mode and the error is returned.
func TestSeedCmd_RunE_Failure(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 1)
	err := seedCmd.RunE(seedCmd, nil)
	assert.Error(t, err)
}

// TestSeedCmd_RunE_FreshFlag — --fresh appends the flag to the child
// command's args (the fresh-branch of the text-mode announcement is
// also exercised).
func TestSeedCmd_RunE_FreshFlag(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	require := func(b bool) {
		t.Helper()
		if !b {
			t.Fatal("flag set failed")
		}
	}
	require(seedCmd.Flags().Set("fresh", "true") == nil)
	t.Cleanup(func() { _ = seedCmd.Flags().Set("fresh", "false") })
	assert.NoError(t, seedCmd.RunE(seedCmd, nil))
}
