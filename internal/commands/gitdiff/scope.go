package gitdiff

import (
	"bytes"
	"context"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
)

// PackagesForDirs maps a set of directories (relative to the module root)
// to the Go import paths declared by those directories. Directories that
// do not contain a Go package are silently dropped — that's the same
// behavior `go list ./...` has against an empty dir.
//
// Returns CodeGoBuildFailed if `go list` errors (which usually means
// there's a compile error somewhere in the named dirs; surface that to
// the user so they fix the build before running verify).
func PackagesForDirs(ctx context.Context, dirs []string) ([]string, error) {
	if len(dirs) == 0 {
		return nil, nil
	}
	args := []string{"list", "-e", "-f", "{{.ImportPath}}"}
	for _, d := range dirs {
		args = append(args, "./"+d)
	}
	cmd := execCommand(ctx, "go", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, clierr.Newf(clierr.CodeGoBuildFailed,
			"go list failed: %s", msg)
	}
	return nonEmptyLines(stdout.String()), nil
}

// ReverseDeps returns every package in the module that imports (directly
// or transitively) any of the given root packages. Used by `verify --since`
// to scope `go test` to the package set actually affected by a changeset.
//
// Implementation: load `go list -deps` for the whole module, then for each
// candidate package check whether its dependency set intersects with the
// roots. This is O(N) over the module size — adequate for any project
// that compiles in a reasonable time.
func ReverseDeps(ctx context.Context, roots []string) ([]string, error) {
	if len(roots) == 0 {
		return nil, nil
	}
	rootSet := make(map[string]struct{}, len(roots))
	for _, r := range roots {
		rootSet[r] = struct{}{}
	}

	// Get every package in the module along with its dependency list.
	// {{.ImportPath}};{{join .Deps " "}}{{println}}
	const fmtTmpl = `{{.ImportPath}}|{{range .Deps}}{{.}} {{end}}`
	cmd := execCommand(ctx, "go", "list", "-e", "-deps=false", "-f", fmtTmpl, "./...")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, clierr.Newf(clierr.CodeGoBuildFailed,
			"go list ./... failed: %s", msg)
	}

	out := make(map[string]struct{}, 64)
	// roots themselves are always part of the result (they "depend on
	// themselves" for testing purposes).
	for r := range rootSet {
		out[r] = struct{}{}
	}
	for _, line := range nonEmptyLines(stdout.String()) {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		pkg := parts[0]
		deps := strings.Fields(parts[1])
		for _, dep := range deps {
			if _, hit := rootSet[dep]; hit {
				out[pkg] = struct{}{}
				break
			}
		}
	}

	result := make([]string, 0, len(out))
	for p := range out {
		result = append(result, p)
	}
	return result, nil
}
