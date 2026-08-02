package skeleton

import (
	"html/template"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
	texttemplate "text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectFS_NotEmpty(t *testing.T) {
	count := 0
	err := fs.WalkDir(ProjectFS, "project", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	require.NoError(t, err)
	assert.Greater(t, count, 0, "ProjectFS should contain files")
}

func TestProjectFS_ContainsExpectedFiles(t *testing.T) {
	expectedFiles := []string{
		"project/config.yaml.tmpl",
		"project/cmd/serve.go.tmpl",
		"project/cmd/root.go.tmpl",
		"project/Makefile.tmpl",
		"project/Dockerfile.tmpl",
		"project/.golangci.yml",
		"project/dot-gitignore",
		"project/dot-go-version",
	}

	files := make(map[string]bool)
	fs.WalkDir(ProjectFS, "project", func(path string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			files[path] = true
		}
		return nil
	})

	for _, expected := range expectedFiles {
		assert.True(t, files[expected], "expected file %s not found in ProjectFS", expected)
	}
}

func TestProjectFS_TemplatesAreParseable(t *testing.T) {
	err := fs.WalkDir(ProjectFS, "project", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".tmpl") {
			return nil
		}

		data, readErr := fs.ReadFile(ProjectFS, path)
		require.NoError(t, readErr, "failed to read %s", path)

		_, parseErr := template.New(path).Parse(string(data))
		assert.NoError(t, parseErr, "template parse error in %s", path)
		return nil
	})
	require.NoError(t, err)
}

// TestProjectFS_NoMigrationsDirInProject — the project tree must NOT
// ship a db/migrations directory; per-driver foundational migrations
// live in MigrationsFS and are copied in at scaffold time.
func TestProjectFS_NoMigrationsDirInProject(t *testing.T) {
	err := fs.WalkDir(ProjectFS, "project", func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if strings.HasPrefix(path, "project/db/migrations") {
			t.Errorf("project tree must not contain db/migrations (found %q) — see MigrationsFS", path)
		}
		return nil
	})
	require.NoError(t, err)
}

// TestMigrationsFS_HasEveryDriver — MigrationsFS must contain a
// subdirectory per supported driver. Catches regressions where a
// driver's foundational migrations get dropped.
func TestMigrationsFS_HasEveryDriver(t *testing.T) {
	for _, driver := range []string{"postgres", "mysql", "sqlite", "sqlserver", "clickhouse"} {
		t.Run(driver, func(t *testing.T) {
			entries, err := fs.ReadDir(MigrationsFS, "migrations/"+driver)
			require.NoError(t, err)
			assert.NotEmpty(t, entries,
				"migrations/%s must contain at least one .sql file", driver)
		})
	}
}

// TestMigrationsFS_PerDriverShape — pin the expected migration set
// per driver:
//   - postgres ships 5 up + 5 down (citext + 3 functions + users)
//   - every other driver ships exactly 1 up + 1 down (foundational
//     users table only, with inlined triggers)
func TestMigrationsFS_PerDriverShape(t *testing.T) {
	cases := map[string]struct{ up, down int }{
		"postgres":   {5, 5},
		"mysql":      {1, 1},
		"sqlite":     {1, 1},
		"sqlserver":  {1, 1},
		"clickhouse": {1, 1},
	}
	for driver, want := range cases {
		t.Run(driver, func(t *testing.T) {
			entries, err := fs.ReadDir(MigrationsFS, "migrations/"+driver)
			require.NoError(t, err)
			var up, down int
			for _, e := range entries {
				switch {
				case strings.HasSuffix(e.Name(), ".up.sql"):
					up++
				case strings.HasSuffix(e.Name(), ".down.sql"):
					down++
				}
			}
			assert.Equal(t, want.up, up, "%s up.sql count", driver)
			assert.Equal(t, want.down, down, "%s down.sql count", driver)
		})
	}
}

