package templates

import (
	"html/template"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllTemplatesAreParseable(t *testing.T) {
	templates := map[string]string{
		"Model":         Model,
		"Controller":    Controller,
		"DTOs":          DTOs,
		"Repo":          Repo,
		"RepoInterface": RepoInterface,
		"Svc":           Svc,
		"SvcInterface":  SvcInterface,
		"Routes":        Routes,
		"WireProvider":  WireProvider,
		"GraphQL":       GraphQL,
		"SvcTest":       SvcTest,
		"RepoTest":      RepoTest,
		"Resolvers":     Resolvers,
		"Inputs":        Inputs,
	}
	for name, tmpl := range templates {
		t.Run(name, func(t *testing.T) {
			_, err := template.New(name).Funcs(funcMap).Parse(tmpl)
			require.NoError(t, err)
		})
	}
}

func TestAllTemplatesRenderWithSampleData(t *testing.T) {
	data := sampleData()
	templates := map[string]string{
		"Model":         Model,
		"Controller":    Controller,
		"DTOs":          DTOs,
		"Repo":          Repo,
		"RepoInterface": RepoInterface,
		"Svc":           Svc,
		"SvcInterface":  SvcInterface,
		"Routes":        Routes,
		"WireProvider":  WireProvider,
		"GraphQL":       GraphQL,
		"SvcTest":       SvcTest,
		"RepoTest":      RepoTest,
		"Resolvers":     Resolvers,
		"Inputs":        Inputs,
	}
	for name, tmpl := range templates {
		t.Run(name, func(t *testing.T) {
			parsed, err := template.New(name).Funcs(funcMap).Parse(tmpl)
			require.NoError(t, err)
			var buf strings.Builder
			err = parsed.Execute(&buf, data)
			require.NoError(t, err)
			assert.NotEmpty(t, buf.String())
		})
	}
}
