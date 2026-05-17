// Package configutil reads config.yaml for CLI commands without importing the framework.
package configutil

import (
	"fmt"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// defaultDatabasePorts maps each supported driver to its conventional
// default port. Consulted by BuildMigrationURL and BuildDatabaseEndpoint
// when the user hasn't set `database.port` explicitly. SQLite is absent
// (file-based driver — no port concept; callers route around it).
var defaultDatabasePorts = map[string]string{
	"postgres":   "5432",
	"mysql":      "3306",
	"sqlserver":  "1433",
	"clickhouse": "9000",
}

// DefaultPortForDriver returns the conventional default port for the
// given driver. Falls back to Postgres' 5432 for unknown drivers so the
// callers' previous behavior is preserved when a typo'd driver value
// lands. Returns "" for sqlite/sqlite3 (file-based — no port).
func DefaultPortForDriver(driver string) string {
	d := strings.ToLower(strings.TrimSpace(driver))
	if d == "sqlite" || d == "sqlite3" {
		return ""
	}
	if p, ok := defaultDatabasePorts[d]; ok {
		return p
	}
	return "5432"
}

// BuildMigrationURL reads config.yaml and env vars to build a database migration URL.
func BuildMigrationURL() string {
	k := loadConfig()
	driver := k.String("database.driver")
	if driver == "" {
		driver = "postgres"
	}
	user := k.String("database.user")
	password := k.String("database.password")
	host := k.String("database.host")
	if host == "" {
		host = "localhost"
	}
	port := k.String("database.port")
	if port == "" {
		port = DefaultPortForDriver(driver)
	}
	name := k.String("database.name")
	sslmode := k.String("database.sslmode")
	if sslmode == "" {
		sslmode = "disable"
	}

	switch driver {
	case "mysql":
		return fmt.Sprintf("mysql://%s:%s@tcp(%s:%s)/%s", user, password, host, port, name)
	case "sqlite":
		return fmt.Sprintf("sqlite3://%s", name)
	case "sqlserver":
		return fmt.Sprintf("sqlserver://%s:%s@%s:%s?database=%s", user, password, host, port, name)
	case "clickhouse":
		return fmt.Sprintf("clickhouse://%s:%s@%s:%s/%s", user, password, host, port, name)
	default:
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", user, password, host, port, name, sslmode)
	}
}

// BuildDatabaseEndpoint reads database.* from config.yaml + env vars
// and returns the DB backend's host:port endpoint, plus an `enabled`
// flag indicating whether the app actually wants a network DB.
//
// `enabled` is false when `database.driver` is "sqlite"/"sqlite3" —
// file-backed drivers don't connect anywhere, and the dev preflight
// must skip the network probe entirely rather than report
// "unreachable" for a driver that has no host:port at all.
//
// When enabled is true and host/port resolve to their scaffold
// defaults (localhost:5432), the returned endpoint reflects those
// defaults so the TCP probe targets the same host:port the framework
// would in production.
//
// This is the source of truth for "is the DB accepting connections?"
// — separate from BuildMigrationURL, which builds a fully-formed DSN
// suitable for `migrate` and is the wrong tool for a connectivity
// probe (it requires schema_migrations to exist, which fails the
// first time a fresh Postgres comes up).
func BuildDatabaseEndpoint() (endpoint string, enabled bool) {
	k := loadConfig()
	driver := strings.ToLower(strings.TrimSpace(k.String("database.driver")))
	if driver == "sqlite" || driver == "sqlite3" {
		return "", false
	}
	host := k.String("database.host")
	if host == "" {
		host = "localhost"
	}
	port := k.String("database.port")
	if port == "" {
		port = DefaultPortForDriver(driver)
	}
	return fmt.Sprintf("%s:%s", host, port), true
}

// GetPort reads the server port from config.yaml or env.
func GetPort() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	if port := loadConfig().String("server.port"); port != "" {
		return port
	}
	return "8080"
}

// ReadDBDriver reads the database.driver from config.yaml.
func ReadDBDriver() string {
	driver := loadConfig().String("database.driver")
	if driver == "" {
		return "postgres"
	}
	return driver
}

// BuildCacheEndpoint reads cache.* from config.yaml + env vars and
// returns the cache backend's host:port endpoint, plus an `enabled`
// flag indicating whether the app actually wants a network cache.
//
// `enabled` is false when `cache.driver` is "memory" or empty —
// memory-backed caches don't connect anywhere, and the dev preflight
// must skip the probe entirely rather than report "unreachable" and
// nag a user who explicitly opted out of Redis.
//
// When enabled is true but host/port can't be assembled, the returned
// endpoint is empty and the caller should treat that as a config
// error (not as "unreachable").
func BuildCacheEndpoint() (endpoint string, enabled bool) {
	k := loadConfig()
	driver := strings.ToLower(strings.TrimSpace(k.String("cache.driver")))
	if driver == "" || driver == "memory" {
		return "", false
	}
	host := k.String("cache.redis.host")
	if host == "" {
		host = "localhost"
	}
	port := k.String("cache.redis.port")
	if port == "" {
		port = "6379"
	}
	return fmt.Sprintf("%s:%s", host, port), true
}

