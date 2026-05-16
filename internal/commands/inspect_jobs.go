package commands

import (
	"fmt"
	"go/ast"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/cobra"
)

// inspectJobsCmd is the introspection counterpart to `gofasta inspect` for
// the job layer. Mirrors the static-AST approach: parse app/jobs/*.go,
// classify types that implement the Name() + Run() contract, then pair
// each with its schedule from config.yaml.
//
// This command is static-only — it does not require a running app. A
// future `--live` flag will attach recent-run telemetry via /debug/jobs
// once that endpoint exists in the skeleton's devtools package.
var inspectJobsCmd = &cobra.Command{
	Use:   "inspect-jobs [<name>]",
	Short: "List registered cron jobs with their schedules (static AST scan of app/jobs/)",
	Long: `Scan app/jobs/*.go for types that implement the gofasta job contract
(Name() string + Run(ctx context.Context) error). For each, pair the
job's Name() value with its schedule from config.yaml under jobs.<name>.

Pass a job name as the positional argument to filter to a single entry.
Without devtools, the output is purely static — schedule and source
file location, but no run history. Future versions will attach recent
runs from /debug/jobs when the app is reachable.

JSON output:

  {
    "jobs_dir": "app/jobs",
    "jobs": [{
      "name": "cleanup-tokens",       // value returned by Name()
      "type": "CleanupTokensJob",     // Go type name
      "file": "app/jobs/cleanup_tokens.go",
      "schedule": "0 0 * * *",        // from config.yaml; empty if not set
      "devtools_enabled": false       // true when /debug/jobs replied
    }],
    "count": 1
  }`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		filter := ""
		if len(args) == 1 {
			filter = args[0]
		}
		return runInspectJobs(filter)
	},
}

func init() {
	rootCmd.AddCommand(inspectJobsCmd)
}

// inspectJobEntry is one job's static profile.
type inspectJobEntry struct {
	Name     string `json:"name"`               // value returned by Name()
	Type     string `json:"type"`               // Go type name
	File     string `json:"file"`               // source path, repo-relative
	Schedule string `json:"schedule,omitempty"` // from config.yaml jobs.<name>.schedule
}

// inspectJobsResult is the JSON envelope.
type inspectJobsResult struct {
	JobsDir         string            `json:"jobs_dir"`
	Jobs            []inspectJobEntry `json:"jobs"`
	Count           int               `json:"count"`
	DevtoolsEnabled bool              `json:"devtools_enabled"`
}

func runInspectJobs(filter string) error {
	dir := filepath.Join("app", "jobs")
	if _, err := os.Stat(dir); err != nil {
		return clierr.Newf(clierr.CodeJobsDirMissing,
			"%s not found — generate a job with `gofasta g job <name> \"<cron>\"` first", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return clierr.Wrap(clierr.CodeFileIO, err, "reading "+dir)
	}

	schedules := loadJobSchedules()
	result := inspectJobsResult{JobsDir: dir}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Skip test files + the example wrapper (it's a generated stub).
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		jobs, err := scanJobsFile(path)
		if err != nil {
			// One bad file shouldn't kill the whole scan — record nothing
			// for it and move on. The user can re-run with -v style debug
			// once we add it.
			continue
		}
		for _, j := range jobs {
			if filter != "" && j.Name != filter && j.Type != filter {
				continue
			}
			if s, ok := schedules[j.Name]; ok {
				j.Schedule = s
			}
			result.Jobs = append(result.Jobs, j)
		}
	}

	sort.Slice(result.Jobs, func(i, j int) bool { return result.Jobs[i].Name < result.Jobs[j].Name })
	result.Count = len(result.Jobs)

	cliout.Print(result, func(w io.Writer) { printInspectJobsText(w, result) })
	return nil
}

// scanJobsFile parses one Go file and returns every struct type that
// implements the (Name() string, Run(...) error) shape. The check is
// shallow — we only verify the method *signatures* exist; the real
// compile-time contract is enforced by the project's tests.
func scanJobsFile(path string) ([]inspectJobEntry, error) {
	f, err := parseGoFile(path)
	if err != nil {
		return nil, err
	}

	// First pass: collect every struct type declared in this file.
	structs := map[string]bool{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if _, isStruct := ts.Type.(*ast.StructType); isStruct {
				structs[ts.Name.Name] = true
			}
		}
	}

	// Second pass: collect methods per receiver.
	type methodSet struct {
		hasName bool
		hasRun  bool
		nameLit string // string literal returned by Name(), if any
	}
	methods := map[string]*methodSet{}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 {
			continue
		}
		recv := strings.TrimPrefix(exprToString(fd.Recv.List[0].Type), "*")
		if !structs[recv] {
			continue
		}
		m, exists := methods[recv]
		if !exists {
			m = &methodSet{}
			methods[recv] = m
		}
		switch fd.Name.Name {
		case "Name":
			m.hasName = true
			m.nameLit = extractSingleReturnString(fd)
		case "Run":
			m.hasRun = true
		}
	}

	var out []inspectJobEntry
	for typ, m := range methods {
		if !m.hasName || !m.hasRun {
			continue
		}
		name := m.nameLit
		if name == "" {
			// Fall back to the type name lowered to kebab-case so the
			// entry is still distinguishable when Name() returns a
			// computed value.
			name = strings.ToLower(typ)
		}
		out = append(out, inspectJobEntry{
			Name: name,
			Type: typ,
			File: path,
		})
	}
	return out, nil
}

// extractSingleReturnString returns the string-literal value of a function
// whose body is exactly `return "literal"`. For non-trivial bodies (e.g.
// computed names, multiple statements) returns "" so the caller falls back
// to a derived label.
func extractSingleReturnString(fd *ast.FuncDecl) string {
	if fd.Body == nil || len(fd.Body.List) != 1 {
		return ""
	}
	ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return ""
	}
	bl, ok := ret.Results[0].(*ast.BasicLit)
	if !ok {
		return ""
	}
	// Strip the surrounding quotes — BasicLit.Value includes them.
	s := bl.Value
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// loadJobSchedules reads config.yaml's `jobs:` section and returns a
// name → cron map. Empty map on any error — schedules are advisory; the
// inspect output should still surface every job found in source.
func loadJobSchedules() map[string]string {
	out := map[string]string{}
	if _, err := os.Stat("config.yaml"); err != nil {
		return out
	}
	k := koanf.New(".")
	if err := k.Load(file.Provider("config.yaml"), yaml.Parser()); err != nil {
		return out
	}
	// koanf flattens nested keys to "jobs.<name>.schedule" by default.
	for _, key := range k.Keys() {
		if !strings.HasPrefix(key, "jobs.") || !strings.HasSuffix(key, ".schedule") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, "jobs."), ".schedule")
		out[name] = k.String(key)
	}
	return out
}

func printInspectJobsText(w io.Writer, r inspectJobsResult) {
	if r.Count == 0 {
		_, _ = fmt.Fprintf(w, "No jobs registered under %s.\n", r.JobsDir)
		return
	}
	_, _ = fmt.Fprintf(w, "%d job(s) under %s:\n\n", r.Count, r.JobsDir)
	for _, j := range r.Jobs {
		_, _ = fmt.Fprintf(w, "  %s  (%s)\n", j.Name, j.Type)
		_, _ = fmt.Fprintf(w, "    file:     %s\n", j.File)
		if j.Schedule != "" {
			_, _ = fmt.Fprintf(w, "    schedule: %s\n", j.Schedule)
		} else {
			_, _ = fmt.Fprintf(w, "    schedule: (not set in config.yaml)\n")
		}
		_, _ = fmt.Fprintln(w)
	}
}