// TestMigrationsFS_NotDeletableTriggerPresent — every triggerable
// driver's users-table migration must include the not-deletable
// trigger reference. ClickHouse is excluded (engine doesn't support
// triggers; documented as app-layer-only).
func TestMigrationsFS_NotDeletableTriggerPresent(t *testing.T) {
	cases := map[string]string{
		"postgres":  "avoid_deleting_record_with_is_deletable_equal_to_false",
		"mysql":     "avoid_deleting_not_deletable_users_trigger",
		"sqlite":    "avoid_deleting_not_deletable_users_trigger",
		"sqlserver": "trg_users_avoid_not_deletable",
	}
	for driver, marker := range cases {
		t.Run(driver, func(t *testing.T) {
			entries, err := fs.ReadDir(MigrationsFS, "migrations/"+driver)
			require.NoError(t, err)
			var found bool
			for _, e := range entries {
				if !strings.HasSuffix(e.Name(), "create_users.up.sql") {
					continue
				}
				body, err := fs.ReadFile(MigrationsFS, "migrations/"+driver+"/"+e.Name())
				require.NoError(t, err)
				if strings.Contains(string(body), marker) {
					found = true
					break
				}
			}
			assert.True(t, found,
				"%s users migration must reference %q (DB-level not-deletable guard)",
				driver, marker)
		})
	}
}

// TestMigrationsFS_ClickHouseDocumentsLimitation — the ClickHouse
// migration must document that DB-level enforcement is impossible and
// the invariants are app-layer-only. Prevents future contributors
// from silently dropping the disclaimer.
func TestMigrationsFS_ClickHouseDocumentsLimitation(t *testing.T) {
	body, err := fs.ReadFile(MigrationsFS, "migrations/clickhouse/000001_create_users.up.sql")
	require.NoError(t, err)
	lower := strings.ToLower(string(body))
	assert.Contains(t, lower, "clickhouse does not support")
	assert.Contains(t, lower, "application layer")
}

// funcDeclPattern matches any top-level func declaration and captures whether
// it has a receiver.
var funcDeclPattern = regexp.MustCompile(`(?m)^func\s+(\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)`)

// resolverTemplates returns the embedded *.resolvers.go.tmpl paths.
func resolverTemplates(t *testing.T) []string {
	t.Helper()
	var out []string
	require.NoError(t, fs.WalkDir(ProjectFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".resolvers.go.tmpl") {
			out = append(out, p)
		}
		return nil
	}))
	return out
}

// TestResolverTemplates_DeclareOnlyResolverMethods is the guard: a gqlgen-owned
// template must contain nothing but methods, because anything else will be
// commented out the moment gqlgen runs.
func TestResolverTemplates_DeclareOnlyResolverMethods(t *testing.T) {
	templates := resolverTemplates(t)
	require.NotEmpty(t, templates, "expected at least one *.resolvers.go.tmpl in the skeleton")

	for _, tmpl := range templates {
		t.Run(tmpl, func(t *testing.T) {
			body, err := fs.ReadFile(ProjectFS, tmpl)
			require.NoError(t, err)

			for _, m := range funcDeclPattern.FindAllStringSubmatch(string(body), -1) {
				receiver, name := m[1], m[2]
				assert.NotEmpty(t, receiver,
					"%s declares plain function %q — gqlgen will comment it out when it "+
						"regenerates this file. Move it to a file gqlgen does not own "+
						"(e.g. app/graphql/resolvers/gql_errors.go.tmpl).", tmpl, name)
			}
		})
	}
}

