package sqllint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAllRules_NamesUnique exercises every Rule.Name() implementation
// in one shot (gives the 9 Name methods 100% coverage) and at the same
// time asserts they're distinct — duplicate names in JSON output would
// confuse the agents that pattern-match on them.
func TestAllRules_NamesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range allRules {
		name := r.Name()
		require.NotEmpty(t, name, "rule name should not be empty")
		require.False(t, strings.Contains(name, " "),
			"rule name %q should not contain spaces", name)
		require.False(t, seen[name], "duplicate rule name: %s", name)
		seen[name] = true
	}
	require.Equal(t, len(allRules), len(seen))
}
