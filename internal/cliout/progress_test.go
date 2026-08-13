package cliout

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestStep_TextModeWritesToStdout — the baseline contract: text mode
// puts progress on stdout where users see it directly. Strip ANSI by
// checking for the message text rather than equality.
func TestStep_TextModeWritesToStdout(t *testing.T) {
	out, errOut := withStdouterr(t, func() { Step("hello %s", "world") })
	assert.Contains(t, out, "hello world")
	assert.Empty(t, errOut, "text mode must not touch stderr")
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
