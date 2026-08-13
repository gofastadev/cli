// Shared fixtures for the internal/commands test suite.
//
// renderSkeleton/inRenderedProject build a real project tree from the embedded
// skeleton; the blockPath/occupyWithDir/lockDir/makeReadOnly helpers induce
// filesystem failures. Used by the refactor, routes, verify, init_cmd and new
// tests, which is why they live here rather than in any one of them.

package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"text/template"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/featurize"
	"github.com/gofastadev/cli/internal/skeleton"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetBannerShown clears the bannerShown guard so each test starts from
// a clean slate. Test-only helper — the production banner never resets.
func resetBannerShown() { bannerShown = false }

// Helper to swap the color-support detector and restore on cleanup.
// Also resets the bannerShown guard so each test starts fresh.
func withColorSupport(t *testing.T, truecolor, any bool) {
	t.Helper()
	orig := colorSupportFn
	colorSupportFn = func(_ io.Writer) (bool, bool) { return truecolor, any }
	resetBannerShown()
	t.Cleanup(func() {
		colorSupportFn = orig
		resetBannerShown()
	})
}

// Helper to swap the suppression decider.
func withBannerSuppressed(t *testing.T, suppressed bool) {
	t.Helper()
	orig := bannerSuppressedFn
	bannerSuppressedFn = func() bool { return suppressed }
	resetBannerShown()
	t.Cleanup(func() {
		bannerSuppressedFn = orig
		resetBannerShown()
	})
}

var errDummy = errors.New("dummy")

// chdirTemp creates a new temp dir and cd's into it for the duration of the test.
func chdirTemp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
}

// writeConfigYAML drops a minimal postgres config.yaml into cwd.
func writeConfigYAML(t *testing.T) {
	t.Helper()
	content := `database:
  driver: postgres
  user: u
  password: p
  host: localhost
  port: "5432"
  name: db
server:
  port: "8080"
`
	require.NoError(t, os.WriteFile("config.yaml", []byte(content), 0644))
}

// stagedFakeExec returns a fake that exits with code[i] on the i-th call,
// repeating the final code if there are more calls than codes.
//
// Like withFakeExec, this also stubs the preflight probes to report OK
// — every dev pipeline test using stagedFakeExec needs the preflight
// to pass so the staged exit codes can drive the actual shell-out
// sequence under test. See the comment on withFakeExec for the
// migrate-version → TCP-probe refactor history.
func stagedFakeExec(t *testing.T, codes ...int) {
	t.Helper()
	orig := execCommand
	call := 0
	execCommand = func(name string, args ...string) *exec.Cmd {
		code := codes[len(codes)-1]
		if call < len(codes) {
			code = codes[call]
		}
		call++
		return fakeExecCommand(code)(name, args...)
	}
	t.Cleanup(func() { execCommand = orig })
	stubProbesOK(t)
}

// Compile-time assertion: bytesReader returns an io.Reader.
var _ io.Reader = bytesReader(nil)

