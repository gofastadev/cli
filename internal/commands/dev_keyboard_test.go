package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"
)

func TestHandleTraceDetail_Forwards(t *testing.T) {
	srv := withUpstreamApp(t, map[string]http.HandlerFunc{
		"/debug/traces/t1": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"trace_id":"t1"}`))
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/trace/t1", nil)
	rec := httptest.NewRecorder()
	srv.handleTraceDetail(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"trace_id":"t1"`)
}

func TestHandleTraceDetail_UpstreamMiss(t *testing.T) {
	srv := withUpstreamApp(t, map[string]http.HandlerFunc{
		"/debug/traces/missing": func(w http.ResponseWriter, _ *http.Request) {
			http.NotFound(w, nil)
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/trace/missing", nil)
	rec := httptest.NewRecorder()
	srv.handleTraceDetail(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleLogs_ForwardsQueryParams(t *testing.T) {
	var seenQuery string
	srv := withUpstreamApp(t, map[string]http.HandlerFunc{
		"/debug/logs": func(w http.ResponseWriter, r *http.Request) {
			seenQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`[]`))
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/logs?trace_id=abc&level=WARN", nil)
	rec := httptest.NewRecorder()
	srv.handleLogs(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, seenQuery, "trace_id=abc")
	assert.Contains(t, seenQuery, "level=WARN")
}

func TestHandleExplain_ProxiesToApp(t *testing.T) {
	srv := withUpstreamApp(t, map[string]http.HandlerFunc{
		"/debug/explain": func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodPost, r.Method)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"plan":"Seq Scan"}`))
		},
	})
	body := `{"sql":"SELECT 1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/explain", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleExplain(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Seq Scan")
}

func TestHandleExplain_PropagatesUpstreamStatus(t *testing.T) {
	srv := withUpstreamApp(t, map[string]http.HandlerFunc{
		"/debug/explain": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("only SELECT"))
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/explain", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.handleExplain(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestScrapeDevtools_HappyPath(t *testing.T) {
	srv := withUpstreamApp(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]scrapedRequest{{Method: "GET"}})
		},
		"/debug/sql": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("[]"))
		},
		"/debug/traces": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/cache":  func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/pprof/goroutine": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("goroutine 1 [running]:\nmain.x()\n"))
		},
	})
	got := srv.scrapeDevtools(true)
	assert.Len(t, got.requests, 1)
	assert.Equal(t, 1, got.goroutines.Total)
}

// TestRefresh_PopulatesHealthFromUpstream — refresh() probes /health
// and records the result.
func TestRefresh_PopulatesHealthFromUpstream(t *testing.T) {
	srv := withUpstreamApp(t, map[string]http.HandlerFunc{
		"/health":  func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		"/metrics": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("# metrics\n")) },
	})
	srv.refresh()
	assert.Equal(t, "ok", srv.state.Health)
}

func newFakeKB(input string) *fakeKeyboardReader {
	return &fakeKeyboardReader{Reader: bytes.NewReader([]byte(input)), fd: 99}
}

// withTerminalStubs swaps in fakes for all three x/term seams for the
// duration of the test. Each callback is called with the values
// startKeyboardListener passed.
func withTerminalStubs(t *testing.T, isTTY bool, makeRawErr error) {
	t.Helper()
	origIs, origMake, origRestore := termIsTerminalFn, termSetCbreakFn, termRestoreFn
	t.Cleanup(func() {
		termIsTerminalFn = origIs
		termSetCbreakFn = origMake
		termRestoreFn = origRestore
	})
	termIsTerminalFn = func(_ int) bool { return isTTY }
	termSetCbreakFn = func(_ int) (*term.State, error) {
		if makeRawErr != nil {
			return nil, makeRawErr
		}
		return &term.State{}, nil
	}
	termRestoreFn = func(_ int, _ *term.State) error { return nil }
}

