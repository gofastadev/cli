package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/layout"
	"github.com/spf13/cobra"
)

var routesCmd = &cobra.Command{
	Use:   "routes",
	Short: "List every registered REST route in a table (method, path, source)",
	Long: `Statically parse every route file for chi router
method calls (` + "`r.Get`" + `, ` + "`r.Post`" + `, ` + "`r.Put`" + `, ` + "`r.Delete`" + `, ` + "`r.Patch`" + `) and print
a formatted table showing HTTP method, full path (including mounted
subrouter prefixes), and source file. Does not import or run your project
code — purely a grep-and-format pass.

Useful for debugging route conflicts, documenting the public API, and
spotting unregistered handlers. Pass --json (inherited from the root
command) to emit machine-parseable output suitable for agents and CI.

For GraphQL schema introspection use the standard ` + "`/graphql-playground`" + `
endpoint instead.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRoutes()
	},
}

func init() {
	rootCmd.AddCommand(routesCmd)
}

// routeEntry is the internal record produced by extractRoutes. The JSON
// tags drive --json output; the struct is also rendered as a text table
// by the default formatter in runRoutes.
type routeEntry struct {
	Method   string `json:"method"`
	Path     string `json:"path"`
	Filename string `json:"file"`
}

var (
	routeMethodRe = regexp.MustCompile(`\.(Get|Post|Put|Delete|Patch|Head|Options)\("([^"]+)",`)
	mountRe       = regexp.MustCompile(`\.Mount\("([^"]+)",`)
	wildcardRe    = regexp.MustCompile(`\.Handle\("([^"]+\*)",`)
)

func runRoutes() error {
	// Layout-aware. Layered keeps every route under app/rest/routes/, so we
	// retain the original directory scan (with its missing/unreadable-dir
	// diagnostics). Feature moves per-resource registrations to
	// app/<resource>/routes.go, which the layout resolves via RouteFiles().
	var files []string
	if layout.Detect().IsFeature() {
		files = layout.Detect().RouteFiles()
	} else {
		var err error
		if files, err = layeredRouteFiles(); err != nil {
			return err
		}
	}

	// Extract the API prefix from the index file's chi Mount call.
	apiPrefix := ""
	for _, f := range files {
		if filepath.Base(f) != "index.routes.go" {
			continue
		}
		if indexContent, err := os.ReadFile(f); err == nil {
			if matches := mountRe.FindSubmatch(indexContent); len(matches) > 1 {
				apiPrefix = string(matches[1])
			}
		}
	}

	var allRoutes []routeEntry

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		base := filepath.Base(file)
		prefix := ""
		if base != "index.routes.go" {
			prefix = apiPrefix
		}

		allRoutes = append(allRoutes, extractRoutes(string(content), prefix, base)...)
	}

	// Render: JSON (array, always — empty list for no routes) or a
	// human-formatted table. The JSON contract is the stable one agents
	// read; the text form can evolve freely.
	cliout.Print(allRoutes, func(w io.Writer) {
		if len(allRoutes) == 0 {
			_, _ = fmt.Fprintln(w, "No routes found.")
			return
		}
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		_, _ = fmt.Fprintln(tw, "METHOD\tPATH\tFILE")
		for _, r := range allRoutes {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Method, r.Path, r.Filename)
		}
		_ = tw.Flush()
	})
	return nil
}

// layeredRouteFiles returns the *.routes.go files under app/rest/routes for the
// layered layout, preserving the missing-dir and unreadable-dir diagnostics.
func layeredRouteFiles() ([]string, error) {
	routesDir := "app/rest/routes"
	if _, err := os.Stat(routesDir); os.IsNotExist(err) {
		return nil, clierr.Newf(clierr.CodeRoutesDirMissing,
			"routes directory not found: %s", routesDir)
	}
	entries, err := os.ReadDir(routesDir)
	if err != nil {
		return nil, clierr.Wrapf(clierr.CodeFileIO, err,
			"failed to read routes directory %s", routesDir)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".routes.go") {
			continue
		}
		files = append(files, filepath.Join(routesDir, entry.Name()))
	}
	return files, nil
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
			Method:   strings.ToUpper(m[1]),
			Path:     prefix + m[2],
			Filename: filename,
		})
	}

	// Match r.Handle("path/*", ...) — used by swagger UI and other
	// wildcard-mounted handlers. Shown as GET since they serve content.
	// chi patterns already include the trailing wildcard, so display as-is.
	for _, m := range wildcardMatches {
		routes = append(routes, routeEntry{
			Method:   "GET",
			Path:     m[1],
			Filename: filename,
		})
	}

	return routes
}
