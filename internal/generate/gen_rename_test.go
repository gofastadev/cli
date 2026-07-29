package generate

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenHelperProcess(t *testing.T) {
	if os.Getenv("GOFASTA_GEN_HELPER") != "1" {
		return
	}
	code, _ := strconv.Atoi(os.Getenv("GOFASTA_GEN_EXIT"))
	os.Exit(code)
}

// TestHelperSub is the subprocess entry point used by fakeExec.
func TestHelperSub(t *testing.T) {
	if os.Getenv("GENERATE_HELPER") != "1" {
		return
	}
	if out := os.Getenv("GENERATE_STDOUT"); out != "" {
		_, _ = os.Stdout.WriteString(out)
	}
	code, _ := strconv.Atoi(os.Getenv("GENERATE_EXIT"))
	os.Exit(code)
}

func TestCmd_HasSubcommands(t *testing.T) {
	cmds := Cmd.Commands()
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Name())
	}

	expectedCmds := []string{"scaffold", "model", "repository", "service", "controller",
		"dto", "migration", "route", "resolver", "provider", "email-template", "job", "task"}
	for _, expected := range expectedCmds {
		assert.Contains(t, names, expected, "Cmd should have subcommand: %s", expected)
	}
}

// TestGenRename_ValidationError_Missing — one of Resource/Old/New missing.
func TestGenRename_ValidationError_Missing(t *testing.T) {
	chdirTest(t, t.TempDir())
	err := GenRename(RenameData{Resource: "Order"})
	require.Error(t, err)
}

// TestGenRename_ValidationError_SameNames — old == new.
func TestGenRename_ValidationError_SameNames(t *testing.T) {
	chdirTest(t, t.TempDir())
	err := GenRename(RenameData{
		Resource: "Order", OldField: "Total", NewField: "Total",
	})
	require.Error(t, err)
}

// TestGenRename_PreviewModeNoWrites — preview mode records actions
// without touching disk; exercises the recordPatch / recordCreate
// branches.
func TestGenRename_PreviewMode(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"),
		[]byte(`package models
type Order struct{ Total int }
`), 0o644))
	chdirTest(t, tmp)

	resetPlannerState(t)
	require.NoError(t, GenRename(RenameData{
		Resource: "Order", OldField: "Total", NewField: "AmountCents",
	}))
	plan := Plan()
	require.GreaterOrEqual(t, len(plan), 1)

	// Disk untouched.
	body, _ := os.ReadFile(filepath.Join(models, "order.model.go"))
	require.Contains(t, string(body), "Total")
}

// TestGenRename_ApplyWritesToDisk — Apply=true commits the rewrites.
func TestGenRename_ApplyWritesToDisk(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"),
		[]byte(`package models
type Order struct{ Total int }
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "db", "migrations"), 0o755))
	chdirTest(t, tmp)
	require.NoError(t, GenRename(RenameData{
		Resource: "Order", OldField: "Total", NewField: "AmountCents", Apply: true,
	}))
	body, _ := os.ReadFile(filepath.Join(models, "order.model.go"))
	require.Contains(t, string(body), "AmountCents")
}

// TestGenRename_FileWithoutMatchesSkipped — file exists but doesn't
// contain the OldField; applyRenameRules returns body unchanged, the
// bytes.Equal branch fires and the loop continues without recording.
func TestGenRename_FileWithoutMatchesSkipped(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	// Model has UnrelatedField, not Total.
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"),
		[]byte("package models\ntype Order struct{ UnrelatedField int }\n"), 0o644))
	chdirTest(t, tmp)
	resetPlannerState(t)
	require.NoError(t, GenRename(RenameData{
		Resource: "Order", OldField: "Total", NewField: "AmountCents",
	}))
}

// TestGenRename_ApplyMigrationWriteError — make db/migrations a file
// (not a dir) so writeOrRecordCreate's mkdir fails.
func TestGenRename_ApplyMigrationWriteError(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "db"), 0o755))
	// Put a regular file where the migrations dir should be.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "db", "migrations"),
		[]byte("not a dir"), 0o644))
	chdirTest(t, tmp)
	err := GenRename(RenameData{
		Resource: "Order", OldField: "Total", NewField: "AmountCents", Apply: true,
	})
	require.Error(t, err)
}

// TestGenRename_ApplyWriteError — chmod target so os.WriteFile fails.
func TestGenRename_ApplyWriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod")
	}
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	modelPath := filepath.Join(models, "order.model.go")
	require.NoError(t, os.WriteFile(modelPath,
		[]byte("package models\ntype Order struct{ Total int }\n"), 0o644))
	require.NoError(t, os.Chmod(modelPath, 0o444))
	t.Cleanup(func() { _ = os.Chmod(modelPath, 0o644) })
	chdirTest(t, tmp)
	err := GenRename(RenameData{
		Resource: "Order", OldField: "Total", NewField: "AmountCents", Apply: true,
	})
	require.Error(t, err)
}

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
