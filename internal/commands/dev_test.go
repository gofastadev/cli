package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunMigration_FakeSuccess(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	assert.NoError(t, runMigration("up"))
}

func TestRunMigration_FakeFailure(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 1)
	err := runMigration("down")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "migration down failed")
}

func TestDevCmd_RunE(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	assert.NoError(t, devCmd.RunE(devCmd, nil))
}

func TestRunDev_FakeSuccess(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	// runDev starts air in foreground, fake exits 0 immediately, returns nil
	assert.NoError(t, runDev(devFlags{envFile: ".env"}))
}

func TestRunDev_WithGraphQLFile(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	os.WriteFile("gqlgen.yml", []byte("schema: s\n"), 0644)
	withFakeExec(t, 0)
	assert.NoError(t, runDev(devFlags{envFile: ".env"}))
}

func TestRunDev_AirFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 1)
	// Both migrate + air "fail" — migrate is non-fatal, air error returns
	err := runDev(devFlags{envFile: ".env"})
	assert.Error(t, err)
}

// TestRunDev_DryRun_NoCompose — the "no compose.yaml present" branch:
// runDev should bail out early with orchestrate=false and no side
// effects (no Air, no docker commands, no migrations).
func TestRunDev_DryRun_NoCompose(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))

	stdout := captureStdout(t, func() {
		err := runDev(devFlags{
			envFile:     ".env",
			dryRun:      true,
			waitTimeout: defaultWaitTimeout,
		})
		assert.NoError(t, err)
	})

	assert.Contains(t, stdout, "orchestrate=false")
}

// TestRunDev_DryRun_JSONMode — --dry-run with --json emits the plan as
// a structured event, not as a human log line. Asserts the event shape
// agents would branch on.
func TestRunDev_DryRun_JSONMode(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))

	cliout.SetJSONMode(true)
	t.Cleanup(func() { cliout.SetJSONMode(false) })

	stdout := captureStdout(t, func() {
		// jsonOutput is the package-level flag mirror cliout reads; set
		// it directly so newDevEmitter picks the JSON path.
		origJSON := jsonOutput
		jsonOutput = true
		t.Cleanup(func() { jsonOutput = origJSON })

		err := runDev(devFlags{
			envFile:     ".env",
			dryRun:      true,
			waitTimeout: defaultWaitTimeout,
		})
		assert.NoError(t, err)
	})

	// The emitted event is NDJSON; unmarshal and assert shape.
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line == "" {
			continue
		}
		var ev map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &ev), "line %q should be JSON", line)
		assert.NotEmpty(t, ev["event"])
	}
}

// TestRunDev_DryRun_ServicesAll_PlanShape — `--services all` is the
// canonical "full stack in docker" invocation under the host-first
// redesign. The dry-run output must surface (1) in_docker=true (app
// is in the service list), (2) the full selected set, (3) any
// profiles. Operators read this output to predict what `gofasta dev`
// will do without actually starting docker, so the shape is a
// contract.
func TestRunDev_DryRun_ServicesAll_PlanShape(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	fakeExecOutput(t, `{"services":{"app":{},"db":{},"cache":{},"queue":{}}}`, 0)

	stdout := captureStdout(t, func() {
		err := runDev(devFlags{
			envFile:      ".env",
			dryRun:       true,
			servicesList: []string{"app", "db", "cache", "queue"},
			servicesRaw:  "all",
			waitTimeout:  defaultWaitTimeout,
		})
		assert.NoError(t, err)
	})

	assert.Contains(t, stdout, "in_docker=true")
	assert.Contains(t, stdout, "app")
	assert.Contains(t, stdout, "cache")
	assert.Contains(t, stdout, "queue")
}

// TestRunInDockerSupervisor_RestartSignal — when the user presses R
// (sigKeyboardRestart on the channel), the supervisor calls teardown
// with reason "restart" and returns true so the outer pipeline loop
// re-runs from scratch.
//
// Subtle: `exited` MUST NOT be ready at the moment the outer select
// fires, or Go's random-case selection can pick it over keySignals
// (50% failure rate). We deliver to it from a delayed goroutine so
// the outer select definitively picks keySignals first; the inner
// `<-exited` (after interruptCompose) then unblocks on the delivery.
func TestRunInDockerSupervisor_RestartSignal(t *testing.T) {
	keyCh := make(chan keyboardSignal, 1)
	exited := make(chan error, 1)
	keyCh <- sigKeyboardRestart
	go func() {
		time.Sleep(50 * time.Millisecond)
		exited <- nil
	}()
	var called string
	restart := runInDockerSupervisor(nil, exited, func(r string) { called = r }, keyCh)
	assert.True(t, restart)
	assert.Equal(t, "restart", called)
}

// TestRunInDockerSupervisor_QuitSignal — Q press is the same teardown
// path as Ctrl+C; restart=false so the outer loop exits.
// Same delayed-exited pattern as the Restart test (see comment above).
func TestRunInDockerSupervisor_QuitSignal(t *testing.T) {
	keyCh := make(chan keyboardSignal, 1)
	exited := make(chan error, 1)
	keyCh <- sigKeyboardQuit
	go func() {
		time.Sleep(50 * time.Millisecond)
		exited <- nil
	}()
	var called string
	restart := runInDockerSupervisor(nil, exited, func(r string) { called = r }, keyCh)
	assert.False(t, restart)
	assert.Equal(t, "quit", called)
}

// TestRunInDockerSupervisor_ChildExits — if the foreground compose
// process exits on its own (app crashed, container died), the
// supervisor must call teardown("app-exited") and return false so the
// outer loop exits cleanly. This is the path the user gets when the
// app inside the container crashes.
func TestRunInDockerSupervisor_ChildExits(t *testing.T) {
	keyCh := make(chan keyboardSignal, 1)
	exited := make(chan error, 1)
	exited <- nil // child exited cleanly
	var called string
	restart := runInDockerSupervisor(nil, exited, func(r string) { called = r }, keyCh)
	assert.False(t, restart)
	assert.Equal(t, "app-exited", called)
}

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

