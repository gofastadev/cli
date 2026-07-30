package commands

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/featurize"
	"github.com/gofastadev/cli/internal/skeleton"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

// newResult is the JSON contract for `gofasta new --json`. Captures
// the resolved paths plus the GraphQL toggle so an agent has every
// derived value at a glance — no need to rerun the command in text
// mode to learn what got created.
type newResult struct {
	Action     string `json:"action"`
	Project    string `json:"project"`
	Directory  string `json:"directory"`
	ModulePath string `json:"module_path"`
	GraphQL    bool   `json:"graphql"`
	DBDriver   string `json:"db_driver"`
	Layout     string `json:"layout"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

// ProjectData holds template variables for project generation.
type ProjectData struct {
	ProjectName      string // PascalCase or as-provided: "MyApp"
	ProjectNameLower string // lowercase: "myapp"
	ProjectNameUpper string // UPPERCASE: "MYAPP"
	ModulePath       string // Go module path: "github.com/myorg/myapp"
	GraphQL          bool   // true when --graphql flag is passed
	DBDriver         string // "postgres" | "mysql" | "sqlite" | "sqlserver" | "clickhouse"
	Layout           string // "layered" | "feature"
	JWTSecret        string // per-project random JWT signing secret
	SessionSecret    string // per-project random session store secret
}

// scaffoldGoVersion is the Go language version every generated project
// declares. It is the toolkit's stated support floor, deliberately decoupled
// from whatever toolchain the developer running `gofasta new` happens to have.
//
// Keep in sync with:
//   - internal/skeleton/project/dot-go-version
//   - the `go` directive in this repo's go.mod
const scaffoldGoVersion = "1.25.0"

// Tool dependency versions, pinned rather than tracked at @latest.
//
// Two reasons the versions are pinned:
//
//  1. Reproducibility — two developers running `gofasta new` weeks apart get
//     identical tool versions in go.mod.
//
//  2. The Go floor. A tool dependency lives in the generated project's go.mod,
//     so ITS `go` directive raises the project's. air v1.67.2 moved to
//     `go 1.26.0`, which silently bumped every new scaffold from 1.25.0 to
//     1.26.0 and broke the project's own `make lint`: golangci-lint is itself
//     a go1.25 module, so the pinned binary refuses to load a config targeting
//     a newer language version. Pinning air to v1.67.1 — the last release
//     declaring go 1.25 — keeps the floor where scaffoldGoVersion says it is.
//
// When bumping any of these, re-run `make integration`: it scaffolds a project
// and runs that project's full preflight, which is what catches a version whose
// `go` directive exceeds scaffoldGoVersion.
const (
	toolVersionGqlgen      = "v0.17.94"
	toolVersionWire        = "v0.7.0"
	toolVersionAir         = "v1.67.1" // last release declaring go 1.25 — see above
	toolVersionSwag        = "v1.16.6"
	toolVersionHTTPSwagger = "v2.0.2"
	toolVersionChi         = "v5.3.1"
	// toolVersionGofasta pins the gofasta library release scaffolds are
	// generated against — the version this CLI's templates were written
	// for and its integration suite verified. Previously @latest, which
	// meant a library release could change behavior under every new
	// scaffold before the CLI had been tested against it. Bump in
	// lockstep with library releases, then re-run `make integration`.
	toolVersionGofasta = "v0.1.10"
)

// goDirectivePattern extracts the `go` directive from a go.mod file.
var goDirectivePattern = regexp.MustCompile(`(?m)^go\s+(\S+)\s*$`)

// readGoDirective returns the `go` version declared in the go.mod at path, or
// "" when the file cannot be read or carries no directive.
func readGoDirective(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	m := goDirectivePattern.FindSubmatch(content)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// verifyGoFloor checks that installing the tool dependencies did not raise the
// generated project's Go language version above the declared floor.
//
// This is the guard for the failure mode described on the tool version block:
// `go get` silently rewrites the `go` directive upward when a dependency
// requires a newer language version, and nothing downstream complains until the
// developer runs the project's own lint — by which point the cause is several
// steps behind them. Warn rather than abort: the project is already written and
// otherwise usable, and the developer can pin the offending tool themselves.
func verifyGoFloor(projectName string) {
	got := readGoDirective("go.mod")
	if got == "" || got == scaffoldGoVersion {
		return
	}
	cliout.Blank()
	cliout.Warn("A tool dependency raised this project's Go version to %s (expected %s).", got, scaffoldGoVersion)
	cliout.Hint("`make lint` in %s will fail until this is resolved — golangci-lint cannot", projectName)
	cliout.Hint("lint a module targeting a newer Go version than the linter was built with.")
	cliout.Hint("Pin the offending tool in go.mod, or upgrade the toolchain and linter together.")
	cliout.Blank()
}

// randReadFn is the crypto/rand seam. Production reads real entropy; tests
// swap it to drive the failure path, which is otherwise unreachable —
// rand.Read only errors when the OS entropy source is broken. Same pattern as
// runDevPipelineFn in dev.go.
var randReadFn = rand.Read

// randomSecret returns a cryptographically-random, URL-safe secret string.
// Each `gofasta new` mints fresh JWT and session secrets so a generated
// project is never seeded with the framework's publicly-known placeholder
// (which pkg/config.ValidateSecrets rejects at boot).
func randomSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := randReadFn(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// supportedDrivers is the canonical set of --driver values. The first
// entry is the default. Wire-format string values match what
// `database.driver` in config.yaml accepts and what
// `internal/skeleton/migrations/<driver>/` directories are named.
var supportedDrivers = []string{"postgres", "mysql", "sqlite", "sqlserver", "clickhouse"}

// isSupportedDriver reports whether v is one of the canonical driver
// strings. Used to validate the --driver flag before scaffolding.
func isSupportedDriver(v string) bool {
	for _, d := range supportedDrivers {
		if d == v {
			return true
		}
	}
	return false
}

// supportedLayouts is the canonical set of --layout values. The first
// entry is the default. Values match what configutil.ReadLayout returns
// and what layout.ParseKind accepts.
var supportedLayouts = []string{"layered", "feature"}

// isSupportedLayout reports whether v is one of the canonical layout
// strings. Used to validate the --layout flag before scaffolding.
func isSupportedLayout(v string) bool {
	for _, l := range supportedLayouts {
		if l == v {
			return true
		}
	}
	return false
}

// graphqlOnlyPaths lists skeleton paths that should be skipped for REST-only projects.
var graphqlOnlyPaths = []string{
	"app/graphql/",
	"app/di/providers/graphql.go.tmpl",
	"gqlgen.yml.tmpl",
}

var newCmd = &cobra.Command{
	Use:   "new [name-or-module-path]",
	Short: "Scaffold a complete, ready-to-run Go backend project",
	Long: `Create a new gofasta project from the embedded template. Generates ~78 files
covering models, services, repositories, REST controllers, routes, DTOs,
database migrations, Docker/Compose setup, CI configs, Wire dependency
injection, and a Makefile — the full production layout described in the
project README.

The single argument is either a bare project name (used as the directory
name and module path) or a fully-qualified Go module path. When given a
module path, the directory is named after the last segment:

  gofasta new myapp                      # module: myapp, dir: myapp/
  gofasta new github.com/acme/myapp      # module: github.com/acme/myapp, dir: myapp/

What the command does, in order:
  1. Creates the project directory (fails if it already exists)
  2. Runs ` + "`go mod init`" + ` with the resolved module path
  3. Renders every template file from the embedded skeleton, replacing
     {{.ModulePath}} / {{.ProjectNameLower}} / {{.ProjectNameUpper}}
  4. Copies .env from the generated .env.example
  5. Runs ` + "`go get`" + ` for github.com/gofastadev/gofasta and the tool deps
     (Wire, Air, swag — and gqlgen if --graphql is set)
  6. Registers those tools via ` + "`go mod edit -tool`" + ` so ` + "`go tool wire`" + ` works
  7. Runs ` + "`go mod tidy`" + `
  8. Generates Wire DI code and (if --graphql) the gqlgen resolver stubs
  9. Initializes a git repository with an initial commit

Use --graphql (or the shorthand --gql) to additionally scaffold an
app/graphql/ directory with gqlgen, a GraphQL provider, and the generated
resolver stubs. Without the flag, the project is REST-only.

After the command finishes, ` + "`cd`" + ` into the new directory and run
` + "`make up`" + ` (Docker-based dev loop) or ` + "`docker compose up db -d && make dev`" + `
(host-based with hot reload via Air).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		gql, _ := cmd.Flags().GetBool("graphql")
		gqlShort, _ := cmd.Flags().GetBool("gql")
		driver, _ := cmd.Flags().GetString("driver")
		driver = strings.ToLower(strings.TrimSpace(driver))
		if !isSupportedDriver(driver) {
			return clierr.Newf(clierr.CodeInvalidName,
				"--driver %q is not supported — valid values: %s",
				driver, strings.Join(supportedDrivers, ", "))
		}
		layoutFlag, _ := cmd.Flags().GetString("layout")
		layoutFlag = strings.ToLower(strings.TrimSpace(layoutFlag))
		if !isSupportedLayout(layoutFlag) {
			return clierr.Newf(clierr.CodeInvalidName,
				"--layout %q is not supported — valid values: %s",
				layoutFlag, strings.Join(supportedLayouts, ", "))
		}
		return runNew(args[0], gql || gqlShort, driver, layoutFlag)
	},
}

