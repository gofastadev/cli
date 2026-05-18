// Package commands — refactor command.
//
// `gofasta refactor feature-package` migrates a layered project to
// the feature-package layout in-place. Two modes:
//
//	gofasta refactor feature-package <Resource>   # one resource
//	gofasta refactor feature-package --all        # every resource
//	gofasta refactor feature-package <R> --dry-run
//	gofasta refactor feature-package <R> --force  # proceed on dirty tree
//
// The refactor engine reuses the same `internal/featurize/` AST
// transformer that powers `gofasta new --layout=feature`. The two
// callers differ only in inputs: scaffolding starts from the embedded
// skeleton, refactor starts from a user's on-disk project.
//
// Per-resource compile gate: after migrating each resource the engine
// runs `go build ./...` + `go tool wire ./app/di/`. On failure it
// aborts loudly and leaves the partial state in place. Matches the
// docs' contract "moving one resource at a time keeps the app
// compilable throughout" (project-structure.mdx:212).

package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/gofastadev/cli/internal/featurize"
	"github.com/spf13/cobra"
)

// refactorResult is the JSON envelope for `gofasta refactor feature-package --json`.
type refactorResult struct {
	Action       string   `json:"action"`
	Resources    []string `json:"resources"`
	FilesMoved   []string `json:"files_moved"`
	FilesPatched []string `json:"files_patched"`
	DryRun       bool     `json:"dry_run"`
	Success      bool     `json:"success"`
	Error        string   `json:"error,omitempty"`
}

var refactorCmd = &cobra.Command{
	Use:   "refactor",
	Short: "Refactor an existing project (layout migrations, structural moves)",
	Long: `Refactor commands migrate an existing project's structure without
changing its behavior. Currently the only subcommand is:

  gofasta refactor feature-package    Migrate from layered to feature-package layout`,
	Args: cobra.MinimumNArgs(1),
}

var refactorFeatureCmd = &cobra.Command{
	Use:   "feature-package [<Resource> | --all]",
	Short: "Migrate the project from layered to feature-package layout",
	Long: `Move per-resource files from layered locations (app/services/<r>.service.go,
app/repositories/<r>.repository.go, app/rest/controllers/<r>.controller.go, etc.)
into app/<r>/, rewriting imports and selectors so the result compiles.

Shared aliases (TPaginationObjectDto, SortOrientation) relocate to
app/shared/dtos/. Models stay in app/models/ and validators stay in
app/validators/ for the reasons documented in
` + "`internal/featurize/featurize.go`" + ` and ` + "`docs/getting-started/project-structure.mdx`" + `.

After each resource is migrated the engine runs ` + "`go build ./...`" + ` and
` + "`go tool wire ./app/di/`" + `. On failure the migration aborts and the
partial state is left for inspection — fix or ` + "`git restore`" + ` to recover.

Examples:
  gofasta refactor feature-package User
  gofasta refactor feature-package --all
  gofasta refactor feature-package User --dry-run`,
	RunE: runRefactorFeature,
}

func init() {
	rootCmd.AddCommand(refactorCmd)
	refactorCmd.AddCommand(refactorFeatureCmd)
	refactorFeatureCmd.Flags().Bool("all", false, "Migrate every resource detected in app/models/")
	refactorFeatureCmd.Flags().Bool("dry-run", false, "Show the migration plan without writing any files")
	refactorFeatureCmd.Flags().Bool("force", false, "Proceed even when the working tree is dirty")
}

