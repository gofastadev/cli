package commands

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var doCmd = &cobra.Command{
	Use:   "do <workflow>",
	Short: "Run a named development workflow — a pre-defined chain of gofasta commands",
	Long: `Development workflows are named sequences of gofasta sub-commands that
together accomplish one higher-level task. Each one is a transparent
chain — no hidden logic, no extra state, no side effects beyond what
the individual commands already do. Running a workflow is equivalent to
typing its commands in order; the wrapper exists to save keystrokes,
document common sequences, and give CI and AI agents atomic named steps
to invoke.

Registered workflows:

  new-rest-endpoint   Generate a REST resource + apply its migration +
                      regenerate Swagger. One command replaces the three
                      you'd otherwise type after scaffolding.
  rebuild             Regenerate every derived artifact (Wire, Swagger).
                      Useful after git pull.
  fresh-start         First-time setup after cloning a project — run
                      ` + "`init`" + ` to install tools, apply migrations, and seed.
  clean-slate         Reset the dev database to a known state — drop,
                      re-migrate, re-seed.
  health-check        Run the full preflight gauntlet (` + "`verify`" + `) plus the
                      project status report (` + "`status`" + `).

Pass --dry-run to print the exact commands the workflow would run
without executing them.

Examples:
  gofasta do list
  gofasta do new-rest-endpoint Invoice total:float status:string
  gofasta do rebuild
  gofasta do fresh-start --dry-run
  gofasta do health-check --json`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		rest := args[1:]
		return runWorkflow(name, rest)
	},
}

// doDryRun previews each step's command without running it.
var doDryRun bool

func init() {
	doCmd.GroupID = groupWorkflow
	doCmd.Flags().BoolVar(&doDryRun, "dry-run", false,
		"Print the commands the workflow would run without executing them")
	rootCmd.AddCommand(doCmd)
}

// workflow describes one registered workflow. Build is a closure so
// workflows that accept positional arguments (e.g. new-rest-endpoint
// <Name> <fields>) can compose them into the step list at runtime.
type workflow struct {
	Key         string
	Description string
	Args        string
	Build       func(passed []string) ([]workflowStep, error)
}

// workflowStep is one command in the chain. Args is the slice of CLI
// tokens passed to the running gofasta binary (without the binary name
// itself). Description is shown to the user/agent as each step runs.
type workflowStep struct {
	Description string
	Args        []string
}

// workflows is the stable registry. Adding a new workflow: append an
// entry — no other code changes required.
var workflows = []workflow{
	{
		Key:         "new-rest-endpoint",
		Description: "Scaffold a REST resource, apply its migration, regenerate Swagger",
		Args:        "<ResourceName> [field:type ...]",
		Build: func(passed []string) ([]workflowStep, error) {
			if len(passed) < 1 {
				return nil, clierr.New(clierr.CodeInvalidName,
					"new-rest-endpoint requires a resource name — e.g. `gofasta do new-rest-endpoint Invoice amount:float`")
			}
			scaffoldArgs := append([]string{"g", "scaffold"}, passed...)
			return []workflowStep{
				{Description: "scaffold resource", Args: scaffoldArgs},
				{Description: "apply migrations", Args: []string{"migrate", "up"}},
				{Description: "regenerate Swagger", Args: []string{"swagger"}},
			}, nil
		},
	},
	{
		Key:         "rebuild",
		Description: "Regenerate every derived artifact (Wire + Swagger)",
		Args:        "",
		Build: func(_ []string) ([]workflowStep, error) {
			return []workflowStep{
				{Description: "regenerate Wire", Args: []string{"wire"}},
				{Description: "regenerate Swagger", Args: []string{"swagger"}},
			}, nil
		},
	},
	{
		Key:         "fresh-start",
		Description: "First-time project setup after `git clone` — install tool deps, migrate, seed",
		Args:        "",
		Build: func(_ []string) ([]workflowStep, error) {
			return []workflowStep{
				{Description: "install tools + regenerate DI/GraphQL", Args: []string{"init"}},
				{Description: "apply migrations", Args: []string{"migrate", "up"}},
				{Description: "run seeders", Args: []string{"seed"}},
			}, nil
		},
	},
	{
		Key:         "clean-slate",
		Description: "Reset the dev database to a known state — drop + re-migrate + re-seed",
		Args:        "",
		Build: func(_ []string) ([]workflowStep, error) {
			return []workflowStep{
				{Description: "reset database (drop + migrate up)", Args: []string{"db", "reset"}},
				{Description: "run seeders", Args: []string{"seed"}},
			}, nil
		},
	},
	{
		Key:         "health-check",
		Description: "Run `verify` + `status` together — the full project health report",
		Args:        "",
		Build: func(_ []string) ([]workflowStep, error) {
			return []workflowStep{
				{Description: "preflight gauntlet", Args: []string{"verify"}},
				{Description: "project status report", Args: []string{"status"}},
			}, nil
		},
	},
}

// workflowResult is the --json payload emitted at the end of a run.
// Agents parse this to decide whether to branch on success/failure.
type workflowResult struct {
	Workflow   string               `json:"workflow"`
	Status     string               `json:"status"` // "ok" | "failed" | "planned"
	DryRun     bool                 `json:"dry_run"`
	Steps      []workflowStepResult `json:"steps"`
	DurationMS int64                `json:"duration_ms"`
}

