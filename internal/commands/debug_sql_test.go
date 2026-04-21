package commands

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetSQLFlags() {
	debugSQLTrace = ""
	debugSQLSlowerThan = ""
	debugSQLContains = ""
	debugSQLErrorsOnly = false
	debugSQLLimit = 0
}

func sampleQueries() []scrapedQuery {
	now := time.Now()
	return []scrapedQuery{
		{Time: now, SQL: "SELECT * FROM users", Rows: 20, DurationMS: 4, TraceID: "t1"},
		{Time: now, SQL: "SELECT * FROM orders WHERE user_id = ?", Rows: 3, DurationMS: 60, TraceID: "t1"},
		{Time: now, SQL: "INSERT INTO sessions VALUES (?)", Rows: 1, DurationMS: 8, TraceID: "t2"},
		{Time: now, SQL: "UPDATE users SET last_seen = NOW()", Rows: 0, DurationMS: 3, TraceID: "", Error: "duplicate key"},
	}
}

// TestApplySQLFilters_ByTrace — exact trace ID match.
func TestApplySQLFilters_ByTrace(t *testing.T) {
	resetSQLFlags()
	debugSQLTrace = "t1"
	got, err := applySQLFilters(sampleQueries())
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

// TestApplySQLFilters_BySlowerThan — duration filter.
func TestApplySQLFilters_BySlowerThan(t *testing.T) {
	resetSQLFlags()
	debugSQLSlowerThan = "10ms"
	got, err := applySQLFilters(sampleQueries())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, int64(60), got[0].DurationMS)
}

// TestApplySQLFilters_Contains — SQL substring.
func TestApplySQLFilters_Contains(t *testing.T) {
	resetSQLFlags()
	debugSQLContains = "orders"
	got, err := applySQLFilters(sampleQueries())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].SQL, "orders")
}

// TestApplySQLFilters_ErrorsOnly — keeps only rows with non-empty Error.
func TestApplySQLFilters_ErrorsOnly(t *testing.T) {
	resetSQLFlags()
	debugSQLErrorsOnly = true
	got, err := applySQLFilters(sampleQueries())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "duplicate key", got[0].Error)
}

// TestApplySQLFilters_BadDuration — invalid --slower-than.
func TestApplySQLFilters_BadDuration(t *testing.T) {
	resetSQLFlags()
	debugSQLSlowerThan = "xyz"
	_, err := applySQLFilters(sampleQueries())
	require.Error(t, err)
}

// TestOneLine_CollapsesWhitespace — multi-line SQL becomes single line.
func TestOneLine_CollapsesWhitespace(t *testing.T) {
	in := "SELECT *\n  FROM users\n  WHERE id = ?"
	assert.Equal(t, "SELECT * FROM users WHERE id = ?", oneLine(in))
}

// TestRunDebugSQL_DevtoolsError — unreachable app URL short-circuits
// the requireDevtools pre-check.
func TestRunDebugSQL_DevtoolsError(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	resetSQLFlags()
	require.Error(t, runDebugSQL())
}

// TestRunDebugSQL_GetJSONError — /debug/sql returns 500.
func TestRunDebugSQL_GetJSONError(t *testing.T) {
	url := debug500(t, "/debug/sql")
	withDebugAppURL(t, url)
	resetSQLFlags()
	require.Error(t, runDebugSQL())
}

// TestRunDebugSQL_LimitTrims — --limit shortens the output.
func TestRunDebugSQL_LimitTrims(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/sql": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, sampleQueries()) },
	})
	withDebugAppURL(t, url)
	resetSQLFlags()
	debugSQLLimit = 1
	t.Cleanup(resetSQLFlags)
	require.NoError(t, runDebugSQL())
}

// TestRunDebugSQL_EmptyWithFilters — no rows match but filters were
// present; renderer reports the empty set.
func TestRunDebugSQL_EmptyWithFilters(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/sql": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, []scrapedQuery{}) },
	})
	withDebugAppURL(t, url)
	resetSQLFlags()
	debugSQLContains = "xyz" // any filter value to make the filters map populated
	t.Cleanup(resetSQLFlags)
	require.NoError(t, runDebugSQL())
}

// TestDebugSQLCmd_RunE — exercises the Cobra RunE wrapper.
func TestDebugSQLCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugSQLCmd.RunE(debugSQLCmd, nil))
}
