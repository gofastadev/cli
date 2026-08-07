package commands

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/commands/ai"
	"github.com/gofastadev/cli/internal/docs"
	"github.com/gofastadev/cli/internal/generate"
	"github.com/gofastadev/cli/internal/layout"
	"github.com/gofastadev/cli/internal/skeleton"
)

var factsCmd = &cobra.Command{
	Use:   "facts",
	Short: "Emit the CLI's machine-readable facts document (commands, flags, scaffold outputs, defaults)",
	Long: `Emit one JSON document describing what this CLI build actually is: the
full command/flag tree, what ` + "`g scaffold`" + ` creates and patches, the embedded
skeleton's shape, supported drivers/layouts/field types, ` + "`do`" + ` workflows,
` + "`ai`" + ` agents, release platforms, and default endpoints.

This document is the ground truth the documentation is checked against
(` + "`make docs-check`" + ` here, the website's CI, and the generated README
sections) — and a stable API for agents and CI tooling. Field layout
changes are gated by schemaVersion.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		return runFacts()
	},
}

// factsRepo is the repository root the sync/check subcommands operate on.
// Hidden because they are maintainer tooling (invoked via make docs-sync /
// docs-check), not part of the user-facing surface.
var factsRepo string

var factsSyncCmd = &cobra.Command{
	Use:    "sync",
	Short:  "Regenerate the marker-delimited README blocks from the facts document",
	Hidden: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		return runFactsSync()
	},
}

var factsCheckCmd = &cobra.Command{
	Use:    "check",
	Short:  "Verify docs match the facts document (README blocks, inline facts, code fences, release matrix)",
	Hidden: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		return runFactsCheck()
	},
}

func init() {
	factsSyncCmd.Flags().StringVar(&factsRepo, "repo", ".", "repository root to operate on")
	factsCheckCmd.Flags().StringVar(&factsRepo, "repo", ".", "repository root to operate on")
	factsCmd.AddCommand(factsSyncCmd)
	factsCmd.AddCommand(factsCheckCmd)
	rootCmd.AddCommand(factsCmd)
}

func runFactsSync() error {
	facts, err := buildFacts()
	if err != nil {
		return err
	}
	blocks, err := docs.RenderBlocks(facts)
	if err != nil {
		return err
	}

	readmePath := filepath.Join(factsRepo, "README.md")
	src, err := os.ReadFile(readmePath)
	if err != nil {
		return err
	}
	updated, err := docs.ReplaceBlocks(string(src), blocks)
	if err != nil {
		return err
	}
	if updated == string(src) {
		cliout.Success("README.md generated blocks already in sync.")
		return nil
	}
	if err := os.WriteFile(readmePath, []byte(updated), 0o644); err != nil {
		return err
	}
	cliout.Success("README.md generated blocks rewritten.")
	return nil
}

// factsDocFiles lists the markdown files `facts check` scans for inline
// facts and gofasta invocations, relative to the repo root. Missing files
// are skipped (e.g. ../.claude/docs does not exist in CI checkouts).
func factsDocFiles() []string {
	files := []string{
		"README.md",
		"internal/skeleton/project/README.md.tmpl",
	}
	workspaceDocs, err := filepath.Glob(filepath.Join(factsRepo, "..", ".claude", "docs", "*.md"))
	if err == nil {
		for _, p := range workspaceDocs {
			if rel, err := filepath.Rel(factsRepo, p); err == nil {
				files = append(files, rel)
			}
		}
	}
	return files
}

func runFactsCheck() error {
	facts, err := buildFacts()
	if err != nil {
		return err
	}
	blocks, err := docs.RenderBlocks(facts)
	if err != nil {
		return err
	}

	var problems []string

	readme, err := os.ReadFile(filepath.Join(factsRepo, "README.md"))
	if err != nil {
		return err
	}
	for _, p := range docs.BlockMismatches(string(readme), blocks) {
		problems = append(problems, "README.md: "+p)
	}

	for _, rel := range factsDocFiles() {
		content, err := os.ReadFile(filepath.Join(factsRepo, rel))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		problems = append(problems, docs.CheckInlineFacts(rel, content, facts)...)
		problems = append(problems, docs.ValidateInvocations(facts, docs.ExtractInvocations(rel, content))...)
	}

	problems = append(problems, docs.CheckGoreleaserPlatforms(factsRepo, releaseGoos, releaseGoarch)...)
	problems = append(problems, docs.CheckLintVersionParity(factsRepo)...)

	if len(problems) > 0 {
		for _, p := range problems {
			cliout.Fail("%s", p)
		}
		return fmt.Errorf("docs-check found %d problem(s) — fix the docs (or run `make docs-sync` for generated blocks)", len(problems))
	}
	cliout.Success("docs-check green — docs match the facts document.")
	return nil
}

func runFacts() error {
	facts, err := buildFacts()
	if err != nil {
		return err
	}
	cliout.Print(facts, func(w io.Writer) {
		fprintf(w, "facts schema v%d — gofasta %s\n\n", facts.SchemaVersion, facts.CLIVersion)
		fprintf(w, "  commands:      %d top-level (%d groups)\n", len(facts.Commands), len(facts.Groups))
		fprintf(w, "  scaffold:      %d created / %d patched (REST)\n", facts.Scaffold.CreatedCount, facts.Scaffold.PatchedCount)
		fprintf(w, "  skeleton:      %d template files, %d drivers\n", facts.Skeleton.FileCount, len(facts.Skeleton.Migrations))
		fprintf(w, "  field types:   %d\n", len(facts.FieldTypes))
		fprintf(w, "  workflows:     %d\n", len(facts.Workflows))
		fprintf(w, "  ai agents:     %d\n", len(facts.AIAgents))
		fprintf(w, "\nRun with --json for the full document.\n")
	})
	return nil
}

// buildFacts assembles the facts document from the live registries. It
// re-runs the same lazy setup runExecute performs so the command tree is
// complete and grouped even when called from tests.
func buildFacts() (docs.Facts, error) {
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	assignGroups()

	groups := make([]docs.Group, 0, len(groupOrder))
	for _, id := range groupOrder {
		groups = append(groups, docs.Group{ID: id, Title: groupTitles[id]})
	}

	layeredCreated, layeredPatched := generate.ScaffoldPlan(layout.For(layout.Layered), false)
	featureCreated, featurePatched := generate.ScaffoldPlan(layout.For(layout.Feature), false)
	gqlCreated, gqlPatched := generate.ScaffoldPlan(layout.For(layout.Layered), true)

	skeletonCount, err := countFiles(skeleton.ProjectFS, "project")
	if err != nil {
		return docs.Facts{}, fmt.Errorf("counting skeleton files: %w", err)
	}
	migrations := make([]docs.DriverFileList, 0, len(supportedDrivers))
	for _, driver := range supportedDrivers {
		n, err := countFiles(skeleton.MigrationsFS, "migrations/"+driver)
		if err != nil {
			return docs.Facts{}, fmt.Errorf("counting %s migrations: %w", driver, err)
		}
		migrations = append(migrations, docs.DriverFileList{Driver: driver, Count: n})
	}

	fieldTypes := make([]docs.FieldType, 0, len(generate.SupportedFieldTypes()))
	for _, ft := range generate.SupportedFieldTypes() {
		fieldTypes = append(fieldTypes, docs.FieldType{
			Key:     ft.Key,
			Aliases: ft.Aliases,
			GoType:  ft.GoType,
			GQLType: ft.GQLType,
			SQL: docs.SQLTypes{
				Postgres:   ft.SQLTypePostgres,
				MySQL:      ft.SQLTypeMySQL,
				SQLite:     ft.SQLTypeSQLite,
				SQLServer:  ft.SQLTypeSQLServer,
				ClickHouse: ft.SQLTypeClickHouse,
			},
		})
	}

	agents := make([]docs.AIAgent, 0, len(ai.Agents))
	for _, a := range ai.Agents {
		agents = append(agents, docs.AIAgent{Key: a.Key, Name: a.Name, Description: a.Description})
	}

	return docs.Facts{
		SchemaVersion: docs.SchemaVersion,
		CLIVersion:    displayVersion(rootCmd.Version),
		Groups:        groups,
		Commands:      docs.BuildCommandFacts(rootCmd),
		GlobalFlags:   docs.GlobalFlagFacts(rootCmd),
		Scaffold: docs.Scaffold{
			Layered: docs.ScaffoldFiles{Created: layeredCreated, Patched: layeredPatched},
			Feature: docs.ScaffoldFiles{Created: featureCreated, Patched: featurePatched},
			GraphQLExtra: docs.ScaffoldFiles{
				Created: gqlCreated[len(layeredCreated):],
				Patched: gqlPatched[len(layeredPatched):],
			},
			CreatedCount: len(layeredCreated),
			PatchedCount: len(layeredPatched),
		},
		Skeleton:   docs.Skeleton{FileCount: skeletonCount, Migrations: migrations},
		Drivers:    append([]string(nil), supportedDrivers...),
		Layouts:    append([]string(nil), supportedLayouts...),
		FieldTypes: fieldTypes,
		Workflows:  workflowFacts(),
		AIAgents:   agents,
		ReleasePlatforms: docs.ReleasePlatforms{
			Goos:   append([]string(nil), releaseGoos...),
			Goarch: append([]string(nil), releaseGoarch...),
		},
		Defaults: docs.Defaults{
			ServerPort:    8080,
			DashboardPort: 9090,
			Endpoints: []docs.Endpoint{
				{Name: "apiBase", Path: "/api/v1"},
				{Name: "health", Path: "/health"},
				{Name: "healthLive", Path: "/health/live"},
				{Name: "healthReady", Path: "/health/ready"},
				{Name: "graphql", Path: "/graphql"},
				{Name: "graphqlPlayground", Path: "/graphql-playground"},
				{Name: "metrics", Path: "/metrics"},
				{Name: "swaggerUI", Path: "/swagger/index.html"},
			},
		},
	}, nil
}

// countFiles counts regular files under root in an embedded FS.
func countFiles(fsys fs.FS, root string) (int, error) {
	n := 0
	err := fs.WalkDir(fsys, root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	return n, err
}
