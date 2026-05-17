// gen_endpoint.go — `gofasta g endpoint <Resource> <METHOD> <path> [--handler=<name>]`
//
// Adds a single REST endpoint to an existing resource. Patches:
//
//   - app/rest/controllers/<snake>.controller.go — handler method on the controller
//   - app/rest/routes/<snake>.routes.go          — route registration line
//   - app/services/interfaces/<snake>_service.go — service method (unless --no-service)
//
// The handler is wired through gofasta's httputil.Handle adapter so the
// generated method has the same shape as scaffold-produced handlers.
// Method names are auto-derived from "<METHOD> /path" when --handler is
// omitted (e.g. `POST /orders/{id}/archive` → `ArchiveOrder`).
package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/generate/astpatch"
)

// EndpointData is the resolved input for the endpoint generator.
type EndpointData struct {
	Resource    string // PascalCase ("Order")
	Snake       string // snake_case ("order")
	HTTPMethod  string // "GET" | "POST" | "PUT" | "DELETE" | "PATCH"
	Path        string // chi-style path with optional placeholders, e.g. "/orders/{id}/archive"
	HandlerName string // PascalCase ("ArchiveOrder"). Auto-derived when empty.
	WithService bool   // also append a matching method to the service interface

	ControllerFile string
	RoutesFile     string
	ServiceFile    string
}

// GenEndpoint is the entry point invoked by the Cobra command.
func GenEndpoint(d EndpointData) error {
	d = endpointDataDefaults(d)
	if err := validateEndpoint(d); err != nil {
		return err
	}
	if err := ensureExists(d.ControllerFile); err != nil {
		return err
	}
	if err := ensureExists(d.RoutesFile); err != nil {
		return err
	}

	// Step 1: append the handler to the controller.
	cf, err := astpatch.Parse(d.ControllerFile)
	if err != nil {
		return err
	}
	controllerType := d.Resource + "Controller"
	receiver := strings.ToLower(d.Resource[:1]) + d.Resource[1:] + "Controller"
	_ = receiver
	if _, err := astpatch.FindStruct(cf, controllerType); err != nil {
		return err
	}
	if _, err := astpatch.FindFunc(cf, controllerType, d.HandlerName); err == nil {
		return clierr.Newf(clierr.CodeMethodAlreadyExists,
			"controller %s already has handler %s — pick a different name",
			controllerType, d.HandlerName)
	}
	astpatch.EnsureImport(cf, "net/http")
	stub := buildEndpointHandlerStub(d, controllerType)
	if err := astpatch.AppendFuncDecl(cf, stub); err != nil {
		return err
	}
	if err := writeBackOrRecord(cf,
		fmt.Sprintf("add %s handler to %s", d.HandlerName, controllerType)); err != nil {
		return err
	}

	// Step 2: insert the route registration line into the routes function.
	rfBody, err := readFile(d.RoutesFile)
	if err != nil {
		return err
	}
	if endpointRouteRegistered(rfBody, d.HTTPMethod, d.Path) {
		return clierr.Newf(clierr.CodeRouteAlreadyExists,
			"%s %s is already registered in %s",
			d.HTTPMethod, d.Path, d.RoutesFile)
	}
	newRoute := fmt.Sprintf("\tr.%s(%q, httputil.Handle(c.%s))",
		toChiVerb(d.HTTPMethod), d.Path, d.HandlerName)
	patched, ok := injectIntoRoutesFunc(rfBody, d.Resource, newRoute)
	if !ok {
		return clierr.Newf(clierr.CodeASTPatchFailed,
			"could not locate %sRoutes() in %s — file may have been restructured",
			d.Resource, d.RoutesFile)
	}
	if err := writeBytesOrRecord(d.RoutesFile, patched,
		fmt.Sprintf("register %s %s in %sRoutes", d.HTTPMethod, d.Path, d.Resource)); err != nil {
		return err
	}

	// Step 3: optionally extend the service interface with a matching method.
	if d.WithService {
		sf, err := astpatch.Parse(d.ServiceFile)
		if err != nil {
			return err
		}
		iface, err := astpatch.FindInterface(sf, d.Resource+"ServiceInterface")
		if err != nil {
			return err
		}
		if !astpatch.InterfaceHasMethod(iface, d.HandlerName) {
			astpatch.EnsureImport(sf, "context")
			if err := astpatch.AppendInterfaceMethod(iface,
				fmt.Sprintf("%s(ctx context.Context) error", d.HandlerName)); err != nil {
				return err
			}
			if err := writeBackOrRecord(sf,
				fmt.Sprintf("add %s to %sServiceInterface", d.HandlerName, d.Resource)); err != nil {
				return err
			}
		}
	}
	return nil
}

