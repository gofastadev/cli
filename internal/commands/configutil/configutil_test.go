package configutil

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func setupConfigDir(t *testing.T, configContent string) {
	t.Helper()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	if configContent != "" {
		os.WriteFile(dir+"/config.yaml", []byte(configContent), 0644)
	}
	os.Chdir(dir)
}

func TestBuildMigrationURL_Postgres(t *testing.T) {
	setupConfigDir(t, `database:
  driver: postgres
  user: testuser
  password: testpass
  host: localhost
  port: "5432"
  name: testdb
  sslmode: disable
`)
	url := BuildMigrationURL()
	assert.Equal(t, "postgres://testuser:testpass@localhost:5432/testdb?sslmode=disable", url)
}

func TestBuildMigrationURL_MySQL(t *testing.T) {
	setupConfigDir(t, `database:
  driver: mysql
  user: root
  password: secret
  host: 127.0.0.1
  port: "3306"
  name: mydb
`)
	url := BuildMigrationURL()
	assert.Equal(t, "mysql://root:secret@tcp(127.0.0.1:3306)/mydb", url)
}

func TestBuildMigrationURL_SQLite(t *testing.T) {
	setupConfigDir(t, `database:
  driver: sqlite
  name: test.db
`)
	url := BuildMigrationURL()
	assert.Equal(t, "sqlite3://test.db", url)
}

func TestBuildMigrationURL_SQLServer(t *testing.T) {
	setupConfigDir(t, `database:
  driver: sqlserver
  user: sa
  password: pass
  host: localhost
  port: "1433"
  name: mydb
`)
	url := BuildMigrationURL()
	assert.Equal(t, "sqlserver://sa:pass@localhost:1433?database=mydb", url)
}

func TestBuildMigrationURL_ClickHouse(t *testing.T) {
	setupConfigDir(t, `database:
  driver: clickhouse
  user: default
  password: ""
  host: localhost
  port: "9000"
  name: mydb
`)
	url := BuildMigrationURL()
	assert.Equal(t, "clickhouse://default:@localhost:9000/mydb", url)
}

func TestBuildMigrationURL_Defaults(t *testing.T) {
	setupConfigDir(t, `database:
  name: mydb
`)
	url := BuildMigrationURL()
	// defaults: driver=postgres, host=localhost, port=5432, sslmode=disable
	assert.Equal(t, "postgres://:@localhost:5432/mydb?sslmode=disable", url)
}

func TestBuildMigrationURL_NoConfig(t *testing.T) {
	setupConfigDir(t, "")
	url := BuildMigrationURL()
	// loadConfig returns a koanf instance even without a file, so it won't be nil
	// but it will have default values
	assert.Contains(t, url, "postgres://")
}

func TestBuildMigrationURL_EnvOverride(t *testing.T) {
	setupConfigDir(t, `database:
  driver: postgres
  name: yamldb
`)
	t.Setenv("GOFASTA_DATABASE_NAME", "envdb")
	url := BuildMigrationURL()
	assert.Contains(t, url, "envdb")
}

func TestGetPort_FromEnv(t *testing.T) {
	setupConfigDir(t, "")
	t.Setenv("PORT", "9090")
	assert.Equal(t, "9090", GetPort())
}

func TestGetPort_FromConfig(t *testing.T) {
	setupConfigDir(t, `server:
  port: "3000"
`)
	// Clear PORT env
	t.Setenv("PORT", "")
	assert.Equal(t, "3000", GetPort())
}

func TestGetPort_Default(t *testing.T) {
	setupConfigDir(t, "")
	t.Setenv("PORT", "")
	assert.Equal(t, "8080", GetPort())
}

func TestReadDBDriver_FromConfig(t *testing.T) {
	setupConfigDir(t, `database:
  driver: mysql
`)
	assert.Equal(t, "mysql", ReadDBDriver())
}

func TestReadDBDriver_Default(t *testing.T) {
	setupConfigDir(t, "")
	assert.Equal(t, "postgres", ReadDBDriver())
}

func TestReadDBDriver_EmptyDriver(t *testing.T) {
	setupConfigDir(t, `database:
  driver: ""
`)
	assert.Equal(t, "postgres", ReadDBDriver())
}

func TestLoadConfig_ReturnsKoanf(t *testing.T) {
	setupConfigDir(t, `server:
  port: "4000"
`)
	k := loadConfig()
	assert.NotNil(t, k)
	assert.Equal(t, "4000", k.String("server.port"))
}

func TestLoadConfig_EnvOverride(t *testing.T) {
	setupConfigDir(t, `database:
  driver: postgres
`)
	t.Setenv("GOFASTA_DATABASE_DRIVER", "mysql")
	k := loadConfig()
	assert.Equal(t, "mysql", k.String("database.driver"))
}
