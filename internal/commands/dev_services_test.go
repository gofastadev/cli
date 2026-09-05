package commands

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveServices_QueueSurfacesAsynqmonURL — a healthy queue
// service in compose produces a non-empty asynqmon URL.
func TestResolveServices_QueueSurfacesAsynqmonURL(t *testing.T) {
	out := `[{"Service":"queue","State":"running","Health":"healthy"}]`
	fakeExecOutput(t, out, 0)
	srv := &dashboardServer{svc: &devServices{selected: []string{"queue"}}}
	states, asynqmonURL := srv.resolveServices()
	assert.NotEmpty(t, states)
	assert.NotEmpty(t, asynqmonURL)
}

// TestResolveServices_EmptySelected — selected=nil also short-circuits.
func TestResolveServices_EmptySelected(t *testing.T) {
	srv := &dashboardServer{svc: &devServices{selected: nil}}
	states, asynqmonURL := srv.resolveServices()
	assert.Nil(t, states)
	assert.Empty(t, asynqmonURL)
}

// TestResolveServices_QueryError — queryServiceStates exits non-zero.
func TestResolveServices_QueryError(t *testing.T) {
	withFakeExec(t, 1)
	srv := &dashboardServer{svc: &devServices{selected: []string{"db"}}}
	states, asynqmonURL := srv.resolveServices()
	assert.Nil(t, states)
	assert.Empty(t, asynqmonURL)
}

func TestComposeFileExists_Missing(t *testing.T) {
	chdirTemp(t)
	assert.False(t, composeFileExists())
}

func TestComposeFileExists_Present(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	assert.True(t, composeFileExists())
}

// TestComposeAvailable_DockerMissing — execLookPath stubbed to return
// an error (docker not found) → composeAvailable returns false.
func TestComposeAvailable_DockerMissing(t *testing.T) {
	orig := execLookPath
	execLookPath = func(_ string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { execLookPath = orig })
	assert.False(t, composeAvailable())
}

// TestComposeAvailable_DaemonUp — docker on $PATH + `docker info`
// exits 0 → true.
func TestComposeAvailable_DaemonUp(t *testing.T) {
	orig := execLookPath
	execLookPath = func(_ string) (string, error) { return "/usr/bin/docker", nil }
	t.Cleanup(func() { execLookPath = orig })
	withFakeExec(t, 0)
	assert.True(t, composeAvailable())
}

// TestComposeAvailable_DaemonDown — docker on $PATH, `docker info`
// exits 1 → false.
func TestComposeAvailable_DaemonDown(t *testing.T) {
	orig := execLookPath
	execLookPath = func(_ string) (string, error) { return "/usr/bin/docker", nil }
	t.Cleanup(func() { execLookPath = orig })
	withFakeExec(t, 1)
	assert.False(t, composeAvailable())
}

// TestStartServices_Empty — nil / empty names is a no-op.
func TestStartServices_Empty(t *testing.T) {
	assert.NoError(t, startServices(nil, nil))
	assert.NoError(t, startServices([]string{}, []string{"cache"}))
}

func TestStartServices_HappyPath(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, startServices([]string{"db"}, nil))
}

func TestStartServices_WithProfile(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, startServices([]string{"cache"}, []string{"cache"}))
}

func TestStartServices_MultipleProfiles(t *testing.T) {
	withFakeExec(t, 0)
	// Multi-profile activation per the compose docs is additive — we
	// just verify the call doesn't error when both profiles are passed.
	assert.NoError(t, startServices([]string{"cache", "queue"}, []string{"cache", "queue"}))
}

func TestStartServices_DockerFails(t *testing.T) {
	withFakeExec(t, 1)
	err := startServices([]string{"db"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker compose up")
}

func TestStopServices_Empty(t *testing.T) {
	assert.NoError(t, stopServices(nil))
}

func TestStopServices_HappyPath(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, stopServices([]string{"db"}))
}

func TestStopServices_Failure(t *testing.T) {
	withFakeExec(t, 1)
	assert.Error(t, stopServices([]string{"db"}))
}

func TestResetVolumes_HappyPath(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, resetVolumes())
}

func TestResetVolumes_Failure(t *testing.T) {
	withFakeExec(t, 1)
	assert.Error(t, resetVolumes())
}

func TestQueryServiceStates_ExecFails(t *testing.T) {
	withFakeExec(t, 1)
	_, err := queryServiceStates()
	require.Error(t, err)
}