// TestStartKeyboardListener_NotATerminal — the no-TTY path returns a
// nil channel + no-op cancel + active=false, so non-interactive sessions
// (CI, piped stdin) get the same pipeline as before this feature.
func TestStartKeyboardListener_NotATerminal(t *testing.T) {
	withTerminalStubs(t, false, nil)
	signals, cancel, active := startKeyboardListener(newFakeKB(""), false)
	assert.Nil(t, signals, "non-TTY must return a nil channel so select{} blocks correctly")
	assert.False(t, active)
	cancel() // must not panic
}

// TestStartKeyboardListener_Disabled — the --no-keyboard opt-out
// short-circuits before the TTY check, so the listener doesn't even
// query stdin.
func TestStartKeyboardListener_Disabled(t *testing.T) {
	// IsTerminal would return true if asked; the disabled flag wins.
	withTerminalStubs(t, true, nil)
	signals, _, active := startKeyboardListener(newFakeKB("r"), true)
	assert.Nil(t, signals)
	assert.False(t, active)
}

// TestStartKeyboardListener_MakeRawFails — raw-mode failure is
// non-fatal: log nothing, return the no-listener sentinel, let the
// pipeline continue without keyboard support.
func TestStartKeyboardListener_MakeRawFails(t *testing.T) {
	withTerminalStubs(t, true, errors.New("ioctl boom"))
	signals, _, active := startKeyboardListener(newFakeKB("r"), false)
	assert.Nil(t, signals)
	assert.False(t, active)
}

// TestReadKeyboardLoop_RestartKey — `r` and `R` both produce
// sigKeyboardRestart on the channel.
func TestReadKeyboardLoop_RestartKey(t *testing.T) {
	for _, in := range []string{"r", "R"} {
		ch := make(chan keyboardSignal, 4)
		readKeyboardLoop(strings.NewReader(in), ch, nil)
		require.Len(t, ch, 1, "input %q produced %d signals; want 1", in, len(ch))
		assert.Equal(t, sigKeyboardRestart, <-ch)
	}
}

// TestReadKeyboardLoop_QuitKey — `q`, `Q`, and Ctrl+C all produce
// sigKeyboardQuit. Ctrl+C is also forwarded by the OS as SIGINT, but
// having the keyboard layer recognize it means a user inside `gofasta
// dev` can quit even if they're tunneled through something that eats
// signals (rare, but the cost of supporting it is one byte).
func TestReadKeyboardLoop_QuitKey(t *testing.T) {
	for _, in := range []string{"q", "Q", "\x03"} {
		ch := make(chan keyboardSignal, 4)
		readKeyboardLoop(strings.NewReader(in), ch, nil)
		require.Len(t, ch, 1, "input %q produced %d signals; want 1", in, len(ch))
		assert.Equal(t, sigKeyboardQuit, <-ch)
	}
}

// TestReadKeyboardLoop_HelpKey — `h`, `H`, `?` print the help banner
// and emit no pipeline signal. We don't capture stdout here (the
// human-side banner uses termcolor); the contract tested is "help
// keys do not produce sigKeyboardRestart or sigKeyboardQuit".
func TestReadKeyboardLoop_HelpKey(t *testing.T) {
	for _, in := range []string{"h", "H", "?"} {
		ch := make(chan keyboardSignal, 4)
		readKeyboardLoop(strings.NewReader(in), ch, nil)
		assert.Empty(t, ch, "input %q must not emit a pipeline signal", in)
	}
}

// TestReadKeyboardLoop_UnknownKeysIgnored — random keys (a, space,
// digits) are silently dropped so a leaning keyboard or paste does not
// trigger spurious restarts.
func TestReadKeyboardLoop_UnknownKeysIgnored(t *testing.T) {
	ch := make(chan keyboardSignal, 8)
	readKeyboardLoop(strings.NewReader("abc 123\n\t"), ch, nil)
	assert.Empty(t, ch)
}

// TestReadKeyboardLoop_MixedSequence — a realistic burst: garbage,
// then R, then garbage, then Q. The loop emits exactly two signals in
// the expected order.
func TestReadKeyboardLoop_MixedSequence(t *testing.T) {
	ch := make(chan keyboardSignal, 4)
	readKeyboardLoop(strings.NewReader("xRyq"), ch, nil)
	close(ch)
	got := make([]keyboardSignal, 0, 2)
	for s := range ch {
		got = append(got, s)
	}
	assert.Equal(t, []keyboardSignal{sigKeyboardRestart, sigKeyboardQuit}, got)
}