// menuInputFn / menuOutputFn / menuStartServicesFn default closures
// — existing tests always stub these via the seams, so the default
// implementations report uncovered. Invoke each through the
// package-init snapshot above to exercise the original closure bodies.
func TestMenuSeamDefaults(t *testing.T) {
	assert.Equal(t, os.Stdin, initialMenuInputFn())
	assert.Equal(t, os.Stdout, initialMenuOutputFn())

	withFakeExec(t, 0)
	require.NoError(t, initialMenuStartServicesFn([]string{"db"}))
}

func TestBuildCacheEndpoint_MemoryDriver(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "cache:\n  driver: memory\n")
	endpoint, enabled := configutil.BuildCacheEndpoint()
	assert.False(t, enabled)
	assert.Empty(t, endpoint)
}

func TestBuildCacheEndpoint_EmptyDriver(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "cache: {}\n")
	endpoint, enabled := configutil.BuildCacheEndpoint()
	assert.False(t, enabled)
	assert.Empty(t, endpoint)
}

func TestBuildCacheEndpoint_RedisExplicit(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "cache:\n  driver: redis\n  redis:\n    host: redis-host\n    port: \"6380\"\n")
	endpoint, enabled := configutil.BuildCacheEndpoint()
	assert.True(t, enabled)
	assert.Equal(t, "redis-host:6380", endpoint)
}

func TestBuildCacheEndpoint_RedisDefaultsLocalhost(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "cache:\n  driver: redis\n")
	endpoint, enabled := configutil.BuildCacheEndpoint()
	assert.True(t, enabled)
	assert.Equal(t, "localhost:6379", endpoint)
}

func TestBuildQueueEndpoint_DisabledDefault(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "queue:\n  enabled: false\n")
	endpoint, enabled := configutil.BuildQueueEndpoint()
	assert.False(t, enabled)
	assert.Empty(t, endpoint)
}

func TestBuildQueueEndpoint_NoSection(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "database:\n  driver: postgres\n")
	endpoint, enabled := configutil.BuildQueueEndpoint()
	assert.False(t, enabled)
	assert.Empty(t, endpoint)
}

func TestBuildQueueEndpoint_Enabled(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "queue:\n  enabled: true\n  redis:\n    host: queue-host\n    port: \"6381\"\n")
	endpoint, enabled := configutil.BuildQueueEndpoint()
	assert.True(t, enabled)
	assert.Equal(t, "queue-host:6381", endpoint)
}

func TestBuildQueueEndpoint_EnabledDefaults(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "queue:\n  enabled: true\n")
	endpoint, enabled := configutil.BuildQueueEndpoint()
	assert.True(t, enabled)
	assert.Equal(t, "localhost:6379", endpoint)
}

// TestAppendTag_NoExistingGOFLAGS — fresh env, no GOFLAGS set. Returns
// a new GOFLAGS= value containing just the tag.
func TestAppendTag_NoExistingGOFLAGS(t *testing.T) {
	got := appendTag("", "devtools")
	assert.Equal(t, "GOFLAGS=-tags=devtools", got)
}

// TestAppendTag_WithOtherFlags — existing GOFLAGS has non-tag flags;
// we append a fresh -tags= fragment.
func TestAppendTag_WithOtherFlags(t *testing.T) {
	got := appendTag("-mod=mod", "devtools")
	assert.Equal(t, "GOFLAGS=-mod=mod -tags=devtools", got)
}

// TestAppendTag_WithExistingTags — existing -tags=foo; we merge the new
// tag in comma-separated form without duplication.
func TestAppendTag_WithExistingTags(t *testing.T) {
	got := appendTag("-tags=foo", "devtools")
	assert.Equal(t, "GOFLAGS=-tags=foo,devtools", got)
}

// TestAppendTag_TagAlreadyPresent — idempotent when the target tag is
// already present in the existing -tags= fragment.
func TestAppendTag_TagAlreadyPresent(t *testing.T) {
	got := appendTag("-tags=devtools,foo", "devtools")
	assert.Equal(t, "GOFLAGS=-tags=devtools,foo", got)
}

// TestAppendTag_AcceptsFullPrefix — tolerant of a "GOFLAGS=" prefix on
// the input string so callers don't have to strip it.
func TestAppendTag_AcceptsFullPrefix(t *testing.T) {
	got := appendTag("GOFLAGS=-mod=mod", "devtools")
	assert.Equal(t, "GOFLAGS=-mod=mod -tags=devtools", got)
}

// TestRunSeedDelegation_FakeSuccess — `gofasta seed` delegation,
// stubbed exec.
func TestRunSeedDelegation_FakeSuccess(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	assert.NoError(t, runSeedDelegation())
}

func TestRunSeedDelegation_FakeFailure(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 1)
	assert.Error(t, runSeedDelegation())
}

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

// runDev happy path — .env loaded, migration + air mocked to succeed,
// function returns nil. Covers:
//   - the loadDotEnv success branch (loaded > 0 "Loaded N variables" step)
//   - the "Running migrations" step with a mocked migrate CLI returning 0
//   - the "Starting air" step with a mocked `go tool air` returning 0
//   - full happy-path traversal of runDev
func TestRunDev_HappyPathWithEnv(t *testing.T) {
	setupDevTempdir(t)
	require.NoError(t, os.WriteFile(".env",
		[]byte("DEV_TEST_RUN_HAPPY_VAR=loaded\n"), 0o644))
	t.Cleanup(func() { _ = os.Unsetenv("DEV_TEST_RUN_HAPPY_VAR") })

	withFakeExec(t, 0)

	err := runDev(devFlags{envFile: ".env"})
	assert.NoError(t, err)
	// The .env was loaded — value now in process env.
	assert.Equal(t, "loaded", os.Getenv("DEV_TEST_RUN_HAPPY_VAR"))
}

// runDev when .env is missing — loadDotEnv returns (0, nil), the "Loaded"
// step is skipped, and the rest of the flow still runs to completion.
func TestRunDev_NoDotEnv(t *testing.T) {
	setupDevTempdir(t)
	withFakeExec(t, 0)

	err := runDev(devFlags{envFile: ".env"})
	assert.NoError(t, err)
}

