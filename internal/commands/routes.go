package commands

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var routesCmd = &cobra.Command{
	Use:   "routes",
	Short: "List every registered REST route in a table (method, path, source)",
	Long: `Statically parse every file under app/rest/routes/ for ` + "`router.GET`" + ` /
` + "`router.POST`" + ` / ` + "`router.PUT`" + ` / ` + "`router.DELETE`" + ` / ` + "`router.PATCH`" + ` calls and print
a formatted table showing HTTP method, full path (including router group
prefixes), and source file. Does not import or run your project code —
purely a grep-and-format pass.

Useful for debugging route conflicts, documenting the public API, and
spotting unregistered handlers. For GraphQL schema introspection use
the standard ` + "`/graphql-playground`" + ` endpoint instead.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRoutes()
	},
}

func init() {
	rootCmd.AddCommand(routesCmd)
}

type routeEntry struct {
	method   string
	path     string
	filename string
}

var (
	handleFuncRe  = regexp.MustCompile(`\.HandleFunc\("([^"]+)",.+\.Methods\("([^"]+)"\)`)
	prefixRe      = regexp.MustCompile(`\.PathPrefix\("([^"]+)"\)\.Subrouter\(\)`)
	pathHandlerRe = regexp.MustCompile(`\.PathPrefix\("([^"]+)"\)\.Handler\(`)
)

func runRoutes() error {
	routesDir := "app/rest/routes"
	if _, err := os.Stat(routesDir); os.IsNotExist(err) {
		return fmt.Errorf("routes directory not found: %s — are you in a gofasta project?", routesDir)
	}

	entries, err := os.ReadDir(routesDir)
	if err != nil {
		return fmt.Errorf("failed to read routes directory: %w", err)
	}

	// Extract API prefix from index file
	apiPrefix := ""
	indexPath := routesDir + "/index.routes.go"
	if indexContent, err := os.ReadFile(indexPath); err == nil {
		if matches := prefixRe.FindSubmatch(indexContent); len(matches) > 1 {
			apiPrefix = string(matches[1])
		}
	}

	var allRoutes []routeEntry

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".routes.go") {
			continue
		}

		content, err := os.ReadFile(routesDir + "/" + name)
		if err != nil {
			continue
		}

		prefix := ""
		if name != "index.routes.go" {
			prefix = apiPrefix
		}

		allRoutes = append(allRoutes, extractRoutes(string(content), prefix, name)...)
	}

	if len(allRoutes) == 0 {
		fmt.Println("No routes found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	_, _ = fmt.Fprintln(w, "METHOD\tPATH\tFILE")
	for _, r := range allRoutes {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", r.method, r.path, r.filename)
	}
	return w.Flush()
}

func extractRoutes(content, prefix, filename string) []routeEntry {
	var routes []routeEntry

	// Match .HandleFunc("path", ...).Methods("METHOD")
	for _, m := range handleFuncRe.FindAllStringSubmatch(content, -1) {
		routes = append(routes, routeEntry{
			method:   m[2],
			path:     prefix + m[1],
			filename: filename,
		})
	}

	// Match .PathPrefix("path").Handler(...) — used by swagger UI and
	// other prefix-mounted handlers. Shown as GET since they serve content.
	for _, m := range pathHandlerRe.FindAllStringSubmatch(content, -1) {
		path := m[1]
		// Don't pick up the API subrouter prefix (e.g. /api/v1) — that's
		// already handled by prefixRe and used as a prefix for child routes.
		if path == prefix || path == prefix+"/" {
			continue
		}
		routes = append(routes, routeEntry{
			method:   "GET",
			path:     path + "*",
			filename: filename,
		})
	}

	return routes
}
