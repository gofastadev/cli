package commands

import (
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeConfigYAMLBody writes the given YAML body to ./config.yaml in
// cwd. Distinct from writeConfigYAML (commands_exec_test.go) which
// writes a fixed default config; this one takes a body so each probe
// test can describe exactly the config-yaml shape it wants.
func writeConfigYAMLBody(t *testing.T, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile("config.yaml", []byte(body), 0o644))
}

// swapTCPDial replaces tcpDialFn for one test.
func swapTCPDial(t *testing.T, fn func(string, string, time.Duration) (net.Conn, error)) {
	t.Helper()
	orig := tcpDialFn
	tcpDialFn = fn
	t.Cleanup(func() { tcpDialFn = orig })
}

// TestProbeDatabase_SQLiteSkips — sqlite drivers have no network
// endpoint; the probe must silently report not-configured without
// touching tcpDialFn.
func TestProbeDatabase_SQLiteSkips(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "database:\n  driver: sqlite\n")
	swapTCPDial(t, func(_, _ string, _ time.Duration) (net.Conn, error) {
		t.Fatal("tcpDialFn must not be called for sqlite driver")
		return nil, nil
	})
	got := probeDatabase()
	assert.Equal(t, "database", got.Dep)
	assert.Equal(t, probeNotConfigured, got.Status)
}

// TestProbeDatabase_OK — happy path: TCP dial succeeds, so the
// probe reports OK with the migration URL surfaced for display.
func TestProbeDatabase_OK(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "database:\n  driver: postgres\n  host: localhost\n  port: \"5432\"\n")
	swapTCPDial(t, func(_, addr string, _ time.Duration) (net.Conn, error) {
		assert.Equal(t, "localhost:5432", addr, "probe must dial database.host:port")
		client, _ := net.Pipe()
		return client, nil
	})
	got := probeDatabase()
	assert.Equal(t, probeOK, got.Status)
	assert.Contains(t, got.Endpoint, "postgres://", "OK probe surfaces the migration URL for display")
}

// TestProbeDatabase_Unreachable — TCP dial errors → probeUnreachable
// with the dial error wrapped in Reason. The Endpoint still surfaces
// the full migration URL so users can copy-paste the failing target.
func TestProbeDatabase_Unreachable(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "database:\n  driver: postgres\n  host: localhost\n  port: \"5432\"\n")
	swapTCPDial(t, func(_, _ string, _ time.Duration) (net.Conn, error) {
		return nil, errors.New("dial tcp: connection refused")
	})
	got := probeDatabase()
	assert.Equal(t, probeUnreachable, got.Status)
	assert.Contains(t, got.Reason, "connection refused")
	assert.Contains(t, got.Endpoint, "postgres://", "unreachable probe still surfaces the URL")
}

// TestProbeCache_MemoryDriverSkips — cache.driver=memory means the
// app doesn't use a network cache; probe returns probeNotConfigured
// without touching the network.
func TestProbeCache_MemoryDriverSkips(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "cache:\n  driver: memory\n")
	swapTCPDial(t, func(_, _ string, _ time.Duration) (net.Conn, error) {
		t.Fatal("tcpDialFn must not be called when cache.driver=memory")
		return nil, nil
	})
	got := probeCache()
	assert.Equal(t, "cache", got.Dep)
	assert.Equal(t, probeNotConfigured, got.Status)
}

// TestProbeCache_NoConfigSkips — no cache section at all → not
// configured. Same skip semantics as memory driver.
func TestProbeCache_NoConfigSkips(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "database:\n  driver: postgres\n")
	got := probeCache()
	assert.Equal(t, probeNotConfigured, got.Status)
}

// TestProbeCache_RedisDriverOK — TCP probe succeeds → probeOK with
// the endpoint we dialed.
func TestProbeCache_RedisDriverOK(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "cache:\n  driver: redis\n  redis:\n    host: localhost\n    port: \"6379\"\n")
	swapTCPDial(t, func(network, address string, _ time.Duration) (net.Conn, error) {
		assert.Equal(t, "tcp", network)
		assert.Equal(t, "localhost:6379", address)
		// Return a connected pair so .Close() doesn't error.
		client, _ := net.Pipe()
		return client, nil
	})
	got := probeCache()
	assert.Equal(t, probeOK, got.Status)
	assert.Equal(t, "localhost:6379", got.Endpoint)
}

// TestProbeCache_RedisUnreachable — dial errors → probeUnreachable.
func TestProbeCache_RedisUnreachable(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "cache:\n  driver: redis\n  redis:\n    host: localhost\n    port: \"6379\"\n")
	swapTCPDial(t, func(_, _ string, _ time.Duration) (net.Conn, error) {
		return nil, errors.New("connection refused")
	})
	got := probeCache()
	assert.Equal(t, probeUnreachable, got.Status)
	assert.Equal(t, "localhost:6379", got.Endpoint)
	assert.Contains(t, got.Reason, "connection refused")
}