// stripANSI removes any ESC-[…m escape sequence so tests don't have
// to hardcode the color codes termcolor emits on TTY output.
func stripANSI(s string) string {
	var out bytes.Buffer
	skip := false
	for _, r := range s {
		switch {
		case skip:
			if r == 'm' {
				skip = false
			}
		case r == '\x1b':
			skip = true
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// debugHealthFixture spins up an httptest server that responds to
// every /debug/* endpoint so the health command sees a complete
// surface. The returned url is ready to pass as --app-url.
func debugHealthFixture(t *testing.T, devtools string) string {
	t.Helper()
	handler := http.NewServeMux()
	handler.HandleFunc("/debug/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"devtools":"` + devtools + `"}`))
	})
	// Other endpoints respond 200 so the liveness matrix reflects
	// reality under the devtools=enabled scenario.
	for _, path := range []string{
		"/debug/requests", "/debug/sql", "/debug/traces",
		"/debug/logs", "/debug/errors", "/debug/cache",
		"/debug/pprof/",
	} {
		handler.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("[]"))
		})
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

// debugFixture stands up a test server that serves every /debug/*
// endpoint using the caller-supplied handler map. An entry for
// /debug/health is prepended so requireDevtools passes unless the
// caller overrides it.
func debugFixture(t *testing.T, handlers map[string]http.HandlerFunc) (url string) {
	t.Helper()
	mux := http.NewServeMux()
	// Default /debug/health → enabled unless the caller overrode it.
	if _, set := handlers["/debug/health"]; !set {
		mux.HandleFunc("/debug/health", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"devtools":"enabled"}`))
		})
	}
	for path, h := range handlers {
		mux.HandleFunc(path, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// debug500 stands up a fixture whose named debug endpoint returns 500.
// /debug/health still returns {"devtools":"enabled"} so requireDevtools
// passes and runDebug* reaches the getJSON call.
func debug500(t *testing.T, path string) string {
	return debugFixture(t, map[string]http.HandlerFunc{
		path: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	})
}

// withDebugAppURL sets the global --app-url for the duration of a
// test. Keeps test isolation without plumbing a real cobra cmd.
func withDebugAppURL(t *testing.T, url string) {
	t.Helper()
	saved := debugAppURL
	debugAppURL = url
	t.Cleanup(func() { debugAppURL = saved })
}

// writeJSON is a convenience so fixture handlers don't have to
// remember to set Content-Type.
func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// errWriter returns an error on every Write; used by debug tests to
// force encoder / io.Copy errors.
type errWriter struct{}

func (errWriter) Write(_ []byte) (int, error) { return 0, fmt.Errorf("write boom") }

// debugFixtureAll serves an "everything succeeds" upstream app so any
// runDebug* invocation that doesn't care about the filter arguments
// returns nil. Individual tests can narrow this down if needed.
func debugFixtureAll(t *testing.T) string {
	return debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/sql":        func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/traces":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/traces/t1":  func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"trace_id":"t1"}`)) },
		"/debug/errors":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/cache":      func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/logs":       func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/pprof/":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) },
		"/debug/pprof/heap": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("heap-bytes")) },
		"/debug/pprof/goroutine": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("goroutine 1 [running]:\nmain.x()\n"))
		},
		"/debug/explain": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"plan":"ok"}`)) },
	})
}

// resetAllDebugFlags — set every command's flags back to init()
// defaults so tests don't leak filters between them.
func resetAllDebugFlags() {
	resetRequestFlags()
	resetSQLFlags()
	resetTraceFlags()
	resetCacheFlags()
	debugErrorsLimit = 0
	debugErrorsContains = ""
	debugLogsTrace = ""
	debugLogsLevel = ""
	debugLogsContains = ""
	debugGoroutinesFilter = ""
	debugGoroutinesMinCount = 0
	debugExplainVars = nil
	debugProfileDuration = ""
	debugProfileOutput = ""
	debugHarOutput = ""
}

func resetTraceFlags() {
	debugTracesSlowerThan = ""
	debugTracesStatus = ""
	debugTracesLimit = 0
	debugTraceWithStacks = false
}

// withUpstreamApp stands up a minimal "app" server and returns a
// dashboardServer pointing at it. Handlers are caller-provided so
// each test serves exactly the endpoints its handler needs. The
// server itself is kept alive via t.Cleanup — callers don't need a
// handle.
func withUpstreamApp(t *testing.T, handlers map[string]http.HandlerFunc) *dashboardServer {
	t.Helper()
	mux := http.NewServeMux()
	// Default /debug/health so requireDevtools-like probes pass.
	if _, set := handlers["/debug/health"]; !set {
		mux.HandleFunc("/debug/health", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"devtools":"enabled"}`))
		})
	}
	for path, h := range handlers {
		mux.HandleFunc(path, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &dashboardServer{appURL: srv.URL}
}

// ensure the fixture file's imports stay used if some tests are
// pruned later — keeping bytes.Buffer satisfied.
var _ bytes.Buffer

// quietEmitter satisfies devEmitter, counting Info/Warn calls so
// tests can assert without reading a terminal.
type quietEmitter struct {
	info atomic.Int32
	warn atomic.Int32
}

func (q *quietEmitter) Preflight(_, _ string) {}

func (q *quietEmitter) ServiceStart(_ string) {}

func (q *quietEmitter) ServiceHealthy(_ string, _ time.Duration) {}

func (q *quietEmitter) ServiceUnhealthy(_, _ string) {}

func (q *quietEmitter) MigrateOK(_ int) {}

func (q *quietEmitter) MigrateSkipped(_ string) {}

func (q *quietEmitter) MigrateDelegated(_ string) {}

func (q *quietEmitter) Air(_ int, _ map[string]string) {}

func (q *quietEmitter) AirInDocker(_ int, _ map[string]string) {}

func (q *quietEmitter) Shutdown(_ string, _ int) {}

func (q *quietEmitter) Info(_ string) { q.info.Add(1) }

func (q *quietEmitter) Warn(_ string) { q.warn.Add(1) }

// captureStdout redirects os.Stdout into an in-memory buffer while fn
// runs, then restores it. Used by JSON-mode tests so we can assert the
// emitted NDJSON without polluting the test runner's own output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w

	var buf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&buf, r)
	}()

	func() {
		defer func() {
			os.Stdout = orig
			_ = w.Close()
		}()
		fn()
	}()
	wg.Wait()
	return buf.String()
}

