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
	Short: "Display all registered API routes",
	Long: `Parse route files in app/rest/routes/ and display a formatted table of all
registered routes, including HTTP method, path, and source file.`,
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
	handleFuncRe = regexp.MustCompile(`\.HandleFunc\("([^"]+)",.+\.Methods\("([^"]+)"\)`)
	prefixRe     = regexp.MustCompile(`\.PathPrefix\("([^"]+)"\)`)
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
	fmt.Fprintln(w, "METHOD\tPATH\tFILE")
	for _, r := range allRoutes {
		fmt.Fprintf(w, "%s\t%s\t%s\n", r.method, r.path, r.filename)
	}
	return w.Flush()
}

func extractRoutes(content, prefix, filename string) []routeEntry {
	matches := handleFuncRe.FindAllStringSubmatch(content, -1)
	routes := make([]routeEntry, 0, len(matches))
	for _, m := range matches {
		path := prefix + m[1]
		method := m[2]
		routes = append(routes, routeEntry{
			method:   method,
			path:     path,
			filename: filename,
		})
	}
	return routes
}