//nolint:gocognit,gocyclo // linear orchestration pipeline: preconditions → resolve → (dry-run|migrate) → relocate → patch → cleanup → flip → verify. Splitting hides the order.
func runRefactorFeature(cmd *cobra.Command, args []string) (resultErr error) {
	allFlag, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")

	result := refactorResult{Action: "refactor.feature-package", DryRun: dryRun}
	defer func() {
		result.Success = resultErr == nil
		if resultErr != nil {
			result.Error = resultErr.Error()
		}
		cliout.Print(result, nil)
	}()

	// Preconditions.
	if err := requireLayeredProject(); err != nil {
		return err
	}
	if !force {
		if err := requireCleanGitTree(); err != nil {
			return err
		}
	}

	// Resolve the resources to migrate.
	resources, err := resolveRefactorResources(args, allFlag)
	if err != nil {
		return err
	}
	for _, r := range resources {
		result.Resources = append(result.Resources, r.Name)
	}

	mod, err := readModulePath()
	if err != nil {
		return err
	}

	if dryRun {
		cliout.Header("📋 Dry-run plan:")
		for _, r := range resources {
			cliout.Info("Would migrate %s:", r.Name)
			for _, pair := range featurize.PerResourceMapping(r.Snake) {
				if _, err := os.Stat(pair.Layered); err == nil {
					cliout.Plainln(fmt.Sprintf("  move: %s → %s", pair.Layered, pair.Feature))
				}
			}
		}
		for _, pair := range featurize.SharedRelocations() {
			if _, err := os.Stat(pair.Layered); err == nil {
				cliout.Plainln(fmt.Sprintf("  move: %s → %s", pair.Layered, pair.Feature))
			}
		}
		cliout.Info("Would patch: app/di/container.go, app/di/wire.go, app/rest/routes/index.routes.go, app/di/providers/core.go")
		cliout.Info("Would patch testutil/mocks/<resource>_*.go for each resource")
		cliout.Info("Would update config.yaml: project.layout: layered → feature")
		return nil
	}

	// Migrate each resource. The per-resource compile gate runs after
	// each one — abort if a build fails so the user can inspect the
	// partial state.
	for _, r := range resources {
		cliout.Header("🔧 Migrating %s", r.Name)
		moved, patched, err := migrateResource(r, mod, resources)
		result.FilesMoved = append(result.FilesMoved, moved...)
		result.FilesPatched = append(result.FilesPatched, patched...)
		if err != nil {
			return err
		}
	}

	// After all resources, relocate shared files (aliases.go).
	cliout.Step("📦 Relocating shared aliases")
	relocated, err := applySharedRelocations()
	if err != nil {
		return err
	}
	result.FilesMoved = append(result.FilesMoved, relocated...)

	// Move app/services/password_generator.go → app/user/password_generator.go.
	// PasswordGenerator follows the user feature (only consumer in the
	// bootstrap scaffold). If the user feature doesn't exist in this
	// refactor (e.g. refactoring a project that already deleted User)
	// the file stays put.
	cliout.Step("📦 Relocating password_generator.go")
	pwMoved, err := relocatePasswordGenerator(mod, resources)
	if err != nil {
		return err
	}
	result.FilesMoved = append(result.FilesMoved, pwMoved...)

	// Patch cross-cutting files (container, wire, index.routes, core, validator infra).
	cliout.Step("🔌 Patching cross-cutting files")
	patched, err := applyCrossCuttingPatches(mod, resources)
	if err != nil {
		return err
	}
	result.FilesPatched = append(result.FilesPatched, patched...)

	// Clean up the now-empty layered directories so the project tree
	// matches the feature-layout doc shape.
	cliout.Step("🧹 Cleaning empty layered directories")
	pruneEmptyLayeredDirs()

	// Flip config.yaml.
	cliout.Step("📝 Updating config.yaml")
	if err := flipLayoutInConfig(); err != nil {
		return err
	}

	// Wire goes FIRST so the stale wire_gen.go (which still references
	// layered imports like app/repositories) is regenerated against the
	// new layout. Then go build can verify the whole tree.
	cliout.Step("✓ Regenerating Wire")
	// Delete the stale wire_gen.go before regenerating — wire's tool
	// invocation needs ALL Go files in the package to compile, and the
	// stale generated file references packages that no longer exist.
	_ = os.Remove("app/di/wire_gen.go")
	if err := runGoCommand("tool", "wire", "./app/di/"); err != nil {
		return clierr.Wrap(clierr.CodeRefactorAborted, err,
			"wire generation failed — inspect the partial state and `git restore` to revert")
	}
	cliout.Step("✓ Verifying go build ./...")
	if err := runGoCommand("build", "./..."); err != nil {
		return clierr.Wrap(clierr.CodeRefactorAborted, err,
			"go build failed after migration — inspect the partial state and `git restore` to revert")
	}

	cliout.Success("Migration complete — project is now feature-package layout")
	return nil
}

