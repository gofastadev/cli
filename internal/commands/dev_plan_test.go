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
		profiles:    []string{"cache"},
		services:    devServices{selected: []string{"db"}, profiles: []string{"cache"}},
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

// TestResolveDevPlan_AllInDockerWithoutCompose — --all-in-docker set
// but no compose.yaml → CodeDevComposeNotFound.
func TestResolveDevPlan_AllInDockerWithoutCompose(t *testing.T) {
	chdirTemp(t)
	_, err := resolveDevPlan(devFlags{allInDocker: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compose.yaml")
}

// TestResolveDevPlan_AllInDockerNoServicesConflict — mutual-exclusion
// guard: --all-in-docker + --no-services is incoherent.
func TestResolveDevPlan_AllInDockerNoServicesConflict(t *testing.T) {
	chdirTemp(t)
	_, err := resolveDevPlan(devFlags{allInDocker: true, noServices: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// TestResolveDevPlan_AllInDockerNoDBConflict — the in-container app
// needs the db; --no-db with --all-in-docker is rejected.
func TestResolveDevPlan_AllInDockerNoDBConflict(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	_, err := resolveDevPlan(devFlags{allInDocker: true, noDB: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// TestResolveDevPlan_AllInDockerLocalReplace — a filesystem-path
// replace in go.mod (the common cross-repo dev case) cannot be
// resolved inside the docker build context. Detect it before docker
// compose runs and surface a clear message naming the offending
// module + path, rather than letting buildkit emit
// "reading /foo/go.mod: no such file or directory".
func TestResolveDevPlan_AllInDockerLocalReplace(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))

	origFn := findLocalReplacesFn
	t.Cleanup(func() { findLocalReplacesFn = origFn })
	findLocalReplacesFn = func(_ string) ([]localReplace, error) {
		return []localReplace{
			{Module: "github.com/example/foo", Path: "../../foo"},
		}, nil
	}

	_, err := resolveDevPlan(devFlags{allInDocker: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "filesystem-path replace")
	assert.Contains(t, err.Error(), "github.com/example/foo")
	assert.Contains(t, err.Error(), "../../foo")
}

// TestResolveDevPlan_AllInDockerWithoutAppService — compose.yaml has
// no `app` service → CodeDevFlagConflict so the user gets a clear
// "your compose.yaml is missing the app service" message instead of a
// silent empty-services run.
func TestResolveDevPlan_AllInDockerWithoutAppService(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	fakeExecOutput(t, `{"services":{"db":{}}}`, 0)
	_, err := resolveDevPlan(devFlags{allInDocker: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "`app` service")
}

// TestResolveDevPlan_DefaultProfilesIncludeCacheAndQueue — the
// behavior change at the heart of this work: default mode auto-on
// activates the cache + queue compose profiles. Without this, the
// existing --no-cache / --no-queue flags can never do real work.
func TestResolveDevPlan_DefaultProfilesIncludeCacheAndQueue(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	fakeExecOutput(t, `{"services":{"db":{},"cache":{},"queue":{}}}`, 0)
	plan, err := resolveDevPlan(devFlags{})
	require.NoError(t, err)
	assert.Contains(t, plan.profiles, "cache")
	assert.Contains(t, plan.profiles, "queue")
}

// TestResolveDevPlan_NoCacheRemovesCacheProfile — opt-out for cache
// drops the cache profile but leaves queue alone.
func TestResolveDevPlan_NoCacheRemovesCacheProfile(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	fakeExecOutput(t, `{"services":{"db":{},"queue":{}}}`, 0)
	plan, err := resolveDevPlan(devFlags{noCache: true})
	require.NoError(t, err)
	assert.NotContains(t, plan.profiles, "cache")
	assert.Contains(t, plan.profiles, "queue")
}

// TestResolveDevPlan_NoQueueRemovesQueueProfile — symmetric.
func TestResolveDevPlan_NoQueueRemovesQueueProfile(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	fakeExecOutput(t, `{"services":{"db":{},"cache":{}}}`, 0)
	plan, err := resolveDevPlan(devFlags{noQueue: true})
	require.NoError(t, err)
	assert.Contains(t, plan.profiles, "cache")
	assert.NotContains(t, plan.profiles, "queue")
}

// TestResolveDevPlan_UserProfileMergesWithDefaults — user's --profile
// is merged with the auto-on cache + queue. Order matters for
// deterministic assertions; ElementsMatch covers it.
func TestResolveDevPlan_UserProfileMergesWithDefaults(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	fakeExecOutput(t, `{"services":{"db":{},"cache":{},"queue":{},"observability":{}}}`, 0)
	plan, err := resolveDevPlan(devFlags{profile: "observability"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"observability", "cache", "queue"}, plan.profiles)
}

// TestResolveDevPlan_UserProfileDuplicatesDeduped — user passes
// --profile cache; default also adds cache; the result has cache only
// once.
func TestResolveDevPlan_UserProfileDuplicatesDeduped(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	fakeExecOutput(t, `{"services":{"db":{},"cache":{}}}`, 0)
	plan, err := resolveDevPlan(devFlags{profile: "cache"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"cache", "queue"}, plan.profiles)
}
