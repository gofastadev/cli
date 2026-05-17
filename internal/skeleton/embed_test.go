package skeleton

import (
	"io/fs"
	"strings"
	"testing"
	"text/template"

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
