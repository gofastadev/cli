package termcolor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The decorated builders are string-returning siblings of the Print* helpers.
// They are what CLI code is allowed to use (cliout does the writing), so each
// one is pinned here in both color modes: the icon vocabulary and indentation
// must survive with color off, since that is what CI logs and piped output see.

func TestBuilders_UndecoratedWithColorDisabled(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "")

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"Header", Header("build %s", "v2"), "build v2"},
		{"Step", Step("applying %d", 3), "▶ applying 3"},
		{"Success", Success("done %s", "ok"), "✓ done ok"},
		{"Fail", Fail("broke %s", "bad"), "✗ broke bad"},
		{"Warn", Warn("careful %s", "now"), "⚠ careful now"},
		{"Info", Info("plain %s", "text"), "plain text"},
		{"Hint", Hint("try %s", "again"), "   try again"},
		{"Path", Path("app/models/user.go"), "   app/models/user.go"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
		})
	}
}

// TestBuilders_DecorateWhenColorEnabled checks the other mode. The assertions
// stay on "the message survives and an escape was added" rather than on exact
// escape sequences, which belong to the C* wrappers already tested elsewhere.
func TestBuilders_DecorateWhenColorEnabled(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	cases := []struct {
		name    string
		got     string
		message string
	}{
		{"Header", Header("build %s", "v2"), "build v2"},
		{"Step", Step("applying %d", 3), "applying 3"},
		{"Success", Success("done %s", "ok"), "done ok"},
		{"Fail", Fail("broke %s", "bad"), "broke bad"},
		{"Warn", Warn("careful %s", "now"), "careful now"},
		{"Hint", Hint("try %s", "again"), "try again"},
		{"Path", Path("app/models/user.go"), "app/models/user.go"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Contains(t, tc.got, tc.message)
			assert.Contains(t, tc.got, Reset, "an enabled color mode must emit a reset")
		})
	}

	// Info is deliberately undecorated in every mode: it exists so callers can
	// route every line through one set of builders without a bare Sprintf.
	assert.Equal(t, "plain text", Info("plain %s", "text"))
}