func TestDetectComposeServices_ExecFails(t *testing.T) {
	withFakeExec(t, 1)
	_, _, err := detectComposeServices(nil, false)
	require.Error(t, err)
}

// TestDetectComposeServices_HappyPath — fake compose config returns
// three services with one healthcheck. Parser should surface them, and
// "app" is filtered out by default (host-Air mode).
func TestDetectComposeServices_HappyPath(t *testing.T) {
	out := `{"services":{"db":{"healthcheck":{"test":["CMD","pg_isready"]}},"cache":{},"app":{}}}`
	fakeExecOutput(t, out, 0)
	available, hasHealth, err := detectComposeServices(nil, false)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"db", "cache"}, available)
	assert.True(t, hasHealth["db"])
	assert.False(t, hasHealth["cache"])
}

// TestDetectComposeServices_IncludeAppFlag — --all-in-docker mode
// passes includeApp=true so the app service survives the filter.
func TestDetectComposeServices_IncludeAppFlag(t *testing.T) {
	out := `{"services":{"db":{"healthcheck":{"test":["CMD","pg_isready"]}},"app":{}}}`
	fakeExecOutput(t, out, 0)
	available, _, err := detectComposeServices(nil, true)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"db", "app"}, available)
}

// TestDetectComposeServices_MultiProfile — passing multiple profiles
// is valid; we only assert the call returns cleanly. Compose v2's
// additive --profile semantics are the contract; the CLI just forwards
// the list.
func TestDetectComposeServices_MultiProfile(t *testing.T) {
	fakeExecOutput(t, `{"services":{"db":{},"cache":{},"queue":{}}}`, 0)
	available, _, err := detectComposeServices([]string{"cache", "queue"}, false)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"db", "cache", "queue"}, available)
}

// TestDetectComposeServices_MalformedJSON — docker compose config
// exits 0 but prints garbage. detectComposeServices surfaces the
// parse error cleanly.
func TestDetectComposeServices_MalformedJSON(t *testing.T) {
	fakeExecOutput(t, "not-json", 0)
	_, _, err := detectComposeServices(nil, false)
	require.Error(t, err)
}

// TestQueryServiceStates_ArrayFormat — newer compose returns a JSON
// array.
func TestQueryServiceStates_ArrayFormat(t *testing.T) {
	out := `[{"Service":"db","State":"running","Health":"healthy"},
	        {"Service":"cache","State":"running","Health":"starting"}]`
	fakeExecOutput(t, out, 0)
	states, err := queryServiceStates()
	require.NoError(t, err)
	require.Len(t, states, 2)
	assert.Equal(t, "db", states[0].Name)
	assert.Equal(t, "healthy", states[0].Health)
}

// TestQueryServiceStates_LineFormat — older compose returns one
// JSON object per line.
func TestQueryServiceStates_LineFormat(t *testing.T) {
	out := `{"Service":"db","State":"running","Health":"healthy"}
{"Service":"cache","State":"running"}`
	fakeExecOutput(t, out, 0)
	states, err := queryServiceStates()
	require.NoError(t, err)
	require.Len(t, states, 2)
	assert.Equal(t, "cache", states[1].Name)
}

// TestQueryServiceStates_EmptyOutput — compose reports zero services
// → nil slice, nil error.
func TestQueryServiceStates_EmptyOutput(t *testing.T) {
	fakeExecOutput(t, "", 0)
	states, err := queryServiceStates()
	require.NoError(t, err)
	assert.Nil(t, states)
}

// TestQueryServiceStates_MalformedArray — parse error path (array).
func TestQueryServiceStates_MalformedArray(t *testing.T) {
	fakeExecOutput(t, "[not-json", 0)
	_, err := queryServiceStates()
	require.Error(t, err)
}

// TestQueryServiceStates_MalformedLine — parse error path (line).
func TestQueryServiceStates_MalformedLine(t *testing.T) {
	fakeExecOutput(t, "not-json-line", 0)
	_, err := queryServiceStates()
	require.Error(t, err)
}

// TestWaitHealthy_EmptyReturnsNil — no services to wait on → nil.
func TestWaitHealthy_EmptyReturnsNil(t *testing.T) {
	assert.NoError(t, waitHealthy(nil, nil, time.Second, nil))
}