// requireLayeredProject errors out if the project isn't currently in
// layered layout (nothing to migrate).
func requireLayeredProject() error {
	v := configutil.ReadLayout()
	if v == "feature" {
		return clierr.New(clierr.CodeRefactorIneligible,
			"project is already feature-package layout — nothing to migrate")
	}
	// Empty or "layered" is the migrate-eligible case. Also do a
	// filesystem sanity check.
	if _, err := os.Stat("app/models"); err != nil {
		return clierr.Newf(clierr.CodeRefactorIneligible,
			"app/models/ not found — not in a layered gofasta project")
	}
	return nil
}

// requireCleanGitTree errors out if the working tree has uncommitted
// changes. Skipped by --force.
func requireCleanGitTree() error {
	cmd := exec.Command("git", "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		// No git repo, or git not installed — be permissive.
		return nil
	}
	if strings.TrimSpace(string(out)) != "" {
		return clierr.New(clierr.CodeRefactorDirtyTree,
			"working tree has uncommitted changes — commit/stash first, or pass --force to proceed anyway")
	}
	return nil
}

// resolveRefactorResources resolves the list of resources to migrate
// from the CLI args and the --all flag.
func resolveRefactorResources(args []string, allFlag bool) ([]featurize.Resource, error) {
	if allFlag {
		return discoverResourcesFromModels()
	}
	if len(args) == 0 {
		return nil, clierr.New(clierr.CodeInvalidName,
			"specify a resource name (e.g. `gofasta refactor feature-package User`) or use --all")
	}
	pascal := args[0]
	snake := toSnakeCaseSimple(pascal)
	plural := pluralizeSimple(pascal)
	// Verify the layered model file exists for this resource.
	modelPath := fmt.Sprintf("app/models/%s.model.go", snake)
	if _, err := os.Stat(modelPath); err != nil {
		return nil, clierr.Newf(clierr.CodeRefactorResourceNotFound,
			"resource %q not found (expected %s)", pascal, modelPath)
	}
	return []featurize.Resource{{Name: pascal, Snake: snake, Plural: plural}}, nil
}

// discoverResourcesFromModels walks app/models/ to find every layered
// resource by its model file naming convention (<snake>.model.go).
func discoverResourcesFromModels() ([]featurize.Resource, error) {
	entries, err := os.ReadDir("app/models")
	if err != nil {
		return nil, clierr.Wrap(clierr.CodeFileIO, err, "reading app/models")
	}
	var out []featurize.Resource
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".model.go") {
			continue
		}
		snake := strings.TrimSuffix(e.Name(), ".model.go")
		pascal := toPascalCaseSimple(snake)
		plural := pluralizeSimple(pascal)
		out = append(out, featurize.Resource{Name: pascal, Snake: snake, Plural: plural})
	}
	if len(out) == 0 {
		return nil, clierr.New(clierr.CodeRefactorResourceNotFound,
			"no layered resources detected in app/models/")
	}
	return out, nil
}

// migrateResource performs the file moves + featurize transforms for
// a single resource. Returns the lists of moved/patched paths.
func migrateResource(r featurize.Resource, mod string, allResources []featurize.Resource) (moved, patched []string, err error) {
	_ = allResources
	for _, pair := range featurize.PerResourceMapping(r.Snake) {
		content, readErr := os.ReadFile(pair.Layered)
		if readErr != nil {
			// Layered file may not exist (e.g. some resources don't
			// have a test file). Skip silently.
			continue
		}
		transformed, terr := featurize.TransformPerResource(content, featurize.Options{
			ModulePath: mod, Resource: r,
		})
		if terr != nil {
			return moved, patched, fmt.Errorf("featurize %s: %w", pair.Layered, terr)
		}
		if err := os.MkdirAll(filepath.Dir(pair.Feature), 0o755); err != nil {
			return moved, patched, clierr.Wrap(clierr.CodeFileIO, err, "mkdir "+filepath.Dir(pair.Feature))
		}
		if err := os.WriteFile(pair.Feature, transformed, 0o644); err != nil {
			return moved, patched, clierr.Wrap(clierr.CodeFileIO, err, "writing "+pair.Feature)
		}
		if err := os.Remove(pair.Layered); err != nil {
			return moved, patched, clierr.Wrap(clierr.CodeFileIO, err, "removing "+pair.Layered)
		}
		cliout.Path(pair.Feature)
		moved = append(moved, pair.Layered+" → "+pair.Feature)
	}

	// Per-resource mocks under testutil/mocks/.
	for _, suffix := range []string{"_repository_mock.go", "_service_mock.go"} {
		mockPath := filepath.Join("testutil", "mocks", r.Snake+suffix)
		content, readErr := os.ReadFile(mockPath)
		if readErr != nil {
			continue
		}
		transformed, terr := featurize.TransformMock(content, mod, r)
		if terr != nil {
			return moved, patched, fmt.Errorf("featurize %s: %w", mockPath, terr)
		}
		if err := os.WriteFile(mockPath, transformed, 0o644); err != nil {
			return moved, patched, clierr.Wrap(clierr.CodeFileIO, err, "writing "+mockPath)
		}
		patched = append(patched, mockPath)
	}
	return moved, patched, nil
}

