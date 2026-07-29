// Coverage for config.go — the host and app-name validation that keeps
// untrusted config.yaml values out of an ssh argv.

package deploy

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadDeployConfig_RejectsUnsafeHost covers the deployHostPattern branch.
// A host is interpolated into an ssh argv, so a value starting with "-" would
// be read by ssh as an option — "-oProxyCommand=..." executes a local command
// (CVE-2017-1000117 class). config.yaml can come from a cloned repo, so this
// rejection is a security boundary, not input tidiness.
func TestLoadDeployConfig_RejectsUnsafeHost(t *testing.T) {
	unsafe := map[string]string{
		"leading dash option":  "-oProxyCommand=touch /tmp/pwned",
		"shell metacharacter":  "server.com; rm -rf /",
		"command substitution": "$(whoami)@server.com",
		"space separated":      "server.com extra",
		"backtick":             "`id`",
	}

	for name, host := range unsafe {
		t.Run(name, func(t *testing.T) {
			chdirProject(t, "deploy:\n  host: \""+host+"\"\n  method: docker\n")
			_, err := LoadDeployConfig(nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid deploy.host")
		})
	}
}

// TestLoadDeployConfig_AcceptsValidHosts is the counterpart, so the pattern
// cannot be tightened into rejecting ordinary hosts without a test failing.
func TestLoadDeployConfig_AcceptsValidHosts(t *testing.T) {
	for _, host := range []string{"server.com", "user@server.com", "10.0.0.1", "deploy-1.eu-west.example.com"} {
		t.Run(host, func(t *testing.T) {
			chdirProject(t, "deploy:\n  host: \""+host+"\"\n  method: docker\n")
			cfg, err := LoadDeployConfig(nil)
			require.NoError(t, err)
			assert.Equal(t, host, cfg.Host)
		})
	}
}

// TestLoadDeployConfig_RejectsUnsafeAppName covers the deployAppNamePattern
// branch. The app name is derived from go.mod's module path and lands in image
// tags and remote command strings, so the same argument-injection reasoning
// applies to a hostile module line.
func TestLoadDeployConfig_RejectsUnsafeAppName(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	// The last path segment becomes the app name.
	require.NoError(t, os.WriteFile("go.mod", []byte("module github.com/test/app;rm -rf /\n\ngo 1.21\n"), 0o644))
	require.NoError(t, os.WriteFile("config.yaml", []byte("deploy:\n  host: server.com\n  method: docker\n"), 0o644))

	_, err = LoadDeployConfig(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid app name")
}
