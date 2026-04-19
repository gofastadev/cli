package commands

import (
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