func init() {
	rootCmd.AddCommand(newCmd)
	newCmd.Flags().Bool("graphql", false, "Include GraphQL support (gqlgen) alongside REST")
	newCmd.Flags().Bool("gql", false, "Shorthand for --graphql")
	newCmd.Flags().String("driver", "postgres",
		"Database driver: "+strings.Join(supportedDrivers, "|"))
	newCmd.Flags().String("layout", "layered",
		"Project layout: "+strings.Join(supportedLayouts, "|"))
}

// dotfileRenames maps embedded names to actual dotfile names.
var dotfileRenames = map[string]string{
	"dot-env.example": ".env.example",
	"dot-env":         ".env",
	"dot-gitignore":   ".gitignore",
	"dot-go-version":  ".go-version",
	"air.toml":        ".air.toml",
}

// resolveProjectPaths extracts the project directory, display name, and Go module path
// from the user-provided nameOrPath argument.
func resolveProjectPaths(nameOrPath string) (projectDir, projectName, modulePath string) {
	projectName = filepath.Base(nameOrPath)

	// Use full path for absolute paths, otherwise just the base name.
	projectDir = nameOrPath
	if !filepath.IsAbs(nameOrPath) {
		projectDir = projectName
	}

	// Module path: use nameOrPath only if it looks like a Go module path
	// (e.g. github.com/org/repo), not a filesystem path.
	modulePath = projectName
	if strings.Contains(nameOrPath, "/") && !filepath.IsAbs(nameOrPath) {
		modulePath = nameOrPath
	}
	return
}

