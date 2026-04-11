package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

// Cmd is the parent "generate" command, registered on rootCmd by the thin stub.
var Cmd = &cobra.Command{
	Use:     "generate",
	Short:   "Scaffold resources, models, DTOs, controllers, jobs, and tasks",
	Aliases: []string{"g"},
	Long: `Code generators for every layer of a gofasta project. Every generator
renders from embedded Go templates, writes files under the conventional
paths (app/models/, app/repositories/, app/services/, app/dtos/,
app/rest/controllers/, app/rest/routes/, app/di/providers/, db/migrations/,
app/jobs/, app/tasks/, app/graphql/, templates/emails/), and patches the
dependency-injection container, wire.go, route config, and serve.go so the
new code is wired automatically.

Field syntax is ` + "`name:type`" + `. Supported types: string, text, int, float,
bool, uuid, time. The underlying SQL type is chosen based on the database
driver in config.yaml (postgres, mysql, sqlite, sqlserver, clickhouse).

Generators, by layer:

  scaffold        model + migration + repo + service + DTOs + provider +
                  controller + routes + full auto-wire (optionally GraphQL)
  model           model struct + matching up/down migration
  migration       standalone up/down migration files
  repository      model + migration + repo interface + impl
  service         repo stack + service interface + impl + DTOs + provider
  controller      service stack + REST controller + route file + wiring
  dto             DTO file only
  route           route file only (assumes controller exists)
  provider        Wire provider + container + wire.go patches
  resolver        add a service dependency to an existing GraphQL resolver
  job             cron job file + scheduler registration + schedule config
  task            async task handler wired into the asynq queue
  email-template  HTML email template under templates/emails/

After scaffolding, generators that affect wiring automatically run
` + "`go tool wire`" + ` (and ` + "`go tool gqlgen`" + ` for --graphql) so the project compiles
without manual intervention.`,
}

// WireCmd is a standalone command to regenerate Wire DI code.
var WireCmd = &cobra.Command{
	Use:   "wire",
	Short: "Regenerate Google Wire dependency injection code",
	Long: `Run ` + "`go tool wire ./app/di/`" + ` to regenerate wire_gen.go from the current
provider set. Use this after manually editing app/di/wire.go or a provider
file — ` + "`gofasta g`" + ` commands already run this automatically as their last
step, so you only need it after hand edits.`,
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
	//nolint:gocritic // split intentionally around optional resolver step above.
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
	//nolint:gocritic // split intentionally around optional resolver step above.
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
	Short: "Generate a full REST resource (model → controller) with every layer auto-wired",
	Long: `The one-command shortcut for creating a complete resource domain. Runs
every file generator in sequence and patches all wiring files so the new
resource is compilable, routed, and ready for business logic with no
manual edits.

Files created (11 per resource):

  app/models/<name>.model.go
  app/repositories/interfaces/<name>_repository.go
  app/repositories/<name>.repository.go
  app/services/interfaces/<name>_service.go
  app/services/<name>.service.go
  app/dtos/<name>.dtos.go
  app/di/providers/<name>.go
  app/rest/controllers/<name>.controller.go
  app/rest/routes/<name>.routes.go
  db/migrations/NNNNNN_create_<names>.up.sql
  db/migrations/NNNNNN_create_<names>.down.sql

Files patched (4 per resource):

  app/di/container.go
  app/di/wire.go
  app/rest/routes/index.routes.go
  cmd/serve.go

Runs ` + "`go tool wire`" + ` as the final step. Use --graphql (alias --gql) to
additionally generate a .gql schema fragment and patch the GraphQL
resolver — both gqlgen and Wire regeneration then run at the end.

Field syntax is ` + "`name:type`" + `. Supported types: string, text, int, float,
bool, uuid, time.

Examples:
  gofasta g s Product name:string price:float
  gofasta g s Post title:string body:text author_id:uuid --graphql

After scaffolding, run ` + "`gofasta migrate up`" + ` and write your business
logic in app/services/<name>.service.go.`,
	Aliases: []string{"s"},
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := buildFromArgs(args)
		d.IncludeController = true
		d.IncludeGraphQL = hasGraphQLFlag(cmd)
		if err := RunSteps(d, scaffoldSteps(d)); err != nil {
			return err
		}
		fmt.Println()
		termcolor.PrintSuccess("Scaffold complete for %s. All files generated and wired.", termcolor.CBold(d.Name))
		fmt.Printf("  %s  %s\n", termcolor.CDim("Run migrations:"), termcolor.CBold("gofasta migrate up"))
		fmt.Printf("  %s  %s\n", termcolor.CDim("Write logic:"), termcolor.CBold(fmt.Sprintf("app/services/%s.service.go", d.SnakeName)))
		return nil
	},
}

var modelCmd = &cobra.Command{
	Use:   "model [Name] [field:type ...]",
	Short: "Generate a GORM model struct and a matching schema migration",
	Long: `Generate the smallest useful unit — a model struct under app/models/ plus
paired .up.sql / .down.sql migration files under db/migrations/. Use this
when you only need persistence scaffolding and will write the repository
and service layers by hand.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunSteps(buildFromArgs(args), modelSteps())
	},
}

var repositoryCmd = &cobra.Command{
	Use:     "repository [Name] [field:type ...]",
	Short:   "Generate model, migration, and repository interface + implementation",
	Aliases: []string{"repo"},
	Long: `Generate everything in ` + "`gofasta g model`" + ` plus the repository layer:

  app/repositories/interfaces/<name>_repository.go  — repository contract
  app/repositories/<name>.repository.go             — GORM implementation

Use this when you want persistence + data-access but plan to write your
own service or expose the repository directly.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunSteps(buildFromArgs(args), repositorySteps())
	},
}