// runDev when .env exists but is unreadable — loadDotEnv returns an error,
// runDev emits a PrintWarn and continues. Covers the error branch at
// dev.go:52-53.
func TestRunDev_UnreadableDotEnv(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod-based read denial")
	}
	setupDevTempdir(t)
	require.NoError(t, os.WriteFile(".env", []byte("FOO=bar\n"), 0o644))
	require.NoError(t, os.Chmod(".env", 0o000))
	t.Cleanup(func() { _ = os.Chmod(".env", 0o644) })

	withFakeExec(t, 0)
	err := runDev(devFlags{envFile: ".env"})
	// runDev treats the load error as non-fatal — it prints a warning and
	// carries on. No error is returned.
	assert.NoError(t, err)
}

// runDev when the migration step fails — warn branch of the migration
// block is exercised and runDev still proceeds to start air.
func TestRunDev_MigrationFails(t *testing.T) {
	setupDevTempdir(t)
	withFakeExec(t, 1) // every exec returns non-zero
	// Provide a migrate binary on PATH so runMigrations doesn't short-circuit
	// at the LookPath check. fakeExecCommand produces the binary path from
	// os.Args[0] (the test binary itself) which is always on PATH.
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "/usr/bin/migrate", nil }
	t.Cleanup(func() { execLookPath = origLookPath })

	err := runDev(devFlags{envFile: ".env"})
	// Air also fails (same fakeExec) — runDev returns the air error.
	assert.Error(t, err)
}

// TestRunDev_WithFlagPort — flags.port != "" sets the PORT env var and
// takes the port override branch when picking URLs.
func TestRunDev_WithFlagPort(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	t.Setenv("PORT", "") // reset
	err := runDev(devFlags{envFile: ".env", port: "9999"})
	assert.NoError(t, err)
	assert.Equal(t, "9999", os.Getenv("PORT"))
}

// TestRunDev_DryRun — dry-run path prints the plan and returns.
func TestRunDev_DryRun(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	err := runDev(devFlags{envFile: ".env", dryRun: true})
	assert.NoError(t, err)
}

// TestRunDev_Seed — flags.seed triggers runSeedDelegation. We stub
// exec so it succeeds.
func TestRunDev_Seed(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	err := runDev(devFlags{envFile: ".env", seed: true})
	assert.NoError(t, err)
}

// TestRunDev_SeedFails — seed returns error; runDev continues.
func TestRunDev_SeedFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	// Pin the migrate lookup to success so the staged exec codes line up
	// the same way on hosts where `migrate` is on $PATH and on CI where it
	// is not — otherwise the migrate stage silently skips its exec call
	// and every subsequent code shifts by one.
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	t.Cleanup(func() { execLookPath = origLookPath })
	// stagedFakeExec: migrate=0, seed=1, then air=0 (final code repeats).
	stagedFakeExec(t, 0, 1, 0)
	err := runDev(devFlags{envFile: ".env", seed: true})
	assert.NoError(t, err)
}

// TestRunDev_WithComposeOrchestration — compose.yaml present and
// docker fake responds to everything.
func TestRunDev_WithComposeOrchestration(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	stubProbesOK(t)
	// compose.yaml makes plan.orchestrate true.
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	// Stub every docker call: info, version, compose version, compose
	// config, compose up, compose ps, migrate, air. Use fakeExecOutput
	// with a config blob that has one service with a healthcheck.
	composeConfig := `{"services":{"db":{"healthcheck":{"test":["CMD","pg_isready"]}}}}`
	composePS := `[{"Service":"db","State":"running","Health":"healthy"}]`
	orig := execCommand
	call := 0
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		// Decide stdout based on the argv shape.
		if len(args) > 0 && args[0] == "info" {
			stdout = ""
		} else if len(args) >= 2 && args[0] == "version" {
			stdout = "28.0\n"
		} else if hasComposeSub(args, "version") {
			stdout = "v2.26\n"
		} else if hasComposeSub(args, "config") {
			stdout = composeConfig
		} else if hasComposeSub(args, "ps") {
			stdout = composePS
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"=0",
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		call++
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })

	err := runDev(devFlags{envFile: ".env", waitTimeout: 5e9, keepVolumes: true})
	assert.NoError(t, err)
}

// TestRunDev_Fresh_WithCompose — fresh=true with orchestrate triggers
// resetVolumes call.
func TestRunDev_Fresh_WithCompose(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	composeConfig := `{"services":{"db":{}}}`
	composePS := `[{"Service":"db","State":"running","Health":""}]`
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		if hasComposeSub(args, "config") {
			stdout = composeConfig
		} else if hasComposeSub(args, "ps") {
			stdout = composePS
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"=0",
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })

	err := runDev(devFlags{envFile: ".env", waitTimeout: 5e9,
		keepVolumes: false, fresh: true})
	assert.NoError(t, err)
}

// TestRunDev_ComposeUnavailable — orchestrate=true but composeAvailable
// returns false → error.
func TestRunDev_ComposeUnavailable(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	orig := composeAvailableFn
	composeAvailableFn = func() bool { return false }
	t.Cleanup(func() { composeAvailableFn = orig })
	// Also stub execCommand so docker compose config doesn't try to
	// run for real.
	execOrig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		if hasComposeSub(args, "config") {
			stdout = `{"services":{"db":{}}}`
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"=0",
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = execOrig })
	err := runDev(devFlags{
		envFile:      ".env",
		waitTimeout:  5e9,
		keepVolumes:  true,
		servicesList: []string{"db"},
		servicesRaw:  "db",
		noKeyboard:   true,
	})
	require.Error(t, err)
}