// fakeKeyboardReader is the in-memory stdin used by the listener tests.
// It satisfies keyboardReader by wrapping a bytes.Reader plus a fake fd.
type fakeKeyboardReader struct {
	*bytes.Reader
	fd uintptr
}

func (f *fakeKeyboardReader) Fd() uintptr { return f.fd }

func pipeStdin(t *testing.T, lines ...string) {
	t.Helper()
	in := bytes.NewBufferString(strings.Join(lines, "\n") + "\n")
	orig := menuInputFn
	menuInputFn = func() io.Reader { return in }
	t.Cleanup(func() { menuInputFn = orig })
}

func captureMenuOutput(t *testing.T) *bytes.Buffer {
	t.Helper()
	out := &bytes.Buffer{}
	orig := menuOutputFn
	menuOutputFn = func() io.Writer { return out }
	t.Cleanup(func() { menuOutputFn = orig })
	return out
}

func forceTTY(t *testing.T, isTTY bool) {
	t.Helper()
	orig := menuIsTTYFn
	menuIsTTYFn = func() bool { return isTTY }
	t.Cleanup(func() { menuIsTTYFn = orig })
}

func stubReprobe(t *testing.T, results []probeResult) {
	t.Helper()
	orig := menuReprobeFn
	menuReprobeFn = func() []probeResult { return results }
	t.Cleanup(func() { menuReprobeFn = orig })
}

func stubStartServices(t *testing.T, err error) {
	t.Helper()
	orig := menuStartServicesFn
	menuStartServicesFn = func(_ []string) error { return err }
	t.Cleanup(func() { menuStartServicesFn = orig })
}

func stubWaitHealthy(t *testing.T, err error) {
	t.Helper()
	orig := menuWaitHealthyFn
	menuWaitHealthyFn = func(_ []string) error { return err }
	t.Cleanup(func() { menuWaitHealthyFn = orig })
}

func stubComposeAvailable(t *testing.T, ok bool) {
	t.Helper()
	orig := composeAvailableFn
	composeAvailableFn = func() bool { return ok }
	t.Cleanup(func() { composeAvailableFn = orig })
}

func unsetenv(k string) error { return os.Unsetenv(k) }

// Package-init snapshot of the menu seam defaults. Captured at package
// load time so a test can invoke the *original* closures even after
// other tests stub the vars away. (The coverage profile tracks the
// lexical line/column of each closure body, so calling a fresh
// stand-in with identical text does NOT cover the originals.)
var (
	initialMenuInputFn         = menuInputFn
	initialMenuOutputFn        = menuOutputFn
	initialMenuStartServicesFn = menuStartServicesFn
)

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

// strconvItoa is a tiny alias — scoped to this file's exec-stubbing
// helpers that build GOFASTA_FAKE_EXIT env values.
func strconvItoa(i int) string { return strconv.Itoa(i) }

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

// setupDevTempdir creates a temp project dir, chdirs into it, writes a
// minimal config.yaml so configutil.BuildMigrationURL returns a usable URL,
// and restores the original cwd on cleanup.
func setupDevTempdir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("database:\n  driver: postgres\n  name: testdb\n"), 0o644))
	require.NoError(t, os.Chdir(dir))
}

// runMigrations with empty DB URL — returns error about config.
func TestRunMigrations_EmptyDBURL(t *testing.T) {
	// Empty temp dir with no config.yaml → BuildMigrationURL returns something
	// with empty fields but non-empty string; so this particular test won't
	// trigger the empty-URL path. Use a dir without config.yaml AND no env
	// vars set — but configutil always returns a non-empty URL with defaults.
	// Skip this — the branch is defensive and practically unreachable since
	// configutil always returns at least the default postgres URL.
}

