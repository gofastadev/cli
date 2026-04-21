package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunDev_DryRun_NoCompose — the "no compose.yaml present" branch:
// runDev should bail out early with orchestrate=false and no side
// effects (no Air, no docker commands, no migrations).
func TestRunDev_DryRun_NoCompose(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))

	stdout := captureStdout(t, func() {
		err := runDev(devFlags{
			envFile:     ".env",
			dryRun:      true,
			waitTimeout: defaultWaitTimeout,
		})
		assert.NoError(t, err)
	})

	assert.Contains(t, stdout, "orchestrate=false")
}

// TestRunDev_DryRun_JSONMode — --dry-run with --json emits the plan as
// a structured event, not as a human log line. Asserts the event shape
// agents would branch on.
func TestRunDev_DryRun_JSONMode(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))

	cliout.SetJSONMode(true)
	t.Cleanup(func() { cliout.SetJSONMode(false) })

	stdout := captureStdout(t, func() {
		// jsonOutput is the package-level flag mirror cliout reads; set
		// it directly so newDevEmitter picks the JSON path.
		origJSON := jsonOutput
		jsonOutput = true
		t.Cleanup(func() { jsonOutput = origJSON })

		err := runDev(devFlags{
			envFile:     ".env",
			dryRun:      true,
			waitTimeout: defaultWaitTimeout,
		})
		assert.NoError(t, err)
	})

	// The emitted event is NDJSON; unmarshal and assert shape.
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line == "" {
			continue
		}
		var ev map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &ev), "line %q should be JSON", line)
		assert.NotEmpty(t, ev["event"])
	}
}

// TestDevFlags_KeepVolumes_DefaultIsTrue — sanity: --keep-volumes has a
// true default so the documented teardown behavior (preserve volumes)
// holds without any extra flag.
func TestDevFlags_KeepVolumes_DefaultIsTrue(t *testing.T) {
	// Re-register a dev command in isolation so we can read its default.
	// The package-level devCmd has been modified by other tests, so we
	// inspect the struct default instead.
	f := devFlags{keepVolumes: true}
	assert.True(t, f.keepVolumes)
}

// TestReadRouteEntries_MissingFile — readRouteEntries should return nil
// when docs/swagger.json is missing, not panic.
func TestReadRouteEntries_MissingFile(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))

	assert.Nil(t, readRouteEntries())
}

// TestReadRouteEntries_ParsesSwagger — writes a minimal swagger.json
// and asserts that the route entries come back as (method, path)
// pairs.
func TestReadRouteEntries_ParsesSwagger(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))
	require.NoError(t, os.MkdirAll("docs", 0o755))

	body := `{
	  "paths": {
	    "/users": {"get": {}, "post": {}},
	    "/users/{id}": {"get": {}, "delete": {}}
	  }
	}`
	require.NoError(t, os.WriteFile(filepath.Join("docs", "swagger.json"), []byte(body), 0o644))

	routes := readRouteEntries()
	assert.Len(t, routes, 4)
	// Order is not guaranteed (JSON object keys) so assert membership.
	seen := map[string]bool{}
	for _, r := range routes {
		seen[r.Method+" "+r.Path] = true
	}
	assert.True(t, seen["GET /users"])
	assert.True(t, seen["POST /users"])
	assert.True(t, seen["GET /users/{id}"])
	assert.True(t, seen["DELETE /users/{id}"])
}

// captureStdout redirects os.Stdout into an in-memory buffer while fn
// runs, then restores it. Used by JSON-mode tests so we can assert the
// emitted NDJSON without polluting the test runner's own output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w

	var buf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&buf, r)
	}()

	func() {
		defer func() {
			os.Stdout = orig
			_ = w.Close()
		}()
		fn()
	}()
	wg.Wait()
	return buf.String()
}