// TestRunDev_KeepVolumesFalseDestroys — teardown with keepVolumes=false
// calls resetVolumes which we make fail to cover the else branch.
func TestRunDev_KeepVolumesFalseDestroys(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		exitCode := 0
		if hasComposeSub(args, "config") {
			stdout = `{"services":{"db":{}}}`
		} else if hasComposeSub(args, "ps") {
			stdout = `[{"Service":"db","State":"running","Health":""}]`
		} else if hasComposeSub(args, "down") {
			exitCode = 1 // Make teardown fail → emitter.Shutdown(mode+"-failed", 1).
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"="+strconvItoa(exitCode),
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })
	err := runDev(devFlags{envFile: ".env", waitTimeout: 5e9, keepVolumes: false})
	assert.NoError(t, err)
}

// TestIsSignaledExit_NilProcessState — default isSignaledExit handles
// nil gracefully.
func TestIsSignaledExit_NilProcessState(t *testing.T) {
	got := isSignaledExit(nil)
	assert.False(t, got)
}

// TestRunAir_SignaledExit — a child that dies BY a signal (real wait
// status, no stubbing: GOFASTA_FAKE_SIGNAL makes the helper SIGINT
// itself) is a clean shutdown, not a pipeline failure. The previous
// version of this test stubbed isSignaledExit to true while the fake
// child exited normally — a state combination (Exited() && Signaled())
// that cannot occur in production, which is exactly how the dead
// classification branch stayed green.
func TestRunAir_SignaledExit(t *testing.T) {
	chdirTemp(t)
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			"GOFASTA_FAKE_SIGNAL=1",
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })
	stubProbesOK(t)

	_, err := runAir(devFlags{}, func(string) {}, nil)
	assert.NoError(t, err, "a signal-terminated Air must be a clean shutdown")
}

// TestRunAir_UserStoppedExitIsClean — Air trapping SIGINT and calling
// exit(1) itself (Exited()==true, Signaled()==false) is ALSO clean
// when the stop was user-initiated: the keyboard 'q' path records
// userStopped before signaling the child.
func TestRunAir_UserStoppedExitIsClean(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 1) // child exits 1, not signaled
	keyCh := make(chan keyboardSignal, 1)
	keyCh <- sigKeyboardQuit
	_, err := runAir(devFlags{}, func(string) {}, keyCh)
	assert.NoError(t, err, "user-initiated quit must be a clean shutdown even when Air exits non-zero")
}

// TestRunAir_RealFailureStillErrors — a child that dies on its own
// with a non-zero exit (no signal, no user action) keeps erroring,
// now under the accurate DEV_AIR_EXIT code.
func TestRunAir_RealFailureStillErrors(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 1)
	_, err := runAir(devFlags{}, func(string) {}, nil)
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, string(clierr.CodeDevAirExit), ce.Code)
}

// TestRunDev_NoTeardownSkips — flags.noTeardown=true → teardown
// returns early.
func TestRunDev_NoTeardownSkips(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	err := runDev(devFlags{envFile: ".env", noTeardown: true})
	assert.NoError(t, err)
}

// TestRunDev_ResolveDevPlanFails — construct devFlags with
// servicesList set and no compose.yaml → resolveDevPlan errors.
func TestRunDev_ResolveDevPlanFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	err := runDev(devFlags{envFile: ".env",
		servicesList: []string{"db"}})
	require.Error(t, err)
}

// TestRunDev_AttachLogs — attach-logs triggers startLogStreamer when
// orchestrating. We stub exec to be quick so the cancel func can clean
// up without hanging.
func TestRunDev_AttachLogs(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	composeConfig := `{"services":{"db":{}}}`
	composePS := `[{"Service":"db","State":"running","Health":""}]`
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		if hasComposeSub(args, "config") {
			stdout = composeConfig
		} else if hasComposeSub(args, "ps") {
			stdout = composePS
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"=0",
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })

	err := runDev(devFlags{envFile: ".env", waitTimeout: 5e9,
		keepVolumes: true, attachLogs: true})
	assert.NoError(t, err)
}

// TestRunDev_Dashboard — dashboard=true triggers startDashboard.
func TestRunDev_Dashboard(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	// Pick a port nothing else is likely listening on.
	err := runDev(devFlags{envFile: ".env",
		dashboard: true, dashboardPort: 0}) // port 0 → random free port
	assert.NoError(t, err)
}

// TestRunDev_AirRebuild — rebuild=true triggers os.RemoveAll("tmp")
// before air starts.
func TestRunDev_AirRebuild(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	require.NoError(t, os.MkdirAll("tmp", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("tmp", "x"), []byte("x"), 0o644))
	withFakeExec(t, 0)
	err := runDev(devFlags{envFile: ".env", rebuild: true})
	assert.NoError(t, err)
	_, err = os.Stat("tmp")
	assert.True(t, os.IsNotExist(err), "tmp should have been removed")
}

// TestRunMigrationsWithCount_MigrateNotFound — execLookPath fails.
func TestRunMigrationsWithCount_MigrateNotFound(t *testing.T) {
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { execLookPath = origLookPath })
	_, err := runMigrationsWithCount()
	require.Error(t, err)
}

// TestAirURLs_WithSwagger — docs/swagger.json exists → swagger URL set.
func TestAirURLs_WithSwagger(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.MkdirAll("docs", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("docs", "swagger.json"),
		[]byte("{}"), 0o644))
	urls := airURLs("8080")
	assert.Contains(t, urls, "swagger")
}

// TestAppendTag_NoTagPrefix — an existing -tags=... fragment followed
// by a token without the -tags= prefix exercises the continue branch.
func TestAppendTag_NoTagPrefix(t *testing.T) {
	got := appendTag("-tags=foo -mod=mod", "bar")
	assert.Contains(t, got, "-tags=foo,bar")
	assert.Contains(t, got, "-mod=mod")
}

// TestAppendTag_ExistingTagsSkipsNonTagsPrefix — a field that isn't
// a -tags= fragment exercises the "continue" branch.
func TestAppendTag_ExistingTagsSkipsNonTagsPrefix(t *testing.T) {
	got := appendTag("-mod=mod -tags=foo", "bar")
	assert.Contains(t, got, "-tags=foo,bar")
}