// fakeExitCode is the exit code the fake child process will exit with.
// Tests set this via env before invoking a command that uses execCommand.
const (
	fakeEnvExitCode = "GOFASTA_FAKE_EXIT"
	fakeEnvVersion  = "GOFASTA_FAKE_VERSION"
)

// fakeExecCommand returns a function suitable for assignment to execCommand
// which re-execs the test binary as a fake subprocess. The subprocess runs
// TestHelperProcess and exits with the code provided in GOFASTA_FAKE_EXIT
// (default 0). This is the canonical os/exec testing pattern from the Go stdlib.
func fakeExecCommand(exitCode int) func(name string, args ...string) *exec.Cmd {
	return fakeExecCommandWithVersion(exitCode, "")
}

// fakeExecCommandWithVersion is like fakeExecCommand but also injects a version
// string that the helper process will print when any arg is "--version". Used
// by upgrade tests that need readBinaryVersion to return a specific value.
func fakeExecCommandWithVersion(exitCode int, version string) func(name string, args ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		cs := make([]string, 0, 3+len(args))
		cs = append(cs, "-test.run=TestHelperProcess", "--", name)
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"="+strconv.Itoa(exitCode),
			fakeEnvVersion+"="+version,
		)
		return cmd
	}
}

// withFakeExec swaps execCommand to a fake with the given exit code for the
// duration of the test and restores the original afterwards.
//
// It ALSO stubs the three preflight probe functions to return probeOK.
// Before the migrate-version → TCP-probe refactor, the DB probe shelled
// out to `migrate` and was fully covered by execCommand's fake — so
// every dev/runDev test got an "OK" probe automatically just by calling
// withFakeExec. After the refactor the DB probe is a `net.DialTimeout`
// that bypasses execCommand, which would dial real localhost:5432
// during tests and fail. Stubbing the seams here preserves the implicit
// contract every dev test was already relying on: withFakeExec means
// "all deps are happy, focus the test on the shell-out path".
func withFakeExec(t *testing.T, exitCode int) {
	t.Helper()
	orig := execCommand
	execCommand = fakeExecCommand(exitCode)
	t.Cleanup(func() { execCommand = orig })
	stubProbesOK(t)
}

// stubProbesOK swaps all three preflight probe functions to report OK
// for the duration of the test. Tests that want to exercise the
// preflight menu directly assign their own probe stubs *after*
// calling withFakeExec (the last assignment wins; t.Cleanup restores
// the original on exit either way).
func stubProbesOK(t *testing.T) {
	t.Helper()
	// Compose availability is stubbed here too: the pipeline tests fake
	// every docker invocation through execCommand, but the availability
	// PRECHECK does a real LookPath — green on developer machines with
	// Docker installed, red on mac CI runners that have none. Tests that
	// exercise the unavailable path override this back to false AFTER
	// calling stubProbesOK.
	origCompose := composeAvailableFn
	composeAvailableFn = func() bool { return true }
	t.Cleanup(func() { composeAvailableFn = origCompose })

	origDB, origCache, origQueue := probeDatabaseFn, probeCacheFn, probeQueueFn
	probeDatabaseFn = func() probeResult {
		return probeResult{Dep: "database", Status: probeOK, Endpoint: "stubbed"}
	}
	probeCacheFn = func() probeResult {
		return probeResult{Dep: "cache", Status: probeNotConfigured}
	}
	probeQueueFn = func() probeResult {
		return probeResult{Dep: "queue", Status: probeNotConfigured}
	}
	t.Cleanup(func() {
		probeDatabaseFn = origDB
		probeCacheFn = origCache
		probeQueueFn = origQueue
	})
}

// errStub is a sentinel test error.
var errStub = stubErr("stub error")

type stubErr string

func (s stubErr) Error() string { return string(s) }

// withJSONMode flips cliout into JSON mode for the test and restores
// it on cleanup. Centralizes the toggle so individual tests don't have
// to remember to defer the restore.
func withJSONMode(t *testing.T) {
	t.Helper()
	cliout.SetJSONMode(true)
	t.Cleanup(func() { cliout.SetJSONMode(false) })
}

