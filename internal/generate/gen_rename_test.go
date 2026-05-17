package generate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/require"
)

func setupRenameProject(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))

	mustWriteFile(t, filepath.Join(tmp, "app", "models", "order.model.go"), `package models

type Order struct {
	ID    string
	Total int `+"`gorm:\"column:total;not null\"`"+`
}
`)
	mustWriteFile(t, filepath.Join(tmp, "app", "dtos", "order.dtos.go"), `package dtos

type OrderResponse struct {
	Total int `+"`json:\"total\"`"+`
}
`)
	mustWriteFile(t, filepath.Join(tmp, "app", "services", "order.service.go"), `package services

func (s *orderService) Sum(t int) int { return t + 0 /* Total */ }
`)
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "db", "migrations"), 0o755))
	return tmp
}

func TestGenRename_PreviewModeRecordsPlan(t *testing.T) {
	tmp := setupRenameProject(t)
	chdirTest(t, tmp)

	SetDryRun(true)
	defer SetDryRun(false)
	require.NoError(t, GenRename(RenameData{
		Resource: "Order",
		OldField: "Total",
		NewField: "AmountCents",
		Apply:    false,
	}))

	plan := Plan()
	require.NotEmpty(t, plan, "preview mode should record at least one planned action")
	// Disk untouched.
	model, _ := os.ReadFile(filepath.Join(tmp, "app", "models", "order.model.go"))
	require.Contains(t, string(model), "Total",
		"preview must not touch the source file")
	require.NotContains(t, string(model), "AmountCents")
}

func TestGenRename_ApplyMode_RewritesAcrossLayers(t *testing.T) {
	tmp := setupRenameProject(t)
	chdirTest(t, tmp)

	require.NoError(t, GenRename(RenameData{
		Resource: "Order",
		OldField: "Total",
		NewField: "AmountCents",
		Apply:    true,
	}))

	model, _ := os.ReadFile(filepath.Join(tmp, "app", "models", "order.model.go"))
	require.Contains(t, string(model), "AmountCents")
	require.NotContains(t, string(model), "\tTotal int")

	// GORM column tag rewritten.
	require.Contains(t, string(model), "column:amount_cents")

	dto, _ := os.ReadFile(filepath.Join(tmp, "app", "dtos", "order.dtos.go"))
	require.Contains(t, string(dto), "AmountCents")
	require.Contains(t, string(dto), `json:"amountCents"`)

	// Migration files exist.
	entries, _ := os.ReadDir(filepath.Join(tmp, "db", "migrations"))
	require.NotEmpty(t, entries)
	var upFound bool
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			body, _ := os.ReadFile(filepath.Join(tmp, "db", "migrations", e.Name()))
			require.Contains(t, string(body),
				"ALTER TABLE orders RENAME COLUMN total TO amount_cents")
			upFound = true
		}
	}
	require.True(t, upFound)
}

func TestGenRename_ValidationErrors(t *testing.T) {
	t.Run("missing-resource", func(t *testing.T) {
		err := GenRename(RenameData{OldField: "X", NewField: "Y"})
		require.Error(t, err)
		var ce *clierr.Error
		require.True(t, errors.As(err, &ce))
		require.Equal(t, string(clierr.CodeInvalidName), ce.Code)
	})
	t.Run("same-name", func(t *testing.T) {
		err := GenRename(RenameData{Resource: "X", OldField: "Y", NewField: "Y"})
		require.Error(t, err)
	})
}

func TestRenameSubstitutions_TokenAware(t *testing.T) {
	subs := renameSubstitutions("Total", "Amount")
	require.NotEmpty(t, subs)
	// \bTotal\b should NOT match inside TotalCount.
	in := []byte("Total TotalCount totalize")
	out := applyRenameRules(in, subs)
	require.Contains(t, string(out), "Amount TotalCount")
	require.NotContains(t, string(out), "AmountCount",
		"token-aware rename must not rewrite TotalCount → AmountCount")
}

func TestRenameTargets_StandardLayout(t *testing.T) {
	got := renameTargets("Order")
	require.Equal(t, 5, len(got))
	require.Contains(t, got, filepath.Join("app", "models", "order.model.go"))
	require.Contains(t, got, filepath.Join("app", "dtos", "order.dtos.go"))
	require.Contains(t, got, filepath.Join("app", "services", "order.service.go"))
}
