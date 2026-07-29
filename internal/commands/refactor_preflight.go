// Package commands — refactor eligibility preflight.
//
// The layout migrations assume a scaffold-shaped project. On projects
// that deviate they degrade either loudly (the final go-build gate
// aborts mid-flight, leaving a half-rewritten tree) or silently (an
// anchored rewrite no-ops — e.g. a hand-edited gqlgen.yml keeps its
// stale autobind and the NEXT `make gqlgen` regenerates into the wrong
// paths, which the build gate cannot catch).
//
// refactorPreflight traverses the whole project tree BEFORE any write
// and classifies findings:
//
//   - blockers — states where proceeding is provably destructive or
//     silently corrupting. The orchestrators refuse, with no override
//     (--force keeps meaning only "skip the dirty-tree check"; the one
//     exception is the no-git blocker, which --force downgrades because
//     it is a missing safety net rather than a broken project).
//   - warnings — the migration proceeds, but the developer is told
//     exactly which files/symbols fall outside the scaffold surface
//     and will be left behind or not rewritten.
//
// The same report is surfaced by `gofasta refactor status` (eligibility
// section) and `gofasta doctor` (project health), so a developer can
// check eligibility without attempting a migration.

package commands

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gofastadev/cli/internal/featurize"
	"github.com/gofastadev/cli/internal/generate"
)