// projectFSOverride is a package-level seam so tests can swap the
// embedded skeleton FS with a synthetic one that triggers specific
// walk-error / read-error branches. Nil in production → real embed.
var projectFSOverride fs.FS

// osChdir is a seam over os.Chdir so tests can force the "chdir failed"
// branch without racing the actual filesystem.
var osChdir = os.Chdir

// migrationsFSOverride is a package-level seam so tests can swap the
// embedded per-driver migrations FS. Nil in production → real embed.
var migrationsFSOverride fs.FS

//nolint:gocognit,gocyclo // linear scaffold pipeline; refactoring would obscure the flow.
func runNew(nameOrPath string, includeGraphQL bool, driver, layoutKind string) (resultErr error) {
	projectDir, projectName, modulePath := resolveProjectPaths(nameOrPath)
	if driver == "" {
		driver = "postgres"
	}
	if layoutKind == "" {
		layoutKind = "layered"
	}

	// In --json mode, redirect stdout to stderr for the duration of the
	// scaffold so the streamed stdout of every child
	// `go mod`/`go get`/`wire`/`gqlgen`/`swag` invocation goes to stderr.
	// (Decorated progress already routes correctly via cliout; this swap
	// exists solely to catch child-process stdout that is wired directly to
	// os.Stdout.) Restore stdout in a deferred closure and emit a single
	// structured JSON result so agents see one parseable document on stdout.
	// Safe because runNew is fully sequential (no goroutines), so swapping the
	// package-level os.Stdout has no concurrency hazard.
	if cliout.JSON() {
		savedStdout := os.Stdout
		os.Stdout = os.Stderr
		defer func() {
			os.Stdout = savedStdout
			cliout.Print(newResult{
				Action:     "new",
				Project:    projectName,
				Directory:  projectDir,
				ModulePath: modulePath,
				GraphQL:    includeGraphQL,
				DBDriver:   driver,
				Layout:     layoutKind,
				Success:    resultErr == nil,
				Error:      errString(resultErr),
			}, nil)
		}()
	}

	if _, err := os.Stat(projectDir); err == nil {
		return fmt.Errorf("directory %q already exists", projectDir)
	}

	jwtSecret, err := randomSecret()
	if err != nil {
		return clierr.Wrap(clierr.CodeInternal, err, "generating JWT secret")
	}
	sessionSecret, err := randomSecret()
	if err != nil {
		return clierr.Wrap(clierr.CodeInternal, err, "generating session secret")
	}

	data := ProjectData{
		ProjectName:      projectName,
		ProjectNameLower: strings.ToLower(projectName),
		// Upper variant is used as an env-var prefix in compose.yaml,
		// .env.example, CI workflows, and the generated LoadConfig wrapper. Shell variable names only allow
		// [A-Z0-9_], so we strip anything else (dashes, dots, etc.) —
		// otherwise a project named "my-app" would produce invalid env
		// vars like "MY-APP_DATABASE_HOST" and the framework would never
		// read them.
		ProjectNameUpper: envVarSafeUpper(projectName),
		ModulePath:       modulePath,
		GraphQL:          includeGraphQL,
		DBDriver:         driver,
		Layout:           layoutKind,
		JWTSecret:        jwtSecret,
		SessionSecret:    sessionSecret,
	}

	cliout.Header("🚀 Creating new gofasta project: %s", projectName)
	cliout.Blank()

	// Create project directory
	cliout.Step("📁 Creating directory %s/", projectDir)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return err
	}

	// Change into the new directory
	origDir, _ := os.Getwd()
	if err := osChdir(projectDir); err != nil {
		return err
	}
	defer func() { _ = osChdir(origDir) }()

	// Initialize go module
	cliout.Step("📦 Initializing Go module: %s", modulePath)
	if err := runCmdSilent("go", "mod", "init", modulePath); err != nil {
		return fmt.Errorf("go mod init failed: %w", err)
	}
	// `go mod init` writes the current toolchain version as the `go` directive
	// — so a developer running Go 1.27 would get `go 1.27` in their scaffold
	// even though we only require 1.25.0. Normalise to the declared minimum so
	// generated projects match our stated support floor regardless of the
	// developer's local toolchain. Best-effort: if this fails, the scaffold
	// still works, it just ships with the developer's toolchain version
	// instead of the declared minimum.
	if err := runCmdSilent("go", "mod", "edit", "-go="+scaffoldGoVersion); err != nil {
		cliout.Warn("Could not normalise go directive to %s (generated go.mod may pin a higher version): %v", scaffoldGoVersion, err)
	}

	// Walk embedded skeleton and generate files
	cliout.Step("🏗  Creating project structure...")
	projectFS := projectFSOverride
	if projectFS == nil {
		projectFS = skeleton.ProjectFS
	}
	err = fs.WalkDir(projectFS, "project", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Strip the "project/" prefix to get the relative output path
		relPath := strings.TrimPrefix(path, "project/")
		if relPath == "" || relPath == "project" {
			return nil
		}

		// Skip GraphQL-only files when not using --graphql
		if !data.GraphQL {
			for _, prefix := range graphqlOnlyPaths {
				if strings.HasPrefix(relPath, prefix) {
					if d.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
			}
		}

		if d.IsDir() {
			return os.MkdirAll(relPath, 0o755)
		}

		// Read the embedded file
		content, err := fs.ReadFile(projectFS, path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		// Determine output path and whether to template
		outputPath := relPath
		isTemplate := strings.HasSuffix(outputPath, ".tmpl")
		if isTemplate {
			outputPath = strings.TrimSuffix(outputPath, ".tmpl")
		}

		// Handle dotfile renames
		base := filepath.Base(outputPath)
		if renamed, ok := dotfileRenames[base]; ok {
			outputPath = filepath.Join(filepath.Dir(outputPath), renamed)
		}

		// Ensure parent directory exists
		if dir := filepath.Dir(outputPath); dir != "." {
			_ = os.MkdirAll(dir, 0o755)
		}

		var output []byte
		if isTemplate {
			tmpl, err := template.New(filepath.Base(path)).Parse(string(content))
			if err != nil {
				return fmt.Errorf("parsing template %s: %w", path, err)
			}
			var buf strings.Builder
			if err := tmpl.Execute(&buf, data); err != nil {
				return fmt.Errorf("executing template %s: %w", path, err)
			}
			output = []byte(buf.String())
		} else {
			output = content
		}

		// --layout=feature: route per-resource files through the
		// featurize transformer and emit them at app/<snake>/ paths
		// instead of the layered locations. Cross-cutting files
		// (container.go, wire.go, index.routes.go) get a separate
		// transform that adjusts imports + symbol references to
		// reference the per-feature packages.
		if data.Layout == "feature" {
			rerouted, transformed, ferr := featurizeFile(outputPath, output, data.ModulePath, starterResources())
			if ferr != nil {
				return ferr
			}
			outputPath = rerouted
			output = transformed
			// Ensure new parent directory exists after potential reroute.
			if dir := filepath.Dir(outputPath); dir != "." {
				_ = os.MkdirAll(dir, 0o755)
			}
		}

		cliout.Path(outputPath)
		return os.WriteFile(outputPath, output, 0o644)
	})
	if err != nil {
		return fmt.Errorf("generating project files: %w", err)
	}

	// Copy the per-driver foundational migrations into db/migrations/.
	// The project tree intentionally ships none of its own — see
	// internal/skeleton/embed.go for why each driver lives in its
	// own sibling directory under migrations/.
	if err := copyMigrationsForDriver(driver, data); err != nil {
		return fmt.Errorf("copying %s foundational migrations: %w", driver, err)
	}

	// Copy .env from .env.example
	if envExample, err := os.ReadFile(".env.example"); err == nil {
		_ = os.WriteFile(".env", envExample, 0o644)
		cliout.Path(".env")
	}

	// Install gofasta library as a project dependency.
	//
	// This is a LOAD-BEARING step — the scaffold templates import gofasta
	// packages and won't compile without it. If `go get` fails here (usually
	// because sum.golang.org hasn't yet indexed a freshly-published gofasta
	// release; the module proxy and sum DB are eventually-consistent), we
	// must abort the scaffold rather than silently continuing with a broken
	// project. A vague warning misleads the developer into thinking the
	// project is usable.
	cliout.Blank()
	cliout.Step("📦 Installing gofasta library...")
	if err := runCmdSilent("go", "get", "github.com/gofastadev/gofasta@"+toolVersionGofasta); err != nil {
		// Print the longform hint to the user then return a short,
		// punctuation-clean error that satisfies ST1005.
		cliout.Warn("gofasta library install failed. Common causes:")
		cliout.Plainln("  • sum.golang.org hasn't yet indexed a freshly-published release")
		cliout.Plain("    → wait 5-30 minutes and re-run `gofasta new %s`, or\n", projectName)
		cliout.Plainln("    → run `go get github.com/gofastadev/gofasta@" + toolVersionGofasta + "` inside the")
		cliout.Plainln("      generated project to retry after the sum DB catches up.")
		cliout.Plainln("  • your network blocks the Go module proxy or github.com.")
		cliout.Plainln("  • a corporate proxy requires GOPROXY / GOSUMDB overrides.")
		cliout.Blank()
		return fmt.Errorf("failed to install github.com/gofastadev/gofasta: %w", err)
	}

	// Install cobra for project commands
	if err := runCmdSilent("go", "get", "github.com/spf13/cobra@latest"); err != nil {
		cliout.Warn("Could not install cobra")
	}

	// Add tool dependencies. Versions are pinned — see the tool version block
	// for why @latest is not used here.
	cliout.Step("📦 Installing tool dependencies...")
	if includeGraphQL {
		_ = runCmdSilent("go", "get", "github.com/99designs/gqlgen@"+toolVersionGqlgen)
	}
	_ = runCmdSilent("go", "get", "github.com/google/wire/cmd/wire@"+toolVersionWire)
	_ = runCmdSilent("go", "get", "github.com/air-verse/air@"+toolVersionAir)
	_ = runCmdSilent("go", "get", "github.com/swaggo/swag/cmd/swag@"+toolVersionSwag)
	_ = runCmdSilent("go", "get", "github.com/swaggo/http-swagger/v2@"+toolVersionHTTPSwagger)
	_ = runCmdSilent("go", "get", "github.com/go-chi/chi/v5@"+toolVersionChi)
	// Register as Go tools
	if includeGraphQL {
		_ = runCmdSilent("go", "mod", "edit", "-tool", "github.com/99designs/gqlgen")
	}
	_ = runCmdSilent("go", "mod", "edit", "-tool", "github.com/google/wire/cmd/wire")
	_ = runCmdSilent("go", "mod", "edit", "-tool", "github.com/air-verse/air")
	_ = runCmdSilent("go", "mod", "edit", "-tool", "github.com/swaggo/swag/cmd/swag")

	// Tidy
	cliout.Step("📦 Running go mod tidy...")
	_ = runCmdSilent("go", "mod", "tidy")

	// Tidy resolves the final module graph, so this is the first point where
	// the effective Go floor is known.
	verifyGoFloor(projectName)

	// Generate code
	cliout.Blank()
	cliout.Step("🔌 Generating Wire DI code...")
	if err := runCmdSilent("go", "tool", "wire", "./app/di/"); err != nil {
		cliout.Warn("Wire generation skipped (can be run later with: make wire)")
	}

	if includeGraphQL {
		cliout.Step("📊 Generating GraphQL code...")
		if err := runCmdSilent("go", "tool", "gqlgen", "generate"); err != nil {
			cliout.Warn("gqlgen generation skipped (can be run later with: make gqlgen)")
		}
	}

	cliout.Step("📝 Generating Swagger/OpenAPI docs...")
	if err := runCmdSilent("go", "tool", "swag", "init",
		"-g", "app/main/main.go", "-o", "docs/",
		"--parseDependency", "--parseInternal"); err != nil {
		cliout.Warn("Swagger generation skipped (can be run later with: gofasta swagger)")
	}

	// Initialize git
	cliout.Blank()
	cliout.Step("🔧 Initializing git repository...")
	_ = runCmdSilent("git", "init")
	_ = runCmdSilent("git", "add", ".")
	_ = runCmdSilent("git", "commit", "-m", "Initial commit: gofasta project scaffold")

	cliout.Blank()
	cliout.Success("Project %s created successfully!", termcolor.CBold(projectName))
	printGetStarted(projectName)
	return nil
}

