package commands

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ─────────────────────────────────────────────────────────────────────
// Tests for dev_preflight_menu.go.
//
// The menu is driven via three package-level seams:
//   - menuInputFn — stdin reader (replaced with bytes.NewReader)
//   - menuOutputFn — stdout writer (replaced with bytes.Buffer)
//   - menuIsTTYFn — TTY detector (boolean stub)
//   - menuReprobeFn — re-probe after retry (canned results)
//   - menuStartServicesFn / menuWaitHealthyFn — docker stubs
//
// Each test wires the subset of seams it exercises and restores via
// t.Cleanup. The TTY seam defaults to true in every test so the menu
// actually runs; one test explicitly flips it to false to verify the
// non-TTY skip path.
// ─────────────────────────────────────────────────────────────────────

// ── Test scaffolding ─────────────────────────────────────────────────

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

// ── Top-level outcomes ───────────────────────────────────────────────

// TestMenu_NonTTY_SkipsAndCancels — non-TTY environments skip the
// menu entirely and return menuCancel after printing actionable text.
// CI scripts get a deterministic exit code; humans without a terminal
// get the same info they'd see in the prompt.
func TestMenu_NonTTY_SkipsAndCancels(t *testing.T) {
	forceTTY(t, false)
	out := captureMenuOutput(t)
	got := runPreflightMenu([]probeResult{
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
	got := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuCancel, got)
}

// TestMenu_RunWithoutDB — user picks [3]; menuRunWithoutDB returned.
func TestMenu_RunWithoutDB(t *testing.T) {
	forceTTY(t, true)
	_ = captureMenuOutput(t)
	pipeStdin(t, "3")
	got := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuRunWithoutDB, got)
}

// TestMenu_InvalidChoiceLoops — bogus input loops back to the menu.
// We feed an invalid char first, then "4" to exit. The output should
// mention "invalid choice".
func TestMenu_InvalidChoiceLoops(t *testing.T) {
	forceTTY(t, true)
	out := captureMenuOutput(t)
	pipeStdin(t, "z", "4")
	got := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuCancel, got)
	assert.Contains(t, out.String(), "invalid choice")
}

// TestMenu_StdinClosed — EOF mid-read returns cancel and doesn't loop
// forever.
func TestMenu_StdinClosed(t *testing.T) {
	forceTTY(t, true)
	_ = captureMenuOutput(t)
	orig := menuInputFn
	menuInputFn = func() io.Reader { return bytes.NewReader(nil) }
	t.Cleanup(func() { menuInputFn = orig })
	got := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuCancel, got)
}

// ── Option [1] — Enter connection string ─────────────────────────────

// TestMenu_EnterConnString_AppliesAndRecovers — the user provides a
// valid URL; the override is applied, reprobe returns OK, menuOK
// returned. Verifies the env-var overrides land in os.Setenv.
func TestMenu_EnterConnString_AppliesAndRecovers(t *testing.T) {
	forceTTY(t, true)
	_ = captureMenuOutput(t)
	pipeStdin(t, "1", "postgres://newuser:newpass@newhost:9999/newdb")
	t.Cleanup(func() {
		_ = unsetenv("GOFASTA_DATABASE_DRIVER")
		_ = unsetenv("GOFASTA_DATABASE_HOST")
		_ = unsetenv("GOFASTA_DATABASE_PORT")
		_ = unsetenv("GOFASTA_DATABASE_USER")
		_ = unsetenv("GOFASTA_DATABASE_PASSWORD")
		_ = unsetenv("GOFASTA_DATABASE_NAME")
	})
	stubReprobe(t, []probeResult{
		{Dep: "database", Status: probeOK, Endpoint: "x:1"},
	})

	got := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuOK, got)
	assert.Equal(t, "newhost", osGetenv("GOFASTA_DATABASE_HOST"))
	assert.Equal(t, "9999", osGetenv("GOFASTA_DATABASE_PORT"))
	assert.Equal(t, "newuser", osGetenv("GOFASTA_DATABASE_USER"))
	assert.Equal(t, "newpass", osGetenv("GOFASTA_DATABASE_PASSWORD"))
	assert.Equal(t, "newdb", osGetenv("GOFASTA_DATABASE_NAME"))
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
	got := runPreflightMenu([]probeResult{
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
	got := runPreflightMenu([]probeResult{
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
	t.Cleanup(func() { _ = unsetenv("GOFASTA_DATABASE_HOST") })
	stubReprobe(t, []probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "h:9", Reason: "still refused"},
	})
	got := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuCancel, got)
}

