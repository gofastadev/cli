package commands

import (
	"bufio"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tier 2 coverage — gaps reachable via the existing package-level
// seams (execCommand, term*, probe*, configutil overrides, menu*).
// Each test names the function/branch it targets.

// ─────────────────────────────────────────────────────────────────────
// isStdinTTY (dev_preflight_menu.go:99) — never directly called from
// the existing tests.
// ─────────────────────────────────────────────────────────────────────

func TestIsStdinTTY_RespectsTermIsTerminalSeam(t *testing.T) {
	origIs := termIsTerminalFn
	t.Cleanup(func() { termIsTerminalFn = origIs })

	termIsTerminalFn = func(_ int) bool { return true }
	assert.True(t, isStdinTTY())

	termIsTerminalFn = func(_ int) bool { return false }
	assert.False(t, isStdinTTY())
}

// ─────────────────────────────────────────────────────────────────────
// defaultMenuWaitHealthy (dev_preflight_menu.go:107) — 0% before.
// Drives via execCommand fake; the docker `compose config --profiles`
// + `compose config --format json` are scripted, and waitHealthy's
// `compose ps --format json` is too. Then a healthy state matches.
// ─────────────────────────────────────────────────────────────────────

func TestDefaultMenuWaitHealthy_ConfigError(t *testing.T) {
	// detectComposeProfiles uses execCommand; detectComposeServices then
	// calls execCommand again. We want the SECOND call (the `config
	// --format json` invocation) to fail — that turns into the
	// "compose config: %w" error. Easiest path: every exec returns
	// exit 1 with no stdout, so config parse fails.
	stagedFakeExec(t, 0, 1) // first call (profiles) ok, second (config json) fails
	err := defaultMenuWaitHealthy([]string{"db"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compose config")
}

func TestDefaultMenuWaitHealthy_FiltersUnknownAndCallsWaitHealthy(t *testing.T) {
	// Script:
	//   1. detectComposeProfiles (`docker compose config --profiles`) — empty stdout
	//   2. detectComposeServices (`docker compose config --format json`) — valid services list
	//   3+. queryServiceStates (`docker compose ps --format json`) — healthy state
	captureMenuOutput(t) // suppress chatter; defaultMenuWaitHealthy writes progress

	calls := 0
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		calls++
		switch {
		case len(args) >= 3 && args[0] == "compose" && args[1] == "config" && args[2] == "--profiles":
			return fakeCmdWithOutput(t, "", 0)
		case len(args) >= 4 && args[0] == "compose" && args[1] == "config" && args[2] == "--format":
			return fakeCmdWithOutput(t, `{"services":{"db":{"healthcheck":{"test":["CMD","x"]}}}}`, 0)
		case len(args) >= 3 && args[0] == "compose" && args[1] == "ps":
			return fakeCmdWithOutput(t, `[{"Service":"db","State":"running","Health":"healthy"}]`, 0)
		default:
			return fakeCmdWithOutput(t, "", 0)
		}
	}
	t.Cleanup(func() { execCommand = orig })

	err := defaultMenuWaitHealthy([]string{"db"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, calls, 2)
}

// ─────────────────────────────────────────────────────────────────────
// probeDatabase / probeCache / probeQueue — the "endpoint == ''"
// defensive branches (and probeDatabase's "display == ''" branch).
// configutil's real builders never return ("", true), so we drive
// these via the existing tcpDialFn seam after seeding the project
// dir with a config that yields a non-empty endpoint. The defensive
// branches stay unreachable through configutil — to cover them we
// stub the configutil helpers via tiny wrappers added below.
// ─────────────────────────────────────────────────────────────────────
//
// The probes call configutil.BuildDatabaseEndpoint / BuildCacheEndpoint
// / BuildQueueEndpoint / BuildMigrationURL directly. Tests can change
// what those return by mutating the project's config.yaml; the only
// way they return ("", true) would require code in configutil to do
// so, which today is impossible. The branches are kept as defensive
// guards (so a future configutil change can't silently bypass the
// probe). We can still cover them by using the package-level probe
// seams that were already in place for menu tests.
//
// Skip rationale documented inline; the probes remain at 81.8% /
// 87.5% / 87.5%. probeDatabase's "display == ''" branch is similarly
// unreachable: BuildMigrationURL has 5 switch arms, each fmt.Sprintf
// returning a non-empty string.

// ─────────────────────────────────────────────────────────────────────
// menuActionEnterConnString gaps (dev_preflight_menu.go:309 read err,
// 318 invalid URL schema/host check).
// ─────────────────────────────────────────────────────────────────────

func TestMenuActionEnterConnString_ReadError(t *testing.T) {
	captureMenuOutput(t)
	// Reader that returns an error on first ReadString call.
	reader := bufio.NewReader(&tier2ErrReader{err: errors.New("eof boom")})
	_, err := menuActionEnterConnString(reader, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read stdin")
}

func TestMenuActionEnterConnString_MissingSchemeOrHost(t *testing.T) {
	captureMenuOutput(t)
	// url.Parse accepts "abc" (Scheme="", Host=""), so the schema/host
	// guard fires.
	reader := bufio.NewReader(strings.NewReader("abc\n"))
	_, err := menuActionEnterConnString(reader, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheme and host")
}

// ─────────────────────────────────────────────────────────────────────
// Probe defensive branches: configutil's real implementations always
// return ("host:port", true) for the enabled case, but the probes
// defensively check for ("", true). Drive via the configBuild*Fn seams.
// ─────────────────────────────────────────────────────────────────────

func TestProbeDatabase_EmptyEndpointDefensive(t *testing.T) {
	orig := configBuildDatabaseEndpointFn
	configBuildDatabaseEndpointFn = func() (string, bool) { return "", true }
	t.Cleanup(func() { configBuildDatabaseEndpointFn = orig })

	got := probeDatabase()
	assert.Equal(t, probeUnreachable, got.Status)
	assert.Contains(t, got.Reason, "incomplete")
}

func TestProbeDatabase_DisplayFallbackToEndpoint(t *testing.T) {
	// BuildMigrationURL returns "" so display defaults to endpoint.
	origURL := configBuildMigrationURLFn
	configBuildMigrationURLFn = func() string { return "" }
	t.Cleanup(func() { configBuildMigrationURLFn = origURL })

	// Stub tcpDialFn so we don't need a real listener.
	origDial := tcpDialFn
	tcpDialFn = func(_, _ string, _ time.Duration) (net.Conn, error) {
		// Return a half-mocked conn whose Close is a no-op.
		c1, c2 := net.Pipe()
		go func() { _ = c2.Close() }()
		return c1, nil
	}
	t.Cleanup(func() { tcpDialFn = origDial })

	origEndpoint := configBuildDatabaseEndpointFn
	configBuildDatabaseEndpointFn = func() (string, bool) { return "localhost:5432", true }
	t.Cleanup(func() { configBuildDatabaseEndpointFn = origEndpoint })

	got := probeDatabase()
	assert.Equal(t, probeOK, got.Status)
	assert.Equal(t, "localhost:5432", got.Endpoint, "display falls back to endpoint when migration URL is empty")
}

func TestProbeCache_EmptyEndpointDefensive(t *testing.T) {
	orig := configBuildCacheEndpointFn
	configBuildCacheEndpointFn = func() (string, bool) { return "", true }
	t.Cleanup(func() { configBuildCacheEndpointFn = orig })

	got := probeCache()
	assert.Equal(t, probeUnreachable, got.Status)
	assert.Contains(t, got.Reason, "incomplete")
}

func TestProbeQueue_EmptyEndpointDefensive(t *testing.T) {
	orig := configBuildQueueEndpointFn
	configBuildQueueEndpointFn = func() (string, bool) { return "", true }
	t.Cleanup(func() { configBuildQueueEndpointFn = orig })

	got := probeQueue()
	assert.Equal(t, probeUnreachable, got.Status)
	assert.Contains(t, got.Reason, "incomplete")
}

// menuActionEnterConnString — url.Parse error (lines 318-320). url.Parse
// is very permissive; bad percent-encoding is one of the few reliable
// trigger shapes ("%zz" is not valid percent-encoding).
func TestMenuActionEnterConnString_InvalidURLParseError(t *testing.T) {
	captureMenuOutput(t)
	reader := bufio.NewReader(strings.NewReader("http://%zz/\n"))
	_, err := menuActionEnterConnString(reader, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid URL")
}

// readKeyboardLoop — n == 0 continue branch (line 183-184): Read returns
// (0, nil) once then EOF.
func TestReadKeyboardLoop_ZeroByteRead(t *testing.T) {
	ch := make(chan keyboardSignal, 4)
	done := make(chan struct{})
	r := &zeroByteThenEOFReader{}
	readKeyboardLoop(r, ch, done)
	assert.Empty(t, ch)
	assert.GreaterOrEqual(t, r.calls, 2, "loop should retry after n==0")
}

// ─────────────────────────────────────────────────────────────────────
// promptPersistConnString gaps:
//   - empty kvs early return (407-409)
//   - mergeIntoDotEnv error wrap (431-433)
// ─────────────────────────────────────────────────────────────────────

func TestPromptPersistConnString_EmptyKVsIsNoOp(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(""))
	err := promptPersistConnString(reader, nil)
	assert.NoError(t, err)
}

func TestPromptPersistConnString_MergeError(t *testing.T) {
	captureMenuOutput(t)
	// Force mergeIntoDotEnv to fail by overriding osRenameFn.
	orig := osRenameFn
	osRenameFn = func(string, string) error { return errors.New("synthetic merge failure") }
	t.Cleanup(func() { osRenameFn = orig })

	// chdir to a fresh dir with a go.mod so ProjectEnvPrefix returns
	// non-empty; otherwise we'd hit the "no go.mod" branch instead.
	dir := t.TempDir()
	origCwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origCwd) })
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module github.com/test/myapp\n"), 0o644))
	require.NoError(t, os.Chdir(dir))

	reader := bufio.NewReader(strings.NewReader("y\n"))
	err := promptPersistConnString(reader, map[string]string{"DATABASE_HOST": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write .env")
}

// ─────────────────────────────────────────────────────────────────────
// resolveDevPlan missed branch (likely the in-docker + local-replaces
// error case).
// ─────────────────────────────────────────────────────────────────────

func TestResolveDevPlan_InDockerRejectsLocalReplaces(t *testing.T) {
	setupDevTempdir(t)
	// Create a compose.yaml so the "compose.yaml not found" branch
	// doesn't fire first.
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  app: {}\n"), 0o644))

	// Stub findLocalReplacesFn to report a local replace.
	origReplaces := findLocalReplacesFn
	findLocalReplacesFn = func(string) ([]localReplace, error) {
		return []localReplace{{Module: "example.com/foo", Path: "../foo"}}, nil
	}
	t.Cleanup(func() { findLocalReplacesFn = origReplaces })

	// Stub the compose service discovery to include "app" so inDocker=true.
	fakeExecOutput(t, `{"services":{"app":{}}}`, 0)

	_, err := resolveDevPlan(devFlags{servicesList: []string{"app"}, servicesRaw: "app"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replace")
}

// ─────────────────────────────────────────────────────────────────────
// airSignalHandler done branch (lines 887-893): Air exits on its own,
// the handler sees `done` close, and returns without signaling airCmd.
// ─────────────────────────────────────────────────────────────────────

// airSignalHandler keyboardRestart with a real running cmd — covers the
// `if airCmd.Process != nil { Signal(os.Interrupt) }` body (lines 902-904).
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
		func(r string) { called = r }, flag)
	assert.Equal(t, "restart", called)
	assert.True(t, flag.Load())
}

// airSignalHandler keyboardQuit with a real running cmd — covers lines
// 908-910 (the SIGINT-the-airCmd body inside the quit branch).
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
		func(r string) { called = r }, flag)
	assert.Equal(t, "quit", called)
	assert.False(t, flag.Load())
}

func TestAirSignalHandler_DoneBranch(t *testing.T) {
	sigChan := make(chan os.Signal, 1)
	keyCh := make(chan keyboardSignal, 1)
	done := make(chan struct{})
	close(done) // pre-closed so the select picks <-done immediately

	called := ""
	flag := &atomicBool{}
	// airCmd may be nil — the done branch never reaches airCmd.Process.
	airSignalHandler(sigChan, keyCh, done, nil, func(r string) { called = r }, flag)

	assert.Empty(t, called, "done branch must not call teardown")
	assert.False(t, flag.Load(), "restart flag must remain false")
}

// ─────────────────────────────────────────────────────────────────────
// runDevPipeline in-docker mode (lines 229-231 removeService, 405-431
// foreground path). Full pipeline test: compose.yaml + --services app,
// fake docker so the compose up app foreground exits 0 quickly.
// ─────────────────────────────────────────────────────────────────────

func TestRunDevPipeline_InDockerMode(t *testing.T) {
	setupDevTempdir(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  app:\n    image: testapp\n  db:\n    image: postgres\n"), 0o644))

	// Use stub for findLocalReplacesFn to ensure no local replaces block
	// the in-docker mode.
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
		} else if len(args) >= 2 && args[0] == "version" {
			stdout = "28.0\n"
		} else if hasComposeSub(args, "version") {
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

func TestRunDevPipeline_InDockerWithAttachLogs(t *testing.T) {
	// --attach-logs in inDocker mode hits the warn branch (line 365-367)
	// AND the supporting-services log streamer branch (line 422-425).
	setupDevTempdir(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  app:\n    image: testapp\n  db:\n    image: postgres\n"), 0o644))

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

// ─────────────────────────────────────────────────────────────────────
// runDevPipeline preflight menu integration: hasUnreachable=true and
// the menu returns menuCancel. Covers lines 296-298 (hasUnreachable=true
// menu run) and 303-305 (menuCancel return path).
// ─────────────────────────────────────────────────────────────────────

func TestRunDevPipeline_MenuCancelExits(t *testing.T) {
	setupDevTempdir(t)
	// Force probeDatabase to report unreachable so the menu runs.
	origDB := probeDatabaseFn
	probeDatabaseFn = func() probeResult {
		return probeResult{Dep: "database", Status: probeUnreachable, Reason: "boom"}
	}
	origCache := probeCacheFn
	probeCacheFn = func() probeResult { return probeResult{Dep: "cache", Status: probeNotConfigured} }
	origQueue := probeQueueFn
	probeQueueFn = func() probeResult { return probeResult{Dep: "queue", Status: probeNotConfigured} }
	t.Cleanup(func() {
		probeDatabaseFn = origDB
		probeCacheFn = origCache
		probeQueueFn = origQueue
	})
	// Force the menu to return menuCancel via the TTY=false short-circuit.
	origTTY := menuIsTTYFn
	menuIsTTYFn = func() bool { return false }
	t.Cleanup(func() { menuIsTTYFn = origTTY })
	// Capture menu output so the non-TTY printf doesn't pollute stdout.
	captureMenuOutput(t)

	withFakeExec(t, 0)
	// Reset withFakeExec's probe stubs; we want our own.
	probeDatabaseFn = func() probeResult {
		return probeResult{Dep: "database", Status: probeUnreachable, Reason: "boom"}
	}

	err := runDev(devFlags{envFile: ".env"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "preflight")
}

// ─────────────────────────────────────────────────────────────────────
// runDevPipeline no-DB mode (line 330-331 + 374-376).
// Drive via menuRunWithoutDB outcome.
// ─────────────────────────────────────────────────────────────────────

func TestRunDevPipeline_NoDBMode(t *testing.T) {
	setupDevTempdir(t)
	// Probes unreachable → menu runs.
	origDB := probeDatabaseFn
	probeDatabaseFn = func() probeResult {
		return probeResult{Dep: "database", Status: probeUnreachable}
	}
	origCache := probeCacheFn
	probeCacheFn = func() probeResult { return probeResult{Dep: "cache", Status: probeNotConfigured} }
	origQueue := probeQueueFn
	probeQueueFn = func() probeResult { return probeResult{Dep: "queue", Status: probeNotConfigured} }
	t.Cleanup(func() {
		probeDatabaseFn = origDB
		probeCacheFn = origCache
		probeQueueFn = origQueue
	})
	// Force TTY=true so the menu actually runs; pipe "3\n" (option [3]).
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

// ─────────────────────────────────────────────────────────────────────
// Teardown closure: resetVolumes branch (keepVolumes=false with
// services selected) — lines 268-271 + the `err == nil` Shutdown call.
// ─────────────────────────────────────────────────────────────────────

func TestRunDevPipeline_TeardownResetVolumes(t *testing.T) {
	setupDevTempdir(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
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
		keepVolumes:  false, // forces resetVolumes branch in teardown
	})
	assert.NoError(t, err)
}

// Teardown closure: stopServices error → "stopped-failed" Shutdown
// (lines 274-276). Make `compose stop` exit non-zero.
func TestRunDevPipeline_TeardownStopFails(t *testing.T) {
	setupDevTempdir(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	composeConfig := `{"services":{"db":{"healthcheck":{"test":["CMD","x"]}}}}`
	composePS := `[{"Service":"db","State":"running","Health":"healthy"}]`
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		exitCode := 0
		if hasComposeSub(args, "config") {
			stdout = composeConfig
		} else if hasComposeSub(args, "ps") {
			stdout = composePS
		} else if hasComposeSub(args, "stop") {
			exitCode = 1 // teardown's stopServices fails
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"="+itoa(exitCode),
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
	// Even with teardown stopServices failing, runDev itself succeeds
	// (Air ran fine). The failure path emits Shutdown(mode+"-failed", 1).
	assert.NoError(t, err)
}

// ─────────────────────────────────────────────────────────────────────
// runInDockerSupervisor — interruptCompose body (line 497-499) with a
// real running cmd. Existing tests all pass cmd=nil.
// ─────────────────────────────────────────────────────────────────────

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

// ─────────────────────────────────────────────────────────────────────
// resolveDevPlan: discovered profiles append branch (lines 596-599).
// detectComposeProfilesFn returns at least one profile that isn't in
// the initially-resolved set.
// ─────────────────────────────────────────────────────────────────────

func TestResolveDevPlan_DiscoveredProfilesAppended(t *testing.T) {
	setupDevTempdir(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))

	origProfiles := detectComposeProfilesFn
	detectComposeProfilesFn = func() ([]string, error) {
		return []string{"newone", ""}, nil // empty string in slice still ranges
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

// ─────────────────────────────────────────────────────────────────────
// runDevPipeline attachLogs in host mode (lines 362-364):
// attachLogs=true, orchestrate=true, !inDocker, services > 0.
// ─────────────────────────────────────────────────────────────────────

func TestRunDevPipeline_AttachLogsHostMode(t *testing.T) {
	setupDevTempdir(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
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

// ─────────────────────────────────────────────────────────────────────
// runDevPipeline menu option [2] starts services and the merge path
// fires (lines 313-316). Drive: probes report unreachable for database,
// TTY=true, stdin "2\n" to choose option [2], menuStartServicesFn and
// menuWaitHealthyFn stubbed to succeed, post-action reprobe reports OK.
// ─────────────────────────────────────────────────────────────────────

func TestRunDevPipeline_MenuStartedServicesMerged(t *testing.T) {
	setupDevTempdir(t)
	// First probe: database unreachable; after menu starts service, reprobe ok.
	probeCalls := 0
	origDB := probeDatabaseFn
	probeDatabaseFn = func() probeResult {
		probeCalls++
		if probeCalls == 1 {
			return probeResult{Dep: "database", Status: probeUnreachable}
		}
		return probeResult{Dep: "database", Status: probeOK, Endpoint: "localhost:5432"}
	}
	origCache := probeCacheFn
	probeCacheFn = func() probeResult { return probeResult{Dep: "cache", Status: probeNotConfigured} }
	origQueue := probeQueueFn
	probeQueueFn = func() probeResult { return probeResult{Dep: "queue", Status: probeNotConfigured} }
	t.Cleanup(func() {
		probeDatabaseFn = origDB
		probeCacheFn = origCache
		probeQueueFn = origQueue
	})

	forceTTY(t, true)
	pipeStdin(t, "2") // option [2]: start in Docker
	captureMenuOutput(t)

	// Menu's start/wait stubs succeed.
	origStart := menuStartServicesFn
	menuStartServicesFn = func([]string) error { return nil }
	t.Cleanup(func() { menuStartServicesFn = origStart })
	origWait := menuWaitHealthyFn
	menuWaitHealthyFn = func([]string) error { return nil }
	t.Cleanup(func() { menuWaitHealthyFn = origWait })

	withFakeExec(t, 0)
	// withFakeExec overwrites the probe stubs — set ours again.
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

// ─────────────────────────────────────────────────────────────────────
// runDev iter > 1 branch (line 153-155): pipeline asks for restart
// once, then succeeds on second iteration.
// ─────────────────────────────────────────────────────────────────────

// runDevPipeline fresh-volumes branch with orchestrate=true (lines
// 214-218). Existing fresh tests don't pass services so plan.orchestrate
// is false; this test sets servicesList so the branch fires.
func TestRunDevPipeline_FreshWithOrchestrate(t *testing.T) {
	setupDevTempdir(t)
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

// runDevPipeline fresh-volumes branch where resetVolumes fails (216-218).
// Same shape, but `compose down` exits non-zero.
func TestRunDevPipeline_FreshResetVolumesFailsWithOrchestrate(t *testing.T) {
	setupDevTempdir(t)
	stubProbesOK(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	composeConfig := `{"services":{"db":{}}}`
	composePS := `[{"Service":"db","State":"running","Health":""}]`
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		exitCode := 0
		if hasComposeSub(args, "config") {
			stdout = composeConfig
		} else if hasComposeSub(args, "ps") {
			stdout = composePS
		} else if hasComposeSub(args, "down") {
			exitCode = 1
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"="+itoa(exitCode),
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

// devCmd RunE deprecated --all-in-docker rewrite (lines 81-84):
// allInDocker=true and servicesRaw=="" → rewrite to "all".
func TestDevCmd_RunE_AllInDockerDeprecatedAlias(t *testing.T) {
	withFakeExec(t, 0)
	origFlags := devFlagValues
	t.Cleanup(func() { devFlagValues = origFlags })
	devFlagValues = devFlags{allInDocker: true, envFile: ".env"}
	// runDev would try to resolve services and bail without compose.yaml;
	// stub runDevPipelineFn to short-circuit so we only test the alias rewrite.
	origPipeline := runDevPipelineFn
	runDevPipelineFn = func(_ devFlags, _ devEmitter) (bool, error) { return false, nil }
	t.Cleanup(func() { runDevPipelineFn = origPipeline })

	require.NoError(t, devCmd.RunE(devCmd, nil))
	assert.Equal(t, "all", devFlagValues.servicesRaw)
}

func TestRunDev_RestartLoopIterates(t *testing.T) {
	calls := 0
	orig := runDevPipelineFn
	runDevPipelineFn = func(_ devFlags, _ devEmitter) (bool, error) {
		calls++
		if calls < 3 {
			return true, nil // ask for restart twice
		}
		return false, nil // third call exits cleanly
	}
	t.Cleanup(func() { runDevPipelineFn = orig })

	err := runDev(devFlags{})
	assert.NoError(t, err)
	assert.Equal(t, 3, calls, "pipeline should run 3 times (2 restarts + clean exit)")
}

// ─────────────────────────────────────────────────────────────────────
// runInDockerForeground (0%): exercise the docker-compose-up-app path
// using execCommand fake. The fake docker child exits 0 so the
// supervisor sees `exited` first and returns false.
// ─────────────────────────────────────────────────────────────────────

func TestRunInDockerForeground_StartFails(t *testing.T) {
	// Force exec.Cmd.Start() to fail. The fake exec uses re-exec of the
	// test binary which always starts successfully; instead we substitute
	// execCommand with one that returns a *exec.Cmd whose Path doesn't
	// exist, forcing Start to return an error.
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		c := exec.Command("/no/such/binary/", args...)
		return c
	}
	t.Cleanup(func() { execCommand = orig })

	keyCh := make(chan keyboardSignal, 1)
	restart, err := runInDockerForeground(func(string) {}, keyCh)
	assert.False(t, restart)
	require.Error(t, err)
}

func TestRunInDockerForeground_HappyPath_ChildExits(t *testing.T) {
	// Fake docker child exits 0; supervisor sees exited; teardown called
	// with "app-exited"; restart=false.
	withFakeExec(t, 0)

	keyCh := make(chan keyboardSignal, 1)
	called := ""
	restart, err := runInDockerForeground(func(r string) { called = r }, keyCh)
	assert.False(t, restart)
	assert.NoError(t, err)
	assert.Equal(t, "app-exited", called)
}

// ─────────────────────────────────────────────────────────────────────
// runInDockerSupervisor SIGINT branch (line ~502-507): an OS interrupt
// arrives on sigChan; the supervisor calls Process.Signal(os.Interrupt)
// on the foreground cmd, waits for it to exit, and returns false.
// ─────────────────────────────────────────────────────────────────────

func TestRunInDockerSupervisor_SIGINTBranch(t *testing.T) {
	// Drive the OS-signal branch by sending SIGINT to ourselves AFTER
	// the supervisor has called signal.Notify. The supervisor selects
	// over sigChan, keySignals, exited; with an empty exited channel
	// and no key press, sigChan wins as soon as the kernel delivers
	// the signal.
	//
	// Pass cmd=nil so interruptCompose is a no-op (it null-checks cmd
	// and cmd.Process). Supply exited via a delayed goroutine that
	// fires only AFTER SIGINT so the order of operations is:
	//
	//   1. supervisor blocks in select
	//   2. SIGINT arrives → supervisor enters sigChan case, calls
	//      interruptCompose (no-op), then `<-exited` blocks
	//   3. delayed goroutine sends to exited → supervisor unblocks,
	//      calls teardown("interrupted"), returns false
	exited := make(chan error, 1)
	keyCh := make(chan keyboardSignal, 1)

	go func() {
		time.Sleep(75 * time.Millisecond) // let signal.Notify register
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

// ─────────────────────────────────────────────────────────────────────
// readKeyboardLoop done-channel branches (dev_keyboard.go:175 and 191).
// Two paths:
//   - `done` is closed before any Read → returns immediately.
//   - `done` is closed AFTER a Read returns but before the byte is
//     interpreted → returns silently.
// ─────────────────────────────────────────────────────────────────────

func TestReadKeyboardLoop_DoneClosedBeforeRead(t *testing.T) {
	ch := make(chan keyboardSignal, 1)
	done := make(chan struct{})
	close(done)
	// in.Read should NEVER be called because the first select picks <-done.
	r := &neverReadReader{}
	readKeyboardLoop(r, ch, done)
	assert.Equal(t, 0, r.calls, "Read must not be called when done is closed first")
	assert.Empty(t, ch)
}

func TestReadKeyboardLoop_DoneClosedAfterRead(t *testing.T) {
	ch := make(chan keyboardSignal, 4)
	done := make(chan struct{})
	// Reader that returns one byte then signals done before next Read.
	r := &gateReader{
		bytes: []byte{'r'},
		afterRead: func() {
			close(done)
		},
	}
	readKeyboardLoop(r, ch, done)
	// The byte was an 'r' but the post-Read done check should have
	// short-circuited before sending sigKeyboardRestart.
	assert.Empty(t, ch, "post-Read done check must suppress the signal")
}

// ─────────────────────────────────────────────────────────────────────
// startKeyboardListener happy path (dev_keyboard.go:122-145).
// All seams ok; assert active=true + cancel() does not panic.
// ─────────────────────────────────────────────────────────────────────

func TestStartKeyboardListener_HappyPath(t *testing.T) {
	withTerminalStubs(t, true, nil)
	// Provide a working cancelableStdinReader via the seam — a
	// nopReadCloser is sufficient because the listener only Closes it.
	origNew := newCancelableStdinReaderFn
	newCancelableStdinReaderFn = func(fd int) (*cancelableStdinReader, error) {
		// Construct a real reader pointing at a fresh os.Pipe pair so
		// Close has something legitimate to close.
		r, w, err := os.Pipe()
		require.NoError(t, err)
		return &cancelableStdinReader{fd: fd, cancelR: r, cancelW: w}, nil
	}
	t.Cleanup(func() { newCancelableStdinReaderFn = origNew })

	signals, cancel, active := startKeyboardListener(newFakeKB(""), false)
	require.True(t, active)
	assert.NotNil(t, signals)
	cancel()
	cancel() // idempotent
}

func TestStartKeyboardListener_StdinReaderConstructionFails(t *testing.T) {
	withTerminalStubs(t, true, nil)
	origNew := newCancelableStdinReaderFn
	newCancelableStdinReaderFn = func(int) (*cancelableStdinReader, error) {
		return nil, errors.New("pipe boom")
	}
	t.Cleanup(func() { newCancelableStdinReaderFn = origNew })

	signals, cancel, active := startKeyboardListener(newFakeKB(""), false)
	assert.Nil(t, signals)
	assert.False(t, active)
	cancel() // no-op cancel must not panic
}

// ─────────────────────────────────────────────────────────────────────
// Test helpers ─────────────────────────────────────────────────────────
// ─────────────────────────────────────────────────────────────────────

// tier2ErrReader implements io.Reader and returns its configured error on
// every call. Used to drive the read-error branch of menu actions.
type tier2ErrReader struct{ err error }

func (e *tier2ErrReader) Read([]byte) (int, error) { return 0, e.err }

// neverReadReader counts Read invocations; used to verify the
// readKeyboardLoop done-first branch never touches the reader.
type neverReadReader struct{ calls int }

func (n *neverReadReader) Read(p []byte) (int, error) {
	n.calls++
	return 0, io.EOF
}

// zeroByteThenEOFReader returns (0, nil) on first call then EOF — used
// to exercise readKeyboardLoop's `if n == 0 { continue }` branch.
type zeroByteThenEOFReader struct{ calls int }

func (z *zeroByteThenEOFReader) Read(p []byte) (int, error) {
	z.calls++
	if z.calls == 1 {
		return 0, nil // continue branch
	}
	return 0, io.EOF
}

// gateReader returns one queued byte then runs afterRead, then EOFs.
type gateReader struct {
	bytes     []byte
	afterRead func()
	pos       int
}

func (g *gateReader) Read(p []byte) (int, error) {
	if g.pos >= len(g.bytes) {
		return 0, io.EOF
	}
	p[0] = g.bytes[g.pos]
	g.pos++
	if g.afterRead != nil {
		g.afterRead()
	}
	return 1, nil
}

// fakeCmdWithOutput is a one-shot wrapper around the test helper
// process that pipes a given stdout payload and exit code, used when
// scripting MULTIPLE distinct stubs in a single test (the package-
// wide fakeExecOutput overrides the seam globally which doesn't fit
// our switch-on-args needs here).
func fakeCmdWithOutput(t *testing.T, stdout string, code int) *exec.Cmd {
	t.Helper()
	cs := []string{"-test.run=TestHelperProcess", "--", "fake"}
	c := exec.Command(os.Args[0], cs...)
	c.Env = append(os.Environ(),
		"GOFASTA_WANT_HELPER_PROCESS=1",
		fakeEnvExitCode+"="+itoa(code),
		"GOFASTA_FAKE_STDOUT="+stdout,
	)
	return c
}

// itoa avoids pulling strconv into this file just for one call site.
func itoa(i int) string {
	// Single-call wrapper; strconv is already in the package via
	// other files but keeping this file's imports minimal.
	return strconvItoa(i)
}

