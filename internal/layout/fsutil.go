// fsutil.go — filesystem helpers shared by the layout implementations for
// discovering per-resource directories and files under app/. These run at
// generator time against the project on disk, so they tolerate missing
// directories by returning empty results rather than erroring.

package layout

import (
	"os"
	"path/filepath"
	"strings"
)

// sharedFeatureDirs are the app/ subdirectories that hold cross-cutting
// concerns rather than a single resource. They are excluded when walking
// app/*/ for per-resource files under the feature layout.
var sharedFeatureDirs = map[string]bool{
	"di":         true,
	"jobs":       true,
	"tasks":      true,
	"graphql":    true,
	"shared":     true,
	"validators": true,
	"rest":       true,
	"models":     true,
	"devtools":   true,
}

// featureResourceDirs returns the app/<resource>/ directories of a feature
// project — every immediate subdirectory of app/ that is not a shared concern.
func featureResourceDirs() []string {
	entries, err := os.ReadDir("app")
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || sharedFeatureDirs[e.Name()] {
			continue
		}
		dirs = append(dirs, filepath.Join("app", e.Name()))
	}
	return dirs
}

// dirHasSuffixFile reports whether dir contains at least one non-test .go file
// whose name ends with suffix (e.g. "_iface.go").
func dirHasSuffixFile(dir, suffix string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, suffix) && !strings.HasSuffix(name, "_test.go") {
			return true
		}
	}
	return false
}

// globRouteFiles returns every *.routes.go file directly under dir.
func globRouteFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".routes.go") {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	return files
}

// fileExists reports whether path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
