// Package docs holds the machinery behind `gofasta facts` and the docs
// anti-drift checks: the facts document type tree, the cobra command-tree
// walker, the README marker engine, the markdown fence parser, and the
// invocation validator. Everything here is pure logic — no cliout, no
// os.Exit — so it is testable without a cobra context.
package docs

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// SchemaVersion is bumped on any breaking change to the Facts shape.
// Consumers (the website's vendored copy, CI checks) compare it before
// trusting field layouts.
const SchemaVersion = 1

// Facts is the machine-readable ground truth the CLI publishes about
// itself. JSON tags are stable API — agents, CI checks, and the website's
// vendored copy consume this document, so renaming a field is a breaking
// change gated by SchemaVersion. No map types anywhere: ordered slices
// keep serialization deterministic.
type Facts struct {
	SchemaVersion    int              `json:"schemaVersion"`
	CLIVersion       string           `json:"cliVersion"`
	Groups           []Group          `json:"groups"`
	Commands         []Command        `json:"commands"`
	GlobalFlags      []Flag           `json:"globalFlags"`
	Scaffold         Scaffold         `json:"scaffold"`
	Skeleton         Skeleton         `json:"skeleton"`
	Drivers          []string         `json:"drivers"`
	Layouts          []string         `json:"layouts"`
	FieldTypes       []FieldType      `json:"fieldTypes"`
	Workflows        []Workflow       `json:"workflows"`
	AIAgents         []AIAgent        `json:"aiAgents"`
	ReleasePlatforms ReleasePlatforms `json:"releasePlatforms"`
	Defaults         Defaults         `json:"defaults"`
}

// Group is one help-output command group.
type Group struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Command is one node of the command tree.
type Command struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"` // full invocation, e.g. "gofasta generate scaffold"
	Aliases  []string  `json:"aliases,omitempty"`
	Group    string    `json:"group,omitempty"` // top-level commands only
	Short    string    `json:"short"`
	Flags    []Flag    `json:"flags,omitempty"`
	Children []Command `json:"children,omitempty"`
}

// Flag describes one flag of one command.
type Flag struct {
	Name       string `json:"name"`
	Shorthand  string `json:"shorthand,omitempty"`
	Type       string `json:"type"`
	Default    string `json:"default"`
	Usage      string `json:"usage"`
	Persistent bool   `json:"persistent"` // inherited by subcommands
}

// Scaffold describes what `gofasta g scaffold` produces.
type Scaffold struct {
	Layered      ScaffoldFiles `json:"layered"`
	Feature      ScaffoldFiles `json:"feature"`
	GraphQLExtra ScaffoldFiles `json:"graphqlExtra"` // additional files with --graphql (layered paths)
	CreatedCount int           `json:"createdCount"` // REST-only created files
	PatchedCount int           `json:"patchedCount"` // REST-only patched files
}

// ScaffoldFiles is one layout's created/patched path lists. Paths carry
// {resource} / {resources} placeholders for the snake-cased resource name.
type ScaffoldFiles struct {
	Created []string `json:"created"`
	Patched []string `json:"patched"`
}

// Skeleton describes the embedded project template.
type Skeleton struct {
	FileCount  int              `json:"fileCount"`
	Migrations []DriverFileList `json:"migrations"`
}

// DriverFileList is the per-driver foundational migration count.
type DriverFileList struct {
	Driver string `json:"driver"`
	Count  int    `json:"count"`
}

// FieldType is one supported "name:type" field kind.
type FieldType struct {
	Key     string   `json:"key"`
	Aliases []string `json:"aliases,omitempty"`
	GoType  string   `json:"goType"`
	GQLType string   `json:"gqlType"`
	SQL     SQLTypes `json:"sql"`
}

// SQLTypes is the per-driver column type for one field kind.
type SQLTypes struct {
	Postgres   string `json:"postgres"`
	MySQL      string `json:"mysql"`
	SQLite     string `json:"sqlite"`
	SQLServer  string `json:"sqlserver"`
	ClickHouse string `json:"clickhouse"`
}

// Workflow is one `gofasta do` chain.
type Workflow struct {
	Key         string   `json:"key"`
	Description string   `json:"description"`
	Args        string   `json:"args,omitempty"`
	Steps       []string `json:"steps"`
}

// AIAgent is one `gofasta ai` target.
type AIAgent struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ReleasePlatforms is the goreleaser build matrix, mirrored as a build-time
// constant and asserted against .goreleaser.yaml by `facts check`.
type ReleasePlatforms struct {
	Goos   []string `json:"goos"`
	Goarch []string `json:"goarch"`
}

// Defaults captures the scaffold's default runtime surface.
type Defaults struct {
	ServerPort    int        `json:"serverPort"`
	DashboardPort int        `json:"dashboardPort"`
	Endpoints     []Endpoint `json:"endpoints"`
}

// Endpoint is one named default HTTP path.
type Endpoint struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// BuildCommandFacts walks a cobra command tree into the facts shape.
// Hidden commands and cobra's implicit help topics are excluded. Callers
// must have run the same lazy setup Execute performs (InitDefaultHelpCmd,
// InitDefaultCompletionCmd, group assignment) or GroupID will be empty.
func BuildCommandFacts(root *cobra.Command) []Command {
	return childCommands(root, true)
}

func childCommands(parent *cobra.Command, topLevel bool) []Command {
	var out []Command
	for _, c := range parent.Commands() {
		if c.Hidden || c.Name() == "help" {
			continue
		}
		cmd := Command{
			Name:    c.Name(),
			Path:    c.CommandPath(),
			Aliases: c.Aliases,
			Short:   c.Short,
			Flags:   commandFlags(c),
		}
		if topLevel {
			cmd.Group = c.GroupID
		}
		cmd.Children = childCommands(c, false)
		out = append(out, cmd)
	}
	return out
}

// commandFlags lists the flags declared on this command itself (local +
// its own persistent set). The root's persistent flags surface once as
// Facts.GlobalFlags rather than on every node.
func commandFlags(c *cobra.Command) []Flag {
	var out []Flag
	persistent := map[string]bool{}
	c.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		persistent[f.Name] = true
	})
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" {
			return
		}
		out = append(out, Flag{
			Name:       f.Name,
			Shorthand:  f.Shorthand,
			Type:       f.Value.Type(),
			Default:    f.DefValue,
			Usage:      flagUsageOneLine(f.Usage),
			Persistent: persistent[f.Name],
		})
	})
	return out
}

// GlobalFlagFacts lists the root command's persistent flags.
func GlobalFlagFacts(root *cobra.Command) []Flag {
	var out []Flag
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		out = append(out, Flag{
			Name:       f.Name,
			Shorthand:  f.Shorthand,
			Type:       f.Value.Type(),
			Default:    f.DefValue,
			Usage:      flagUsageOneLine(f.Usage),
			Persistent: true,
		})
	})
	return out
}

// flagUsageOneLine collapses multi-line flag usage strings so the JSON
// document stays one value per line when pretty-printed.
func flagUsageOneLine(u string) string {
	return strings.Join(strings.Fields(u), " ")
}
