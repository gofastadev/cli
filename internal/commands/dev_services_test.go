package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsDBLike — DB-name heuristic covers the canonical service names
// scaffolds use and a -db suffix pattern, without false-positive matches
// like "redis" or "metrics".
func TestIsDBLike(t *testing.T) {
	for _, n := range []string{"db", "database", "postgres", "mysql", "mariadb", "clickhouse", "users-db"} {
		assert.True(t, isDBLike(n), "%s should match", n)
	}
	for _, n := range []string{"redis", "cache", "queue", "asynq", "app"} {
		assert.False(t, isDBLike(n), "%s should NOT match", n)
	}
}

// TestIsCacheLike — cache-name heuristic covers redis, valkey, and a
// -cache suffix pattern.
func TestIsCacheLike(t *testing.T) {
	for _, n := range []string{"cache", "redis", "valkey", "session-cache"} {
		assert.True(t, isCacheLike(n), "%s should match", n)
	}
	for _, n := range []string{"db", "postgres", "queue", "app"} {
		assert.False(t, isCacheLike(n), "%s should NOT match", n)
	}
}

// TestIsQueueLike — queue-name heuristic.
func TestIsQueueLike(t *testing.T) {
	for _, n := range []string{"queue", "asynq", "nats", "rabbitmq", "job-queue"} {
		assert.True(t, isQueueLike(n), "%s should match", n)
	}
	for _, n := range []string{"db", "redis", "app"} {
		assert.False(t, isQueueLike(n), "%s should NOT match", n)
	}
}

// TestResolveSelectedServices — flag-resolution matrix. Verifies the
// documented priority order: --no-services > --services list > default
// minus opt-out filters.
func TestResolveSelectedServices(t *testing.T) {
	available := []string{"db", "cache", "queue", "other"}

	t.Run("no-services wins over everything", func(t *testing.T) {
		got := resolveSelectedServices(available, devFlags{noServices: true})
		assert.Empty(t, got)
	})

	t.Run("explicit list overrides no-* flags", func(t *testing.T) {
		got := resolveSelectedServices(available, devFlags{
			servicesList: []string{"db", "cache"},
			noDB:         true, // ignored in favor of explicit list
		})
		assert.Equal(t, []string{"db", "cache"}, got)
	})

	t.Run("explicit list strips app service", func(t *testing.T) {
		got := resolveSelectedServices(available, devFlags{
			servicesList: []string{"db", "app", "cache"},
		})
		assert.Equal(t, []string{"db", "cache"}, got)
	})

	t.Run("no-db filters db-like services", func(t *testing.T) {
		got := resolveSelectedServices(available, devFlags{noDB: true})
		assert.NotContains(t, got, "db")
		assert.Contains(t, got, "cache")
		assert.Contains(t, got, "queue")
	})

	t.Run("no-cache filters cache-like services", func(t *testing.T) {
		got := resolveSelectedServices(available, devFlags{noCache: true})
		assert.Contains(t, got, "db")
		assert.NotContains(t, got, "cache")
	})

	t.Run("no-queue filters queue-like services", func(t *testing.T) {
		got := resolveSelectedServices(available, devFlags{noQueue: true})
		assert.NotContains(t, got, "queue")
	})

	t.Run("all no-* flags combine", func(t *testing.T) {
		got := resolveSelectedServices(available, devFlags{
			noDB: true, noCache: true, noQueue: true,
		})
		assert.Equal(t, []string{"other"}, got)
	})

	t.Run("default selects everything", func(t *testing.T) {
		got := resolveSelectedServices(available, devFlags{})
		assert.ElementsMatch(t, available, got)
	})
}

// TestParseServicesList — input normalization for --services.
func TestParseServicesList(t *testing.T) {
	assert.Nil(t, parseServicesList(""))
	assert.Nil(t, parseServicesList("   "))
	assert.Equal(t, []string{"db"}, parseServicesList("db"))
	assert.Equal(t, []string{"db", "cache"}, parseServicesList("db,cache"))
	assert.Equal(t, []string{"db", "cache"}, parseServicesList(" db , cache "))
	assert.Equal(t, []string{"db", "cache"}, parseServicesList("db,,cache"))
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
