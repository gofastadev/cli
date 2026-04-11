package configutil

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// --- Project-prefix env var support ---
//
// When a go.mod exists in cwd, env vars prefixed with the project name
// (uppercase last-segment + "_") must override config.yaml the same way
// GOFASTA_* does. Critical for `gofasta dev` / `gofasta migrate up` to
// pick up DB connection overrides from a project's .env file.

func TestLoadConfig_ProjectPrefixOverride(t *testing.T) {
	setupConfigDir(t, `database:
  driver: postgres
  name: yamldb
  host: localhost
  port: "5432"
`)
	writeGoMod(t, "github.com/acme/mylearn")
	t.Setenv("MYLEARN_DATABASE_HOST", "docker-host")
	t.Setenv("MYLEARN_DATABASE_PORT", "5433")
	t.Setenv("MYLEARN_DATABASE_NAME", "mylearn_dev")

	url := BuildMigrationURL()
	assert.Contains(t, url, "docker-host")
	assert.Contains(t, url, "5433")
	assert.Contains(t, url, "mylearn_dev")
}

func TestLoadConfig_ProjectPrefixCoexistsWithGofasta(t *testing.T) {
	// When both prefixes are set, the project prefix should take precedence
	// because it's loaded second (koanf overlay order).
	setupConfigDir(t, `database:
  name: yamldb
`)
	writeGoMod(t, "github.com/acme/mylearn")
	t.Setenv("GOFASTA_DATABASE_NAME", "gofasta-wins")
	t.Setenv("MYLEARN_DATABASE_NAME", "project-wins")

	url := BuildMigrationURL()
	assert.Contains(t, url, "project-wins",
		"project-specific prefix should override the generic GOFASTA_ prefix")
}

func TestProjectPrefix_StandardPath(t *testing.T) {
	setupConfigDir(t, "")
	writeGoMod(t, "github.com/acme/mylearn")
	assert.Equal(t, "MYLEARN_", projectPrefix())
}

func TestProjectPrefix_ModuleWithDash(t *testing.T) {
	// "my-learn" must be normalized to MYLEARN_ (shell var names can't
	// contain dashes).
	setupConfigDir(t, "")
	writeGoMod(t, "github.com/acme/my-learn")
	assert.Equal(t, "MYLEARN_", projectPrefix())
}

func TestProjectPrefix_NoGoMod(t *testing.T) {
	setupConfigDir(t, "")
	assert.Equal(t, "", projectPrefix())
}

func TestProjectPrefix_MalformedGoMod(t *testing.T) {
	setupConfigDir(t, "")
	require.NoError(t, os.WriteFile("go.mod", []byte("not a real go.mod\n"), 0o644))
	assert.Equal(t, "", projectPrefix())
}

func TestProjectPrefix_SingleWordModule(t *testing.T) {
	setupConfigDir(t, "")
	writeGoMod(t, "foo")
	assert.Equal(t, "FOO_", projectPrefix())
}

func TestProjectPrefix_OnlyIllegalChars(t *testing.T) {
	setupConfigDir(t, "")
	writeGoMod(t, "github.com/acme/---")
	assert.Equal(t, "", projectPrefix())
}

func TestProjectPrefix_EmptyModulePath(t *testing.T) {
	setupConfigDir(t, "")
	require.NoError(t, os.WriteFile("go.mod", []byte("module \"\"\n"), 0o644))
	assert.Equal(t, "", projectPrefix())
}

func TestEnvPrefixes_DeDupesGofasta(t *testing.T) {
	// A go.mod whose last segment is "gofasta" would produce GOFASTA_ as
	// both the generic and project-specific prefix. envPrefixes should
	// not double-add.
	setupConfigDir(t, "")
	writeGoMod(t, "github.com/gofastadev/gofasta")
	assert.Equal(t, []string{"GOFASTA_"}, envPrefixes(),
		"duplicate prefixes should be collapsed")
}

// writeGoMod writes a minimal go.mod in the current working directory
// (which setupConfigDir has already set to a fresh tempdir).
func writeGoMod(t *testing.T, modulePath string) {
	t.Helper()
	content := "module " + modulePath + "\n\ngo 1.25.8\n"
	require.NoError(t, os.WriteFile("go.mod", []byte(content), 0o644))
}
