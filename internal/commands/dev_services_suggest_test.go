package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Tests for dev_services_suggest.go.
//
// suggestClosest is the user-facing function; levenshtein + minOfThree
// are internal helpers that get covered via suggestClosest's tests.
// ─────────────────────────────────────────────────────────────────────

func TestSuggestClosest_ExactTypoIsLikelyMatch(t *testing.T) {
	got, ok := suggestClosest("lavinmw", []string{"app", "db", "lavinmq"})
	assert.True(t, ok)
	assert.Equal(t, "lavinmq", got)
}

func TestSuggestClosest_SwappedCharsSuggests(t *testing.T) {
	// "qdueue" → "queue" via one substitution + insertion = distance 2
	got, ok := suggestClosest("quueu", []string{"queue", "db"})
	assert.True(t, ok)
	assert.Equal(t, "queue", got)
}

func TestSuggestClosest_FarTypoReturnsNothing(t *testing.T) {
	// Distance from "redis" to "postgres" >> 2.
	got, ok := suggestClosest("postgres", []string{"redis", "queue"})
	assert.False(t, ok)
	assert.Empty(t, got)
}

func TestSuggestClosest_EmptyInputReturnsNothing(t *testing.T) {
	got, ok := suggestClosest("", []string{"db", "cache"})
	assert.False(t, ok)
	assert.Empty(t, got)
}

func TestSuggestClosest_EmptyAvailableReturnsNothing(t *testing.T) {
	got, ok := suggestClosest("lavinmq", nil)
	assert.False(t, ok)
	assert.Empty(t, got)
}

func TestSuggestClosest_CaseInsensitiveMatch(t *testing.T) {
	got, ok := suggestClosest("LAVINMW", []string{"lavinmq"})
	assert.True(t, ok)
	assert.Equal(t, "lavinmq", got)
}

func TestSuggestClosest_SkipsTooShortCandidates(t *testing.T) {
	// "db" is below suggestionMinLength (3). Even though "db" is a
	// distance-1 match for "dx", we don't suggest it — noisier than
	// helpful at 2-char lengths.
	got, ok := suggestClosest("dx", []string{"db", "cache"})
	assert.False(t, ok)
	assert.Empty(t, got)
}

func TestSuggestClosest_PicksClosestWhenMultipleCandidates(t *testing.T) {
	// "cach" → "cache" (d=1) is closer than "cash" (d=1 if cash were
	// in the list, but isn't). Verify deterministic selection.
	got, ok := suggestClosest("cach", []string{"cache", "queue", "scheduler"})
	assert.True(t, ok)
	assert.Equal(t, "cache", got)
}

func TestLevenshtein_KnownDistances(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
		{"flaw", "lawn", 2},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, levenshtein(tc.a, tc.b),
			"levenshtein(%q, %q)", tc.a, tc.b)
	}
}

func TestMinOfThree(t *testing.T) {
	assert.Equal(t, 1, minOfThree(1, 2, 3))
	assert.Equal(t, 1, minOfThree(2, 1, 3))
	assert.Equal(t, 1, minOfThree(3, 2, 1))
	assert.Equal(t, 0, minOfThree(0, 0, 0))
}

// ── selectServices ──────────────────────────────────────────────────

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
