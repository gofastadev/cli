package docs

import (
	"fmt"
	"strings"
)

// RenderBlocks produces every generated README block from the facts
// document, keyed by marker id.
func RenderBlocks(f Facts) (map[string]string, error) {
	scaffold, err := renderScaffoldFiles(f)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"command-index":  renderCommandIndex(f),
		"field-types":    renderFieldTypes(f),
		"scaffold-files": scaffold,
		"do-workflows":   renderWorkflows(f),
		"ai-agents":      renderAIAgents(f),
	}, nil
}

// renderCommandIndex renders the grouped `| Group | Commands |` table.
// Parents list their visible children inline, matching the hand-written
// table this replaces.
func renderCommandIndex(f Facts) string {
	byGroup := map[string][]Command{}
	for _, c := range f.Commands {
		byGroup[c.Group] = append(byGroup[c.Group], c)
	}

	var b strings.Builder
	b.WriteString("| Group | Commands |\n")
	b.WriteString("|---|---|\n")
	for _, g := range f.Groups {
		cmds := byGroup[g.ID]
		if len(cmds) == 0 {
			continue
		}
		entries := make([]string, 0, len(cmds))
		for _, c := range cmds {
			entries = append(entries, commandIndexEntry(c))
		}
		fmt.Fprintf(&b, "| %s | %s |\n", g.Title, strings.Join(entries, ", "))
	}
	return b.String()
}

func commandIndexEntry(c Command) string {
	name := "`" + c.Name + "`"
	if len(c.Aliases) > 0 {
		name += " (alias `" + strings.Join(c.Aliases, "` / `") + "`)"
	}
	if len(c.Children) == 0 {
		return name
	}
	children := make([]string, 0, len(c.Children))
	for _, ch := range c.Children {
		children = append(children, "`"+ch.Name+"`")
	}
	noun := "subcommands"
	if len(c.Children) == 1 {
		noun = "subcommand"
	}
	return fmt.Sprintf("%s (%d %s: %s)", name, len(c.Children), noun, strings.Join(children, ", "))
}

// renderFieldTypes renders the supported field-type table in the README's
// four-column shape.
func renderFieldTypes(f Facts) string {
	var b strings.Builder
	b.WriteString("| Type | Go type | SQL type (Postgres) | GraphQL type |\n")
	b.WriteString("|------|---------|-------------------|-------------|\n")
	for _, ft := range f.FieldTypes {
		name := "`" + ft.Key + "`"
		if len(ft.Aliases) > 0 {
			name += " (alias `" + strings.Join(ft.Aliases, "` / `") + "`)"
		}
		fmt.Fprintf(&b, "| %s | `%s` | `%s` | `%s` |\n", name, ft.GoType, ft.SQL.Postgres, ft.GQLType)
	}
	return b.String()
}

// scaffoldFileDescriptions carries the human column of the scaffold file
// table. Keyed by the placeholder paths ScaffoldPlan emits — when a new
// file joins the scaffold, rendering fails until a description is added
// here, which is exactly the forcing function we want.
var scaffoldFileDescriptions = map[string]string{
	"app/models/{resource}.model.go":                       "Database model with your fields",
	"db/migrations/<seq>_create_{resources}.up.sql":        "SQL to create the table",
	"db/migrations/<seq>_create_{resources}.down.sql":      "SQL to drop the table",
	"app/repositories/interfaces/{resource}_repository.go": "Repository interface (contract)",
	"app/repositories/{resource}.repository.go":            "Repository implementation (GORM queries)",
	"app/repositories/{resource}.repository_test.go":       "Repository tests",
	"app/services/interfaces/{resource}_service.go":        "Service interface (contract)",
	"app/services/{resource}.service.go":                   "Service implementation (business logic)",
	"app/services/{resource}.service_test.go":              "Service tests",
	"app/services/{resource}_errors.go":                    "Per-resource sentinel errors",
	"app/services/{resource}_inputs.go":                    "Domain input types",
	"app/services/{resource}_inputs_test.go":               "Domain input tests",
	"app/dtos/{resource}.dtos.go":                          "Request/response DTOs with validation tags",
	"app/dtos/{resource}.dtos_test.go":                     "DTO tests",
	"app/di/providers/{resource}.go":                       "Wire dependency injection provider",
	"app/rest/controllers/{resource}.controller.go":        "REST controller with CRUD handlers",
	"app/rest/controllers/{resource}.controller_test.go":   "Controller tests",
	"app/rest/routes/{resource}.routes.go":                 "Route definitions (GET, POST, PUT, DELETE)",
}

