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
	"github.com/spf13/cobra"
)

// inspectTasksCmd is the introspection counterpart to inspect-jobs for the
// asynq task layer. Mirrors the same static-AST approach.
//
// Task files follow `gofasta g task` conventions:
//
//	const Task<Name> = "<snake.task.name>"
//	type <Name>Payload struct { ... }
//	func Handle<Name>(ctx context.Context, t *asynq.Task) error { ... }
//	func Enqueue<Name>(ctx context.Context, q *asynq.Client, p <Name>Payload) (...) { ... }
//
// We discover tasks by walking const declarations whose identifier starts
// with "Task" and pairing them with the matching Payload/Handle/Enqueue
// declarations in the same file.
var inspectTasksCmd = &cobra.Command{
	Use:   "inspect-tasks [<name>]",
	Short: "List registered async tasks with payload + handler shapes (static AST scan of app/tasks/)",
	Long: `Scan app/tasks/*.task.go for the four-part task contract that
gofasta's g task generator produces: a Task<Name> constant, a
<Name>Payload struct, a Handle<Name> function, and an Enqueue<Name>
helper. Reports each task's wire name, payload fields, and source
location.

Pass a task name as the positional argument to filter to a single entry.
Static-only — does not require a running app. Future live mode will
attach queue depth + recent runs from /debug/tasks once that endpoint
exists in the skeleton's devtools package.

JSON output:

  {
    "tasks_dir": "app/tasks",
    "tasks": [{
      "name": "SendWelcomeEmail",       // PascalCase suffix of TaskX
      "type_name": "TaskSendWelcomeEmail",
      "wire_name": "task.send.welcome.email",  // string constant value
      "file": "app/tasks/send_welcome_email.task.go",
      "payload": [{"name": "UserID", "type": "uuid.UUID"}],
      "has_handler": true,
      "has_enqueue": true
    }],
    "count": 1
  }`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		filter := ""
		if len(args) == 1 {
			filter = args[0]
		}
		return runInspectTasks(filter)
	},
}

func init() {
	rootCmd.AddCommand(inspectTasksCmd)
}

// inspectTaskEntry is one task's static profile.
type inspectTaskEntry struct {
	Name       string       `json:"name"`
	TypeName   string       `json:"type_name"`
	WireName   string       `json:"wire_name,omitempty"`
	File       string       `json:"file"`
	Payload    []fieldEntry `json:"payload,omitempty"`
	HasHandler bool         `json:"has_handler"`
	HasEnqueue bool         `json:"has_enqueue"`
}

// inspectTasksResult is the JSON envelope.
type inspectTasksResult struct {
	TasksDir string             `json:"tasks_dir"`
	Tasks    []inspectTaskEntry `json:"tasks"`
	Count    int                `json:"count"`
}

func runInspectTasks(filter string) error {
	dir := filepath.Join("app", "tasks")
	if _, err := os.Stat(dir); err != nil {
		return clierr.Newf(clierr.CodeTasksDirMissing,
			"%s not found — generate a task with `gofasta g task <name>` first", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return clierr.Wrap(clierr.CodeFileIO, err, "reading "+dir)
	}

	result := inspectTasksResult{TasksDir: dir}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// gofasta convention: *.task.go (not test files).
		if !strings.HasSuffix(name, ".task.go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		tasks, err := scanTasksFile(path)
		if err != nil {
			continue
		}
		for _, tk := range tasks {
			if filter != "" && tk.Name != filter && tk.TypeName != filter {
				continue
			}
			result.Tasks = append(result.Tasks, tk)
		}
	}

	sort.Slice(result.Tasks, func(i, j int) bool { return result.Tasks[i].Name < result.Tasks[j].Name })
	result.Count = len(result.Tasks)

	cliout.Print(result, func(w io.Writer) { printInspectTasksText(w, result) })
	return nil
}

// scanTasksFile walks one *.task.go file and produces one entry per
// `Task<Name>` constant, pairing it with the same-name Payload + Handle +
// Enqueue declarations.
func scanTasksFile(path string) ([]inspectTaskEntry, error) {
	f, err := parseGoFile(path)
	if err != nil {
		return nil, err
	}
	tasksByShort := collectTaskConsts(f)
	payloadFields, hasHandler, hasEnqueue := collectTaskParts(f, tasksByShort)

	var out []inspectTaskEntry
	for short, info := range tasksByShort {
		out = append(out, inspectTaskEntry{
			Name:       short,
			TypeName:   info.typeName,
			WireName:   info.wireName,
			File:       path,
			Payload:    payloadFields[short],
			HasHandler: hasHandler[short],
			HasEnqueue: hasEnqueue[short],
		})
	}
	return out, nil
}

// taskConst holds the metadata we extract from a `Task<Name>` constant
// declaration: the Go identifier ("TaskSendWelcomeEmail"), the trimmed
// short name ("SendWelcomeEmail"), and the wire-name string value.
type taskConst struct {
	typeName  string
	shortName string
	wireName  string
}

// collectTaskConsts walks every const block looking for identifiers
// starting with "Task". Each found identifier becomes a taskConst keyed
// by its short (post-"Task") form so subsequent passes can join on it.
func collectTaskConsts(f *ast.File) map[string]*taskConst {
	out := map[string]*taskConst{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok.String() != "const" {
			continue
		}
		for _, spec := range gd.Specs {
			// Inside a const block every Spec is a *ast.ValueSpec by the
			// Go spec, so the type assertion is total.
			collectTaskConstSpec(spec.(*ast.ValueSpec), out)
		}
	}
	return out
}

// collectTaskConstSpec handles one ValueSpec line (which may declare
// multiple names) inside a const block.
func collectTaskConstSpec(vs *ast.ValueSpec, out map[string]*taskConst) {
	for i, n := range vs.Names {
		if !strings.HasPrefix(n.Name, "Task") || n.Name == "Task" {
			continue
		}
		short := strings.TrimPrefix(n.Name, "Task")
		tc := &taskConst{typeName: n.Name, shortName: short}
		if i < len(vs.Values) {
			tc.wireName = stringLitValue(vs.Values[i])
		}
		out[short] = tc
	}
}

// stringLitValue returns the unquoted string literal value of e, or ""
// when e isn't a string-shaped *ast.BasicLit.
func stringLitValue(e ast.Expr) string {
	bl, ok := e.(*ast.BasicLit)
	if !ok {
		return ""
	}
	v := bl.Value
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	return ""
}

// collectTaskParts walks the file looking for the three other parts of
// each task: the Payload struct, the Handle function, and the Enqueue
// function. Returns three maps keyed by task short-name.
func collectTaskParts(f *ast.File, tasks map[string]*taskConst) (
	payloads map[string][]fieldEntry,
	handlers map[string]bool,
	enqueuers map[string]bool,
) {
	payloads = map[string][]fieldEntry{}
	handlers = map[string]bool{}
	enqueuers = map[string]bool{}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			collectTaskPayloads(d, tasks, payloads)
		case *ast.FuncDecl:
			collectTaskFuncs(d, tasks, handlers, enqueuers)
		}
	}
	return payloads, handlers, enqueuers
}

