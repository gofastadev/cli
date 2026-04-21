package commands

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetRequestFlags is a test helper so no test leaks flag state into
// the next (global flag vars would otherwise carry over).
func resetRequestFlags() {
	debugRequestsTrace = ""
	debugRequestsMethod = ""
	debugRequestsStatus = ""
	debugRequestsPath = ""
	debugRequestsSlowerThan = ""
	debugRequestsLimit = 0
}

func sampleRequests() []scrapedRequest {
	return []scrapedRequest{
		{Time: time.Now(), Method: "GET", Path: "/api/v1/users", Status: 200, DurationMS: 12, TraceID: "trace-a"},
		{Time: time.Now(), Method: "POST", Path: "/api/v1/users", Status: 201, DurationMS: 45, TraceID: "trace-b"},
		{Time: time.Now(), Method: "GET", Path: "/api/v1/orders", Status: 500, DurationMS: 210, TraceID: "trace-c"},
		{Time: time.Now(), Method: "DELETE", Path: "/api/v1/tokens/abc", Status: 204, DurationMS: 14, TraceID: "trace-d"},
		{Time: time.Now(), Method: "GET", Path: "/health", Status: 200, DurationMS: 2, TraceID: ""},
	}
}

// TestApplyRequestFilters_ByMethod — case-insensitive match.
func TestApplyRequestFilters_ByMethod(t *testing.T) {
	resetRequestFlags()
	debugRequestsMethod = "get"
	got, err := applyRequestFilters(sampleRequests())
	require.NoError(t, err)
	assert.Len(t, got, 3)
	for _, r := range got {
		assert.Equal(t, "GET", r.Method)
	}
}

// TestApplyRequestFilters_ByTrace — exact match required.
func TestApplyRequestFilters_ByTrace(t *testing.T) {
	resetRequestFlags()
	debugRequestsTrace = "trace-c"
	got, err := applyRequestFilters(sampleRequests())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "trace-c", got[0].TraceID)
}

// TestApplyRequestFilters_ByStatusClass — `5xx` matches 500-599.
func TestApplyRequestFilters_ByStatusClass(t *testing.T) {
	resetRequestFlags()
	debugRequestsStatus = "5xx"
	got, err := applyRequestFilters(sampleRequests())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 500, got[0].Status)
}

// TestApplyRequestFilters_ByStatusRange — `200-299`.
func TestApplyRequestFilters_ByStatusRange(t *testing.T) {
	resetRequestFlags()
	debugRequestsStatus = "200-299"
	got, err := applyRequestFilters(sampleRequests())
	require.NoError(t, err)
	assert.Len(t, got, 4) // 200, 201, 204, 200
}

// TestApplyRequestFilters_BySlowerThan — duration filter.
func TestApplyRequestFilters_BySlowerThan(t *testing.T) {
	resetRequestFlags()
	debugRequestsSlowerThan = "100ms"
	got, err := applyRequestFilters(sampleRequests())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, int64(210), got[0].DurationMS)
}

// TestApplyRequestFilters_BadDuration — surfaces DEBUG_BAD_DURATION.
func TestApplyRequestFilters_BadDuration(t *testing.T) {
	resetRequestFlags()
	debugRequestsSlowerThan = "not-a-duration"
	_, err := applyRequestFilters(sampleRequests())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not-a-duration")
}

// TestApplyRequestFilters_ByPathSubstring — substring match.
func TestApplyRequestFilters_ByPathSubstring(t *testing.T) {
	resetRequestFlags()
	debugRequestsPath = "orders"
	got, err := applyRequestFilters(sampleRequests())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Path, "orders")
}

// TestApplyRequestFilters_Composed — multiple filters AND together.
func TestApplyRequestFilters_Composed(t *testing.T) {
	resetRequestFlags()
	debugRequestsMethod = "GET"
	debugRequestsStatus = "2xx"
	got, err := applyRequestFilters(sampleRequests())
	require.NoError(t, err)
	assert.Len(t, got, 2) // GET 200 /api/v1/users + GET 200 /health
}

// TestParseStatusRange — exercises every supported syntax.
func TestParseStatusRange(t *testing.T) {
	cases := []struct {
		in               string
		wantMin, wantMax int
		wantErr          bool
	}{
		{"", 0, 0, false},
		{"200", 200, 200, false},
		{"2xx", 200, 299, false},
		{"5XX", 500, 599, false},
		{"200-299", 200, 299, false},
		{"200,201,500", 200, 500, false},
		{"xyz", 0, 0, true},
		{"6xx", 0, 0, true},
		{"-", 0, 0, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			min, max, err := parseStatusRange(c.in)
			if c.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.wantMin, min)
			assert.Equal(t, c.wantMax, max)
		})
	}
}

// TestParseInt_LeadingZero — "0" → 0 without error.
func TestParseInt_LeadingZero(t *testing.T) {
	v, err := parseInt("0")
	require.NoError(t, err)
	assert.Equal(t, 0, v)
}

// TestParseInt_EmptyString — explicit error path.
func TestParseInt_EmptyString(t *testing.T) {
	_, err := parseInt("")
	require.Error(t, err)
}

// TestParseInt_NonDigit — non-digit char → error.
func TestParseInt_NonDigit(t *testing.T) {
	_, err := parseInt("12x")
	require.Error(t, err)
}

// TestParseStatusExplicitRange_TrailingDashEmpty — "200-" fails the
// right-side int parse.
func TestParseStatusExplicitRange_TrailingDashEmpty(t *testing.T) {
	_, _, ok, err := parseStatusExplicitRange("200-")
	assert.True(t, ok)
	require.Error(t, err)
}

// TestRunDebugRequests_GetJSONError — /debug/requests returns 500.
func TestRunDebugRequests_GetJSONError(t *testing.T) {
	url := debug500(t, "/debug/requests")
	withDebugAppURL(t, url)
	resetRequestFlags()
	require.Error(t, runDebugRequests())
}

// TestRunDebugRequests_DevtoolsError — unreachable app URL short-
// circuits the requireDevtools pre-check.
func TestRunDebugRequests_DevtoolsError(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	resetRequestFlags()
	require.Error(t, runDebugRequests())
}

// TestCompileRequestFilters_BadStatus — parseStatusRange fails, error
// propagates.
func TestCompileRequestFilters_BadStatus(t *testing.T) {
	resetRequestFlags()
	debugRequestsStatus = "not-a-number"
	t.Cleanup(resetRequestFlags)
	_, err := compileRequestFilters()
	require.Error(t, err)
}

// TestParseStatusCommaList_Invalid — comma-separated entry that isn't
// an integer.
func TestParseStatusCommaList_Invalid(t *testing.T) {
	_, _, _, err := parseStatusCommaList("200,abc")
	require.Error(t, err)
}

// TestDebugRequestsCmd_RunE — exercises the Cobra RunE wrapper.
func TestDebugRequestsCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugRequestsCmd.RunE(debugRequestsCmd, nil))
}