// printGetStarted renders the post-scaffold onboarding block. Extracted so
// `gofasta init` can reuse the exact same messaging — developers should see
// the same next-steps regardless of whether they created a fresh project or
// cloned an existing one. Pass an empty projectName to skip the `cd` line
// (useful for init, which runs from inside the project directory).
func printGetStarted(projectName string) {
	cliout.Blank()
	cliout.Header("Next steps:")
	cliout.Blank()
	if projectName != "" {
		cliout.Plain("  %s\n", termcolor.CBold("cd "+projectName))
		cliout.Blank()
	}

	// --- Development workflows ---------------------------------------------
	//
	// Three workflows, most-containerized first. Each is labeled with its
	// tradeoff so the developer can pick without having to read the docs.
	// gofasta commands are shown first; `make` targets are demoted to an
	// "Also available as" block at the bottom.
	cliout.Header("Pick a development workflow:")
	cliout.Blank()

	cliout.Plain("  %s  %s%s\n",
		termcolor.CBold("A."),
		termcolor.CBold("Everything in Docker"),
		termcolor.CDim(" — fully containerized, zero host setup"))
	cliout.Plain("     %s   %s\n", termcolor.CBold("docker compose up -d        "), termcolor.CDim("# build + start app and db"))
	cliout.Plain("     %s   %s\n", termcolor.CBold("docker compose logs -f app  "), termcolor.CDim("# tail application logs"))
	cliout.Plain("     %s   %s\n", termcolor.CBold("docker compose down         "), termcolor.CDim("# stop everything"))
	cliout.Blank()

	cliout.Plain("  %s  %s%s\n",
		termcolor.CBold("B."),
		termcolor.CBold("App on host, db in Docker"),
		termcolor.CDim(" — fastest iteration, Air hot reload"))
	cliout.Plain("     %s   %s\n", termcolor.CBold("docker compose up db -d     "), termcolor.CDim("# start only the database"))
	cliout.Plain("     %s   %s\n", termcolor.CBold("gofasta dev                 "), termcolor.CDim("# run app with hot reload + auto-migrate"))
	cliout.Blank()

	cliout.Plain("  %s  %s%s\n",
		termcolor.CBold("C."),
		termcolor.CBold("Everything on host"),
		termcolor.CDim(" — you manage your own database"))
	cliout.Plain("     %s   %s\n", termcolor.CBold("gofasta dev                 "), termcolor.CDim("# expects db at the address in config.yaml"))
	cliout.Blank()

	// --- Common tasks -------------------------------------------------------
	cliout.Header("Common tasks:")
	cliout.Blank()
	tasks := [][2]string{
		{"gofasta g scaffold Product name:string price:float", "generate a full REST resource, auto-wired end-to-end"},
		{"gofasta g model Product name:string price:float", "just the model + matching migration"},
		{"gofasta g job cleanup-tokens \"0 0 0 * * *\"", "scheduled cron job"},
		{"gofasta g task send-welcome-email", "async background task for the queue worker"},
		{"gofasta migrate up", "apply all pending database migrations"},
		{"gofasta migrate down", "roll back the most recent migration"},
		{"gofasta seed", "run seed functions (--fresh drops + re-migrates first)"},
		{"gofasta routes", "list every registered REST route"},
		{"gofasta swagger", "regenerate OpenAPI docs from code annotations"},
		{"gofasta doctor", "check prerequisites and project health"},
	}
	for _, ln := range tasks {
		cliout.Plain("  %-55s %s\n", termcolor.CBold(ln[0]), termcolor.CDim("# "+ln[1]))
	}
	cliout.Blank()

	// --- Make shortcuts (thin wrappers over the gofasta commands above) ---
	cliout.Header("Also available as Make targets:")
	cliout.Blank()
	makeShortcuts := [][2]string{
		{"make up", "docker compose up -d"},
		{"make down", "docker compose down"},
		{"make dev", "gofasta dev"},
		{"make migrate", "gofasta migrate up"},
		{"make seed", "gofasta seed"},
	}
	for _, ln := range makeShortcuts {
		cliout.Plain("  %-14s %s\n", termcolor.CBold(ln[0]), termcolor.CDim("→ "+ln[1]))
	}
	cliout.Blank()

	// --- Where to go next ---------------------------------------------------
	cliout.Header("Full command reference:")
	cliout.Blank()
	cliout.Plain("  %s            %s\n", termcolor.CBold("gofasta --help           "), termcolor.CDim("# every command, grouped by purpose"))
	cliout.Plain("  %s            %s\n", termcolor.CBold("gofasta <command> --help "), termcolor.CDim("# details for a specific command"))
	cliout.Blank()
}

