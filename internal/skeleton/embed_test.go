package skeleton

import (
	"html/template"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"

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
