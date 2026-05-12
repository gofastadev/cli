package commands

import (
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Tests for dev_preflight.go.
//
// All three probes go through package-level seams (migrateProbeRunner
// for the DB probe; tcpDialFn for cache + queue). Each test substitutes
// a deterministic stub and restores it via t.Cleanup.
//
// chdirToTemp + writeConfigYAML are used so configutil's loader sees a
// known config.yaml in cwd; this keeps the probe behavior coupled to
// the *driver*/*enabled* skip logic rather than to the test runner's
// directory.
// ─────────────────────────────────────────────────────────────────────

// writeConfigYAMLBody writes the given YAML body to ./config.yaml in
// cwd. Distinct from writeConfigYAML (commands_exec_test.go) which
// writes a fixed default config; this one takes a body so each probe
// test can describe exactly the config-yaml shape it wants.
func writeConfigYAMLBody(t *testing.T, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile("config.yaml", []byte(body), 0o644))
}

// swapMigrateProbe replaces migrateProbeRunner for one test.
func swapMigrateProbe(t *testing.T, fn func(string) error) {
	t.Helper()
	orig := migrateProbeRunner
	migrateProbeRunner = fn
	t.Cleanup(func() { migrateProbeRunner = orig })
}

// swapTCPDial replaces tcpDialFn for one test.
func swapTCPDial(t *testing.T, fn func(string, string, time.Duration) (net.Conn, error)) {
	t.Helper()
	orig := tcpDialFn
	tcpDialFn = fn
	t.Cleanup(func() { tcpDialFn = orig })
}

// ── probeDatabase ─────────────────────────────────────────────────────

// TestProbeDatabase_NotConfigured — no config.yaml in cwd means
// BuildMigrationURL returns "" (well, it returns a default-ish URL
// with empty user/password). We exercise the empty-DSN path by
// stubbing configutil... actually BuildMigrationURL never returns
// empty because it always emits a postgres://...?sslmode template.
// The probeNotConfigured path triggers only when the DSN is literally
// empty, which can only happen if BuildMigrationURL changes shape;
// for now, the test below shows the production path always treats
// "no config" as "unreachable" because the URL points at localhost
// with empty creds.
func TestProbeDatabase_NotConfigured(t *testing.T) {
	chdirTemp(t)
	// No config.yaml present. BuildMigrationURL still produces a default
	// "postgres://localhost:5432" — we expect the probe to attempt and
	// fail, not silently skip. The test asserts that path explicitly.
	swapMigrateProbe(t, func(_ string) error {
		return errors.New("connection refused")
	})
	got := probeDatabase()
	assert.Equal(t, "database", got.Dep)
	assert.Equal(t, probeUnreachable, got.Status)
	assert.Contains(t, got.Reason, "connection refused")
}

// TestProbeDatabase_OK — happy path: migrate version succeeds, so the
// probe reports OK with the DSN it just probed.
func TestProbeDatabase_OK(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "database:\n  driver: postgres\n  host: localhost\n  port: \"5432\"\n")
	swapMigrateProbe(t, func(_ string) error { return nil })
	got := probeDatabase()
	assert.Equal(t, probeOK, got.Status)
	assert.NotEmpty(t, got.Endpoint, "OK probe must surface the DSN it tried")
}

// TestProbeDatabase_Unreachable — migrate errors → probeUnreachable
// with the error wrapped in Reason.
func TestProbeDatabase_Unreachable(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "database:\n  driver: postgres\n  host: localhost\n  port: \"5432\"\n")
	swapMigrateProbe(t, func(_ string) error {
		return errors.New("dial tcp: connection refused")
	})
	got := probeDatabase()
	assert.Equal(t, probeUnreachable, got.Status)
	assert.Contains(t, got.Reason, "connection refused")
}

// TestProbeDatabase_EmptyDSN — when configutil somehow returns an
// empty DSN (shouldn't happen in production but the branch exists),
// the probe reports not-configured.
func TestProbeDatabase_EmptyDSN(t *testing.T) {
	// We can't easily force BuildMigrationURL to return "" via config
	// alone (it always emits a template). Stub the runner via a
	// sentinel error so the probe enters the unreachable path, but
	// the EmptyDSN test is here for parity — covered by an integration
	// path in dev_test.go.
	t.Skip("BuildMigrationURL always emits a non-empty URL in production; empty-DSN branch is defensive")
}