var scaffoldPatchDescriptions = map[string]string{
	"app/di/container.go":             "adds the service and controller fields",
	"app/di/wire.go":                  "adds the provider set to the Wire build",
	"app/rest/routes/index.routes.go": "registers the resource routes",
	"cmd/serve.go":                    "wires the controller into the route config",
}

func renderScaffoldFiles(f Facts) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "This single command creates **%d files** (tests included) and patches %d existing files:\n\n",
		f.Scaffold.CreatedCount, f.Scaffold.PatchedCount)
	b.WriteString("| Created file | What it is |\n")
	b.WriteString("|-------------|-----------|\n")
	for _, path := range f.Scaffold.Layered.Created {
		desc, ok := scaffoldFileDescriptions[path]
		if !ok {
			return "", fmt.Errorf("scaffold file %q has no description — add one to scaffoldFileDescriptions in internal/docs/render.go", path)
		}
		fmt.Fprintf(&b, "| `%s` | %s |\n", displayScaffoldPath(path), desc)
	}
	b.WriteString("\nIt also patches these files automatically:\n")
	for _, path := range f.Scaffold.Layered.Patched {
		desc, ok := scaffoldPatchDescriptions[path]
		if !ok {
			return "", fmt.Errorf("scaffold patch target %q has no description — add one to scaffoldPatchDescriptions in internal/docs/render.go", path)
		}
		fmt.Fprintf(&b, "- `%s` — %s\n", path, desc)
	}
	b.WriteString("\n(With `--layout feature`, the same files land in per-resource packages under `app/<resource>/` instead.)\n")
	return b.String(), nil
}

// displayScaffoldPath renders plan placeholders in the sample-resource
// style the docs use ("product").
func displayScaffoldPath(p string) string {
	p = strings.ReplaceAll(p, "{resource}", "product")
	p = strings.ReplaceAll(p, "{resources}", "products")
	p = strings.ReplaceAll(p, "<seq>", "000006")
	return p
}

// renderWorkflows renders the `gofasta do` bash block.
func renderWorkflows(f Facts) string {
	lines := make([][2]string, 0, len(f.Workflows)+1)
	for _, wf := range f.Workflows {
		invocation := "gofasta do " + wf.Key
		if wf.Args != "" {
			invocation += " " + wf.Args
		}
		lines = append(lines, [2]string{invocation, wf.Description})
	}
	lines = append(lines, [2]string{"gofasta do list", "every supported workflow"})
	return renderAlignedBashBlock(lines)
}

// renderAIAgents renders the `gofasta ai` bash block.
func renderAIAgents(f Facts) string {
	lines := make([][2]string, 0, len(f.AIAgents)+2)
	for _, a := range f.AIAgents {
		lines = append(lines, [2]string{"gofasta ai " + a.Key, a.Name + " — " + a.Description})
	}
	lines = append(lines,
		[2]string{"gofasta ai list", "supported agents"},
		[2]string{"gofasta ai status", "what's currently installed in this project"})
	return renderAlignedBashBlock(lines)
}

// renderAlignedBashBlock renders command/comment pairs as a fenced bash
// block with aligned # comments.
func renderAlignedBashBlock(lines [][2]string) string {
	width := 0
	for _, l := range lines {
		if len(l[0]) > width {
			width = len(l[0])
		}
	}
	var b strings.Builder
	b.WriteString("```bash\n")
	for _, l := range lines {
		fmt.Fprintf(&b, "%-*s   # %s\n", width, l[0], l[1])
	}
	b.WriteString("```\n")
	return b.String()
}