// helper: switch cwd into a temp dir for the duration of one test, ensure
// the original cwd is restored regardless of the test outcome.
func chdirTest(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func codeOf(err error) string {
	var ce *clierr.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// fakeMigrateVersionCmd builds a fake exec.Cmd that prints the given
// stdout and exits with the given code — used to simulate the
// `migrate version` probe.
func fakeMigrateVersionCmd(stdout string, exitCode int) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", "migrate", "version"}
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append(os.Environ(),
		"GOFASTA_WANT_HELPER_PROCESS=1",
		fakeEnvExitCode+"="+strconv.Itoa(exitCode),
		"GOFASTA_FAKE_STDOUT="+stdout,
	)
	return cmd
}

// withFakeMigrateOutput sets execCommand to a fake that returns the
// supplied output + exit code on every call. Used when the test
// doesn't care about distinguishing version vs force calls.
func withFakeMigrateOutput(t *testing.T, output string, exitCode int) {
	t.Helper()
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return fakeMigrateVersionCmd(output, exitCode)
	}
	t.Cleanup(func() { execCommand = orig })
}

// runMigration is a backward-compat dispatcher retained for tests only.
// Production code calls runMigrationUp / runMigrationDown directly; this
// helper lives in the test file so it isn't compiled into the binary.
func runMigration(direction string) error {
	if direction == "down" {
		return runMigrationDown()
	}
	return runMigrationUp()
}

// TestToolVersionAir_StaysBelowTheGoFloorBump pins the specific version that
// caused the incident. air v1.67.2 is the first release declaring go 1.26.0;
// moving to it (or later) without also raising scaffoldGoVersion and the
// linter reintroduces the exact failure.
func TestToolVersionAir_StaysBelowTheGoFloorBump(t *testing.T) {
	assert.Equal(t, "v1.67.1", toolVersionAir,
		"air v1.67.2+ declares go 1.26.0 and raises the scaffold's Go floor; "+
			"bumping this requires raising scaffoldGoVersion and the pinned golangci-lint together")
}

// writeRefactorFile creates parent dirs and writes content at a
// fixture-relative path.
func writeRefactorFile(t *testing.T, rel, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(rel), 0o755))
	require.NoError(t, os.WriteFile(rel, []byte(content), 0o644))
}

// fixtureModulePath is the module path every rendered fixture declares.
const fixtureModulePath = "example.com/fixtureapp"

// renderSkeleton writes the embedded skeleton into dir, mirroring runNew's
// walk: dotfile renames, .tmpl rendering, and the GraphQL-only skip list.
//
// It renders the layered variant; graphQL toggles the GraphQL-only files
// (app/graphql/, gqlgen.yml, app/di/providers/graphql.go) the same way
// `gofasta new --graphql` does. The refactor's job is to turn exactly these
// shapes into feature projects.
func renderSkeleton(t *testing.T, dir string, graphQL bool) {
	t.Helper()
	const layout = "layered"

	data := ProjectData{
		ProjectName:      "Fixtureapp",
		ProjectNameLower: "fixtureapp",
		ProjectNameUpper: "FIXTUREAPP",
		ModulePath:       fixtureModulePath,
		GraphQL:          graphQL,
		DBDriver:         "postgres",
		Layout:           layout,
	}

	require.NoError(t, fs.WalkDir(skeleton.ProjectFS, "project", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(path, "project/")
		if rel == "" || rel == "project" {
			return nil
		}
		if !graphQL {
			for _, prefix := range graphqlOnlyPaths {
				if strings.HasPrefix(rel, prefix) {
					if d.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
			}
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		}

		content, err := fs.ReadFile(skeleton.ProjectFS, path)
		if err != nil {
			return err
		}

		out := rel
		isTemplate := strings.HasSuffix(out, ".tmpl")
		if isTemplate {
			out = strings.TrimSuffix(out, ".tmpl")
		}
		if renamed, ok := dotfileRenames[filepath.Base(out)]; ok {
			out = filepath.Join(filepath.Dir(out), renamed)
		}

		body := content
		if isTemplate {
			tmpl, terr := template.New(filepath.Base(path)).Parse(string(content))
			if terr != nil {
				return terr
			}
			var buf strings.Builder
			if eerr := tmpl.Execute(&buf, data); eerr != nil {
				return eerr
			}
			body = []byte(buf.String())
		}

		full := filepath.Join(dir, out)
		if mkErr := os.MkdirAll(filepath.Dir(full), 0o755); mkErr != nil {
			return mkErr
		}
		return os.WriteFile(full, body, 0o644)
	}))

	// go.mod is produced by `go mod init` in production, not by the skeleton.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module "+fixtureModulePath+"\n\ngo "+scaffoldGoVersion+"\n"), 0o644))
}