// workflowStepResult mirrors each step's outcome. ExitCode is set when
// a step's command returns a non-zero exit; Error captures the
// underlying Go error message for debugging.
type workflowStepResult struct {
	Description string   `json:"description"`
	Command     []string `json:"command"`
	Status      string   `json:"status"` // "ok" | "failed" | "planned"
	ExitCode    int      `json:"exit_code,omitempty"`
	Error       string   `json:"error,omitempty"`
	DurationMS  int64    `json:"duration_ms,omitempty"`
}

// runWorkflow is the entry point. Resolves the named workflow, builds
// the step list, runs every step (or prints them in dry-run mode), and
// emits a structured summary.
func runWorkflow(name string, passed []string) error {
	if name == "list" {
		return runWorkflowList()
	}
	wf := findWorkflow(name)
	if wf == nil {
		return clierr.Newf(clierr.CodeInvalidName,
			"unknown workflow %q — run `gofasta do list` to see supported workflows", name)
	}
	steps, err := wf.Build(passed)
	if err != nil {
		return err
	}

	start := time.Now()
	result := workflowResult{
		Workflow: wf.Key,
		DryRun:   doDryRun,
	}
	if doDryRun {
		result.Status = "planned"
	} else {
		result.Status = "ok"
	}

	for _, step := range steps {
		stepResult := workflowStepResult{
			Description: step.Description,
			Command:     append([]string{"gofasta"}, step.Args...),
		}

		if doDryRun {
			stepResult.Status = "planned"
			result.Steps = append(result.Steps, stepResult)
			continue
		}

		if !cliout.JSON() {
			fprintf(os.Stdout, "%s %s %s\n",
				termcolor.CBrand("→"),
				step.Description,
				termcolor.CDim("(gofasta "+strings.Join(step.Args, " ")+")"))
		}
		stepStart := time.Now()
		err := runGofastaStep(step.Args)
		stepResult.DurationMS = time.Since(stepStart).Milliseconds()
		if err != nil {
			stepResult.Status = "failed"
			stepResult.Error = err.Error()
			if exitErr, ok := err.(*exec.ExitError); ok {
				stepResult.ExitCode = exitErr.ExitCode()
			}
			result.Status = "failed"
			result.Steps = append(result.Steps, stepResult)
			result.DurationMS = time.Since(start).Milliseconds()
			cliout.Print(result, func(w io.Writer) { printWorkflowText(w, &result) })
			return clierr.Wrapf(clierr.CodeGeneratorFailed, err,
				"workflow %q failed at step %q", wf.Key, step.Description)
		}
		stepResult.Status = "ok"
		result.Steps = append(result.Steps, stepResult)
	}

	result.DurationMS = time.Since(start).Milliseconds()
	cliout.Print(result, func(w io.Writer) { printWorkflowText(w, &result) })
	return nil
}

// runGofastaStep shells out to the running binary (os.Args[0]) with the
// step's argv. Using the current binary path — not `gofasta` on $PATH
// — means the workflow always invokes the exact version the user ran,
// avoiding version-skew surprises when two gofasta binaries exist.
func runGofastaStep(args []string) error {
	binary := os.Args[0]
	cmd := exec.Command(binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// runWorkflowList handles `gofasta do list`. Emits the registry as
// either a table or a JSON array.
func runWorkflowList() error {
	cliout.Print(workflows, func(w io.Writer) {
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		fprintln(tw, "WORKFLOW\tARGS\tDESCRIPTION")
		for _, wf := range workflows {
			args := wf.Args
			if args == "" {
				args = "—"
			}
			fprintf(tw, "%s\t%s\t%s\n", wf.Key, args, wf.Description)
		}
		_ = tw.Flush()
	})
	return nil
}

func findWorkflow(key string) *workflow {
	for i := range workflows {
		if workflows[i].Key == key {
			return &workflows[i]
		}
	}
	return nil
}

// printWorkflowText renders the final text-mode summary. Success shows
// a green check per step; failure highlights the broken step in red.
func printWorkflowText(w io.Writer, r *workflowResult) {
	fprintln(w)
	if r.DryRun {
		fprintf(w, "Dry run — workflow %q would execute:\n\n", r.Workflow)
	}
	for _, s := range r.Steps {
		mark := stepStatusMark(s.Status)
		switch s.Status {
		case "ok":
			fprintf(w, "  %s %s %s\n", mark, s.Description,
				termcolor.CDim(fmt.Sprintf("(%dms)", s.DurationMS)))
		case "failed":
			fprintf(w, "  %s %s — %s\n", mark, s.Description, s.Error)
		case "planned":
			fprintf(w, "  %s %s %s\n", mark, s.Description,
				termcolor.CDim("(gofasta "+strings.Join(s.Command[1:], " ")+")"))
		}
	}
	fprintln(w)
	switch {
	case r.DryRun:
		fprintln(w, "No commands were executed. Re-run without --dry-run to apply.")
	case r.Status == "ok":
		fprintf(w, "Workflow %s completed successfully (%dms).\n", r.Workflow, r.DurationMS)
	default:
		fprintf(w, "Workflow %s failed.\n", r.Workflow)
	}
}

func stepStatusMark(s string) string {
	switch s {
	case "ok":
		return termcolor.CGreen("✓")
	case "failed":
		return termcolor.CRed("✗")
	case "planned":
		return termcolor.CBrand("·")
	default:
		return "?"
	}
}
