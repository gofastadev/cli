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
// Returns empty string if config cannot be loaded.
func BuildMigrationURL() string {
	k := loadConfig()
	if k == nil {
		return ""
	}

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
	k := loadConfig()
	if k != nil {
		if port := k.String("server.port"); port != "" {
			return port
		}
	}
	return "8080"
}

// ReadDBDriver reads the database.driver from config.yaml.
func ReadDBDriver() string {
	k := loadConfig()
	if k == nil {
		return "postgres"
	}
	driver := k.String("database.driver")
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
	// Overlay with GOFASTA_ prefixed env vars
	_ = k.Load(env.Provider("GOFASTA_", ".", func(s string) string {
		return strings.ReplaceAll(
			strings.ToLower(strings.TrimPrefix(s, "GOFASTA_")),
			"_", ".",
		)
	}), nil)
	return k
}
