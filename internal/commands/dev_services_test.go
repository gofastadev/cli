package commands

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The isDBLike/isCacheLike/isQueueLike heuristics and the old
// resolveSelectedServices function were removed in the host-first
// redesign — `--services <names>` now validates against the
// compose-declared service list directly via selectServices
// (covered in dev_services_suggest_test.go). The remaining tests in
// this file cover compose-service detection + healthcheck polling,
// which the new design uses unchanged.

// TestParseServicesList — input normalization for --services.
func TestParseServicesList(t *testing.T) {
	assert.Nil(t, parseServicesList(""))
	assert.Nil(t, parseServicesList("   "))
	assert.Equal(t, []string{"db"}, parseServicesList("db"))
	assert.Equal(t, []string{"db", "cache"}, parseServicesList("db,cache"))
	assert.Equal(t, []string{"db", "cache"}, parseServicesList(" db , cache "))
	assert.Equal(t, []string{"db", "cache"}, parseServicesList("db,,cache"))
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
