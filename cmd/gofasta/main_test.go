package main

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMain_InvokesRunWithResolvedVersion(t *testing.T) {
	var captured string
	orig := run
	run = func(v string) { captured = v }
	t.Cleanup(func() { run = orig })

	main()

	assert.NotEmpty(t, captured)
}

// resolveVersion: ldflags-overridden Version wins outright.
func TestResolveVersion_LdflagsWins(t *testing.T) {
	origVer := Version
	Version = "v1.2.3"
	t.Cleanup(func() { Version = origVer })

	// The build info stub should NOT be consulted in this branch.
	origBI := resolveBuildInfo
	resolveBuildInfo = func() (*debug.BuildInfo, bool) {
		t.Fatal("resolveBuildInfo should not be called when Version is not 'dev'")
		return nil, false
	}
	t.Cleanup(func() { resolveBuildInfo = origBI })

	assert.Equal(t, "v1.2.3", resolveVersion())
}

// resolveVersion: no ldflags + ReadBuildInfo returns a real module version.
func TestResolveVersion_FallsBackToBuildInfo(t *testing.T) {
	origVer := Version
	Version = "dev"
	t.Cleanup(func() { Version = origVer })

	origBI := resolveBuildInfo
	resolveBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "v0.1.2"}}, true
	}
	t.Cleanup(func() { resolveBuildInfo = origBI })

	assert.Equal(t, "v0.1.2", resolveVersion())
}

// resolveVersion: no ldflags + ReadBuildInfo unavailable → returns "dev".
func TestResolveVersion_BuildInfoUnavailable(t *testing.T) {
	origVer := Version
	Version = "dev"
	t.Cleanup(func() { Version = origVer })

	origBI := resolveBuildInfo
	resolveBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}
	t.Cleanup(func() { resolveBuildInfo = origBI })

	assert.Equal(t, "dev", resolveVersion())
}

// resolveVersion: no ldflags + ReadBuildInfo returns "(devel)" → stays "dev".
// This is what go build (not go install) from source produces.
func TestResolveVersion_BuildInfoDevel(t *testing.T) {
	origVer := Version
	Version = "dev"
	t.Cleanup(func() { Version = origVer })

	origBI := resolveBuildInfo
	resolveBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true
	}
	t.Cleanup(func() { resolveBuildInfo = origBI })

	assert.Equal(t, "dev", resolveVersion())
}

// resolveVersion: no ldflags + ReadBuildInfo returns empty version → stays "dev".
func TestResolveVersion_BuildInfoEmpty(t *testing.T) {
	origVer := Version
	Version = "dev"
	t.Cleanup(func() { Version = origVer })

	origBI := resolveBuildInfo
	resolveBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: ""}}, true
	}
	t.Cleanup(func() { resolveBuildInfo = origBI })

	assert.Equal(t, "dev", resolveVersion())
}
