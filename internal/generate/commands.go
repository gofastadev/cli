package generate

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Cmd is the parent "generate" command, registered on rootCmd by the thin stub.
var Cmd = &cobra.Command{
	Use:     "generate",
	Short:   "Generate boilerplate code",
	Aliases: []string{"g"},
}

// WireCmd is a standalone command to regenerate Wire DI code.
var WireCmd = &cobra.Command{
	Use:   "wire",
	Short: "Run Wire to regenerate dependency injection code",
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunWire(ScaffoldData{})
	},
}

func init() {
	Cmd.AddCommand(scaffoldCmd)
	Cmd.AddCommand(modelCmd)
	Cmd.AddCommand(repositoryCmd)
	Cmd.AddCommand(serviceCmd)
	Cmd.AddCommand(controllerCmd)
	Cmd.AddCommand(dtoCmd)
	Cmd.AddCommand(migrationCmd)
	Cmd.AddCommand(routeCmd)
	Cmd.AddCommand(resolverCmd)
	Cmd.AddCommand(providerCmd)
	Cmd.AddCommand(emailTemplateCmd)
	Cmd.AddCommand(jobCmd)
	Cmd.AddCommand(taskCmd)

	// Register --graphql flag on commands that support it
	for _, cmd := range []*cobra.Command{scaffoldCmd, serviceCmd, controllerCmd} {
		cmd.Flags().Bool("graphql", false, "Also generate GraphQL schema and wire resolver")
		cmd.Flags().Bool("gql", false, "Shorthand for --graphql")
	}
}

// --- Step chain builders ---
// Pattern: generate ALL files first, then patch, then run tools.
// Steps are built dynamically based on ScaffoldData flags.

func modelSteps() []Step {
	return []Step{
		{"model", GenModel},
		{"migration", GenMigration},
	}
}

func dtoSteps() []Step {
	return []Step{
		{"DTOs", GenDTOs},
	}
}

func migrationSteps() []Step {
	return []Step{
		{"migration", GenMigration},
	}
}

func repositorySteps() []Step {
	return []Step{
		{"model", GenModel},
		{"migration", GenMigration},
		{"repository interface", GenRepoInterface},
		{"repository", GenRepo},
	}
}

func serviceSteps(d ScaffoldData) []Step {
	steps := []Step{
		// Files
		{"model", GenModel},
		{"migration", GenMigration},
		{"repository interface", GenRepoInterface},
		{"repository", GenRepo},
		{"service interface", GenSvcInterface},
		{"service", GenSvc},
		{"DTOs", GenDTOs},
		{"Wire provider", GenWireProvider},
	}
	if d.IncludeGraphQL {
		steps = append(steps, Step{"GraphQL schema", GenGraphQL})
	}
	// Patch
	steps = append(steps,
		Step{"auto-wire: container", PatchContainer},
		Step{"auto-wire: wire.go", PatchWireFile},
	)
	if d.IncludeGraphQL {
		steps = append(steps, Step{"auto-wire: resolver", PatchResolver})
	}
	// Regenerate
	steps = append(steps, Step{"regenerate Wire", RunWire})
	if d.IncludeGraphQL {
		steps = append(steps, Step{"regenerate gqlgen", RunGqlgen})
	}
	return steps
}

func controllerSteps(d ScaffoldData) []Step {
	steps := []Step{
		// Files
		{"model", GenModel},
		{"migration", GenMigration},
		{"repository interface", GenRepoInterface},
		{"repository", GenRepo},
		{"service interface", GenSvcInterface},
		{"service", GenSvc},
		{"DTOs", GenDTOs},
		{"Wire provider", GenWireProvider},
		{"controller", GenController},
		{"routes", GenRoutes},
	}
	if d.IncludeGraphQL {
		steps = append(steps, Step{"GraphQL schema", GenGraphQL})
	}
	// Patch
	steps = append(steps,
		Step{"auto-wire: container", PatchContainer},
		Step{"auto-wire: wire.go", PatchWireFile},
	)
	if d.IncludeGraphQL {
		steps = append(steps, Step{"auto-wire: resolver", PatchResolver})
	}
	steps = append(steps,
		Step{"auto-wire: route config", PatchRouteConfig},
		Step{"auto-wire: serve.go", PatchServeFile},
	)
	// Regenerate
	steps = append(steps, Step{"regenerate Wire", RunWire})
	if d.IncludeGraphQL {
		steps = append(steps, Step{"regenerate gqlgen", RunGqlgen})
	}
	return steps
}