// preflightIssue is one finding. Check is a stable machine key so
// --json consumers can branch without parsing messages.
type preflightIssue struct {
	Severity string `json:"severity"` // "blocker" | "warning"
	Check    string `json:"check"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
}

// preflightReport groups the findings of one preflight run.
type preflightReport struct {
	Blockers []preflightIssue `json:"blockers,omitempty"`
	Warnings []preflightIssue `json:"warnings,omitempty"`
}

func (r *preflightReport) block(check, path, msg string) {
	r.Blockers = append(r.Blockers, preflightIssue{Severity: "blocker", Check: check, Path: path, Message: msg})
}

func (r *preflightReport) warn(check, path, msg string) {
	r.Warnings = append(r.Warnings, preflightIssue{Severity: "warning", Check: check, Path: path, Message: msg})
}

// eligible reports whether the migration may proceed.
func (r *preflightReport) eligible() bool { return len(r.Blockers) == 0 }

// downgradeNoGit moves the no-git blocker (if present) into the
// warnings — the --force escape hatch. Every other blocker stays.
func (r *preflightReport) downgradeNoGit() {
	kept := r.Blockers[:0]
	for _, b := range r.Blockers {
		if b.Check == "no-git" {
			b.Severity = "warning"
			b.Message += " (proceeding under --force)"
			r.Warnings = append(r.Warnings, b)
			continue
		}
		kept = append(kept, b)
	}
	r.Blockers = kept
}

// Directions. The string doubles as the migration target's name.
const (
	preflightToFeature = "feature"
	preflightToLayered = "layered"
)

// refactorPreflight traverses the project and returns every blocker and
// warning for migrating in the given direction. Read-only; every check
// tolerates missing files and directories.
//
//nolint:gocognit,gocyclo // linear check pipeline: state → git → tree walk → symbols → gqlgen → markers. Splitting hides the order the report is assembled in.
func refactorPreflight(direction string) preflightReport {
	var report preflightReport

	// Resource discovery from models: app/models/ never moves, so the
	// model files are the complete resource list in BOTH layouts.
	resources, err := discoverResourcesFromModels()
	if err != nil {
		// No models at all — the orchestrators' own precondition
		// checks produce the user-facing error; nothing to scan here.
		return report
	}

	checkTornResources(&report, direction, resources)
	checkGitRepo(&report)
	managed := buildManagedSet(direction, resources)
	walkProjectTree(&report, managed)
	checkRenamedSymbols(&report, direction, resources)
	checkGqlgen(&report, direction)
	checkGeneratorMarkers(&report)

	return report
}

// checkTornResources detects the one provably broken layout state: the
// SAME resource having files on both the layered and the feature side
// (an aborted or hand-moved migration). A resource fully on either
// side is legal — the documented per-resource workflow migrates one
// resource at a time — so mixed projects only warn about resources
// that already sit on the target side (they will be skipped).
func checkTornResources(report *preflightReport, direction string, resources []featurize.Resource) {
	for _, r := range resources {
		layered, feature := 0, 0
		for _, pair := range featurize.PerResourceMapping(r.Snake) {
			if fileExistsPreflight(pair.Layered) {
				layered++
			}
			if fileExistsPreflight(pair.Feature) {
				feature++
			}
		}
		switch {
		case layered > 0 && feature > 0:
			report.block("layout-state", "app/"+r.Snake,
				fmt.Sprintf("%s exists in BOTH layouts (%d layered file(s), %d feature file(s)) — a previous migration aborted mid-resource or files were moved by hand; `git restore .` or reconcile manually before migrating", r.Name, layered, feature))
		case direction == preflightToFeature && feature > 0:
			report.warn("layout-state", "app/"+r.Snake,
				r.Name+" is already in feature layout — it will be skipped")
		case direction == preflightToLayered && layered > 0:
			report.warn("layout-state", "app/"+r.Snake,
				r.Name+" is already in layered layout — it will be skipped")
		}
	}
}

// checkGitRepo emits the no-git blocker. The migration's entire
// recovery contract is "partial state + git restore"; without a repo
// there is no undo. The orchestrators downgrade this one blocker to a
// warning under --force.
func checkGitRepo(report *preflightReport) {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	if err := cmd.Run(); err != nil {
		report.block("no-git", "",
			"not a git repository — an aborted migration cannot be undone; `git init && git add -A && git commit -m checkpoint` first, or pass --force to accept the risk")
	}
}

// managedSet classifies the paths the migration touches.
type managedSet struct {
	// transformed: files the engine parses and rewrites (or moves with
	// rewriting). An unparseable file here is a blocker — the AST
	// transform would abort mid-flight anyway.
	transformed map[string]bool
	// movedOnly: files relocated byte-for-byte (never parsed).
	movedOnly map[string]bool
	// allowedDirs: directories whose contents the migration never
	// touches — legal in any state.
	allowedDirs []string
	// drainedDirs: directories the migration empties (or moves out
	// of). Unmanaged files here survive the migration and keep the
	// old package alive — worth a warning each.
	drainedDirs []string
}

// buildManagedSet enumerates every path the migration would touch for
// the given direction, plus the directories it drains.
func buildManagedSet(direction string, resources []featurize.Resource) managedSet {
	m := managedSet{
		transformed: map[string]bool{
			"app/di/container.go":               true,
			"app/di/wire.go":                    true,
			"app/rest/routes/index.routes.go":   true,
			"app/di/providers/core.go":          true,
			"app/validators/app_validator.go":   true,
			"app/rest/controllers/validator.go": true,
		},
		movedOnly: map[string]bool{},
		allowedDirs: []string{
			"app/models", "app/validators", "app/jobs", "app/tasks",
			"app/devtools", "app/shared", "app/graphql/schema",
		},
	}

	// GraphQL: every resolver .go file is transformed in place.
	for _, f := range discoverGraphQLResolverFiles() {
		m.transformed[f] = true
	}
	// Generated files that are deleted/regenerated, never parsed.
	m.movedOnly["app/generated.go"] = true
	m.movedOnly["app/di/wire_gen.go"] = true
	// app/di/providers/graphql.go survives untouched (its resolver
	// import is layout-independent).
	m.movedOnly["app/di/providers/graphql.go"] = true

	for _, pair := range featurize.SharedRelocations() {
		m.movedOnly[pair.Layered] = true
		m.movedOnly[pair.Feature] = true
	}

	for _, r := range resources {
		for _, pair := range featurize.PerResourceMapping(r.Snake) {
			if direction == preflightToFeature {
				m.transformed[pair.Layered] = true
			} else {
				m.transformed[pair.Feature] = true
			}
		}
		m.transformed["testutil/mocks/"+r.Snake+"_repository_mock.go"] = true
		m.transformed["testutil/mocks/"+r.Snake+"_service_mock.go"] = true
	}
	// password_generator.go follows the User feature.
	m.transformed["app/services/password_generator.go"] = true
	m.transformed["app/user/password_generator.go"] = true

	if direction == preflightToFeature {
		m.drainedDirs = []string{
			"app/services", "app/services/interfaces",
			"app/repositories", "app/repositories/interfaces",
			"app/dtos", "app/rest/controllers", "app/rest/routes",
			"app/di/providers",
		}
	} else {
		for _, r := range resources {
			m.drainedDirs = append(m.drainedDirs, "app/"+r.Snake)
		}
	}
	return m
}

// walkProjectTree parses every .go file under app/ and testutil/mocks/
// and classifies it against the managed set:
//
//   - transformed + unparseable  → blocker (the migration would abort)
//   - unmanaged + in drained dir → warning (left behind)
//   - anything else unparseable  → warning (won't stop the migration)
func walkProjectTree(report *preflightReport, m managedSet) {
	fset := token.NewFileSet()
	roots := []string{"app", filepath.Join("testutil", "mocks")}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil //nolint:nilerr // missing roots and unreadable entries are legal project states
			}
			path = filepath.ToSlash(path)

			parseOK := true
			if _, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution); perr != nil {
				parseOK = false
			}

			switch {
			case m.transformed[path]:
				if !parseOK {
					report.block("parse-error", path,
						"file does not parse as Go, and the migration must rewrite it — fix the syntax error first")
				}
			case m.movedOnly[path] || underAnyDir(path, m.allowedDirs):
				// Never rewritten; parse state is the project's business.
			case underAnyDir(path, m.drainedDirs):
				report.warn("unmanaged-file", path,
					"not generated by the scaffold — it will stay behind and keep its package alive; verify the project still compiles after migration")
			default:
				if !parseOK {
					report.warn("parse-error", path,
						"file does not parse as Go (the migration does not touch it)")
				}
			}
			return nil
		})
	}
}

// checkRenamedSymbols parses each resource's managed source files and
// diffs their exported top-level declarations against the scaffold
// surface the migration engines recognize. Renamed types are the
// reverse direction's most common silent breakage: references to the
// new name are simply not rewritten.
func checkRenamedSymbols(report *preflightReport, direction string, resources []featurize.Resource) {
	fset := token.NewFileSet()
	for _, r := range resources {
		expected := featurize.ExpectedResourceSymbols(r)
		for _, pair := range featurize.PerResourceMapping(r.Snake) {
			path := pair.Layered
			if direction == preflightToLayered {
				path = pair.Feature
			}
			if strings.HasSuffix(path, "_test.go") || !fileExistsPreflight(path) {
				continue
			}
			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				continue // reported by walkProjectTree
			}
			exported, recognized := exportedTopLevelNames(file, expected)
			if len(exported) > 0 && recognized == 0 {
				report.warn("renamed-symbols", path,
					fmt.Sprintf("no recognized scaffold symbols for %s — its types appear renamed; the migration will not rewrite references to the new names", r.Name))
				continue
			}
			if len(exported) > 0 {
				sort.Strings(exported)
				report.warn("renamed-symbols", path,
					fmt.Sprintf("exported symbols outside the scaffold surface (%s) — references to them are not re-qualified by the reverse migration", strings.Join(exported, ", ")))
			}
		}
	}
}

// exportedTopLevelNames returns the exported top-level declaration
// names NOT in expected, and the count of names that were recognized.
func exportedTopLevelNames(file *ast.File, expected map[string]bool) (unknown []string, recognized int) {
	record := func(name string) {
		if !ast.IsExported(name) {
			return
		}
		if expected[name] {
			recognized++
			return
		}
		unknown = append(unknown, name)
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil { // methods belong to their type's fate
				record(d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					record(s.Name.Name)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						record(n.Name)
					}
				}
			}
		}
	}
	return unknown, recognized
}

// checkGqlgen validates gqlgen.yml against the anchor lines the
// migration's rewrite targets. Missing anchors are a blocker: the
// rewrite would silently no-op and the next `gqlgen generate` would
// emit code into the pre-migration paths — a failure the go-build gate
// cannot catch. Non-scaffold customizations beyond the anchors only
// warn.
func checkGqlgen(report *preflightReport, direction string) {
	content, err := os.ReadFile("gqlgen.yml")
	if err != nil {
		return // REST-only project
	}
	mod, merr := readModulePath()
	s := string(content)

	wantFilename := "filename: app/dtos/generated-types.dtos.go"
	wantAutobind := `- "` + mod + `/app/dtos"`
	if direction == preflightToLayered {
		wantFilename = "filename: app/shared/dtos/generated-types.dtos.go"
		wantAutobind = `- "` + mod + `/app/shared/dtos"`
	}
	if !strings.Contains(s, wantFilename) {
		report.block("gqlgen-anchors", "gqlgen.yml",
			"model.filename does not match the scaffold shape ("+wantFilename+") — the migration cannot rewrite gqlgen.yml safely; restore the line or migrate the file manually first")
	}
	if merr == nil && !containsTrimmedLine(s, wantAutobind) {
		report.block("gqlgen-anchors", "gqlgen.yml",
			"autobind is missing the scaffold entry "+wantAutobind+" — the migration cannot rewrite gqlgen.yml safely; restore the entry or migrate the file manually first")
	}

	// Customizations the rewrite deliberately does not cover.
	if containsTrimmedLinePrefix(s, "federation:") {
		report.warn("gqlgen-custom", "gqlgen.yml",
			"federation is enabled — the migration's gqlgen.yml rewrite covers only the scaffold shape; review the file after migrating")
	}
	if !strings.Contains(s, "dir: app/graphql/resolvers") {
		report.warn("gqlgen-custom", "gqlgen.yml",
			"resolver.dir deviates from app/graphql/resolvers — resolver files outside that directory are not rewritten")
	}
	if !strings.Contains(s, `filename_template: "{name}.resolvers.go"`) {
		report.warn("gqlgen-custom", "gqlgen.yml",
			`resolver.filename_template deviates from "{name}.resolvers.go" — per-resource resolver discovery may miss files`)
	}

	report.warn("generated-hand-edits", "",
		"app/generated.go, app/di/wire_gen.go and the generated models file are regenerated by the migration — local edits to generated files are lost")
}

// checkGeneratorMarkers warns when a `gofasta g scaffold` anchor marker
// was removed. The refactor itself succeeds without them (its
// transforms are AST-anchored), but every future generation on the
// migrated project would fail to patch that file.
func checkGeneratorMarkers(report *preflightReport) {
	for path, markers := range generate.GeneratorMarkers() {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, marker := range markers {
			if !strings.Contains(string(content), marker) {
				report.warn("generator-markers", path,
					"missing the `"+marker+"` marker — the migration succeeds, but `gofasta g scaffold` can no longer patch this file; restore the marker comment")
			}
		}
	}
}

// ---------- small helpers ----------

func fileExistsPreflight(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func underAnyDir(path string, dirs []string) bool {
	for _, d := range dirs {
		if strings.HasPrefix(path, d+"/") {
			return true
		}
	}
	return false
}

// containsTrimmedLine reports whether any line of s equals want after
// trimming whitespace.
func containsTrimmedLine(s, want string) bool {
	for line := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// containsTrimmedLinePrefix reports whether any line of s starts with
// prefix after trimming leading whitespace (comment lines excluded).
func containsTrimmedLinePrefix(s, prefix string) bool {
	for line := range strings.SplitSeq(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) && !strings.HasPrefix(trimmed, "#") {
			return true
		}
	}
	return false
}
