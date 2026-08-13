package templates

import (
	"html/template"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrationTemplates_AllDrivers(t *testing.T) {
	data := sampleData()
	migrationPairs := map[string][2]string{
		"postgres":   {MigUpPostgres, MigDownPostgres},
		"mysql":      {MigUpMySQL, MigDownMySQL},
		"sqlite":     {MigUpSQLite, MigDownSQLite},
		"sqlserver":  {MigUpSQLServer, MigDownSQLServer},
		"clickhouse": {MigUpClickHouse, MigDownClickHouse},
	}
	for driver, pair := range migrationPairs {
		t.Run(driver+"_up", func(t *testing.T) {
			parsed, err := template.New("up").Funcs(funcMap).Parse(pair[0])
			require.NoError(t, err)
			var buf strings.Builder
			require.NoError(t, parsed.Execute(&buf, data))
			output := buf.String()
			assert.NotEmpty(t, output)
			assert.Contains(t, output, "products")
		})
		t.Run(driver+"_down", func(t *testing.T) {
			parsed, err := template.New("down").Funcs(funcMap).Parse(pair[1])
			require.NoError(t, err)
			var buf strings.Builder
			require.NoError(t, parsed.Execute(&buf, data))
			output := buf.String()
			assert.NotEmpty(t, output)
		})
	}
}
