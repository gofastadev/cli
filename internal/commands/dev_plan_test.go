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

// TestResolveDevPlan_NoServicesByDefault — under the host-first model
// the empty --services list (the default) means no compose orchestration
// runs at all; the app is expected to run on host with Air.
func TestResolveDevPlan_NoServicesByDefault(t *testing.T) {
	chdirTemp(t)
	plan, err := resolveDevPlan(devFlags{})
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

// TestResolveDevPlan_HappyPath — compose.yaml present, --services=db,cache
// names two services that exist in compose.yaml. Plan includes both
// and orchestrate=true.
func TestResolveDevPlan_HappyPath(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	fakeExecOutput(t, `{"services":{"db":{"healthcheck":{"test":["CMD","pg_isready"]}},"cache":{}}}`, 0)
	plan, err := resolveDevPlan(devFlags{
		servicesList: []string{"db", "cache"},
		servicesRaw:  "db,cache",
	})
	require.NoError(t, err)
	assert.True(t, plan.orchestrate)
	assert.ElementsMatch(t, []string{"db", "cache"}, plan.services.available)
}

// TestResolveDevPlan_DetectFails — docker compose config exits
// non-zero; resolveDevPlan surfaces a clierr. We pass --services to
// trip the compose-config call (the empty-services path doesn't call
// compose at all and exits cleanly).
func TestResolveDevPlan_DetectFails(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	withFakeExec(t, 1)
	_, err := resolveDevPlan(devFlags{
		servicesList: []string{"db"},
		servicesRaw:  "db",
	})
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

// TestResolveDevPlan_ServicesWithoutCompose — --services set but no
// compose.yaml → CodeDevComposeNotFound. The error gates the user
// against running with a broken config.
func TestResolveDevPlan_ServicesWithoutCompose(t *testing.T) {
	chdirTemp(t)
	_, err := resolveDevPlan(devFlags{servicesList: []string{"db"}, servicesRaw: "db"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compose.yaml")
}

// TestResolveDevPlan_AppInServicesLocalReplace — when `app` is in
// --services (foreground container mode), a filesystem-path replace
// in go.mod is invisible to the docker build context. Surface the
// pre-emptive error after compose-services lookup but before any
// docker build runs.
func TestResolveDevPlan_AppInServicesLocalReplace(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	fakeExecOutput(t, `{"services":{"app":{},"db":{}}}`, 0)

	origFn := findLocalReplacesFn
	t.Cleanup(func() { findLocalReplacesFn = origFn })
	findLocalReplacesFn = func(_ string) ([]localReplace, error) {
		return []localReplace{
			{Module: "github.com/example/foo", Path: "../../foo"},
		}, nil
	}

	_, err := resolveDevPlan(devFlags{
		servicesList: []string{"db", "app"},
		servicesRaw:  "db,app",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "filesystem-path replace")
	assert.Contains(t, err.Error(), "github.com/example/foo")
}

// TestResolveDevPlan_UnknownServiceErrors — --services <name> where
// the name isn't declared in compose.yaml returns
// CodeDevServiceUnknown with a clear listing of valid names.
func TestResolveDevPlan_UnknownServiceErrors(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	fakeExecOutput(t, `{"services":{"app":{},"db":{},"lavinmq":{}}}`, 0)
	_, err := resolveDevPlan(devFlags{
		servicesList: []string{"lavinmw"},
		servicesRaw:  "lavinmw",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"lavinmw"`)
	assert.Contains(t, err.Error(), "lavinmq")
}

// TestResolveDevPlan_ServicesAllExpands — `--services all` resolves
// to every service compose.yaml declares, with the canonical
// "app in services means in-docker mode" inference flowing through.
func TestResolveDevPlan_ServicesAllExpands(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	fakeExecOutput(t, `{"services":{"app":{},"db":{},"cache":{}}}`, 0)
	plan, err := resolveDevPlan(devFlags{
		servicesList: []string{"app", "db", "cache"},
		servicesRaw:  "all",
	})
	require.NoError(t, err)
	assert.True(t, plan.inDocker, "app present in services → in-docker mode")
	assert.ElementsMatch(t, []string{"app", "db", "cache"}, plan.services.selected)
}

// TestResolveDevPlan_ServicesAppExclusiveInferred — when `app` is the
// only entry, in-docker mode is true; the supporting service set is
// empty (the user has an external db they're pointing at).
func TestResolveDevPlan_ServicesAppExclusiveInferred(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	fakeExecOutput(t, `{"services":{"app":{},"db":{}}}`, 0)
	plan, err := resolveDevPlan(devFlags{
		servicesList: []string{"app"},
		servicesRaw:  "app",
	})
	require.NoError(t, err)
	assert.True(t, plan.inDocker)
	assert.Equal(t, []string{"app"}, plan.services.selected)
}

// TestResolveProfiles_NoUserProfileReturnsEmpty — default profiles
// list is empty under the host-first redesign. The previous auto-on
// cache+queue behavior was tied to the now-removed orchestration
// defaults.
func TestResolveProfiles_NoUserProfileReturnsEmpty(t *testing.T) {
	assert.Empty(t, resolveProfiles(devFlags{}))
}

// TestResolveProfiles_UserProfilePassedThrough — --profile=<name>
// flows through as a single entry.
func TestResolveProfiles_UserProfilePassedThrough(t *testing.T) {
	assert.Equal(t, []string{"observability"}, resolveProfiles(devFlags{profile: "observability"}))
}

// TestResolveDevPlan_UserProfile — user's --profile flows through as
// a single entry. The previous auto-on cache+queue profiles are gone
// under the host-first model.
func TestResolveDevPlan_UserProfile(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	fakeExecOutput(t, `{"services":{"db":{},"observability":{}}}`, 0)
	plan, err := resolveDevPlan(devFlags{
		profile:      "observability",
		servicesList: []string{"db"},
		servicesRaw:  "db",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"observability"}, plan.profiles)
}
