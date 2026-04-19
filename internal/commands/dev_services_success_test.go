package commands

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Success-path coverage for dev_services.go — the exit-0 branches
// where fake exec also supplies stdout via a scripted helper.
// ─────────────────────────────────────────────────────────────────────

// fakeExecOutput returns a function usable as execCommand that spawns
// the test binary's TestHelperProcess with a scripted stdout payload.
// Like fakeExecCommand but also sets GOFASTA_FAKE_STDOUT so the child
// prints it before exiting.
//
// so future "stdout plus non-zero exit" tests don't need to redefine
// the helper.
//
//nolint:unparam // exitCode is always 0 today; keep it parameterized
func fakeExecOutput(t *testing.T, stdout string, exitCode int) {
	t.Helper()
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
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
}

// TestDetectComposeServices_HappyPath — fake compose config returns
// two services with one healthcheck. Parser should surface them.
func TestDetectComposeServices_HappyPath(t *testing.T) {
	// The canonical `docker compose config --format json` shape.
	out := `{"services":{"db":{"healthcheck":{"test":["CMD","pg_isready"]}},"cache":{},"app":{}}}`
	fakeExecOutput(t, out, 0)
	available, hasHealth, err := detectComposeServices("")
	require.NoError(t, err)
	// "app" is the special-cased service name and gets filtered out.
	assert.ElementsMatch(t, []string{"db", "cache"}, available)
	assert.True(t, hasHealth["db"])
	assert.False(t, hasHealth["cache"])
}

// TestDetectComposeServices_MalformedJSON — docker compose config
// exits 0 but prints garbage. detectComposeServices surfaces the
// parse error cleanly.
func TestDetectComposeServices_MalformedJSON(t *testing.T) {
	fakeExecOutput(t, "not-json", 0)
	_, _, err := detectComposeServices("")
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