// TestResolverHelpers_LiveOutsideGqlgenOwnedFiles pins where the helpers
// actually are, so a future move back into a *.resolvers.go file fails here
// with a message explaining why that does not work.
func TestResolverHelpers_LiveOutsideGqlgenOwnedFiles(t *testing.T) {
	const helpersFile = "project/app/graphql/resolvers/gql_errors.go.tmpl"

	body, err := fs.ReadFile(ProjectFS, helpersFile)
	require.NoError(t, err, "the shared GraphQL error helpers must ship in a gqlgen-safe file")

	for _, fn := range []string{"gqlError", "validationGqlError", "internalGqlError"} {
		assert.Contains(t, string(body), "func "+fn+"(",
			"%s must declare %s", helpersFile, fn)
	}

	// The filename must not collide with a generated resolver file, which is
	// what would put it back under gqlgen's control.
	assert.False(t, strings.HasSuffix(path.Base(helpersFile), ".resolvers.go.tmpl"),
		"the helpers file must not be named like a gqlgen-generated resolver")
}

// TestResolverTemplates_DoNotImportGqlerror keeps the import in step with the
// declarations: once the helpers moved out, a lingering gqlerror import in a
// resolvers template would be unused and fail to compile.
func TestResolverTemplates_DoNotImportGqlerror(t *testing.T) {
	for _, tmpl := range resolverTemplates(t) {
		t.Run(tmpl, func(t *testing.T) {
			body, err := fs.ReadFile(ProjectFS, tmpl)
			require.NoError(t, err)
			assert.NotContains(t, string(body), "gqlparser/v2/gqlerror",
				"%s imports gqlerror but declares no helpers that use it", tmpl)
		})
	}
}

// renderProjectTemplate renders a ProjectFS template with representative
// scaffold data. Uses text/template like the generator does (new.go) — the
// html/template import above is parse-only checking, but rendering through
// it would HTML-escape YAML/shell content.
func renderProjectTemplate(t *testing.T, tmplPath, driver string) string {
	t.Helper()
	data, err := fs.ReadFile(ProjectFS, tmplPath)
	require.NoError(t, err)
	tmpl, err := texttemplate.New(path.Base(tmplPath)).Parse(string(data))
	require.NoError(t, err, "parse %s", tmplPath)
	var b strings.Builder
	require.NoError(t, tmpl.Execute(&b, map[string]any{
		"ProjectName":      "Myapp",
		"ProjectNameLower": "myapp",
		"ProjectNameUpper": "MYAPP",
		"ModulePath":       "github.com/example/myapp",
		"DBDriver":         driver,
		"GraphQL":          false,
	}), "execute %s (driver=%s)", tmplPath, driver)
	return b.String()
}

// TestProjectFS_NoDeployScript — the reference deploy.sh was deleted: it
// documented a /etc/<app> layout incompatible with `gofasta deploy`'s
// release directories. The scaffold must ship exactly one deploy story.
func TestProjectFS_NoDeployScript(t *testing.T) {
	_, err := fs.ReadFile(ProjectFS, "project/deployments/systemd/deploy.sh.tmpl")
	assert.Error(t, err, "deploy.sh.tmpl must not ship — it contradicts the gofasta deploy release layout")
}

// TestProductionCompose_DeployContract — the production compose file is
// half of the `gofasta deploy` docker method: the deploy pins the
// transferred image via APP_IMAGE and relies on a stable compose project
// name for container/volume reuse across releases.
func TestProductionCompose_DeployContract(t *testing.T) {
	const tmpl = "project/deployments/docker/compose.production.yaml.tmpl"
	for _, driver := range []string{"postgres", "mysql", "sqlite", "sqlserver", "clickhouse"} {
		t.Run(driver, func(t *testing.T) {
			out := renderProjectTemplate(t, tmpl, driver)

			assert.Contains(t, out, "name: ${PROJECT_NAME:-myapp}",
				"compose needs a stable project name so containers/volumes survive releases")
			assert.Contains(t, out, "image: ${APP_IMAGE:-myapp:latest}",
				"the app service must consume the image gofasta deploy transfers")
			assert.Contains(t, out, "http://127.0.0.1:8080/health/live",
				"the app service needs a container-level healthcheck")

			if driver == "sqlite" {
				assert.Contains(t, out, "app_data:/data",
					"sqlite needs a volume or the database dies with the container")
				assert.Contains(t, out, "/data/myapp.db",
					"sqlite database default must live on the app_data volume")
				assert.NotContains(t, out, "db_data")
			} else {
				assert.Contains(t, out, "db_data")
				assert.NotContains(t, out, "app_data")
			}
		})
	}
}