// TestRunAir_RemoveAllFails — removeAllFn seam returns an error; the
// error is swallowed silently and air still runs.
func TestRunAir_RemoveAllFails(t *testing.T) {
	chdirTemp(t)
	orig := removeAllFn
	removeAllFn = func(_ string) error { return fmt.Errorf("boom") }
	t.Cleanup(func() { removeAllFn = orig })
	withFakeExec(t, 0)
	_, err := runAir(devFlags{rebuild: true}, func(string) {}, nil)
	// Air succeeds despite RemoveAll failing.
	assert.NoError(t, err)
}

// TestRunAir_EnvNilBranch — execCommand returns an *exec.Cmd whose Env
// is nil so runAir's "seed from os.Environ() when nil" branch fires.
func TestRunAir_EnvNilBranch(t *testing.T) {
	chdirTemp(t)
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		// Build a subprocess cmd but keep Env nil.
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = nil // force the branch
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })
	// runAir will fail; we only care about coverage.
	_, _ = runAir(devFlags{}, func(string) {}, nil)
}

// TestRunAir_Rebuild_RemovesTmp — rebuild flag triggers the RemoveAll
// branch. Even if tmp doesn't exist RemoveAll is a no-op error branch.
func TestRunAir_Rebuild_RemovesTmp(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	_, err := runAir(devFlags{rebuild: true}, func(string) {}, nil)
	assert.NoError(t, err)
}

// TestRunAir_GoNotOnPath — execLookPath returns error.
func TestRunAir_GoNotOnPath(t *testing.T) {
	chdirTemp(t)
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { execLookPath = origLookPath })
	_, err := runAir(devFlags{}, func(string) {}, nil)
	require.Error(t, err)
}

// TestRunSeedDelegation — covers the package-level function called by
// runDev when --seed is set.
func TestRunSeedDelegation(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, runSeedDelegation())
	withFakeExec(t, 1)
	assert.Error(t, runSeedDelegation())
}

// TestAirSignalHandler_NilProcess — signal fired before air started;
// teardown still called.
func TestAirSignalHandler_NilProcess(t *testing.T) {
	sigChan := make(chan os.Signal, 1)
	airCmd := exec.Command("true")
	var called string
	sigChan <- os.Interrupt
	airSignalHandler(sigChan, nil, make(chan struct{}), airCmd, func(r string) { called = r }, &atomicBool{}, &atomicBool{})
	assert.Equal(t, "interrupted", called)
}

// TestAirSignalHandler_WithProcess — running process receives SIGINT
// on signal fire.
func TestAirSignalHandler_WithProcess(t *testing.T) {
	sigChan := make(chan os.Signal, 1)
	airCmd := exec.Command("sleep", "60")
	require.NoError(t, airCmd.Start())
	t.Cleanup(func() { _ = airCmd.Wait() })
	var called string
	sigChan <- os.Interrupt
	airSignalHandler(sigChan, nil, make(chan struct{}), airCmd, func(r string) { called = r }, &atomicBool{}, &atomicBool{})
	assert.Equal(t, "interrupted", called)
}

// TestAirSignalHandler_KeyboardRestart — pressing R sends sigKeyboardRestart;
// the handler SIGINTs Air, calls teardown with reason "restart", and sets
// the restart flag so runAir returns restart=true.
func TestAirSignalHandler_KeyboardRestart(t *testing.T) {
	sigChan := make(chan os.Signal, 1)
	keyCh := make(chan keyboardSignal, 1)
	airCmd := exec.Command("true")
	var called string
	flag := &atomicBool{}
	keyCh <- sigKeyboardRestart
	airSignalHandler(sigChan, keyCh, make(chan struct{}), airCmd, func(r string) { called = r }, flag, &atomicBool{})
	assert.Equal(t, "restart", called)
	assert.True(t, flag.Load())
}

// TestAirSignalHandler_KeyboardQuit — pressing Q is the same teardown
// path as Ctrl+C; restart flag stays false.
func TestAirSignalHandler_KeyboardQuit(t *testing.T) {
	sigChan := make(chan os.Signal, 1)
	keyCh := make(chan keyboardSignal, 1)
	airCmd := exec.Command("true")
	var called string
	flag := &atomicBool{}
	keyCh <- sigKeyboardQuit
	airSignalHandler(sigChan, keyCh, make(chan struct{}), airCmd, func(r string) { called = r }, flag, &atomicBool{})
	assert.Equal(t, "quit", called)
	assert.False(t, flag.Load())
}

// runInDockerForeground — Start() fails when execCommand returns a Cmd
// whose Path points at a non-existent binary.
func TestRunInDockerForeground_StartFails(t *testing.T) {
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("/no/such/binary/", args...)
	}
	t.Cleanup(func() { execCommand = orig })

	keyCh := make(chan keyboardSignal, 1)
	restart, err := runInDockerForeground(func(string) {}, keyCh)
	assert.False(t, restart)
	require.Error(t, err)
}

// runInDockerForeground — fake docker child exits 0; supervisor sees
// `exited` first and returns (false, nil) with teardown reason
// "app-exited".
func TestRunInDockerForeground_HappyPath_ChildExits(t *testing.T) {
	withFakeExec(t, 0)
	keyCh := make(chan keyboardSignal, 1)
	called := ""
	restart, err := runInDockerForeground(func(r string) { called = r }, keyCh)
	assert.False(t, restart)
	assert.NoError(t, err)
	assert.Equal(t, "app-exited", called)
}

// runInDockerSupervisor — SIGINT branch. Send os.Interrupt to ourselves
// AFTER the supervisor has registered signal.Notify; signal.Notify
// delivers via the supervisor's internal sigChan. cmd=nil means
// interruptCompose is a no-op (it null-checks cmd.Process). exited is
// delivered by a delayed goroutine so the supervisor's `<-exited`
// inside the SIGINT branch unblocks.
func TestRunInDockerSupervisor_SIGINTBranch(t *testing.T) {
	exited := make(chan error, 1)
	keyCh := make(chan keyboardSignal, 1)

	go func() {
		time.Sleep(75 * time.Millisecond)
		proc, err := os.FindProcess(os.Getpid())
		if err == nil {
			_ = proc.Signal(os.Interrupt)
		}
		time.Sleep(50 * time.Millisecond)
		exited <- nil
	}()

	called := ""
	restart := runInDockerSupervisor(nil, exited, func(r string) { called = r }, keyCh)
	assert.False(t, restart)
	assert.Equal(t, "interrupted", called)
}