var serviceCmd = &cobra.Command{
	Use:     "service [Name] [field:type ...]",
	Short:   "Generate the full persistence + service layer (no controller)",
	Aliases: []string{"svc"},
	Long: `Generate everything in ` + "`gofasta g repository`" + ` plus the service layer and
DTOs, then auto-wire it through the DI container:

  app/services/interfaces/<name>_service.go  — service contract
  app/services/<name>.service.go              — business-logic skeleton
  app/dtos/<name>.dtos.go                     — request / response DTOs
  app/di/providers/<name>.go                  — Wire provider set

Also patches app/di/container.go and app/di/wire.go, then regenerates the
Wire injector. Use --graphql (or --gql) to additionally patch the GraphQL
resolver with the new service dependency.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := buildFromArgs(args)
		d.IncludeGraphQL = hasGraphQLFlag(cmd)
		return RunSteps(d, serviceSteps(d))
	},
}

var controllerCmd = &cobra.Command{
	Use:     "controller [Name] [field:type ...]",
	Short:   "Generate the full REST stack: service layer + controller + routes",
	Aliases: []string{"ctrl"},
	Long: `Generate everything in ` + "`gofasta g service`" + ` plus the REST layer:

  app/rest/controllers/<name>.controller.go  — CRUD HTTP handlers
  app/rest/routes/<name>.routes.go           — route registration

Patches app/rest/routes/index.routes.go and cmd/serve.go so the new routes
are mounted on startup, then regenerates Wire. Use --graphql (or --gql) to
additionally generate a GraphQL schema fragment and resolver wiring. The
only difference from ` + "`gofasta g scaffold`" + ` is that ` + "`scaffold`" + ` is the user-
facing shortcut and this subcommand is the explicit "up through controller"
step.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := buildFromArgs(args)
		d.IncludeController = true
		d.IncludeGraphQL = hasGraphQLFlag(cmd)
		return RunSteps(d, controllerSteps(d))
	},
}

var dtoCmd = &cobra.Command{
	Use:   "dto [Name] [field:type ...]",
	Short: "Generate a standalone DTO file (create / update / response)",
	Long: `Generate app/dtos/<name>.dtos.go containing Create/Update/Response DTOs
with go-playground/validator tags derived from the field list. Produces
no model, repository, or wiring — useful when you already have a model
and want DTOs for an RPC or GraphQL-only resource.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunSteps(buildFromArgs(args), dtoSteps())
	},
}

var migrationCmd = &cobra.Command{
	Use:   "migration [Name] [field:type ...]",
	Short: "Generate a standalone up/down SQL migration pair",
	Long: `Generate paired .up.sql / .down.sql files under db/migrations/ with a
sequential version prefix. Column types are selected based on the
configured database driver (postgres, mysql, sqlite, sqlserver,
clickhouse). Does not touch any Go code — useful for schema-only changes
such as indexes, constraints, or data migrations.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunSteps(buildFromArgs(args), migrationSteps())
	},
}

var routeCmd = &cobra.Command{
	Use:   "route [Name]",
	Short: "Generate a REST route file for an existing controller",
	Long: `Generate app/rest/routes/<name>.routes.go mapping the standard CRUD
endpoints onto an existing controller. Does not create the controller or
patch index.routes.go — use this when you want custom wiring or you have
already written the controller by hand.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunSteps(buildFromArgs(args), routeSteps())
	},
}

var resolverCmd = &cobra.Command{
	Use:   "resolver [Name]",
	Short: "Patch the GraphQL resolver struct to add a service dependency",
	Long: `Add the named service as a dependency on the project's GraphQL resolver
struct and regenerate gqlgen bindings. Use this when you have an existing
GraphQL schema and want the resolver to gain access to a newly-created
service without running full scaffolding.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunSteps(buildFromArgs(args), resolverSteps())
	},
}

var jobCmd = &cobra.Command{
	Use:   "job [name] [schedule]",
	Short: "Generate a cron job, register it with the scheduler, and write the schedule to config",
	Long: `Generate a new cron job under app/jobs/, add it to the scheduler's job
registry, and append its schedule to the ` + "`jobs:`" + ` section of config.yaml.

The schedule argument is a 6-field cron expression (with seconds):

  second minute hour day month weekday

If omitted, the job defaults to "every hour". Provide the schedule
quoted so your shell does not expand ` + "`*`" + `:

  gofasta g job cleanup-tokens "0 0 0 * * *"      # daily at midnight
  gofasta g job send-reports   "0 0 9 * * 1"      # every Monday at 9am
  gofasta g job sync-data                         # every hour (default)`,
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
	Short:   "Generate an HTML email template under templates/emails/",
	Aliases: []string{"email"},
	Long: `Generate templates/emails/<name>.html with a starter layout compatible
with the framework's mailer package. The template is plain Go HTML
templating — substitute variables with ` + "`{{.FieldName}}`" + ` and render it via
mailer.Renderer in your service code.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := buildFromArgs(args)
		return RunSteps(d, []Step{{"email template", GenEmailTemplate}})
	},
}

var taskCmd = &cobra.Command{
	Use:   "task [name]",
	Short: "Generate an async task handler for the asynq queue",
	Long: `Generate app/tasks/<name>.go with an asynq handler stub — the queue-
based equivalent of a job, for fire-and-forget work that should execute
off the request path. The handler is auto-registered with the task
registry and picked up by the asynq worker on startup.

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
	Short: "Generate a Wire provider and patch it into container.go + wire.go",
	Long: `Generate app/di/providers/<name>.go with a Wire provider set for the
named resource and patch app/di/container.go + app/di/wire.go to include
it, then regenerate the Wire injector. Useful when integrating hand-
written services that were not created through ` + "`gofasta g service`" + `.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunSteps(buildFromArgs(args), providerSteps())
	},
}