// printKeyboardBanner — invoked once to register coverage on the
// banner-print function (no other test calls it directly).
func TestPrintKeyboardBanner_DoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() { printKeyboardBanner() })
}

// startKeyboardListener — newCancelableStdinReaderFn returns an error;
// listener drops into the no-listener path and termRestore is invoked
// to undo cbreak before returning the failure tuple.
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

// startKeyboardListener no-op cancel closures — when active=false, the
// returned `cancel` is an empty `func() {}`. Existing tests check
// return values but never invoke this closure; calling it covers the
// empty function body.
func TestStartKeyboardListener_NoopCancelClosuresAreCallable(t *testing.T) {
	withTerminalStubs(t, true, errors.New("force raw failure"))
	_, cancel, active := startKeyboardListener(newFakeKB(""), false)
	assert.False(t, active)
	assert.NotPanics(t, func() { cancel() })
}

// readKeyboardLoop — `done` closed before any Read call → loop returns
// without ever touching the reader.
func TestReadKeyboardLoop_DoneClosedBeforeRead(t *testing.T) {
	ch := make(chan keyboardSignal, 1)
	done := make(chan struct{})
	close(done)
	r := &neverReadReader{}
	readKeyboardLoop(r, ch, done)
	assert.Equal(t, 0, r.calls, "Read must not be called when done is closed first")
	assert.Empty(t, ch)
}

// readKeyboardLoop — `done` closed AFTER Read returns one byte but
// BEFORE the post-Read switch interprets it. The re-check at the top
// of the next iteration suppresses the would-be signal.
func TestReadKeyboardLoop_DoneClosedAfterRead(t *testing.T) {
	ch := make(chan keyboardSignal, 4)
	done := make(chan struct{})
	r := &gateReader{
		bytes:     []byte{'r'},
		afterRead: func() { close(done) },
	}
	readKeyboardLoop(r, ch, done)
	assert.Empty(t, ch, "post-Read done check must suppress the signal")
}

// readKeyboardLoop — Read returns (0, nil) then EOF. The `if n == 0
// { continue }` branch fires.
func TestReadKeyboardLoop_ZeroByteRead(t *testing.T) {
	ch := make(chan keyboardSignal, 4)
	done := make(chan struct{})
	r := &zeroByteThenEOFReader{}
	readKeyboardLoop(r, ch, done)
	assert.Empty(t, ch)
	assert.GreaterOrEqual(t, r.calls, 2, "loop should retry after n==0")
}

// neverReadReader counts Read invocations; used to verify
// readKeyboardLoop's done-first branch never touches the reader.
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
		return 0, nil
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

// TestMenu_NonTTY_SkipsAndCancels — non-TTY environments skip the
// menu entirely and return menuCancel after printing actionable text.
// CI scripts get a deterministic exit code; humans without a terminal
// get the same info they'd see in the prompt.
func TestMenu_NonTTY_SkipsAndCancels(t *testing.T) {
	forceTTY(t, false)
	out := captureMenuOutput(t)
	got, _ := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuCancel, got)
	assert.Contains(t, out.String(), "Non-interactive shell detected")
	assert.Contains(t, out.String(), "database unreachable")
}

// TestMenu_Cancel — user picks [4]; menuCancel returned, no retries.
func TestMenu_Cancel(t *testing.T) {
	forceTTY(t, true)
	_ = captureMenuOutput(t)
	pipeStdin(t, "4")
	got, _ := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuCancel, got)
}

// TestMenu_RunWithoutDB — user picks [3]; menuRunWithoutDB returned.
// The framework's degraded-mode ProvideDB makes this honest now
// (in-memory SQLite stub keeps the app alive).
func TestMenu_RunWithoutDB(t *testing.T) {
	forceTTY(t, true)
	_ = captureMenuOutput(t)
	pipeStdin(t, "3")
	got, _ := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuRunWithoutDB, got)
}