// TestWaitHealthy_HappyPath — fake exec reports the target service
// as running/healthy on the first poll; waitHealthy returns nil.
func TestWaitHealthy_HappyPath(t *testing.T) {
	out := `[{"Service":"db","State":"running","Health":"healthy"}]`
	fakeExecOutput(t, out, 0)

	var progressCalls int
	err := waitHealthy([]string{"db"}, map[string]bool{"db": true},
		2*time.Second, func(_, _ string, _ time.Duration) {
			progressCalls++
		})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, progressCalls, 1,
		"progress should be called at least once for the state transition")
}

// TestWaitHealthy_TimesOut — service never reaches ready state. With
// a short timeout the function returns an error naming the stuck
// service.
func TestWaitHealthy_TimesOut(t *testing.T) {
	out := `[{"Service":"db","State":"running","Health":"starting"}]`
	fakeExecOutput(t, out, 0)

	err := waitHealthy([]string{"db"}, map[string]bool{"db": true},
		750*time.Millisecond, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db")
}

// TestWaitHealthy_NoHealthcheckRunningIsEnough — services without a
// healthcheck declaration count as ready when they reach "running".
func TestWaitHealthy_NoHealthcheckRunningIsEnough(t *testing.T) {
	out := `[{"Service":"cache","State":"running","Health":""}]`
	fakeExecOutput(t, out, 0)
	err := waitHealthy([]string{"cache"}, map[string]bool{"cache": false},
		2*time.Second, nil)
	require.NoError(t, err)
}

// TestWaitHealthy_SleepBranch — covers the poll-interval sleep line.
// First poll returns "starting" (not ready, sleep runs); second poll
// returns "healthy" so the loop exits without blocking on the real
// 500ms poll interval.
func TestWaitHealthy_SleepBranch(t *testing.T) {
	orig := execCommand
	call := 0
	execCommand = func(name string, args ...string) *exec.Cmd {
		out := `[{"Service":"db","State":"running","Health":"starting"}]`
		if call > 0 {
			out = `[{"Service":"db","State":"running","Health":"healthy"}]`
		}
		call++
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"=0",
			"GOFASTA_FAKE_STDOUT="+out,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })

	origSleep := timeSleepFn
	var sleepCalls int
	timeSleepFn = func(_ time.Duration) { sleepCalls++ }
	t.Cleanup(func() { timeSleepFn = origSleep })

	err := waitHealthy([]string{"db"}, map[string]bool{"db": true},
		5*time.Second, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, sleepCalls, 1,
		"expected the poll-interval sleep to run between polls")
}

func TestSelectServices_EmptyRawReturnsEmpty(t *testing.T) {
	got, err := selectServices([]string{"app", "db", "cache"}, "")
	assert.NoError(t, err)
	assert.Empty(t, got)
}

func TestSelectServices_WhitespaceRawReturnsEmpty(t *testing.T) {
	got, err := selectServices([]string{"app", "db"}, "  \t  ")
	assert.NoError(t, err)
	assert.Empty(t, got)
}

func TestSelectServices_AllAliasReturnsEverything(t *testing.T) {
	got, err := selectServices([]string{"app", "db", "cache", "queue"}, "all")
	assert.NoError(t, err)
	assert.Equal(t, []string{"app", "db", "cache", "queue"}, got)
}

func TestSelectServices_AllAliasCaseInsensitive(t *testing.T) {
	got, err := selectServices([]string{"app", "db"}, "ALL")
	assert.NoError(t, err)
	assert.Equal(t, []string{"app", "db"}, got)
}

func TestSelectServices_ExplicitListAllValid(t *testing.T) {
	got, err := selectServices([]string{"app", "db", "cache", "queue"}, "db,cache")
	assert.NoError(t, err)
	assert.Equal(t, []string{"db", "cache"}, got)
}

func TestSelectServices_PreservesInputOrder(t *testing.T) {
	got, err := selectServices([]string{"app", "db", "cache", "queue"}, "queue,db,cache")
	assert.NoError(t, err)
	assert.Equal(t, []string{"queue", "db", "cache"}, got)
}

func TestSelectServices_AppIsValidName(t *testing.T) {
	got, err := selectServices([]string{"app", "db"}, "db,app")
	assert.NoError(t, err)
	assert.Equal(t, []string{"db", "app"}, got)
}

