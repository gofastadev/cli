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
		port = "5432"
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