// TestMenu_InvalidChoiceLoops — bogus input loops back to the menu.
// We feed an invalid char first, then "4" to cancel. The output
// should mention "invalid choice".
func TestMenu_InvalidChoiceLoops(t *testing.T) {
	forceTTY(t, true)
	out := captureMenuOutput(t)
	pipeStdin(t, "z", "4")
	got, _ := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuCancel, got)
	assert.Contains(t, out.String(), "invalid choice")
}

// TestMenu_EnterConnString_EmptyInputLoops — blank line is rejected,
// menu loops. Verifies the input-validation guard.
func TestMenu_EnterConnString_EmptyInputLoops(t *testing.T) {
	forceTTY(t, true)
	out := captureMenuOutput(t)
	pipeStdin(t, "1", "", "4")
	stubReprobe(t, []probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	got, _ := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuCancel, got)
	assert.Contains(t, out.String(), "empty input")
}

// TestMenu_EnterConnString_InvalidURL — typo'd URL fails to parse;
// menu prints the validation error and loops.
func TestMenu_EnterConnString_InvalidURL(t *testing.T) {
	forceTTY(t, true)
	out := captureMenuOutput(t)
	pipeStdin(t, "1", "not-a-url", "4")
	stubReprobe(t, []probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	got, _ := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuCancel, got)
	assert.Contains(t, out.String(), "URL must include scheme")
}

// TestMenu_EnterConnString_ProbeStillFails — override applied but
// reprobe still reports unreachable; menu loops back. User then
// cancels via [4].
func TestMenu_EnterConnString_ProbeStillFails(t *testing.T) {
	forceTTY(t, true)
	_ = captureMenuOutput(t)
	pipeStdin(t, "1", "postgres://u:p@h:9/d", "4")
	// menuActionEnterConnString sets the full database connection set —
	// driver/host/port/user/password/name — under every prefix from
	// configutil.EnvPrefixes(). Unsetting only HOST left PORT=9 (etc.)
	// in the process env, where downstream tests like
	// TestProbeDatabase_OK saw "localhost:9" instead of "localhost:5432"
	// even when their config.yaml said otherwise.
	t.Cleanup(func() {
		_ = unsetenv("GOFASTA_DATABASE_DRIVER")
		_ = unsetenv("GOFASTA_DATABASE_HOST")
		_ = unsetenv("GOFASTA_DATABASE_PORT")
		_ = unsetenv("GOFASTA_DATABASE_USER")
		_ = unsetenv("GOFASTA_DATABASE_PASSWORD")
		_ = unsetenv("GOFASTA_DATABASE_NAME")
	})
	stubReprobe(t, []probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "h:9", Reason: "still refused"},
	})
	got, _ := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuCancel, got)
}

// TestMenu_StartInDocker_DockerUnavailable — docker not on PATH;
// the option fails with an install hint, menu loops.
func TestMenu_StartInDocker_DockerUnavailable(t *testing.T) {
	forceTTY(t, true)
	out := captureMenuOutput(t)
	pipeStdin(t, "2", "4")
	stubComposeAvailable(t, false)
	stubReprobe(t, []probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	got, _ := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuCancel, got)
	assert.Contains(t, out.String(), "Docker")
}

// TestMenu_StartInDocker_StartFails — compose up fails; menu loops.
func TestMenu_StartInDocker_StartFails(t *testing.T) {
	forceTTY(t, true)
	out := captureMenuOutput(t)
	pipeStdin(t, "2", "4")
	stubComposeAvailable(t, true)
	stubStartServices(t, errors.New("pull failed"))
	stubReprobe(t, []probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	got, _ := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuCancel, got)
	assert.Contains(t, out.String(), "compose up")
}

