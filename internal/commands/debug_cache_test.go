package commands

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetCacheFlags() {
	debugCacheTrace = ""
	debugCacheOp = ""
	debugCacheMissOnly = false
	debugCacheLimit = 0
}

func sampleCacheOps() []scrapedCache {
	now := time.Now()
	return []scrapedCache{
		{Time: now, Op: "get", Key: "user:1", Hit: true, DurationMS: 1, TraceID: "t1"},
		{Time: now, Op: "get", Key: "user:2", Hit: false, DurationMS: 1, TraceID: "t1"},
		{Time: now, Op: "set", Key: "user:2", DurationMS: 2, TraceID: "t1"},
		{Time: now, Op: "delete", Key: "session:abc", DurationMS: 1, TraceID: "t2"},
	}
}

// TestApplyCacheFilters_ByTrace.
func TestApplyCacheFilters_ByTrace(t *testing.T) {
	resetCacheFlags()
	debugCacheTrace = "t2"
	got, err := applyCacheFilters(sampleCacheOps())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "delete", got[0].Op)
}

// TestApplyCacheFilters_ByOp.
func TestApplyCacheFilters_ByOp(t *testing.T) {
	resetCacheFlags()
	debugCacheOp = "get"
	got, err := applyCacheFilters(sampleCacheOps())
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

// TestApplyCacheFilters_InvalidOp — returns DEBUG_BAD_FILTER.
func TestApplyCacheFilters_InvalidOp(t *testing.T) {
	resetCacheFlags()
	debugCacheOp = "flipperdoodle"
	_, err := applyCacheFilters(sampleCacheOps())
	require.Error(t, err)
}

// TestApplyCacheFilters_MissOnly.
func TestApplyCacheFilters_MissOnly(t *testing.T) {
	resetCacheFlags()
	debugCacheMissOnly = true
	got, err := applyCacheFilters(sampleCacheOps())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.False(t, got[0].Hit)
}

// TestCacheHitRate — matches the expected hits / gets ratio.
func TestCacheHitRate(t *testing.T) {
	ops := sampleCacheOps() // 1 hit, 1 miss among 2 Gets
	rate := cacheHitRate(ops)
	assert.InDelta(t, 0.5, rate, 0.001)
}

// TestCacheHitRate_NoGets — no Gets returns zero, not NaN.
func TestCacheHitRate_NoGets(t *testing.T) {
	ops := []scrapedCache{{Op: "set"}, {Op: "delete"}}
	assert.Equal(t, 0.0, cacheHitRate(ops))
}

// TestRunDebugCache_DevtoolsError — unreachable app URL short-circuits
// at the requireDevtools pre-check.
func TestRunDebugCache_DevtoolsError(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	resetCacheFlags()
	require.Error(t, runDebugCache())
}

// TestRunDebugCache_GetJSONError — /debug/cache returns 500.
func TestRunDebugCache_GetJSONError(t *testing.T) {
	url := debug500(t, "/debug/cache")
	withDebugAppURL(t, url)
	resetCacheFlags()
	require.Error(t, runDebugCache())
}

// TestRunDebugCache_LimitTrims — --limit N shortens the output set.
func TestRunDebugCache_LimitTrims(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/cache": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, sampleCacheOps()) },
	})
	withDebugAppURL(t, url)
	resetCacheFlags()
	debugCacheLimit = 1
	t.Cleanup(resetCacheFlags)
	require.NoError(t, runDebugCache())
}

// TestDebugCacheCmd_RunE — exercises the Cobra RunE wrapper, counted
// separately from the underlying runDebugCache it delegates to.
func TestDebugCacheCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugCacheCmd.RunE(debugCacheCmd, nil))
}