// copyMigrationsForDriver writes the per-driver foundational migration
// set into the new project's db/migrations/ directory. Reads from the
// embedded skeleton.MigrationsFS (or the migrationsFSOverride test
// seam). Each .sql file is rendered as a Go text/template against
// ProjectData so {{.ProjectNameLower}} etc. work in SQL — same
// pipeline the project FS walk uses for .tmpl files. The .sql files
// themselves don't carry a .tmpl suffix because most are pure DDL;
// only the few that interpolate get a meaningful substitution.
func copyMigrationsForDriver(driver string, data ProjectData) error {
	migFS := migrationsFSOverride
	if migFS == nil {
		migFS = skeleton.MigrationsFS
	}
	root := "migrations/" + driver
	entries, err := fs.ReadDir(migFS, root)
	if err != nil {
		return fmt.Errorf("no foundational migrations for driver %q: %w", driver, err)
	}
	if err := os.MkdirAll("db/migrations", 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		src := root + "/" + e.Name()
		raw, err := fs.ReadFile(migFS, src)
		if err != nil {
			return fmt.Errorf("reading %s: %w", src, err)
		}
		tmpl, err := template.New(e.Name()).Parse(string(raw))
		if err != nil {
			return fmt.Errorf("parsing migration %s: %w", src, err)
		}
		var buf strings.Builder
		if err := tmpl.Execute(&buf, data); err != nil {
			return fmt.Errorf("executing migration template %s: %w", src, err)
		}
		dst := filepath.Join("db", "migrations", e.Name())
		if err := os.WriteFile(dst, []byte(buf.String()), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", dst, err)
		}
		cliout.Path(dst)
	}
	return nil
}