// BuildQueueEndpoint reads queue.* from config.yaml + env vars and
// returns the queue backend's host:port endpoint, plus an `enabled`
// flag.
//
// `enabled` follows `queue.enabled` directly. When false (the
// scaffold's default), the preflight skips the probe so a project
// that doesn't use the queue doesn't get a spurious "unreachable"
// warning at every `gofasta dev` invocation.
//
// The queue's Redis defaults to host=localhost port=6379 when not
// explicitly configured, matching pkg/queue's own resolution.
func BuildQueueEndpoint() (endpoint string, enabled bool) {
	k := loadConfig()
	if !k.Bool("queue.enabled") {
		return "", false
	}
	host := k.String("queue.redis.host")
	if host == "" {
		host = "localhost"
	}
	port := k.String("queue.redis.port")
	if port == "" {
		port = "6379"
	}
	return fmt.Sprintf("%s:%s", host, port), true
}

func loadConfig() *koanf.Koanf {
	k := koanf.New(".")
	if _, err := os.Stat("config.yaml"); err == nil {
		_ = k.Load(file.Provider("config.yaml"), yaml.Parser())
	}
	// Overlay with env vars from the generic GOFASTA_ prefix AND the
	// project-specific prefix derived from go.mod. A project scaffolded as
	// "mylearn" has its .env set MYLEARN_DATABASE_HOST — the CLI must read
	// that too, or `gofasta migrate up` / `gofasta dev` preflight can't
	// find the database when it differs from config.yaml defaults.
	for _, prefix := range envPrefixes() {
		p := prefix // capture for the closure
		_ = k.Load(env.Provider(p, ".", func(s string) string {
			return strings.ReplaceAll(
				strings.ToLower(strings.TrimPrefix(s, p)),
				"_", ".",
			)
		}), nil)
	}
	return k
}

// EnvPrefixes is the exported view of envPrefixes for callers outside
// this package (e.g. the dev preflight menu, which sets env vars to
// override a connection string the user typed at runtime). Production
// readers should still go through loadConfig — this helper is for the
// rare cases where a caller needs to know which prefix wins.
//
// Returns the prefixes in the SAME order loadConfig consumes them, so
// callers that want to "win" should write to the last entry's prefix.
func EnvPrefixes() []string { return envPrefixes() }

// ProjectEnvPrefix returns the project-specific env var prefix derived
// from go.mod's module path (e.g. "DATA_" for `module data`, or
// "IRONJI_SENDA_V2_" → "IRONJISENDAV2_" after illegal-char stripping).
// Returns "" if go.mod is missing or malformed.
//
// Used by callers that need to write project-prefixed values to disk
// (persistence) — toolkit-branded "GOFASTA_*" must NOT appear in a
// project's own .env. The project owns its env namespace; gofasta only
// reads from it.
func ProjectEnvPrefix() string { return projectPrefix() }

// envPrefixes returns the list of env var prefixes to consult when reading
// database connection details. GOFASTA_ is always included so the generic
// fallback works even outside a project directory; when a go.mod file is
// present in cwd we also append the project-specific prefix derived from
// its module path (the last segment, shell-var-safe, uppercased, with an
// underscore suffix). Project-specific prefix is loaded second so it
// overrides GOFASTA_ on conflict.
func envPrefixes() []string {
	prefixes := []string{"GOFASTA_"}
	if name := projectPrefix(); name != "" && name != "GOFASTA_" {
		prefixes = append(prefixes, name)
	}
	return prefixes
}

// projectPrefix reads go.mod in cwd and returns the project's env var prefix
// (uppercased module-last-segment + "_"). Returns an empty string if go.mod
// is missing, malformed, or the last segment can't be extracted. Illegal
// shell-variable characters (dashes, dots) are stripped so a module named
// "my-learn" maps to MYLEARN_ rather than the invalid "MY-LEARN_".
func projectPrefix() string {
	content, err := os.ReadFile("go.mod")
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(line, "module"))
		path = strings.Trim(path, `"`)
		if path == "" {
			return ""
		}
		segments := strings.Split(path, "/")
		last := segments[len(segments)-1]
		cleaned := strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
				return r
			default:
				return -1
			}
		}, last)
		if cleaned == "" {
			return ""
		}
		return strings.ToUpper(cleaned) + "_"
	}
	return ""
}