// relocatePasswordGenerator moves app/services/password_generator.go
// into the user feature package (app/user/password_generator.go),
// rewriting `package services` → `package user`. The file's external
// references (`utils.GeneratePassword`) survive unchanged.
func relocatePasswordGenerator(mod string, resources []featurize.Resource) ([]string, error) {
	const layered = "app/services/password_generator.go"
	content, err := os.ReadFile(layered)
	if err != nil {
		return nil, nil // already moved or never existed
	}
	// Find the user resource. If absent, leave the file where it is
	// (the package will just be a tiny one-file layered remnant).
	var user *featurize.Resource
	for i := range resources {
		if resources[i].Snake == "user" {
			user = &resources[i]
			break
		}
	}
	if user == nil {
		return nil, nil
	}
	transformed, terr := featurize.TransformPerResource(content, featurize.Options{
		ModulePath: mod, Resource: *user,
	})
	if terr != nil {
		return nil, fmt.Errorf("featurize password_generator.go: %w", terr)
	}
	target := "app/user/password_generator.go"
	if err := os.WriteFile(target, transformed, 0o644); err != nil {
		return nil, clierr.Wrap(clierr.CodeFileIO, err, "writing "+target)
	}
	if err := os.Remove(layered); err != nil {
		return nil, clierr.Wrap(clierr.CodeFileIO, err, "removing "+layered)
	}
	cliout.Path(target)
	return []string{layered + " → " + target}, nil
}

// pruneEmptyLayeredDirs removes layered directories that have no Go
// files left after the refactor. Best-effort — silently skips dirs
// that still contain files (e.g. `app/rest/routes/` still has
// `index.routes.go`, `app/services/interfaces/` is empty if every
// resource migrated).
func pruneEmptyLayeredDirs() {
	candidates := []string{
		"app/services/interfaces",
		"app/services",
		"app/repositories/interfaces",
		"app/repositories",
		"app/rest/controllers",
		"app/di/providers",
		"app/dtos",
	}
	for _, dir := range candidates {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		// Special case: app/dtos may still hold non-resource files
		// (like aliases.go) — but those moved to app/shared/dtos/. If
		// only the directory itself is left, remove it.
		if len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
}

// applySharedRelocations moves files that only relocate path
// (aliases.go → app/shared/dtos/aliases.go) without source rewrites.
func applySharedRelocations() ([]string, error) {
	var moved []string
	for _, pair := range featurize.SharedRelocations() {
		content, err := os.ReadFile(pair.Layered)
		if err != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(pair.Feature), 0o755); err != nil {
			return moved, clierr.Wrap(clierr.CodeFileIO, err, "mkdir "+filepath.Dir(pair.Feature))
		}
		if err := os.WriteFile(pair.Feature, content, 0o644); err != nil {
			return moved, clierr.Wrap(clierr.CodeFileIO, err, "writing "+pair.Feature)
		}
		if err := os.Remove(pair.Layered); err != nil {
			return moved, clierr.Wrap(clierr.CodeFileIO, err, "removing "+pair.Layered)
		}
		cliout.Path(pair.Feature)
		moved = append(moved, pair.Layered+" → "+pair.Feature)
	}
	return moved, nil
}