func scaffoldSteps(d ScaffoldData) []Step {
	steps := []Step{
		// Files
		{"model", GenModel},
		{"migration", GenMigration},
		{"repository interface", GenRepoInterface},
		{"repository", GenRepo},
		{"service interface", GenSvcInterface},
		{"service", GenSvc},
		{"DTOs", GenDTOs},
		{"Wire provider", GenWireProvider},
		{"controller", GenController},
		{"routes", GenRoutes},
	}
	if d.IncludeGraphQL {
		steps = append(steps, Step{"GraphQL schema", GenGraphQL})
	}
	// Patch
	steps = append(steps,
		Step{"auto-wire: container", PatchContainer},
		Step{"auto-wire: wire.go", PatchWireFile},
	)
	if d.IncludeGraphQL {
		steps = append(steps, Step{"auto-wire: resolver", PatchResolver})
	}
	steps = append(steps,
		Step{"auto-wire: route config", PatchRouteConfig},
		Step{"auto-wire: serve.go", PatchServeFile},
	)
	// Regenerate
	steps = append(steps, Step{"regenerate Wire", RunWire})
	if d.IncludeGraphQL {
		steps = append(steps, Step{"regenerate gqlgen", RunGqlgen})
	}
	return steps
}

func routeSteps() []Step {
	return []Step{
		{"routes", GenRoutes},
	}
}

func resolverSteps() []Step {
	return []Step{
		{"auto-wire: resolver", GenResolver},
	}
}

func providerSteps() []Step {
	return []Step{
		{"Wire provider", GenWireProvider},
		{"auto-wire: container", PatchContainer},
		{"auto-wire: wire.go", PatchWireFile},
	}
}

// --- Helpers ---

func buildFromArgs(args []string) ScaffoldData {
	return BuildScaffoldData(args[0], ParseFields(args[1:]))
}

func hasGraphQLFlag(cmd *cobra.Command) bool {
	gql, _ := cmd.Flags().GetBool("graphql")
	gqlShort, _ := cmd.Flags().GetBool("gql")
	return gql || gqlShort
}

// --- Cobra command definitions ---

var scaffoldCmd = &cobra.Command{
	Use:   "scaffold [Name] [field:type ...]",
	Short: "Generate a full REST resource with auto-wiring. Use --graphql for GraphQL support.",
	Long: `Generate all files for a new resource domain and auto-wire them into the framework.
No manual wiring needed — the developer only writes business logic.

By default generates REST API resources. Add --graphql to also generate GraphQL schema and resolver.

Examples:
  gofasta g s Product name:string price:float          (REST only)
  gofasta g s Product name:string price:float --graphql (REST + GraphQL)

Supported field types: string, text, int, float, bool, uuid, time`,
	Aliases: []string{"s"},
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := buildFromArgs(args)
		d.IncludeController = true
		d.IncludeGraphQL = hasGraphQLFlag(cmd)
		if err := RunSteps(d, scaffoldSteps(d)); err != nil {
			return err
		}
		fmt.Printf("\nScaffold complete for %s. All files generated and wired.\n", d.Name)
		fmt.Printf("Run migrations: gofasta migrate up\n")
		fmt.Printf("Write business logic: app/services/%s.service.go\n", d.SnakeName)
		return nil
	},
}