// ── Option [2] — Start in Docker ─────────────────────────────────────

// TestMenu_StartInDocker_HappyPath — docker is available, services
// start, healthy, reprobe returns OK → menuOK.
func TestMenu_StartInDocker_HappyPath(t *testing.T) {
	forceTTY(t, true)
	out := captureMenuOutput(t)
	pipeStdin(t, "2")
	stubComposeAvailable(t, true)
	stubStartServices(t, nil)
	stubWaitHealthy(t, nil)
	stubReprobe(t, []probeResult{
		{Dep: "database", Status: probeOK, Endpoint: "x:1"},
	})

	got := runPreflightMenu([]probeResult{
		{Dep: "database", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuOK, got)
	assert.Contains(t, out.String(), "gofasta dev --services db")
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
	got := runPreflightMenu([]probeResult{
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
	got := runPreflightMenu([]probeResult{
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
	got := runPreflightMenu([]probeResult{
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
	got := runPreflightMenu([]probeResult{
		{Dep: "unknown-dep", Status: probeUnreachable, Endpoint: "x:1", Reason: "refused"},
	})
	assert.Equal(t, menuCancel, got)
	assert.Contains(t, out.String(), "no failing services")
}

// ── Pure helpers ─────────────────────────────────────────────────────

func TestPrintPreflightFailures_ThreeStates(t *testing.T) {
	out := captureMenuOutput(t)
	printPreflightFailures([]probeResult{
		{Dep: "database", Status: probeOK, Endpoint: "x:1"},
		{Dep: "cache", Status: probeUnreachable, Endpoint: "y:2", Reason: "boom"},
		{Dep: "queue", Status: probeNotConfigured},
	})
	s := out.String()
	assert.Contains(t, s, "✓ database reachable")
	assert.Contains(t, s, "✗ cache unreachable at y:2")
	assert.Contains(t, s, "• queue not configured")
}

func TestPrintPreflightFailures_NoEndpointShowsPlaceholder(t *testing.T) {
	out := captureMenuOutput(t)
	printPreflightFailures([]probeResult{
		{Dep: "cache", Status: probeUnreachable, Reason: "config invalid"},
	})
	assert.Contains(t, out.String(), "(no endpoint)")
}

func TestFailingDepsCSV(t *testing.T) {
	got := failingDepsCSV([]probeResult{
		{Dep: "database", Status: probeUnreachable},
		{Dep: "cache", Status: probeOK},
		{Dep: "queue", Status: probeUnreachable},
	})
	assert.Equal(t, "database, queue", got)
}

func TestCondense(t *testing.T) {
	assert.Equal(t, "hi", condense("  hi  "))
	assert.Equal(t, "first...", condense("first\nsecond"))
	assert.Equal(t, "", condense("   "))
}

func TestSplitHostPort(t *testing.T) {
	h, p := splitHostPort("localhost:5432")
	assert.Equal(t, "localhost", h)
	assert.Equal(t, "5432", p)

	h, p = splitHostPort("nohost")
	assert.Equal(t, "nohost", h)
	assert.Equal(t, "", p)
}

func TestMapFailingDepsToServices(t *testing.T) {
	got := mapFailingDepsToServices([]probeResult{
		{Dep: "database", Status: probeUnreachable},
		{Dep: "cache", Status: probeUnreachable},
		{Dep: "queue", Status: probeNotConfigured},
		{Dep: "weird", Status: probeUnreachable},
	})
	assert.Equal(t, []string{"db", "cache"}, got)
}

// osGetenv / unsetenv are thin wrappers around os.Getenv / os.Unsetenv
// so the test bodies above read declaratively. They're the only os
// calls in this file.
func osGetenv(k string) string { return os.Getenv(k) }
func unsetenv(k string) error  { return os.Unsetenv(k) }