// TestProbeCache_RedisDefaults — cache.driver=redis with no host/port
// uses the defaults (localhost:6379). Verifies the defaulting path.
func TestProbeCache_RedisDefaults(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "cache:\n  driver: redis\n")
	swapTCPDial(t, func(_, address string, _ time.Duration) (net.Conn, error) {
		assert.Equal(t, "localhost:6379", address)
		client, _ := net.Pipe()
		return client, nil
	})
	got := probeCache()
	assert.Equal(t, probeOK, got.Status)
}

// TestProbeQueue_DisabledSkips — queue.enabled=false means the app
// doesn't run the queue worker; probe is silent.
func TestProbeQueue_DisabledSkips(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "queue:\n  enabled: false\n")
	swapTCPDial(t, func(_, _ string, _ time.Duration) (net.Conn, error) {
		t.Fatal("tcpDialFn must not be called when queue.enabled=false")
		return nil, nil
	})
	got := probeQueue()
	assert.Equal(t, "queue", got.Dep)
	assert.Equal(t, probeNotConfigured, got.Status)
}

// TestProbeQueue_NoConfigSkips — no queue section → not configured.
func TestProbeQueue_NoConfigSkips(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "database:\n  driver: postgres\n")
	got := probeQueue()
	assert.Equal(t, probeNotConfigured, got.Status)
}

// TestProbeQueue_EnabledOK — queue.enabled=true and TCP succeeds.
func TestProbeQueue_EnabledOK(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "queue:\n  enabled: true\n  redis:\n    host: localhost\n    port: \"6379\"\n")
	swapTCPDial(t, func(_, address string, _ time.Duration) (net.Conn, error) {
		assert.Equal(t, "localhost:6379", address)
		client, _ := net.Pipe()
		return client, nil
	})
	got := probeQueue()
	assert.Equal(t, probeOK, got.Status)
	assert.Equal(t, "localhost:6379", got.Endpoint)
}

// TestProbeQueue_EnabledUnreachable — queue.enabled=true and dial fails.
func TestProbeQueue_EnabledUnreachable(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "queue:\n  enabled: true\n  redis:\n    host: localhost\n    port: \"6379\"\n")
	swapTCPDial(t, func(_, _ string, _ time.Duration) (net.Conn, error) {
		return nil, errors.New("network unreachable")
	})
	got := probeQueue()
	assert.Equal(t, probeUnreachable, got.Status)
	assert.Contains(t, got.Reason, "network unreachable")
}

// TestProbeQueue_EnabledDefaults — queue.enabled=true with no
// redis section uses defaults.
func TestProbeQueue_EnabledDefaults(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "queue:\n  enabled: true\n")
	swapTCPDial(t, func(_, address string, _ time.Duration) (net.Conn, error) {
		assert.Equal(t, "localhost:6379", address)
		client, _ := net.Pipe()
		return client, nil
	})
	got := probeQueue()
	assert.Equal(t, probeOK, got.Status)
}

// TestTCPProbe_DialError — dial errors propagate through tcpProbe.
func TestTCPProbe_DialError(t *testing.T) {
	swapTCPDial(t, func(_, _ string, _ time.Duration) (net.Conn, error) {
		return nil, errors.New("no route")
	})
	err := tcpProbe("nowhere:1234")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no route")
}

// TestTCPProbe_DialSuccess — dial succeeds, the conn is closed, no
// error returned. Verifies the shared primitive's happy path.
func TestTCPProbe_DialSuccess(t *testing.T) {
	swapTCPDial(t, func(_, _ string, _ time.Duration) (net.Conn, error) {
		c, _ := net.Pipe()
		return c, nil
	})
	err := tcpProbe("anywhere:1234")
	assert.NoError(t, err)
}

// TestTCPProbe_RealLoopback — small integration check: stand up a
// real local listener, probe it via the unmocked tcpDialFn. Verifies
// the production probe primitive actually works end-to-end.
func TestTCPProbe_RealLoopback(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	addr := l.Addr().String()
	require.NoError(t, tcpProbe(addr))
}

// TestRunPreflight_StableOrder — the three results come back in the
// canonical order: database, cache, queue.
func TestRunPreflight_StableOrder(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "database:\n  driver: postgres\ncache:\n  driver: memory\nqueue:\n  enabled: false\n")
	swapTCPDial(t, func(_, _ string, _ time.Duration) (net.Conn, error) {
		client, _ := net.Pipe()
		return client, nil
	})

	results := runPreflight()
	require.Len(t, results, 3)
	assert.Equal(t, "database", results[0].Dep)
	assert.Equal(t, "cache", results[1].Dep)
	assert.Equal(t, "queue", results[2].Dep)
}

// TestRunPreflight_ParallelExecution — all three probes run, even
// when one fails. Verifies the wait-group + goroutine layout.
func TestRunPreflight_ParallelExecution(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "database:\n  driver: postgres\ncache:\n  driver: redis\nqueue:\n  enabled: true\n")
	swapTCPDial(t, func(_, _ string, _ time.Duration) (net.Conn, error) {
		return nil, errors.New("tcp error")
	})

	results := runPreflight()
	require.Len(t, results, 3)
	assert.Equal(t, probeUnreachable, results[0].Status)
	assert.Equal(t, probeUnreachable, results[1].Status)
	assert.Equal(t, probeUnreachable, results[2].Status)
}