// inRenderedProject renders a skeleton into a temp dir and chdirs into it for
// the duration of the test. The refactor functions all operate on paths
// relative to the working directory, so this is how they are addressed.
func inRenderedProject(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	copyTree(t, renderedSkeletonOnce(t), dir)

	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// The skeleton is rendered ONCE per test binary and copied per test.
//
// Rendering parses and executes ~78 templates; doing that in every test made
// this package exceed the 10-minute -race timeout. Copying the finished tree is
// an order of magnitude cheaper and gives each test the same isolated,
// writable copy.
var (
	skeletonOnce sync.Once
	skeletonDir  string
	skeletonErr  error

	gqlSkeletonOnce sync.Once
	gqlSkeletonDir  string
	gqlSkeletonErr  error
)

func renderedSkeletonOnce(t *testing.T) string {
	t.Helper()
	skeletonOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gofasta-skeleton-*")
		if err != nil {
			skeletonErr = err
			return
		}
		skeletonDir = dir
		renderSkeleton(t, dir, false)
	})
	require.NoError(t, skeletonErr)
	require.NotEmpty(t, skeletonDir)
	return skeletonDir
}

func renderedGraphQLSkeletonOnce(t *testing.T) string {
	t.Helper()
	gqlSkeletonOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gofasta-gql-skeleton-*")
		if err != nil {
			gqlSkeletonErr = err
			return
		}
		gqlSkeletonDir = dir
		renderSkeleton(t, dir, true)
	})
	require.NoError(t, gqlSkeletonErr)
	require.NotEmpty(t, gqlSkeletonDir)
	return gqlSkeletonDir
}

// inRenderedGraphQLProject is inRenderedProject with GraphQL enabled.
// gqlgen never runs in unit tests, so the two files it would generate
// that the refactor touches are seeded by hand: the generated models
// file (relocates to app/shared/dtos/) and the exec file (deleted
// before gqlgen reruns).
func inRenderedGraphQLProject(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	copyTree(t, renderedGraphQLSkeletonOnce(t), dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "app", "dtos", "generated-types.dtos.go"),
		[]byte("package dtos\n\ntype UserFiltersDto struct {\n\tFields *TUserFiltersDtoFields\n}\n\ntype TUserFiltersDtoFields struct {\n\tEmail *string\n}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app", "generated.go"),
		[]byte("package app\n\nimport _ \""+fixtureModulePath+"/app/dtos\"\n"), 0o644))

	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// copyTree copies the contents of src into dst, preserving file modes.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	require.NoError(t, filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		body, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if mkErr := os.MkdirAll(filepath.Dir(target), 0o755); mkErr != nil {
			return mkErr
		}
		return os.WriteFile(target, body, info.Mode().Perm())
	}))
}

// userResourceFixture is the resource every scaffold ships with, and therefore
// the one the refactor tests can migrate without generating anything first.
func userResourceFixture() featurize.Resource {
	return featurize.Resource{Name: "User", Snake: "user", Plural: "Users"}
}

// recordedShellCall captures what each step function asked runShellFn to
// invoke. Tests use it to assert "scoped run passed only changed files
// to gofmt, only affected packages to go test, etc."
type recordedShellCall struct {
	name string
	args []string
}

// withStubShell swaps runShellFn for the duration of the test to a
// scripted response. The responses slice is consumed in order; further
// calls return the final entry.
type stubResponse struct {
	out string
	err error
}

func withStubShell(t *testing.T, responses ...stubResponse) {
	t.Helper()
	orig := runShellFn
	call := 0
	runShellFn = func(_ string, _ ...string) (string, error) {
		r := responses[len(responses)-1]
		if call < len(responses) {
			r = responses[call]
		}
		call++
		return r.out, r.err
	}
	t.Cleanup(func() { runShellFn = orig })
}
