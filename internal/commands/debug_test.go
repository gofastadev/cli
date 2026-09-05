package commands

import "testing"

// withDebugAppURL sets the global --app-url for the duration of a
// test. Keeps test isolation without plumbing a real cobra cmd.
func withDebugAppURL(t *testing.T, url string) {
	t.Helper()
	saved := debugAppURL
	debugAppURL = url
	t.Cleanup(func() { debugAppURL = saved })
}

// resetAllDebugFlags — set every command's flags back to init()
// defaults so tests don't leak filters between them.
func resetAllDebugFlags() {
	resetRequestFlags()
	resetSQLFlags()
	resetTraceFlags()
	resetCacheFlags()
	debugErrorsLimit = 0
	debugErrorsContains = ""
	debugLogsTrace = ""
	debugLogsLevel = ""
	debugLogsContains = ""
	debugGoroutinesFilter = ""
	debugGoroutinesMinCount = 0
	debugExplainVars = nil
	debugProfileDuration = ""
	debugProfileOutput = ""
	debugHarOutput = ""
}