// runInDockerSupervisor — interruptCompose body (the `if cmd != nil &&
// cmd.Process != nil` branch). All existing supervisor tests pass
// cmd=nil; this test supplies a real `sleep 60` cmd so the SIGINT is
// forwarded into a live Process.
func TestRunInDockerSupervisor_InterruptComposeWithRealCmd(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Wait() })

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	keyCh := make(chan keyboardSignal, 1)
	keyCh <- sigKeyboardQuit
	called := ""
	restart := runInDockerSupervisor(cmd, exited, func(r string) { called = r }, keyCh)
	assert.False(t, restart)
	assert.Equal(t, "quit", called)
}

// airSignalHandler — Air exits on its own (done channel closed first).
// Handler must return without signaling airCmd or calling teardown.
func TestAirSignalHandler_DoneBranch(t *testing.T) {
	sigChan := make(chan os.Signal, 1)
	keyCh := make(chan keyboardSignal, 1)
	done := make(chan struct{})
	close(done)

	called := ""
	flag := &atomicBool{}
	airSignalHandler(sigChan, keyCh, done, nil, func(r string) { called = r }, flag, &atomicBool{})
	assert.Empty(t, called, "done branch must not call teardown")
	assert.False(t, flag.Load(), "restart flag must remain false")
}

// airSignalHandler — keyboardRestart with a real running cmd. Exercises
// the SIGINT-the-airCmd body inside the restart branch.
func TestAirSignalHandler_KeyboardRestartWithRunningCmd(t *testing.T) {
	sigChan := make(chan os.Signal, 1)
	keyCh := make(chan keyboardSignal, 1)
	airCmd := exec.Command("sleep", "60")
	require.NoError(t, airCmd.Start())
	t.Cleanup(func() { _ = airCmd.Wait() })

	flag := &atomicBool{}
	called := ""
	keyCh <- sigKeyboardRestart
	airSignalHandler(sigChan, keyCh, make(chan struct{}), airCmd,
		func(r string) { called = r }, flag, &atomicBool{})
	assert.Equal(t, "restart", called)
	assert.True(t, flag.Load())
}

// airSignalHandler — keyboardQuit with a real running cmd. Covers the
// SIGINT-the-airCmd body inside the quit branch.
func TestAirSignalHandler_KeyboardQuitWithRunningCmd(t *testing.T) {
	sigChan := make(chan os.Signal, 1)
	keyCh := make(chan keyboardSignal, 1)
	airCmd := exec.Command("sleep", "60")
	require.NoError(t, airCmd.Start())
	t.Cleanup(func() { _ = airCmd.Wait() })

	flag := &atomicBool{}
	called := ""
	keyCh <- sigKeyboardQuit
	airSignalHandler(sigChan, keyCh, make(chan struct{}), airCmd,
		func(r string) { called = r }, flag, &atomicBool{})
	assert.Equal(t, "quit", called)
	assert.False(t, flag.Load())
}

