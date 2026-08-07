package generate

import "github.com/gofastadev/cli/internal/layout"

// Placeholders used in ScaffoldPlan paths. {resource} is the snake-cased
// resource name, {resources} its plural, <seq> the next migration number —
// values only known at generation time.
const (
	planResource  = "{resource}"
	planResources = "{resources}"
	planMigSeq    = "<seq>"
)

// ScaffoldPlan reports which files `g scaffold` creates and patches for a
// layout, without touching disk. It MUST stay in lockstep with
// scaffoldSteps — TestScaffoldPlan_MatchesScaffoldSteps enforces the 1:1
// mapping, so a new step added there without a path here fails `go test`.
// `gofasta facts` publishes these lists; documentation tables are
// generated from them.
func ScaffoldPlan(l layout.Layout, includeGraphQL bool) (created, patched []string) {
	migrationsDir := l.MigrationsDir()
	created = []string{
		l.ModelFile(planResource),
		migrationsDir + "/" + planMigSeq + "_create_" + planResources + ".up.sql",
		migrationsDir + "/" + planMigSeq + "_create_" + planResources + ".down.sql",
		l.RepoIfaceFile(planResource),
		l.RepoImplFile(planResource),
		l.RepoTestFile(planResource),
		l.ErrorsFile(planResource),
		l.InputsFile(planResource),
		l.InputsTestFile(planResource),
		l.SvcIfaceFile(planResource),
		l.SvcImplFile(planResource),
		l.SvcTestFile(planResource),
		l.DTOsFile(planResource),
		l.DTOsTestFile(planResource),
		l.WireProviderFile(planResource),
		l.ControllerFile(planResource),
		l.ControllerTestFile(planResource),
		l.RoutesFile(planResource),
	}
	patched = []string{
		l.ContainerFile(),
		l.WireFile(),
		l.RouteIndexFile(),
		l.ServeFile(),
	}
	if includeGraphQL {
		created = append(created,
			"app/graphql/schema/"+planResource+".gql",
			l.ResolverResourceFile(planResource),
		)
		patched = append(patched,
			l.ResolverFile(),
			"gqlgen.yml",
		)
	}
	return created, patched
}