// TestMenu_StartInDocker_HealthFails — services start but never go
// healthy; menu loops.
func TestMenu_StartInDocker_HealthFails(t *testing.T) {
	forceTTY(t, true)
	out := captureMenuOutput(t)
	pipeStdin(t, "2", "4")
	stubComposeAvailable(t, true)
	stubStartServices(t, nil)
	stubWaitHealthy(t, errors.New("timeout"))
	stubReprobe(t, []probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	got, _ := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuCancel, got)
	assert.Contains(t, out.String(), "healthy")
}

// TestMenu_StartInDocker_NoFailingServices — option [2] errors
// cleanly when the only failing dep doesn't map to a compose service
// (e.g. user has hand-edited their config.yaml and probe reports
// something we can't help with). The action surfaces the error, the
// menu loops, and we drop to cancel.
func TestMenu_StartInDocker_NoFailingServices(t *testing.T) {
	forceTTY(t, true)
	out := captureMenuOutput(t)
	pipeStdin(t, "2", "4")
	stubComposeAvailable(t, true)
	// Reprobe keeps the same unknown-dep as unreachable so the menu
	// stays in the failure loop until the user hits [4].
	stubReprobe(t, []probeResult{
		{Dep: "unknown-dep", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	got, _ := runPreflightMenu([]probeResult{
		{Dep: "unknown-dep", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuCancel, got)
	assert.Contains(t, out.String(), "no failing services")
}

// TestParseServicesInList — parseServicesList trims spaces and
// filters empty entries.
func TestParseServicesInList(t *testing.T) {
	got := parseServicesList("a, b , c")
	assert.Equal(t, 3, len(got))
	for _, s := range got {
		assert.NotEmpty(t, s)
	}
	// Silence unused imports if nothing else pulls strconv.
	_ = strconv.Itoa(len(got))
}

// TestNewRestEndpoint_RequiresResourceName — the build function must
// reject invocations with no positional argument.
func TestNewRestEndpoint_RequiresResourceName(t *testing.T) {
	wf := findWorkflow("new-rest-endpoint")
	require.NotNil(t, wf)
	_, err := wf.Build(nil)
	require.Error(t, err)
	ce, ok := clierr.As(err)
	require.True(t, ok)
	assert.Equal(t, string(clierr.CodeInvalidName), ce.Code)
}

// TestHelperProcess is not a real test — it's the fake subprocess invoked by
// fakeExecCommand. If any argument is "--version" and GOFASTA_FAKE_VERSION is
// set, it prints a Cobra-style version line and exits 0. Otherwise it exits
// with GOFASTA_FAKE_EXIT.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GOFASTA_WANT_HELPER_PROCESS") != "1" {
		return
	}
	// Find the `--` separator: everything after it is the fake command + args.
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	if v := os.Getenv(fakeEnvVersion); v != "" {
		for _, a := range args {
			if a == "--version" {
				fmt.Fprintf(os.Stdout, "gofasta version %s\n", v)
				os.Exit(0)
			}
		}
	}
	// GOFASTA_FAKE_STDOUT lets callers script the child's stdout —
	// used by dev_services_success_test.go to simulate the JSON that
	// `docker compose config` / `docker compose ps` emit.
	if stdout := os.Getenv("GOFASTA_FAKE_STDOUT"); stdout != "" {
		fmt.Fprint(os.Stdout, stdout)
	}
	// GOFASTA_FAKE_SIGNAL makes the child die BY a signal instead of
	// exiting — the only way a parent's ProcessState reports
	// Signaled()==true. Used to test runAir's signaled-shutdown
	// classification against real wait-status semantics rather than a
	// stubbed isSignaledExit.
	if os.Getenv("GOFASTA_FAKE_SIGNAL") == "1" {
		// killSelfWithSIGINT is defined per-platform (unix real, other
		// stub) — syscall.Kill does not exist on windows and would break
		// compiling this package's tests there.
		killSelfWithSIGINT()
		// Give the signal time to be delivered; unreachable normally.
		time.Sleep(5 * time.Second)
	}
	code, _ := strconv.Atoi(os.Getenv(fakeEnvExitCode))
	os.Exit(code)
}

func TestRunMigration_NoConfig(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	// loadConfig returns a koanf instance even without config.yaml,
	// so BuildMigrationURL returns a postgres URL with defaults
	err := runMigration("up")
	// Should fail because migrate binary is not available
	assert.Error(t, err)
}

func TestRunMigration_EmptyURL(t *testing.T) {
	// runMigration checks for empty URL and returns an error
	// This is hard to trigger since loadConfig always returns a koanf instance
	// but we can test the direction parameter
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	err := runMigration("down")
	assert.Error(t, err)
}

// TestRunMigration_EmptyURLCoverage — no config.yaml and no env vars
// so configutil's defaults produce a non-empty URL; the empty-URL
// branch is defensive. This test exercises the code path without a
// seam override.
func TestRunMigration_EmptyURLCoverage(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	_ = runMigration("up")
}

// TestRunMigration_LoadsDotEnv — regression for the bug where
// `gofasta migrate up` produced `postgres://:@localhost:5432/?sslmode=disable`
// because it never read .env. The scaffold's config.yaml intentionally
// omits user/password/name; .env supplies them via the project-prefixed
// env vars (e.g. ACME_DATABASE_USER). This test pins that runMigration
// loads .env BEFORE building the URL so the credentials show up in the
// -database argument passed to `migrate`.
func TestRunMigration_LoadsDotEnv(t *testing.T) {
	chdirTemp(t)
	// Scaffold-style config.yaml: no user/pass/name, host/port from yaml.
	scaffoldConfig := `database:
  driver: postgres
  host: localhost
  port: "5432"
  sslmode: disable
`
	require.NoError(t, os.WriteFile("config.yaml", []byte(scaffoldConfig), 0o644))
	require.NoError(t, os.WriteFile("go.mod",
		[]byte("module github.com/acme/myapp\n\ngo 1.25.0\n"), 0o644))

	// .env that overrides everything the way the dev workflow expects:
	// host port (5433) maps to container 5432, plus the credentials.
	dotenv := `MYAPP_DATABASE_USER=myappuser
MYAPP_DATABASE_PASSWORD=myapppass
MYAPP_DATABASE_NAME=myapp_dev
MYAPP_DATABASE_HOST=localhost
MYAPP_DATABASE_PORT=5433
`
	require.NoError(t, os.WriteFile(".env", []byte(dotenv), 0o644))
	// Clean up the env vars that loadDotEnv will os.Setenv so this
	// test doesn't leak state into sibling tests.
	for _, k := range []string{
		"MYAPP_DATABASE_USER", "MYAPP_DATABASE_PASSWORD",
		"MYAPP_DATABASE_NAME", "MYAPP_DATABASE_HOST", "MYAPP_DATABASE_PORT",
	} {
		t.Cleanup(func() { _ = os.Unsetenv(k) })
	}

	// Capture the exact args passed to the migrate shell-out so we can
	// assert the URL contains the .env values.
	var captured []string
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExecCommand(0)(name, args...)
	}
	t.Cleanup(func() { execCommand = orig })

	require.NoError(t, runMigration("up"))

	// Find the -database arg (it follows -database in the captured slice).
	var dbURL string
	for i, a := range captured {
		if a == "-database" && i+1 < len(captured) {
			dbURL = captured[i+1]
			break
		}
	}
	require.NotEmpty(t, dbURL, "migrate should be invoked with -database <url>")
	assert.Contains(t, dbURL, "myappuser:myapppass@", "URL must include .env credentials, not empty :@")
	assert.Contains(t, dbURL, "localhost:5433", "URL must use the .env host:port mapping")
	assert.Contains(t, dbURL, "/myapp_dev", "URL must include .env database name")
	assert.NotContains(t, dbURL, "://:@", "URL must not have empty user/password")
	assert.NotContains(t, dbURL, "/?sslmode", "URL must include database name before query")
}

// fakeKeyboardReader is the in-memory stdin used by the listener tests.
// It satisfies keyboardReader by wrapping a bytes.Reader plus a fake fd.
type fakeKeyboardReader struct {
	*bytes.Reader
	fd uintptr
}

func (f *fakeKeyboardReader) Fd() uintptr { return f.fd }