// TestSystemdUnit_DeployLayout — the unit must run from the release layout
// `gofasta deploy` actually provisions (/opt/<app>), not the abandoned
// /etc/<app> scheme nothing populates.
func TestSystemdUnit_DeployLayout(t *testing.T) {
	out := renderProjectTemplate(t, "project/deployments/systemd/app.service.tmpl", "postgres")

	assert.Contains(t, out, "WorkingDirectory=/opt/myapp/current")
	assert.Contains(t, out, "EnvironmentFile=-/opt/myapp/shared/.env",
		"secrets live in shared/.env; the '-' keeps config.yaml-only projects bootable")
	assert.Contains(t, out, "ReadWritePaths=/opt/myapp")
	assert.NotContains(t, out, "/etc/myapp",
		"the /etc/<app> layout is dead — nothing provisions it")
	assert.Contains(t, out, "After=network-online.target")
	assert.NotContains(t, out, "postgresql.service",
		"hardcoded DB units are wrong for sqlite/sqlserver/clickhouse/docker-hosted databases")
}

// TestDeployVpsWorkflow_RunsGofastaDeploy — the CI template must go through
// `gofasta deploy` (one deploy code path) instead of the old git-pull model
// that bypassed releases, health gates, and rollback.
func TestDeployVpsWorkflow_RunsGofastaDeploy(t *testing.T) {
	out := renderProjectTemplate(t, "project/deployments/ci/github-actions-deploy-vps.yml.tmpl", "postgres")

	assert.Contains(t, out, "gofasta deploy")
	assert.Contains(t, out, "workflow_dispatch:")
	for _, secret := range []string{"DEPLOY_HOST", "DEPLOY_USER", "DEPLOY_SSH_KEY", "DEPLOY_PORT"} {
		assert.Contains(t, out, secret)
	}
	assert.NotContains(t, out, "git pull", "the git-pull deploy model contradicts the release layout")
	assert.NotContains(t, out, "VPS_HOST", "stale secret names")
}

// TestConfigYaml_DeployKeys — every key internal/deploy/config.go reads
// must be discoverable in the scaffolded config.
func TestConfigYaml_DeployKeys(t *testing.T) {
	out := renderProjectTemplate(t, "project/config.yaml.tmpl", "postgres")
	for _, key := range []string{
		"host:", "method:", "port:", "path:", "arch:",
		"health_path:", "health_timeout:", "keep_releases:",
		"strict_host_key:", "domain:",
	} {
		assert.Contains(t, out, key, "deploy config key %q missing from config.yaml.tmpl", key)
	}
}

// TestDockerfile_MigrateDriverTags — the migrate CLI build must use the
// project's single driver tag (sqlite needs cgo + a C toolchain).
func TestDockerfile_MigrateDriverTags(t *testing.T) {
	for driver, tag := range map[string]string{
		"postgres":   "-tags 'postgres'",
		"mysql":      "-tags 'mysql'",
		"sqlite":     "-tags 'sqlite3'",
		"sqlserver":  "-tags 'sqlserver'",
		"clickhouse": "-tags 'clickhouse'",
	} {
		t.Run(driver, func(t *testing.T) {
			out := renderProjectTemplate(t, "project/Dockerfile.tmpl", driver)
			assert.Contains(t, out, tag)
			if driver == "sqlite" {
				assert.Contains(t, out, "build-base", "sqlite migrate driver is cgo-backed")
				assert.Contains(t, out, "chown app:app /data")
			} else {
				assert.NotContains(t, out, "build-base")
			}
		})
	}
}
