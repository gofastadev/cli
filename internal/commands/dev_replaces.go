package commands

import (
	"bufio"
	"io"
	"os"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────
// go.mod local-replace detection for `gofasta dev --all-in-docker`.
//
// When a project's go.mod has a replace directive pointing at a
// filesystem path (e.g. `replace example.com/foo => ../foo`), the
// replacement is invisible inside the docker build context — the
// dev.dockerfile COPYs only the project's own files, so `go mod
// download` fails inside the container with a cryptic
//
//   "reading /foo/go.mod: no such file or directory"
//
// We detect this case up-front and bail with a clear, actionable error
// before docker compose ever runs. Host-mode (`gofasta dev` without
// --all-in-docker) is unaffected — there, relative replaces resolve
// normally because the build runs on the host.
// ─────────────────────────────────────────────────────────────────────

// localReplace is a single replace directive whose right-hand side is a
// filesystem path rather than a module path. The CLI uses this to
// surface cross-repo dev mismatches with --all-in-docker.
type localReplace struct {
	// Module is the left-hand side of the => operator: the original
	// module path being replaced (e.g. "github.com/gofastadev/gofasta").
	Module string
	// Path is the filesystem path on the right-hand side (e.g.
	// "../../gofastadev/gofasta"). Preserved verbatim so the error
	// message can quote what the user actually wrote in go.mod.
	Path string
}

// findLocalReplacesFn is the package-level seam used by resolveDevPlan
// so tests can swap in a deterministic stub. Production points at
// findLocalReplaces, which reads go.mod from the filesystem.
var findLocalReplacesFn = findLocalReplaces

// findLocalReplaces opens goModPath and returns every replace directive
// whose RHS is a filesystem path. Returns (nil, nil) if the file does
// not exist — a project without a go.mod is not our problem to surface
// here; the docker build will fail with its own clearer message about
// the missing file.
func findLocalReplaces(goModPath string) ([]localReplace, error) {
	f, err := os.Open(goModPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return parseLocalReplaces(f)
}

// parseLocalReplaces walks a go.mod-shaped stream and collects every
// path-form replace. Handles both the single-line form
//
//	replace example.com/foo => ../foo
//
// and the parenthesized block form
//
//	replace (
//	    example.com/foo => ../foo
//	    example.com/bar v1.0.0 => ./vendor/bar
//	)
//
// Comments (`// ...`) are stripped before parsing. Module-to-module
// replaces (RHS is itself a module path, e.g. `=> example.com/fork`)
// are silently skipped — they fetch normally from the proxy and don't
// break the docker build.
func parseLocalReplaces(r io.Reader) ([]localReplace, error) {
	var out []localReplace
	scanner := bufio.NewScanner(r)
	// go.mod files are small; the default buffer (64KB) is more than
	// enough, but we lift it slightly so a pathologically long line
	// (e.g. a generated comment) does not trip Scanner.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	inBlock := false
	for scanner.Scan() {
		line := strings.TrimSpace(stripLineComment(scanner.Text()))
		if line == "" {
			continue
		}

		if !inBlock {
			if !strings.HasPrefix(line, "replace") {
				continue
			}
			rest := strings.TrimSpace(strings.TrimPrefix(line, "replace"))
			if rest == "(" {
				inBlock = true
				continue
			}
			if rep, ok := parseReplaceClause(rest); ok {
				out = append(out, rep)
			}
			continue
		}

		// Inside a `replace ( ... )` block. A line containing only `)`
		// closes the block; everything else is a clause.
		if line == ")" {
			inBlock = false
			continue
		}
		if rep, ok := parseReplaceClause(line); ok {
			out = append(out, rep)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// parseReplaceClause parses one replace clause:
//
//	example.com/foo => ../foo
//	example.com/foo v1.2.3 => ../foo
//	example.com/foo v1.2.3 => ../foo v1.2.3-dev
//
// Returns (localReplace, true) iff the RHS first token is a filesystem
// path. Everything else returns (_, false) so the caller skips it.
func parseReplaceClause(s string) (localReplace, bool) {
	before, after, ok := strings.Cut(s, "=>")
	if !ok {
		return localReplace{}, false
	}
	lhs := strings.Fields(strings.TrimSpace(before))
	rhs := strings.Fields(strings.TrimSpace(after))
	if len(lhs) == 0 || len(rhs) == 0 {
		return localReplace{}, false
	}
	module := lhs[0]
	path := rhs[0]
	if !isFilesystemPath(path) {
		return localReplace{}, false
	}
	return localReplace{Module: module, Path: path}, true
}

// isFilesystemPath reports whether s is a path-form replacement target
// rather than a module path. Per the go.mod spec, path-form
// replacements must start with `./`, `../`, `/`, or be exactly `.` or
// `..`. Module paths never begin with those prefixes (they must contain
// a domain-like leftmost segment), so this distinction is unambiguous.
func isFilesystemPath(s string) bool {
	switch s {
	case ".", "..":
		return true
	}
	return strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, "/")
}

// stripLineComment removes everything after the first `//` on the line.
// go.mod's tokenizer treats `//` identically to Go's, so this is a safe
// approximation for our purposes (we don't need to handle quoted
// strings — replace clauses contain none).
func stripLineComment(line string) string {
	if before, _, ok := strings.Cut(line, "//"); ok {
		return before
	}
	return line
}
