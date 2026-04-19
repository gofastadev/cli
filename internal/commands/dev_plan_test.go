package commands

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for dev.go plan-resolution + version-detection helpers.
// These sit between the cobra command and the exec boundary, so
// fakeExec + chdir-to-temp give full control without touching docker.
// ─────────────────────────────────────────────────────────────────────

// TestResolveDevPlan_NoServices — --no-services disables the compose
// pipeline regardless of compose.yaml existence.
func TestResolveDevPlan_NoServices(t *testing.T) {
	chdirTemp(t)
	plan, err := resolveDevPlan(devFlags{noServices: true})
	require.NoError(t, err)
	assert.False(t, plan.orchestrate)
}

// TestResolveDevPlan_NoComposeFile — no compose.yaml → orchestrate
// false with no error.
func TestResolveDevPlan_NoComposeFile(t *testing.T) {
	chdirTemp(t)
	plan, err := resolveDevPlan(devFlags{})
	require.NoError(t, err)
	assert.False(t, plan.orchestrate)
}

// TestResolveDevPlan_ServicesWithoutCompose — user supplied
// --services=a,b,c but there's no compose.yaml → clierr.
func TestResolveDevPlan_ServicesWithoutCompose(t *testing.T) {
	chdirTemp(t)
	_, err := resolveDevPlan(devFlags{servicesList: []string{"db"}})
	require.Error(t, err)
}

// TestResolveDevPlan_HappyPath — compose.yaml present; detectComposeServices
// returns two services. Plan includes both.
func TestResolveDevPlan_HappyPath(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	fakeExecOutput(t, `{"services":{"db":{"healthcheck":{"test":["CMD","pg_isready"]}},"cache":{}}}`, 0)
	plan, err := resolveDevPlan(devFlags{})
	require.NoError(t, err)
	assert.True(t, plan.orchestrate)
	assert.ElementsMatch(t, []string{"db", "cache"}, plan.services.available)
}

// TestResolveDevPlan_DetectFails — docker compose config exits
// non-zero; resolveDevPlan surfaces a clierr.
func TestResolveDevPlan_DetectFails(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	withFakeExec(t, 1)
	_, err := resolveDevPlan(devFlags{})
	require.Error(t, err)
}

// TestPrintDevPlan_Orchestrate — orchestrate=true branch.
func TestPrintDevPlan_Orchestrate(t *testing.T) {
	emitter := &quietEmitter{}
	printDevPlan(devPlan{
		orchestrate: true,
		services:    devServices{selected: []string{"db"}, profile: "cache"},
	}, emitter)
	assert.Greater(t, emitter.info.Load(), int32(0))
}

// TestPrintDevPlan_NoOrchestrate — orchestrate=false branch.
func TestPrintDevPlan_NoOrchestrate(t *testing.T) {
	emitter := &quietEmitter{}
	printDevPlan(devPlan{orchestrate: false}, emitter)
	assert.Greater(t, emitter.info.Load(), int32(0))
}

// TestDetectVersions_HappyPath — docker + compose both print their
// versions via scripted stdout. detectVersions returns the first
// non-empty line of each.
func TestDetectVersions_HappyPath(t *testing.T) {
	fakeExecOutput(t, "28.0.1\n", 0)
	docker, compose := detectVersions()
	// Both invocations share the same fake, so both get "28.0.1".
	assert.Equal(t, "28.0.1", docker)
	assert.Equal(t, "28.0.1", compose)
}

// TestDetectVersions_Failure — docker exits non-zero → "unknown"
// for both. captureVersionLine returns "" which detectVersions
// rewrites to "unknown".
func TestDetectVersions_Failure(t *testing.T) {
	withFakeExec(t, 1)
	docker, compose := detectVersions()
	assert.Equal(t, "unknown", docker)
	assert.Equal(t, "unknown", compose)
}

// TestDetectVersions_EmptyStdout — exits 0 but prints nothing →
// "unknown".
func TestDetectVersions_EmptyStdout(t *testing.T) {
	fakeExecOutput(t, "", 0)
	docker, compose := detectVersions()
	assert.Equal(t, "unknown", docker)
	assert.Equal(t, "unknown", compose)
}