func TestHasUnreachable(t *testing.T) {
	cases := []struct {
		name    string
		results []probeResult
		want    bool
	}{
		{
			name: "all OK",
			results: []probeResult{
				{Status: probeOK},
				{Status: probeOK},
				{Status: probeOK},
			},
			want: false,
		},
		{
			name: "mix of OK and not-configured",
			results: []probeResult{
				{Status: probeOK},
				{Status: probeNotConfigured},
				{Status: probeOK},
			},
			want: false,
		},
		{
			name: "one unreachable",
			results: []probeResult{
				{Status: probeOK},
				{Status: probeUnreachable},
				{Status: probeOK},
			},
			want: true,
		},
		{
			name:    "empty",
			results: []probeResult{},
			want:    false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hasUnreachable(tc.results))
		})
	}
}

// probeDatabase — endpoint-empty defensive branch. configutil's real
// implementation always returns a non-empty endpoint when enabled=true,
// so we drive the defensive ("incomplete config") branch via the
// configBuildDatabaseEndpointFn seam.
func TestProbeDatabase_EmptyEndpointDefensive(t *testing.T) {
	orig := configBuildDatabaseEndpointFn
	configBuildDatabaseEndpointFn = func() (string, bool) { return "", true }
	t.Cleanup(func() { configBuildDatabaseEndpointFn = orig })

	got := probeDatabase()
	assert.Equal(t, probeUnreachable, got.Status)
	assert.Contains(t, got.Reason, "incomplete")
}

// probeDatabase — when BuildMigrationURL returns "", display falls back
// to the raw endpoint string.
func TestProbeDatabase_DisplayFallbackToEndpoint(t *testing.T) {
	origURL := configBuildMigrationURLFn
	configBuildMigrationURLFn = func() string { return "" }
	t.Cleanup(func() { configBuildMigrationURLFn = origURL })

	origDial := tcpDialFn
	tcpDialFn = func(_, _ string, _ time.Duration) (net.Conn, error) {
		c1, c2 := net.Pipe()
		go func() { _ = c2.Close() }()
		return c1, nil
	}
	t.Cleanup(func() { tcpDialFn = origDial })

	origEndpoint := configBuildDatabaseEndpointFn
	configBuildDatabaseEndpointFn = func() (string, bool) { return "localhost:5432", true }
	t.Cleanup(func() { configBuildDatabaseEndpointFn = origEndpoint })

	got := probeDatabase()
	assert.Equal(t, probeOK, got.Status)
	assert.Equal(t, "localhost:5432", got.Endpoint, "display falls back to endpoint when migration URL is empty")
}

// probeCache — endpoint-empty defensive branch, same shape as
// probeDatabase. Driven via the configBuildCacheEndpointFn seam.
func TestProbeCache_EmptyEndpointDefensive(t *testing.T) {
	orig := configBuildCacheEndpointFn
	configBuildCacheEndpointFn = func() (string, bool) { return "", true }
	t.Cleanup(func() { configBuildCacheEndpointFn = orig })

	got := probeCache()
	assert.Equal(t, probeUnreachable, got.Status)
	assert.Contains(t, got.Reason, "incomplete")
}

// probeQueue — endpoint-empty defensive branch.
func TestProbeQueue_EmptyEndpointDefensive(t *testing.T) {
	orig := configBuildQueueEndpointFn
	configBuildQueueEndpointFn = func() (string, bool) { return "", true }
	t.Cleanup(func() { configBuildQueueEndpointFn = orig })

	got := probeQueue()
	assert.Equal(t, probeUnreachable, got.Status)
	assert.Contains(t, got.Reason, "incomplete")
}

// stubProbesOK swaps all three preflight probe functions to report OK
// for the duration of the test. Tests that want to exercise the
// preflight menu directly assign their own probe stubs *after*
// calling withFakeExec (the last assignment wins; t.Cleanup restores
// the original on exit either way).
func stubProbesOK(t *testing.T) {
	t.Helper()
	// Compose availability is stubbed here too: the pipeline tests fake
	// every docker invocation through execCommand, but the availability
	// PRECHECK does a real LookPath — green on developer machines with
	// Docker installed, red on mac CI runners that have none. Tests that
	// exercise the unavailable path override this back to false AFTER
	// calling stubProbesOK.
	origCompose := composeAvailableFn
	composeAvailableFn = func() bool { return true }
	t.Cleanup(func() { composeAvailableFn = origCompose })

	origDB, origCache, origQueue := probeDatabaseFn, probeCacheFn, probeQueueFn
	probeDatabaseFn = func() probeResult {
		return probeResult{Dep: "database", Status: probeOK, Endpoint: "stubbed"}
	}
	probeCacheFn = func() probeResult {
		return probeResult{Dep: "cache", Status: probeNotConfigured}
	}
	probeQueueFn = func() probeResult {
		return probeResult{Dep: "queue", Status: probeNotConfigured}
	}
	t.Cleanup(func() {
		probeDatabaseFn = origDB
		probeCacheFn = origCache
		probeQueueFn = origQueue
	})
}