func TestSelectServices_UnknownNameErrorsWithSuggestion(t *testing.T) {
	_, err := selectServices([]string{"app", "db", "cache", "queue", "lavinmq"}, "lavinmw")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no service named "lavinmw"`)
	assert.Contains(t, err.Error(), "Available services:")
	assert.Contains(t, err.Error(), "Did you mean: lavinmq?")
}

func TestSelectServices_UnknownNameNoSuggestionWhenFar(t *testing.T) {
	_, err := selectServices([]string{"app", "db"}, "elasticsearch")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no service named "elasticsearch"`)
	assert.NotContains(t, err.Error(), "Did you mean")
}

func TestSelectServices_StopsAtFirstUnknown(t *testing.T) {
	// "db" is valid, "redes" is a typo for "redis" but redis isn't in
	// available, so the error mentions "redes" and lists what IS
	// available. We don't try to be clever about "process the valid
	// ones and report the invalid one separately" — the dev's command
	// line is wrong, surface that.
	_, err := selectServices([]string{"app", "db", "cache"}, "db,redes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"redes"`)
}

func TestSelectServices_HandlesWhitespaceAroundNames(t *testing.T) {
	got, err := selectServices([]string{"app", "db", "cache"}, "  db ,  cache  ")
	assert.NoError(t, err)
	assert.Equal(t, []string{"db", "cache"}, got)
}

// TestWaitHealthy_WantedNotSeen — a wanted service never appears in
// `docker compose ps` output → allReady=false → timeout. The
// wanted-but-not-seen branch fires.
func TestWaitHealthy_WantedNotSeen(t *testing.T) {
	// Return an empty list so nothing matches.
	out := `[]`
	fakeExecOutput(t, out, 0)
	err := waitHealthy([]string{"db"}, map[string]bool{"db": false},
		700*time.Millisecond, nil)
	require.Error(t, err)
}

// TestIsServiceReady — readiness rules per healthcheck declaration.
func TestIsServiceReady(t *testing.T) {
	t.Run("with healthcheck: healthy = ready", func(t *testing.T) {
		assert.True(t, isServiceReady(serviceState{State: "running", Health: "healthy"}, true))
	})
	t.Run("with healthcheck: starting = not ready", func(t *testing.T) {
		assert.False(t, isServiceReady(serviceState{State: "running", Health: "starting"}, true))
	})
	t.Run("with healthcheck: unhealthy = not ready", func(t *testing.T) {
		assert.False(t, isServiceReady(serviceState{State: "running", Health: "unhealthy"}, true))
	})
	t.Run("without healthcheck: running = ready", func(t *testing.T) {
		assert.True(t, isServiceReady(serviceState{State: "running"}, false))
	})
	t.Run("without healthcheck: exited = not ready", func(t *testing.T) {
		assert.False(t, isServiceReady(serviceState{State: "exited"}, false))
	})
}

// TestDetectComposeServices_WithProfile — non-empty profiles list adds
// one --profile arg per entry.
func TestDetectComposeServices_WithProfile(t *testing.T) {
	fakeExecOutput(t, `{"services":{"db":{}}}`, 0)
	_, _, err := detectComposeServices([]string{"cache"}, false)
	require.NoError(t, err)
}

// TestQueryServiceStates_EmptyLinesSkipped — line-format stdout with
// blank lines between entries still parses.
func TestQueryServiceStates_EmptyLinesSkipped(t *testing.T) {
	out := `{"Service":"db","State":"running"}

{"Service":"cache","State":"running"}
`
	fakeExecOutput(t, out, 0)
	states, err := queryServiceStates()
	require.NoError(t, err)
	require.Len(t, states, 2)
}

// TestWaitHealthy_QueryErrorPropagates — queryServiceStates fails.
func TestWaitHealthy_QueryErrorPropagates(t *testing.T) {
	// fakeExecOutput with non-JSON stdout makes parse fail inside
	// queryServiceStates.
	fakeExecOutput(t, "not-json", 0)
	err := waitHealthy([]string{"db"}, map[string]bool{"db": false},
		time.Second, nil)
	require.Error(t, err)
}

// TestWaitHealthy_UnknownServiceFilteredOut — states returned include
// a service not in wanted set. The continue branch runs.
func TestWaitHealthy_UnknownServiceFilteredOut(t *testing.T) {
	out := `[{"Service":"extra","State":"running"},
	        {"Service":"db","State":"running","Health":""}]`
	fakeExecOutput(t, out, 0)
	err := waitHealthy([]string{"db"}, map[string]bool{"db": false},
		2*time.Second, nil)
	require.NoError(t, err)
}

// removeService — pure slice filter; covers the function which was
// previously unreached from any test.
func TestRemoveService_FiltersTarget(t *testing.T) {
	got := removeService([]string{"a", "b", "c"}, "b")
	assert.Equal(t, []string{"a", "c"}, got)
}

func TestRemoveService_TargetAbsent(t *testing.T) {
	got := removeService([]string{"a", "c"}, "b")
	assert.Equal(t, []string{"a", "c"}, got)
}

func TestRemoveService_EmptyInput(t *testing.T) {
	got := removeService(nil, "b")
	assert.Empty(t, got)
}

// startServices empty-names short-circuit.
func TestStartServices_EmptyNamesShortCircuit(t *testing.T) {
	require.NoError(t, startServices(nil, []string{"x"}))
}

// startServices skip-empty-profile branch — one profile is "", one is
// "p1"; only p1 should become --profile p1.
func TestStartServices_SkipsEmptyProfileEntries(t *testing.T) {
	withFakeExec(t, 0)
	require.NoError(t, startServices([]string{"db"}, []string{"", "p1"}))
}

// detectComposeServices skip-empty-profile branch.
func TestDetectComposeServices_SkipsEmptyProfiles(t *testing.T) {
	fakeExecOutput(t, `{"services":{"db":{}}}`, 0)
	_, _, err := detectComposeServices([]string{""}, false)
	require.NoError(t, err)
}

// detectComposeProfiles parses identifier lines, skipping blanks and
// any line containing JSON-shape characters (defensive guard).
func TestDetectComposeProfiles_ParsesAndSkipsBlanksAndJSON(t *testing.T) {
	fakeExecOutput(t, "p1\n\n{not-a-profile}\np2\n", 0)
	got, err := detectComposeProfiles()
	require.NoError(t, err)
	assert.Equal(t, []string{"p1", "p2"}, got)
}

// TestRunDev_Fresh_ResetVolumesFails — resetVolumes returns an
// error; runDev logs a warning and continues.
func TestRunDev_Fresh_ResetVolumesFails(t *testing.T) {
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
		exitCode := 0
		if hasComposeSub(args, "config") {
			stdout = composeConfig
		} else if hasComposeSub(args, "ps") {
			stdout = composePS
		} else if hasComposeSub(args, "down") {
			exitCode = 1 // resetVolumes fails
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
	err := runDev(devFlags{envFile: ".env", waitTimeout: 5e9,
		keepVolumes: true, fresh: true})
	assert.NoError(t, err)
}

// TestRunDev_StartServicesFails — compose config ok but compose up
// fails.
func TestRunDev_StartServicesFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		exitCode := 0
		if hasComposeSub(args, "config") {
			stdout = `{"services":{"db":{}}}`
		} else if hasComposeSub(args, "up") {
			exitCode = 1
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

// TestRunDev_WaitHealthyFails — compose up succeeds but services never
// become healthy in the short timeout.
func TestRunDev_WaitHealthyFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	require.NoError(t, os.WriteFile("compose.yaml",
		[]byte("services:\n  db:\n    image: postgres\n"), 0o644))
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		stdout := ""
		if hasComposeSub(args, "config") {
			stdout = `{"services":{"db":{"healthcheck":{"test":["CMD","pg_isready"]}}}}`
		} else if hasComposeSub(args, "ps") {
			stdout = `[{"Service":"db","State":"running","Health":"starting"}]`
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
		waitTimeout:  500000000, // 500ms
		keepVolumes:  true,
		servicesList: []string{"db"},
		servicesRaw:  "db",
		noKeyboard:   true,
	})
	require.Error(t, err)
}

func stubComposeAvailable(t *testing.T, ok bool) {
	t.Helper()
	orig := composeAvailableFn
	composeAvailableFn = func() bool { return ok }
	t.Cleanup(func() { composeAvailableFn = orig })
}

// hasComposeSub reports whether `args` represents a `docker compose
// [--profile X]... <sub> ...` invocation. The exec stubs in this file
// matched the subcommand by literal positional index before
// runDev started prepending profile flags; this helper restores the
// match logic to "subcommand-by-content" so multi-profile invocations
// route to the correct stub.
func hasComposeSub(args []string, sub string) bool {
	if len(args) < 2 || args[0] != "compose" {
		return false
	}
	i := 1
	for i+1 < len(args) && args[i] == "--profile" {
		i += 2
	}
	return i < len(args) && args[i] == sub
}
