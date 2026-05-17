// gen_middleware.go — `gofasta g middleware <METHOD> <path> <middleware-ref>`
//
// Wraps an existing route's handler with a chi middleware. Discovers the
// route via regex (matches `r.<Verb>("<path>", ...)`), then rewrites the
// surrounding `.Method(...)` call into `.With(<middleware>).<Method>(...)`.
// Idempotent — re-running on a route that already includes the same
// middleware is a no-op.
package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
)

// MiddlewareData is the resolved input.
type MiddlewareData struct {
	HTTPMethod string // "GET" | "POST" | ...
	Path       string // chi-style path
	Middleware string // expression: "auth.RequireRole(\"admin\")" or "middleware.Logger"
	RoutesFile string // optional override; default scans every *.routes.go
	RoutesDir  string // default app/rest/routes
}

// GenMiddleware is the entry point invoked by the Cobra command.
func GenMiddleware(d MiddlewareData) error {
	d = middlewareDataDefaults(d)
	if d.HTTPMethod == "" || d.Path == "" || d.Middleware == "" {
		return clierr.New(clierr.CodeInvalidName,
			"<METHOD> <path> <middleware> all required (e.g. POST /orders/{id}/archive auth.RequireRole(\"admin\"))")
	}

	// Locate the routes file holding this route.
	target, hit, err := findRouteFile(d)
	if err != nil {
		return err
	}
	if !hit {
		return clierr.Newf(clierr.CodeRouteAlreadyExists,
			"no registered route matches %s %s under %s",
			d.HTTPMethod, d.Path, d.RoutesDir)
	}

	body, err := os.ReadFile(target)
	if err != nil {
		return clierr.Wrap(clierr.CodeFileIO, err, "reading "+target)
	}
	patched, applied := wrapRouteWithMiddleware(body, d.HTTPMethod, d.Path, d.Middleware)
	if !applied {
		// Idempotency hit — middleware was already attached. Surface as a
		// soft "already exists" with a non-error JSON path: emit no
		// patches and return nil.
		return nil
	}
	return writeBytesOrRecord(target, patched,
		fmt.Sprintf("wrap %s %s with %s", d.HTTPMethod, d.Path, d.Middleware))
}

func middlewareDataDefaults(d MiddlewareData) MiddlewareData {
	if d.RoutesDir == "" {
		d.RoutesDir = filepath.Join("app", "rest", "routes")
	}
	return d
}

// findRouteFile walks routes/*.routes.go looking for the file that
// registers <METHOD> <path>. Returns the matching path + hit flag.
func findRouteFile(d MiddlewareData) (string, bool, error) {
	if d.RoutesFile != "" {
		body, err := os.ReadFile(d.RoutesFile)
		if err != nil {
			return "", false, clierr.Wrap(clierr.CodeFileIO, err, "reading "+d.RoutesFile)
		}
		return d.RoutesFile, endpointRouteRegistered(body, d.HTTPMethod, d.Path), nil
	}
	entries, err := os.ReadDir(d.RoutesDir)
	if err != nil {
		return "", false, clierr.Wrap(clierr.CodeRoutesDirMissing, err, "reading "+d.RoutesDir)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".routes.go") {
			continue
		}
		path := filepath.Join(d.RoutesDir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if endpointRouteRegistered(body, d.HTTPMethod, d.Path) {
			return path, true, nil
		}
	}
	return "", false, nil
}

// wrapRouteWithMiddleware rewrites
//
//	r.Method("/path", handler)
//
// into
//
//	r.With(<mw>).Method("/path", handler)
//
// (and turns an existing `r.With(a).Method(...)` into
// `r.With(a, <mw>).Method(...)` when <mw> isn't already in the With(...)
// list).
//
// Returns the rewritten body plus a flag indicating whether anything was
// actually changed (idempotency).
func wrapRouteWithMiddleware(body []byte, httpMethod, path, middleware string) (patched []byte, applied bool) {
	verb := toChiVerb(httpMethod)
	// gocritic's sprintfQuotedString would propose %q, but the literal
	// quotes here are regex metacharacters around a regex-quoted path —
	// %q would inject Go escapes and break the regex.
	//nolint:gocritic // false positive — see comment above.
	bareRe := regexp.MustCompile(
		fmt.Sprintf(`(\br)(\.%s\("%s",)`, verb, regexp.QuoteMeta(path)))
	//nolint:gocritic // same as above.
	withRe := regexp.MustCompile(
		fmt.Sprintf(`(\br\.With\()([^)]+)(\)\.%s\("%s",)`, verb, regexp.QuoteMeta(path)))

	// First try: a `.With(...)` chain already exists.
	if withRe.Match(body) {
		patched = withRe.ReplaceAllFunc(body, func(match []byte) []byte {
			parts := withRe.FindSubmatch(match)
			existing := strings.TrimSpace(string(parts[2]))
			if containsMiddleware(existing, middleware) {
				return match // idempotent — leave as-is
			}
			return []byte(string(parts[1]) + existing + ", " + middleware + string(parts[3]))
		})
		return patched, !regexpMatchEquals(body, patched)
	}

	// Otherwise wrap the bare `r.Method(...)` call.
	patched = bareRe.ReplaceAll(body,
		[]byte(fmt.Sprintf(`${1}.With(%s)${2}`, regexpReplaceEscape(middleware))))
	return patched, !regexpMatchEquals(body, patched)
}

// containsMiddleware splits a chi With(...) argument list and checks
// whether the new middleware is already in it (string-equal after trim).
func containsMiddleware(existing, middleware string) bool {
	for _, p := range strings.Split(existing, ",") {
		if strings.TrimSpace(p) == strings.TrimSpace(middleware) {
			return true
		}
	}
	return false
}

// regexpMatchEquals reports whether the two byte slices have identical
// content. We use a separate helper rather than bytes.Equal directly so
// the call site reads as "did anything change?" — the negation lives in
// the caller.
func regexpMatchEquals(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// regexpReplaceEscape escapes "$" sequences in the replacement string —
// regexp.ReplaceAll treats $N as a capture-group reference. Middleware
// expressions don't normally contain "$" but we play safe.
func regexpReplaceEscape(s string) string {
	return strings.ReplaceAll(s, "$", "$$")
}
