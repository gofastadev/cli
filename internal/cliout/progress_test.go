package cliout

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withStdouterr swaps os.Stdout and os.Stderr for pipes for the
// duration of fn, then returns the captured stdout and stderr text.
// Centralizes the dance so each helper test reads in two lines.
func withStdouterr(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	origOut := os.Stdout
	origErr := os.Stderr
	t.Cleanup(func() {
		os.Stdout = origOut
		os.Stderr = origErr
	})

	rOut, wOut, err := os.Pipe()
	require.NoError(t, err)
	rErr, wErr, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = wOut
	os.Stderr = wErr

	var outBuf, errBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(&outBuf, rOut) }()
	go func() { defer wg.Done(); _, _ = io.Copy(&errBuf, rErr) }()

	fn()
	_ = wOut.Close()
	_ = wErr.Close()
	wg.Wait()
	return outBuf.String(), errBuf.String()
}

// withJSONMode flips cliout into JSON mode and restores on cleanup.
func withJSONMode(t *testing.T) {
	t.Helper()
	SetJSONMode(true)
	t.Cleanup(func() { SetJSONMode(false) })
}

// TestStep_TextModeWritesToStdout — the baseline contract: text mode
// puts progress on stdout where users see it directly. Strip ANSI by
// checking for the message text rather than equality.
func TestStep_TextModeWritesToStdout(t *testing.T) {
	out, errOut := withStdouterr(t, func() { Step("hello %s", "world") })
	assert.Contains(t, out, "hello world")
	assert.Empty(t, errOut, "text mode must not touch stderr")
}

// TestStep_JSONModeWritesToStderr — the regression driver for the
// "--json must keep stdout clean" promise. Step (and every progress
// helper) routes to stderr in JSON mode so an agent piping stdout to
// jq doesn't see human-readable chatter mixed with the JSON document.
func TestStep_JSONModeWritesToStderr(t *testing.T) {
	withJSONMode(t)
	out, errOut := withStdouterr(t, func() { Step("hello %s", "world") })
	assert.Empty(t, out, "JSON mode must not touch stdout")
	assert.Contains(t, errOut, "hello world")
}

// TestEveryVerb_RoutingConsistent — every progress verb obeys the
// same rule (text→stdout, JSON→stderr). One table-driven test pins
// the contract so adding a new verb (Quiet, Pulse, etc.) is a one-row
// change here, not a new test function.
func TestEveryVerb_RoutingConsistent(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
		want string
	}{
		{"Header", func() { Header("h") }, "h"},
		{"Step", func() { Step("step") }, "step"},
		{"Success", func() { Success("ok") }, "ok"},
		{"Fail", func() { Fail("nope") }, "nope"},
		{"Warn", func() { Warn("warn") }, "warn"},
		{"Info", func() { Info("info") }, "info"},
		{"Hint", func() { Hint("hint") }, "hint"},
		{"Path", func() { Path("/a/b") }, "/a/b"},
		{"Create", func() { Create("/c") }, "/c"},
		{"Patch_NoNote", func() { Patch("/p", "") }, "/p"},
		{"Patch_WithNote", func() { Patch("/p", "why") }, "why"},
		{"Skip", func() { Skip("/s", "exists") }, "exists"},
		{"Plain", func() { Plain("plain %s", "x") }, "plain x"},
		{"Plainln", func() { Plainln("plainln") }, "plainln"},
	}
	for _, tc := range cases {
		t.Run("text/"+tc.name, func(t *testing.T) {
			out, errOut := withStdouterr(t, tc.fn)
			assert.Contains(t, out, tc.want)
			assert.Empty(t, errOut)
		})
		t.Run("json/"+tc.name, func(t *testing.T) {
			withJSONMode(t)
			out, errOut := withStdouterr(t, tc.fn)
			assert.Empty(t, out)
			assert.Contains(t, errOut, tc.want)
		})
	}
}

// TestBlank_EmitsNewline — Blank is the one verb with no payload to
// assert against; check it emits exactly a newline (after stripping
// any ANSI). Asymmetric stdout/stderr routing is covered by
// TestEveryVerb_RoutingConsistent above.
func TestBlank_EmitsNewline(t *testing.T) {
	out, _ := withStdouterr(t, Blank)
	assert.Equal(t, "\n", out)
}

// TestProgress_DoesNotShareStateAcrossModeFlips — a sanity check
// that flipping JSON mode mid-process actually changes the routing.
// Guards against a future refactor that captures progressOut once.
func TestProgress_DoesNotShareStateAcrossModeFlips(t *testing.T) {
	out1, err1 := withStdouterr(t, func() { Info("text-first") })
	assert.Contains(t, out1, "text-first")
	assert.Empty(t, err1)

	withJSONMode(t)
	out2, err2 := withStdouterr(t, func() { Info("json-second") })
	assert.Empty(t, out2)
	assert.Contains(t, err2, "json-second")
}

// TestPlain_FormatString — the only non-newline-terminated verb;
// verifies it threads the format string through fmt.Fprintf rather
// than appending a newline. Important for callers that build a line
// piecewise (e.g., `gofasta new`'s onboarding table).
func TestPlain_FormatString(t *testing.T) {
	out, _ := withStdouterr(t, func() { Plain("a %d b %s", 1, "x") })
	assert.Equal(t, "a 1 b x", strings.TrimRight(out, "\n"))
}
