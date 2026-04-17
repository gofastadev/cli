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
	Long: `Statically parse every file under app/rest/routes/ for chi router
method calls (` + "`r.Get`" + `, ` + "`r.Post`" + `, ` + "`r.Put`" + `, ` + "`r.Delete`" + `, ` + "`r.Patch`" + `) and print
a formatted table showing HTTP method, full path (including mounted
subrouter prefixes), and source file. Does not import or run your project
code — purely a grep-and-format pass.

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
	routeMethodRe = regexp.MustCompile(`\.(Get|Post|Put|Delete|Patch|Head|Options)\("([^"]+)",`)
	mountRe       = regexp.MustCompile(`\.Mount\("([^"]+)",`)
	wildcardRe    = regexp.MustCompile(`\.Handle\("([^"]+\*)",`)
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

	// Extract API prefix from index file via the chi Mount call.
	apiPrefix := ""
	indexPath := routesDir + "/index.routes.go"
	if indexContent, err := os.ReadFile(indexPath); err == nil {
		if matches := mountRe.FindSubmatch(indexContent); len(matches) > 1 {
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
	methodMatches := routeMethodRe.FindAllStringSubmatch(content, -1)
	wildcardMatches := wildcardRe.FindAllStringSubmatch(content, -1)
	routes := make([]routeEntry, 0, len(methodMatches)+len(wildcardMatches))

	// Match r.Get("path", ...) / r.Post / r.Put / r.Delete / r.Patch — chi's
	// method-based API. The captured method name is already uppercase-first,
	// so convert to HTTP verb with ToUpper.
	for _, m := range methodMatches {
		routes = append(routes, routeEntry{
			method:   strings.ToUpper(m[1]),
			path:     prefix + m[2],
			filename: filename,
		})
	}

	// Match r.Handle("path/*", ...) — used by swagger UI and other
	// wildcard-mounted handlers. Shown as GET since they serve content.
	// chi patterns already include the trailing wildcard, so display as-is.
	for _, m := range wildcardMatches {
		routes = append(routes, routeEntry{
			method:   "GET",
			path:     m[1],
			filename: filename,
		})
	}

	return routes
}