var modelCmd = &cobra.Command{
	Use:   "model [Name] [field:type ...]",
	Short: "Generate model + migration",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunSteps(buildFromArgs(args), modelSteps())
	},
}

var repositoryCmd = &cobra.Command{
	Use:     "repository [Name] [field:type ...]",
	Short:   "Generate model + migration + repository interface + repository",
	Aliases: []string{"repo"},
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunSteps(buildFromArgs(args), repositorySteps())
	},
}

var serviceCmd = &cobra.Command{
	Use:     "service [Name] [field:type ...]",
	Short:   "Generate model + repo + service + DTOs + Wire provider, auto-wired. Use --graphql for resolver.",
	Aliases: []string{"svc"},
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := buildFromArgs(args)
		d.IncludeGraphQL = hasGraphQLFlag(cmd)
		return RunSteps(d, serviceSteps(d))
	},
}

var controllerCmd = &cobra.Command{
	Use:     "controller [Name] [field:type ...]",
	Short:   "Generate everything up to controller + routes, auto-wired. Use --graphql for GraphQL.",
	Aliases: []string{"ctrl"},
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := buildFromArgs(args)
		d.IncludeController = true
		d.IncludeGraphQL = hasGraphQLFlag(cmd)
		return RunSteps(d, controllerSteps(d))
	},
}

var dtoCmd = &cobra.Command{
	Use:   "dto [Name] [field:type ...]",
	Short: "Generate DTOs only (standalone)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunSteps(buildFromArgs(args), dtoSteps())
	},
}

var migrationCmd = &cobra.Command{
	Use:   "migration [Name] [field:type ...]",
	Short: "Generate SQL migration files only",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunSteps(buildFromArgs(args), migrationSteps())
	},
}

var routeCmd = &cobra.Command{
	Use:   "route [Name]",
	Short: "Generate route file only (assumes controller exists)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunSteps(buildFromArgs(args), routeSteps())
	},
}

var resolverCmd = &cobra.Command{
	Use:   "resolver [Name]",
	Short: "Patch GraphQL resolver to add service dependency",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunSteps(buildFromArgs(args), resolverSteps())
	},
}

var jobCmd = &cobra.Command{
	Use:   "job [name] [schedule]",
	Short: "Generate a cron job, register it in the scheduler, and add schedule to config",
	Long: `Generate a new cron job file and auto-wire it into the scheduler.

Examples:
  gofasta g job cleanup-tokens "0 0 0 * * *"     (daily at midnight)
  gofasta g job send-reports "0 0 9 * * 1"        (every Monday at 9am)
  gofasta g job sync-data                          (defaults to every hour)

Schedule uses 6-field cron (with seconds): second minute hour day month weekday`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := buildFromArgs(args)
		if len(args) >= 2 {
			d.Schedule = args[1]
		}
		return RunSteps(d, []Step{
			{"job file", GenJob},
			{"job registry", PatchJobRegistry},
			{"job config", PatchJobConfig},
		})
	},
}

var emailTemplateCmd = &cobra.Command{
	Use:     "email-template [name]",
	Short:   "Generate an HTML email template in templates/emails/",
	Aliases: []string{"email"},
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := buildFromArgs(args)
		return RunSteps(d, []Step{{"email template", GenEmailTemplate}})
	},
}

var taskCmd = &cobra.Command{
	Use:   "task [name]",
	Short: "Generate an async task handler for the queue system",
	Long: `Generate a new async task file in app/tasks/ with an asynq handler.

Examples:
  gofasta g task send-welcome-email
  gofasta g task process-payment
  gofasta g task resize-image`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunSteps(buildFromArgs(args), []Step{{"task handler", GenTask}})
	},
}

var providerCmd = &cobra.Command{
	Use:   "provider [Name]",
	Short: "Generate Wire provider + auto-wire container and wire.go",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunSteps(buildFromArgs(args), providerSteps())
	},
}