// collectTaskPayloads scans one GenDecl for `<Short>Payload` structs
// whose short name matches a known task, recording each field.
func collectTaskPayloads(d *ast.GenDecl, tasks map[string]*taskConst, payloads map[string][]fieldEntry) {
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || !strings.HasSuffix(ts.Name.Name, "Payload") {
			continue
		}
		short := strings.TrimSuffix(ts.Name.Name, "Payload")
		if _, hit := tasks[short]; !hit {
			continue
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok || st.Fields == nil {
			continue
		}
		for _, fld := range st.Fields.List {
			typ := exprToString(fld.Type)
			for _, fn := range fld.Names {
				payloads[short] = append(payloads[short], fieldEntry{
					Name: fn.Name,
					Type: typ,
				})
			}
		}
	}
}

// collectTaskFuncs records the presence of `Handle<Short>` and
// `Enqueue<Short>` top-level functions per known task.
func collectTaskFuncs(d *ast.FuncDecl, tasks map[string]*taskConst, handlers, enqueuers map[string]bool) {
	if d.Recv != nil {
		return // skip methods
	}
	switch {
	case strings.HasPrefix(d.Name.Name, "Handle"):
		short := strings.TrimPrefix(d.Name.Name, "Handle")
		if _, hit := tasks[short]; hit {
			handlers[short] = true
		}
	case strings.HasPrefix(d.Name.Name, "Enqueue"):
		short := strings.TrimPrefix(d.Name.Name, "Enqueue")
		if _, hit := tasks[short]; hit {
			enqueuers[short] = true
		}
	}
}

func printInspectTasksText(w io.Writer, r inspectTasksResult) {
	if r.Count == 0 {
		_, _ = fmt.Fprintf(w, "No tasks registered under %s.\n", r.TasksDir)
		return
	}
	_, _ = fmt.Fprintf(w, "%d task(s) under %s:\n\n", r.Count, r.TasksDir)
	for _, t := range r.Tasks {
		_, _ = fmt.Fprintf(w, "  %s  (%s)\n", t.Name, t.TypeName)
		_, _ = fmt.Fprintf(w, "    file:      %s\n", t.File)
		if t.WireName != "" {
			_, _ = fmt.Fprintf(w, "    wire_name: %s\n", t.WireName)
		}
		switch {
		case t.HasHandler && t.HasEnqueue:
			_, _ = fmt.Fprintf(w, "    handlers:  Handle%s + Enqueue%s\n", t.Name, t.Name)
		case t.HasHandler:
			_, _ = fmt.Fprintf(w, "    handlers:  Handle%s (missing Enqueue%s)\n", t.Name, t.Name)
		case t.HasEnqueue:
			_, _ = fmt.Fprintf(w, "    handlers:  Enqueue%s (missing Handle%s)\n", t.Name, t.Name)
		default:
			_, _ = fmt.Fprintf(w, "    handlers:  (Handle%s and Enqueue%s both missing)\n", t.Name, t.Name)
		}
		if len(t.Payload) > 0 {
			_, _ = fmt.Fprintf(w, "    payload:   %d field(s)\n", len(t.Payload))
			for _, fld := range t.Payload {
				_, _ = fmt.Fprintf(w, "      - %s %s\n", fld.Name, fld.Type)
			}
		}
		_, _ = fmt.Fprintln(w)
	}
}