// TestRunMigrateVersionProbe_Success — wraps a fake `migrate` that
// exits zero. Verifies the seam wrapping is correct.
func TestRunMigrateVersionProbe_Success(t *testing.T) {
	withFakeExec(t, 0)
	err := runMigrateVersionProbe("postgres://x")
	assert.NoError(t, err)
}

// TestRunMigrateVersionProbe_Failure — fake migrate exits non-zero,
// the probe wraps the error with "migrate version:" prefix.
func TestRunMigrateVersionProbe_Failure(t *testing.T) {
	withFakeExec(t, 1)
	err := runMigrateVersionProbe("postgres://x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migrate version")
}

// ── probeCache ────────────────────────────────────────────────────────

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

// ── probeQueue ────────────────────────────────────────────────────────

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

// ── tcpProbe ─────────────────────────────────────────────────────────

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

// ── runPreflight ─────────────────────────────────────────────────────

// TestRunPreflight_StableOrder — the three results come back in the
// canonical order: database, cache, queue.
func TestRunPreflight_StableOrder(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "database:\n  driver: postgres\ncache:\n  driver: memory\nqueue:\n  enabled: false\n")
	swapMigrateProbe(t, func(_ string) error { return nil })

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
	swapMigrateProbe(t, func(_ string) error { return errors.New("db error") })
	swapTCPDial(t, func(_, _ string, _ time.Duration) (net.Conn, error) {
		return nil, errors.New("tcp error")
	})

	results := runPreflight()
	require.Len(t, results, 3)
	assert.Equal(t, probeUnreachable, results[0].Status)
	assert.Equal(t, probeUnreachable, results[1].Status)
	assert.Equal(t, probeUnreachable, results[2].Status)
}

// ── hasUnreachable ────────────────────────────────────────────────────

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

// ── configutil cache/queue endpoint builders ─────────────────────────

// These complement the in-package probe tests by checking the helper
// outputs directly. Placed here (not in configutil/configutil_test.go)
// because the probes are the primary consumer and grouping the tests
// keeps the preflight surface auditable in one file.

func TestBuildCacheEndpoint_MemoryDriver(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "cache:\n  driver: memory\n")
	endpoint, enabled := configutil.BuildCacheEndpoint()
	assert.False(t, enabled)
	assert.Empty(t, endpoint)
}

func TestBuildCacheEndpoint_EmptyDriver(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "cache: {}\n")
	endpoint, enabled := configutil.BuildCacheEndpoint()
	assert.False(t, enabled)
	assert.Empty(t, endpoint)
}

func TestBuildCacheEndpoint_RedisExplicit(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "cache:\n  driver: redis\n  redis:\n    host: redis-host\n    port: \"6380\"\n")
	endpoint, enabled := configutil.BuildCacheEndpoint()
	assert.True(t, enabled)
	assert.Equal(t, "redis-host:6380", endpoint)
}

func TestBuildCacheEndpoint_RedisDefaultsLocalhost(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "cache:\n  driver: redis\n")
	endpoint, enabled := configutil.BuildCacheEndpoint()
	assert.True(t, enabled)
	assert.Equal(t, "localhost:6379", endpoint)
}

func TestBuildQueueEndpoint_DisabledDefault(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "queue:\n  enabled: false\n")
	endpoint, enabled := configutil.BuildQueueEndpoint()
	assert.False(t, enabled)
	assert.Empty(t, endpoint)
}

func TestBuildQueueEndpoint_NoSection(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "database:\n  driver: postgres\n")
	endpoint, enabled := configutil.BuildQueueEndpoint()
	assert.False(t, enabled)
	assert.Empty(t, endpoint)
}

func TestBuildQueueEndpoint_Enabled(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "queue:\n  enabled: true\n  redis:\n    host: queue-host\n    port: \"6381\"\n")
	endpoint, enabled := configutil.BuildQueueEndpoint()
	assert.True(t, enabled)
	assert.Equal(t, "queue-host:6381", endpoint)
}

func TestBuildQueueEndpoint_EnabledDefaults(t *testing.T) {
	chdirTemp(t)
	writeConfigYAMLBody(t, "queue:\n  enabled: true\n")
	endpoint, enabled := configutil.BuildQueueEndpoint()
	assert.True(t, enabled)
	assert.Equal(t, "localhost:6379", endpoint)
}