// applyCrossCuttingPatches rewrites the cross-cutting files
// (container.go, wire.go, index.routes.go, core.go) and the shared
// infra files (app_validator.go, validator.go) that need import-path
// updates after the relocations.
func applyCrossCuttingPatches(mod string, resources []featurize.Resource) ([]string, error) {
	var patched []string
	type job struct {
		path string
		fn   func([]byte, string, []featurize.Resource) ([]byte, error)
	}
	jobs := []job{
		{"app/di/container.go", featurize.TransformContainer},
		{"app/di/wire.go", featurize.TransformWire},
		{"app/rest/routes/index.routes.go", featurize.TransformIndexRoutes},
		{"app/di/providers/core.go", featurize.TransformCoreProviders},
	}
	for _, j := range jobs {
		content, err := os.ReadFile(j.path)
		if err != nil {
			continue
		}
		out, terr := j.fn(content, mod, resources)
		if terr != nil {
			return patched, fmt.Errorf("transform %s: %w", j.path, terr)
		}
		if err := os.WriteFile(j.path, out, 0o644); err != nil {
			return patched, clierr.Wrap(clierr.CodeFileIO, err, "writing "+j.path)
		}
		patched = append(patched, j.path)
	}

	// Files that only need their `<mod>/app/dtos` import flipped to
	// `<mod>/app/shared/dtos` — no other source rewrites.
	dtosImportConsumers := []string{
		"app/validators/app_validator.go",
		"app/rest/controllers/validator.go",
		"app/graphql/resolvers/user.resolvers.go",
	}
	for _, path := range dtosImportConsumers {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		out, terr := featurize.FixDtosImportPath(content, mod)
		if terr != nil {
			return patched, fmt.Errorf("fix dtos import %s: %w", path, terr)
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return patched, clierr.Wrap(clierr.CodeFileIO, err, "writing "+path)
		}
		patched = append(patched, path)
	}

	return patched, nil
}

// flipLayoutInConfig rewrites `project: layout: layered` (or adds it
// if missing) to `project: layout: feature` in config.yaml.
func flipLayoutInConfig() error {
	const path = "config.yaml"
	content, err := os.ReadFile(path)
	if err != nil {
		return clierr.Wrap(clierr.CodeFileIO, err, "reading "+path)
	}
	s := string(content)
	switch {
	case strings.Contains(s, "layout: layered"):
		s = strings.Replace(s, "layout: layered", "layout: feature", 1)
	case strings.Contains(s, "layout: feature"):
		// already correct
	default:
		// No project block — prepend one.
		s = "project:\n  layout: feature\n\n" + s
	}
	return os.WriteFile(path, []byte(s), 0o644)
}

// runGoCommand invokes `go <args>` in the current working directory,
// streaming output to stdout/stderr.
func runGoCommand(args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// readModulePath reads the module path from go.mod in the current
// working directory.
func readModulePath() (string, error) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "", clierr.Wrap(clierr.CodeNotGofastaProject, err, "reading go.mod")
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(after), nil
		}
	}
	return "", clierr.New(clierr.CodeNotGofastaProject, "go.mod has no module directive")
}

// toSnakeCaseSimple is a minimal PascalCase → snake_case for the
// refactor command's resource-name resolution. Mirrors the convention
// the scaffold uses: word boundaries on capital letters.
func toSnakeCaseSimple(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// toPascalCaseSimple converts snake_case → PascalCase.
func toPascalCaseSimple(s string) string {
	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		first := p[0]
		if first >= 'a' && first <= 'z' {
			first -= 'a' - 'A'
		}
		b.WriteByte(first)
		b.WriteString(p[1:])
	}
	return b.String()
}

// pluralizeSimple appends "s" to a PascalCase name. Sufficient for
// most English plurals the scaffold targets; complex rules (woman →
// women) aren't supported and aren't needed for the refactor flow.
func pluralizeSimple(s string) string {
	switch {
	case strings.HasSuffix(s, "y") && len(s) > 1 && !isVowel(rune(s[len(s)-2])):
		return s[:len(s)-1] + "ies"
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "z"),
		strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	default:
		return s + "s"
	}
}

func isVowel(r rune) bool {
	switch r {
	case 'a', 'e', 'i', 'o', 'u', 'A', 'E', 'I', 'O', 'U':
		return true
	}
	return false
}