func endpointDataDefaults(d EndpointData) EndpointData {
	if d.Snake == "" && d.Resource != "" {
		d.Snake = toSnakeCase(d.Resource)
	}
	if d.HandlerName == "" {
		d.HandlerName = deriveHandlerName(d.HTTPMethod, d.Path, d.Resource)
	}
	if d.ControllerFile == "" {
		d.ControllerFile = filepath.Join("app", "rest", "controllers", d.Snake+".controller.go")
	}
	if d.RoutesFile == "" {
		d.RoutesFile = filepath.Join("app", "rest", "routes", d.Snake+".routes.go")
	}
	if d.ServiceFile == "" {
		d.ServiceFile = filepath.Join("app", "services", "interfaces", d.Snake+"_service.go")
	}
	return d
}

func validateEndpoint(d EndpointData) error {
	if d.Resource == "" {
		return clierr.New(clierr.CodeInvalidName, "resource name required")
	}
	if d.HTTPMethod == "" || d.Path == "" {
		return clierr.New(clierr.CodeInvalidName,
			"both <METHOD> and <path> are required (e.g. POST /orders/{id}/archive)")
	}
	switch strings.ToUpper(d.HTTPMethod) {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS":
		// ok
	default:
		return clierr.Newf(clierr.CodeInvalidName,
			"unsupported HTTP method %q (use GET/POST/PUT/DELETE/PATCH/HEAD/OPTIONS)", d.HTTPMethod)
	}
	if !strings.HasPrefix(d.Path, "/") {
		return clierr.Newf(clierr.CodeInvalidName, "path must start with `/`, got %q", d.Path)
	}
	return nil
}

// deriveHandlerName turns "POST /orders/{id}/archive" into "ArchiveOrder".
// Falls back to "<Verb><Resource>" when the path has no trailing segment.
//
// Rules:
//
//   - Use the last non-placeholder path segment as the action verb.
//   - Prefix verbs that match standard chi verbs (Create / Update / Delete /
//     List / Get) with no modification — that produces e.g. "CreateOrder".
//   - Method-only signal as a fallback ("POST /orders" with no action
//     segment → "CreateOrder").
func deriveHandlerName(httpMethod, path, resource string) string {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	var action string
	for i := len(segs) - 1; i >= 0; i-- {
		s := segs[i]
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "{") {
			continue
		}
		// First non-placeholder from the end is our verb.
		if i > 0 || (httpMethod == "POST" && len(segs) == 1) {
			action = s
		}
		break
	}
	if action == "" {
		switch strings.ToUpper(httpMethod) {
		case "POST":
			action = "Create"
		case "PUT", "PATCH":
			action = "Update"
		case "DELETE":
			action = "Delete"
		case "GET":
			// "GET /orders" → List, "GET /orders/{id}" → Get
			if strings.Contains(path, "{") {
				action = "Get"
			} else {
				action = "List"
			}
		default:
			action = strings.ToUpper(httpMethod[:1]) + strings.ToLower(httpMethod[1:])
		}
	}
	return toPascalCase(action) + resource
}