// envVarSafeUpper returns name uppercased with every non-[A-Z0-9_] character
// stripped. Used to derive a shell-variable-safe prefix from a project name
// that may contain dashes, dots, or other characters legal in go.mod paths
// but illegal in shell env var names.
func envVarSafeUpper(name string) string {
	upper := strings.ToUpper(name)
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			return r
		default:
			return -1
		}
	}, upper)
}

func runCmdSilent(name string, args ...string) error {
	cmd := execCommand(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// starterResources is the list of resources the User starter scaffold
// produces. Only one — "User" — today. When the scaffold ships a
// multi-resource starter, extend this slice.
func starterResources() []featurize.Resource {
	return []featurize.Resource{
		{Name: "User", Snake: "user", Plural: "Users"},
	}
}

// featurizeFile re-routes and transforms one already-rendered template
// file when `--layout=feature` is active. Returns:
//
//	rerouted   — new output path (== outputPath when no reroute needed)
//	transformed— possibly-rewritten content (== output when no transform)
//	err        — non-nil if the transform fails
//
// Three categories of files:
//
//  1. Per-resource: app/models/<s>.model.go, app/services/<s>.service.go,
//     etc. — re-routed to app/<s>/* and content collapsed via
//     featurize.TransformPerResource.
//
//  2. Cross-cutting per-layout: app/di/container.go, app/di/wire.go,
//     app/rest/routes/index.routes.go — stay at their paths, but
//     content rewritten to reference per-feature packages.
//
//  3. Shared infra that exists in `app/services/` but isn't a resource
//     file: `password_generator.go` — moved into the feature package
//     it serves (currently `app/user/`).
//
// Files that match none of these pass through unchanged (they're shared
// infra that lives in the same place in both layouts).
//
//nolint:gocognit,gocyclo // dispatch over 5 distinct file categories — splitting into helpers would just move the conditional tree without reducing branches.
func featurizeFile(outputPath string, content []byte, mod string, resources []featurize.Resource) (rerouted string, transformed []byte, err error) {
	// Category 1: per-resource reroute + featurize.
	for _, r := range resources {
		for _, pair := range featurize.PerResourceMapping(r.Snake) {
			if outputPath == pair.Layered {
				out, terr := featurize.TransformPerResource(content, featurize.Options{
					ModulePath: mod,
					Resource:   r,
				})
				if terr != nil {
					return outputPath, content, fmt.Errorf("featurize %s: %w", outputPath, terr)
				}
				return pair.Feature, out, nil
			}
		}
	}

	// Category 3: password_generator.go follows the user feature (only
	// consumer today). Move into app/user/ with the package rewritten.
	if outputPath == "app/services/password_generator.go" {
		out, terr := featurize.TransformPerResource(content, featurize.Options{
			ModulePath: mod,
			Resource:   featurize.Resource{Name: "User", Snake: "user", Plural: "Users"},
		})
		if terr != nil {
			return outputPath, content, fmt.Errorf("featurize %s: %w", outputPath, terr)
		}
		return "app/user/password_generator.go", out, nil
	}

	// Category 2: cross-cutting per-layout files.
	switch outputPath {
	case "app/di/container.go":
		out, terr := featurize.TransformContainer(content, mod, resources)
		if terr != nil {
			return outputPath, content, fmt.Errorf("featurize %s: %w", outputPath, terr)
		}
		return outputPath, out, nil
	case "app/di/wire.go":
		out, terr := featurize.TransformWire(content, mod, resources)
		if terr != nil {
			return outputPath, content, fmt.Errorf("featurize %s: %w", outputPath, terr)
		}
		return outputPath, out, nil
	case "app/rest/routes/index.routes.go":
		out, terr := featurize.TransformIndexRoutes(content, mod, resources)
		if terr != nil {
			return outputPath, content, fmt.Errorf("featurize %s: %w", outputPath, terr)
		}
		return outputPath, out, nil
	case "app/di/providers/core.go":
		out, terr := featurize.TransformCoreProviders(content, mod, resources)
		if terr != nil {
			return outputPath, content, fmt.Errorf("featurize %s: %w", outputPath, terr)
		}
		return outputPath, out, nil
	}

	// Mock files in testutil/mocks/ — reroute the import block to
	// the per-feature package. Match by filename suffix so future
	// per-feature mocks (Order, Product, etc.) get the same treatment.
	if strings.HasPrefix(outputPath, "testutil/mocks/") &&
		(strings.HasSuffix(outputPath, "_repository_mock.go") ||
			strings.HasSuffix(outputPath, "_service_mock.go")) {
		base := filepath.Base(outputPath)
		// extract <snake> from "<snake>_repository_mock.go" / "<snake>_service_mock.go"
		snake := strings.TrimSuffix(base, "_repository_mock.go")
		snake = strings.TrimSuffix(snake, "_service_mock.go")
		for _, r := range resources {
			if r.Snake == snake {
				out, terr := featurize.TransformMock(content, mod, r)
				if terr != nil {
					return outputPath, content, fmt.Errorf("featurize %s: %w", outputPath, terr)
				}
				return outputPath, out, nil
			}
		}
	}

	// Shared relocations — files that just move path with no source
	// rewrites (e.g. app/dtos/aliases.go → app/shared/dtos/aliases.go).
	for _, pair := range featurize.SharedRelocations() {
		if outputPath == pair.Layered {
			return pair.Feature, content, nil
		}
	}

	// GraphQL resolver files stay in app/graphql/resolvers/ (gqlgen owns
	// the directory) but reference the moved per-resource symbols and the
	// relocated shared dtos package. EVERY .go file in the directory gets
	// the full re-qualification — matching by prefix, not by name, so a
	// project's own resolver files are covered too (the historical
	// hardcoded user.resolvers.go case missed every other file).
	if strings.HasPrefix(outputPath, "app/graphql/resolvers/") && strings.HasSuffix(outputPath, ".go") {
		out, terr := featurize.TransformGraphQL(content, mod, resources)
		if terr != nil {
			return outputPath, content, fmt.Errorf("featurize %s: %w", outputPath, terr)
		}
		return outputPath, out, nil
	}

	// gqlgen.yml follows the dtos relocation: model.filename and autobind
	// point at app/shared/dtos plus the per-resource feature packages, so
	// the `go tool gqlgen generate` step of `new` emits code that
	// compiles against the feature layout.
	if outputPath == "gqlgen.yml" {
		return outputPath, featurize.RewriteGqlgenConfig(content, mod, resources), nil
	}

	// Shared infra files that stay in their layered location but
	// reference the relocated shared dtos package. Only their import
	// path needs flipping — no other source rewrite.
	switch outputPath {
	case "app/validators/app_validator.go",
		"app/rest/controllers/validator.go":
		out, terr := featurize.FixDtosImportPath(content, mod)
		if terr != nil {
			return outputPath, content, fmt.Errorf("featurize %s: %w", outputPath, terr)
		}
		return outputPath, out, nil
	}

	// Per-resource validator files stay in app/validators/ as-is.
	// The user.validators.go template only references gorm + validator
	// + slog (no per-resource types), so no transformation needed.
	// If a future per-resource validator references the feature
	// package, add a transformer case here.

	return outputPath, content, nil
}