// resolveDevPlan — in-docker mode (app in selected) plus a local
// filesystem replace in go.mod is a configuration error.
func TestResolveDevPlan_InDockerRejectsLocalReplaces(t *testing.T) {
	setupDevTempdir(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  app: {}\n"), 0o644))

	origReplaces := findLocalReplacesFn
	findLocalReplacesFn = func(string) ([]localReplace, error) {
		return []localReplace{{Module: "example.com/foo", Path: "../foo"}}, nil
	}
	t.Cleanup(func() { findLocalReplacesFn = origReplaces })

	fakeExecOutput(t, `{"services":{"app":{}}}`, 0)

	_, err := resolveDevPlan(devFlags{servicesList: []string{"app"}, servicesRaw: "app"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replace")
}

// resolveDevPlan — detectComposeProfilesFn returns a profile that isn't
// in the initially-resolved set; the slices.Contains check + append
// branch runs.
func TestResolveDevPlan_DiscoveredProfilesAppended(t *testing.T) {
	setupDevTempdir(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db: {}\n"), 0o644))

	origProfiles := detectComposeProfilesFn
	detectComposeProfilesFn = func() ([]string, error) {
		return []string{"newone", ""}, nil
	}
	t.Cleanup(func() { detectComposeProfilesFn = origProfiles })

	origReplaces := findLocalReplacesFn
	findLocalReplacesFn = func(string) ([]localReplace, error) { return nil, nil }
	t.Cleanup(func() { findLocalReplacesFn = origReplaces })

	fakeExecOutput(t, `{"services":{"db":{}}}`, 0)
	plan, err := resolveDevPlan(devFlags{
		servicesList: []string{"db"},
		servicesRaw:  "db",
	})
	require.NoError(t, err)
	assert.Contains(t, plan.profiles, "newone")
}

// runDevPipeline — in-docker mode. With "app" in --services, the
// pipeline brings up supporting services detached and runs the app
// container in foreground.
func TestRunDevPipeline_InDockerMode(t *testing.T) {
	setupDevTempdir(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  app: {}\n  db: {}\n"), 0o644))

	origReplaces := findLocalReplacesFn
	findLocalReplacesFn = func(string) ([]localReplace, error) { return nil, nil }
	t.Cleanup(func() { findLocalReplacesFn = origReplaces })

	composeConfig := `{"services":{"app":{},"db":{"healthcheck":{"test":["CMD","x"]}}}}`
	composePS := `[{"Service":"db","State":"running","Health":"healthy"}]`
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		switch {
		case hasComposeSub(args, "config"):
			stdout = composeConfig
		case hasComposeSub(args, "ps"):
			stdout = composePS
		case len(args) >= 2 && args[0] == "version":
			stdout = "28.0\n"
		case hasComposeSub(args, "version"):
			stdout = "v2.26\n"
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"=0",
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })

	err := runDev(devFlags{
		envFile:      ".env",
		servicesRaw:  "app,db",
		servicesList: []string{"app", "db"},
		waitTimeout:  5 * time.Second,
		keepVolumes:  true,
	})
	assert.NoError(t, err)
}

// runDevPipeline — in-docker mode + --attach-logs. Covers the warn
// branch ("attach-logs is implicit ...") AND the supporting-services
// log streamer branch.
func TestRunDevPipeline_InDockerWithAttachLogs(t *testing.T) {
	setupDevTempdir(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  app: {}\n  db: {}\n"), 0o644))

	origReplaces := findLocalReplacesFn
	findLocalReplacesFn = func(string) ([]localReplace, error) { return nil, nil }
	t.Cleanup(func() { findLocalReplacesFn = origReplaces })

	composeConfig := `{"services":{"app":{},"db":{"healthcheck":{"test":["CMD","x"]}}}}`
	composePS := `[{"Service":"db","State":"running","Health":"healthy"}]`
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		if hasComposeSub(args, "config") {
			stdout = composeConfig
		} else if hasComposeSub(args, "ps") {
			stdout = composePS
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"=0",
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })

	err := runDev(devFlags{
		envFile:      ".env",
		servicesRaw:  "app,db",
		servicesList: []string{"app", "db"},
		waitTimeout:  5 * time.Second,
		keepVolumes:  true,
		attachLogs:   true,
	})
	assert.NoError(t, err)
}

// runDevPipeline — host mode + --attach-logs. Covers the host-mode log
// streamer branch (orchestrate=true && !inDocker && services > 0).
func TestRunDevPipeline_AttachLogsHostMode(t *testing.T) {
	setupDevTempdir(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db: {}\n"), 0o644))

	composeConfig := `{"services":{"db":{"healthcheck":{"test":["CMD","x"]}}}}`
	composePS := `[{"Service":"db","State":"running","Health":"healthy"}]`
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		if hasComposeSub(args, "config") {
			stdout = composeConfig
		} else if hasComposeSub(args, "ps") {
			stdout = composePS
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"=0",
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })

	err := runDev(devFlags{
		envFile:      ".env",
		servicesRaw:  "db",
		servicesList: []string{"db"},
		waitTimeout:  5 * time.Second,
		keepVolumes:  true,
		attachLogs:   true,
	})
	assert.NoError(t, err)
}

// runDevPipeline — hasUnreachable=true and the menu (non-TTY)
// short-circuits to menuCancel. Pipeline returns clierr indicating
// preflight unresolved.
func TestRunDevPipeline_MenuCancelExits(t *testing.T) {
	setupDevTempdir(t)
	origDB, origCache, origQueue := probeDatabaseFn, probeCacheFn, probeQueueFn
	probeDatabaseFn = func() probeResult {
		return probeResult{Dep: "database", Status: probeUnreachable, Reason: "boom"}
	}
	probeCacheFn = func() probeResult { return probeResult{Dep: "cache", Status: probeNotConfigured} }
	probeQueueFn = func() probeResult { return probeResult{Dep: "queue", Status: probeNotConfigured} }
	t.Cleanup(func() {
		probeDatabaseFn = origDB
		probeCacheFn = origCache
		probeQueueFn = origQueue
	})
	origTTY := menuIsTTYFn
	menuIsTTYFn = func() bool { return false }
	t.Cleanup(func() { menuIsTTYFn = origTTY })
	captureMenuOutput(t)

	withFakeExec(t, 0)
	probeDatabaseFn = func() probeResult {
		return probeResult{Dep: "database", Status: probeUnreachable, Reason: "boom"}
	}

	err := runDev(devFlags{envFile: ".env"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "preflight")
}

// runDevPipeline — menuRunWithoutDB outcome. User picks option [3];
// flags.noDB becomes true, migrate is skipped, the no-DB warn banner
// fires.
func TestRunDevPipeline_NoDBMode(t *testing.T) {
	setupDevTempdir(t)
	origDB, origCache, origQueue := probeDatabaseFn, probeCacheFn, probeQueueFn
	probeDatabaseFn = func() probeResult {
		return probeResult{Dep: "database", Status: probeUnreachable}
	}
	probeCacheFn = func() probeResult { return probeResult{Dep: "cache", Status: probeNotConfigured} }
	probeQueueFn = func() probeResult { return probeResult{Dep: "queue", Status: probeNotConfigured} }
	t.Cleanup(func() {
		probeDatabaseFn = origDB
		probeCacheFn = origCache
		probeQueueFn = origQueue
	})
	forceTTY(t, true)
	pipeStdin(t, "3")
	captureMenuOutput(t)

	withFakeExec(t, 0)
	probeDatabaseFn = func() probeResult {
		return probeResult{Dep: "database", Status: probeUnreachable}
	}

	err := runDev(devFlags{envFile: ".env"})
	assert.NoError(t, err)
}

// runDevPipeline — menu option [2] starts services and the
// `mergeServices(menuStarted)` branch fires. Probes return unreachable
// once, then OK after the menu finishes "starting" the service.
func TestRunDevPipeline_MenuStartedServicesMerged(t *testing.T) {
	setupDevTempdir(t)
	probeCalls := 0
	origDB, origCache, origQueue := probeDatabaseFn, probeCacheFn, probeQueueFn
	probeDatabaseFn = func() probeResult {
		probeCalls++
		if probeCalls == 1 {
			return probeResult{Dep: "database", Status: probeUnreachable}
		}
		return probeResult{Dep: "database", Status: probeOK, Endpoint: "localhost:5432"}
	}
	probeCacheFn = func() probeResult { return probeResult{Dep: "cache", Status: probeNotConfigured} }
	probeQueueFn = func() probeResult { return probeResult{Dep: "queue", Status: probeNotConfigured} }
	t.Cleanup(func() {
		probeDatabaseFn = origDB
		probeCacheFn = origCache
		probeQueueFn = origQueue
	})

	forceTTY(t, true)
	pipeStdin(t, "2")
	captureMenuOutput(t)

	origStart := menuStartServicesFn
	menuStartServicesFn = func([]string) error { return nil }
	t.Cleanup(func() { menuStartServicesFn = origStart })
	origWait := menuWaitHealthyFn
	menuWaitHealthyFn = func([]string) error { return nil }
	t.Cleanup(func() { menuWaitHealthyFn = origWait })

	withFakeExec(t, 0)
	probeDatabaseFn = func() probeResult {
		probeCalls++
		if probeCalls == 1 {
			return probeResult{Dep: "database", Status: probeUnreachable}
		}
		return probeResult{Dep: "database", Status: probeOK}
	}

	err := runDev(devFlags{envFile: ".env"})
	assert.NoError(t, err)
}

// runDevPipeline — teardown closure resetVolumes branch (keepVolumes
// is false AND services are present).
func TestRunDevPipeline_TeardownResetVolumes(t *testing.T) {
	setupDevTempdir(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db: {}\n"), 0o644))
	composeConfig := `{"services":{"db":{"healthcheck":{"test":["CMD","x"]}}}}`
	composePS := `[{"Service":"db","State":"running","Health":"healthy"}]`
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		if hasComposeSub(args, "config") {
			stdout = composeConfig
		} else if hasComposeSub(args, "ps") {
			stdout = composePS
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"=0",
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })

	err := runDev(devFlags{
		envFile:      ".env",
		servicesRaw:  "db",
		servicesList: []string{"db"},
		waitTimeout:  5 * time.Second,
		keepVolumes:  false,
	})
	assert.NoError(t, err)
}

// runDevPipeline — teardown closure stopServices error branch.
// `compose stop` exits non-zero, which emits "stopped-failed".
func TestRunDevPipeline_TeardownStopFails(t *testing.T) {
	setupDevTempdir(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db: {}\n"), 0o644))
	composeConfig := `{"services":{"db":{"healthcheck":{"test":["CMD","x"]}}}}`
	composePS := `[{"Service":"db","State":"running","Health":"healthy"}]`
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		exitCode := 0
		switch {
		case hasComposeSub(args, "config"):
			stdout = composeConfig
		case hasComposeSub(args, "ps"):
			stdout = composePS
		case hasComposeSub(args, "stop"):
			exitCode = 1
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"="+strconv.Itoa(exitCode),
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })

	err := runDev(devFlags{
		envFile:      ".env",
		servicesRaw:  "db",
		servicesList: []string{"db"},
		waitTimeout:  5 * time.Second,
		keepVolumes:  true,
	})
	assert.NoError(t, err)
}

// runDevPipeline — --fresh with orchestrate=true triggers the
// resetVolumes call (Stage 3). Existing fresh tests don't pass
// services so plan.orchestrate is false; this one does.
func TestRunDevPipeline_FreshWithOrchestrate(t *testing.T) {
	setupDevTempdir(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db: {}\n"), 0o644))
	composeConfig := `{"services":{"db":{}}}`
	composePS := `[{"Service":"db","State":"running","Health":""}]`
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		if hasComposeSub(args, "config") {
			stdout = composeConfig
		} else if hasComposeSub(args, "ps") {
			stdout = composePS
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"=0",
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })

	err := runDev(devFlags{
		envFile:      ".env",
		servicesRaw:  "db",
		servicesList: []string{"db"},
		waitTimeout:  5 * time.Second,
		keepVolumes:  true,
		fresh:        true,
	})
	assert.NoError(t, err)
}

// runDevPipeline — Stage 3 resetVolumes errors but pipeline continues
// (the error is logged as a warning, not fatal).
func TestRunDevPipeline_FreshResetVolumesFailsWithOrchestrate(t *testing.T) {
	setupDevTempdir(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db: {}\n"), 0o644))
	composeConfig := `{"services":{"db":{}}}`
	composePS := `[{"Service":"db","State":"running","Health":""}]`
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		exitCode := 0
		switch {
		case hasComposeSub(args, "config"):
			stdout = composeConfig
		case hasComposeSub(args, "ps"):
			stdout = composePS
		case hasComposeSub(args, "down"):
			exitCode = 1
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"="+strconv.Itoa(exitCode),
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })

	err := runDev(devFlags{
		envFile:      ".env",
		servicesRaw:  "db",
		servicesList: []string{"db"},
		waitTimeout:  5 * time.Second,
		keepVolumes:  true,
		fresh:        true,
	})
	assert.NoError(t, err)
}

// devCmd RunE — the deprecated --all-in-docker alias rewrites to
// --services all when servicesRaw is empty.
func TestDevCmd_RunE_AllInDockerDeprecatedAlias(t *testing.T) {
	withFakeExec(t, 0)
	origFlags := devFlagValues
	t.Cleanup(func() { devFlagValues = origFlags })
	devFlagValues = devFlags{allInDocker: true, envFile: ".env"}

	origPipeline := runDevPipelineFn
	runDevPipelineFn = func(_ devFlags, _ devEmitter) (bool, error) { return false, nil }
	t.Cleanup(func() { runDevPipelineFn = origPipeline })

	require.NoError(t, devCmd.RunE(devCmd, nil))
	assert.Equal(t, "all", devFlagValues.servicesRaw)
}

// runDev restart loop — runDevPipelineFn returns (true, nil) twice
// then (false, nil); the outer loop iterates three times.
func TestRunDev_RestartLoopIterates(t *testing.T) {
	calls := 0
	orig := runDevPipelineFn
	runDevPipelineFn = func(_ devFlags, _ devEmitter) (bool, error) {
		calls++
		if calls < 3 {
			return true, nil
		}
		return false, nil
	}
	t.Cleanup(func() { runDevPipelineFn = orig })

	err := runDev(devFlags{})
	assert.NoError(t, err)
	assert.Equal(t, 3, calls, "pipeline should run 3 times (2 restarts + clean exit)")
}