// buildEndpointHandlerStub emits the controller method body. We use the
// same shape every scaffold-generated handler uses: signature
// (w http.ResponseWriter, r *http.Request) error, no business logic.
func buildEndpointHandlerStub(d EndpointData, controllerType string) string {
	receiver := "c"
	return fmt.Sprintf(`// %s handles %s %s.
//
// @Summary  %s
// @Tags     %s
// @Accept   json
// @Produce  json
// @Router   %s [%s]
func (%s *%s) %s(w http.ResponseWriter, r *http.Request) error {
	// TODO: implement
	return nil
}`,
		d.HandlerName, d.HTTPMethod, d.Path,
		d.HandlerName, strings.ToLower(toSnakeCase(d.Resource)),
		d.Path, strings.ToLower(d.HTTPMethod),
		receiver, controllerType, d.HandlerName)
}

// endpointRouteRegistered tells whether a route registration for the
// METHOD + path combo already exists in the routes file (regex match
// against the chi `r.<Verb>(...)` lines).
func endpointRouteRegistered(body []byte, httpMethod, path string) bool {
	verb := toChiVerb(httpMethod)
	// %q would inject Go-style escapes; the regex needs literal quotes
	// around the regex-quoted path, so the explicit "%s" form is correct.
	//nolint:gocritic // sprintfQuotedString is a false positive here — the literal quotes are regex metacharacters, not Go string escapes.
	pattern := fmt.Sprintf(`\br\.%s\("%s"`, verb, regexp.QuoteMeta(path))
	re := regexp.MustCompile(pattern)
	return re.Match(body)
}

// injectIntoRoutesFunc finds `func <Resource>Routes(...)`'s closing brace
// and inserts the new route line just before it. Returns the patched
// bytes plus a hit/miss flag so callers can branch on "couldn't find
// the function" cleanly.
func injectIntoRoutesFunc(body []byte, resource, newRouteLine string) ([]byte, bool) {
	s := string(body)
	marker := fmt.Sprintf("func %sRoutes(", resource)
	idx := strings.Index(s, marker)
	if idx == -1 {
		return body, false
	}
	// Walk forward to the function's opening brace, then track depth.
	openBrace := strings.Index(s[idx:], "{")
	if openBrace == -1 {
		return body, false
	}
	openBrace += idx
	depth := 1
	end := openBrace + 1
	for end < len(s) && depth > 0 {
		switch s[end] {
		case '{':
			depth++
		case '}':
			depth--
		}
		if depth == 0 {
			break
		}
		end++
	}
	if depth != 0 {
		return body, false
	}
	// Walk back from `end` to find the previous newline so we insert at
	// the start of the line containing the closing brace.
	insertAt := end
	for insertAt > 0 && s[insertAt-1] != '\n' {
		insertAt--
	}
	patched := s[:insertAt] + newRouteLine + "\n" + s[insertAt:]
	return []byte(patched), true
}

// toChiVerb maps an uppercase HTTP method to chi's go-camel method name
// ("GET" → "Get", "DELETE" → "Delete"). chi exposes one method per verb.
func toChiVerb(httpMethod string) string {
	switch strings.ToUpper(httpMethod) {
	case "GET":
		return "Get"
	case "POST":
		return "Post"
	case "PUT":
		return "Put"
	case "DELETE":
		return "Delete"
	case "PATCH":
		return "Patch"
	case "HEAD":
		return "Head"
	case "OPTIONS":
		return "Options"
	default:
		return strings.ToUpper(httpMethod[:1]) + strings.ToLower(httpMethod[1:])
	}
}

// ----- small file helpers ------------------------------------------------

// readFile mirrors os.ReadFile but wraps the error in clierr so the user
// sees a useful hint instead of a bare syscall error.
func readFile(path string) ([]byte, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, clierr.Wrap(clierr.CodeFileIO, err, "reading "+path)
	}
	return body, nil
}

// writeBytesOrRecord is the dry-run-aware writer for files we patch
// outside of astpatch (e.g. routes files we modify with string surgery
// rather than full AST manipulation).
func writeBytesOrRecord(path string, body []byte, detail string) error {
	if GetDryRun() {
		recordPatch(path, detail, len(body))
		return nil
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return clierr.Wrap(clierr.CodeFileIO, err, "writing "+path)
	}
	return nil
}
