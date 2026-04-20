package commands

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for deploy.go runDeploy / runDeployStatus / runDeployLogs
// error branches. LoadDeployConfig requires a deploy.yaml or flags,
// so we drive it with a bare cobra.Command that has no flags set →
// expected error.
// ─────────────────────────────────────────────────────────────────────

// TestRunDeploy_NoConfig — blank cmd → LoadDeployConfig errors →
// propagates out.
func TestRunDeploy_NoConfig(t *testing.T) {
	chdirTemp(t)
	cmd := &cobra.Command{}
	err := runDeploy(cmd)
	require.Error(t, err)
}

// TestRunDeployStatus_NoConfig — same as above.
func TestRunDeployStatus_NoConfig(t *testing.T) {
	chdirTemp(t)
	cmd := &cobra.Command{}
	err := runDeployStatus(cmd)
	require.Error(t, err)
}

// TestRunDeployLogs_NoConfig — same as above.
func TestRunDeployLogs_NoConfig(t *testing.T) {
	chdirTemp(t)
	cmd := &cobra.Command{}
	err := runDeployLogs(cmd)
	require.Error(t, err)
}

var _ = assert.Equal // silence unused import

// TestRunDeploy_UnknownMethodCoverage — uses the deployMethodOverride
// seam to force the switch's default branch. LoadDeployConfig would
// normally reject "bogus" before runDeploy reached the switch.
func TestRunDeploy_UnknownMethodCoverage(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("go.mod",
		[]byte("module example.com/t\n\ngo 1.25.0\n"), 0o644))
	require.NoError(t, os.WriteFile("config.yaml",
		[]byte("deploy:\n  host: user@example.com\n  method: docker\n"), 0o644))
	cmd := &cobra.Command{}
	cmd.Flags().String("host", "", "")
	cmd.Flags().String("method", "", "")
	cmd.Flags().Int("port", 0, "")
	cmd.Flags().String("path", "", "")
	cmd.Flags().String("arch", "", "")
	cmd.Flags().Bool("dry-run", false, "")
	_ = cmd.Flags().Set("dry-run", "true")
	deployMethodOverride = "bogus"
	t.Cleanup(func() { deployMethodOverride = "" })
	err := runDeploy(cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown deploy method")
}
