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
	"regexp"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/generate/astpatch"
	"github.com/gofastadev/cli/internal/layout"
)

// EndpointData is the resolved input for the endpoint generator.
type EndpointData struct {
	Resource    string // PascalCase ("Order")
	Snake       string // snake_case ("order")
	HTTPMethod  string // "GET" | "POST" | "PUT" | "DELETE" | "PATCH"
	Path        string // chi-style path with optional placeholders, e.g. "/orders/{id}/archive"
	HandlerName string // PascalCase ("ArchiveOrder"). Auto-derived when empty.
	WithService bool   // also append a matching method to the service interface + impl

	ControllerFile  string
	RoutesFile      string
	ServiceFile     string // the interface file
	ServiceImplFile string // the impl file gaining the stub
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
	if d.WithService {
		if err := ensureExists(d.ServiceImplFile); err != nil {
			return err
		}
	}
	if err := patchEndpointController(d); err != nil {
		return err
	}
	if err := patchEndpointRoutes(d); err != nil {
		return err
	}
	if d.WithService {
		return patchEndpointService(d)
	}
	return nil
}

// patchEndpointController appends the handler method to the controller
// struct's file. Idempotency check returns METHOD_ALREADY_EXISTS so
// agents can branch on "already done" without re-parsing.
func patchEndpointController(d EndpointData) error {
	cf, err := astpatch.Parse(d.ControllerFile)
	if err != nil {
		return err
	}
	controllerType := d.Resource + "Controller"
	if _, err := astpatch.FindStruct(cf, controllerType); err != nil {
		return err
	}
	if _, err := astpatch.FindFunc(cf, controllerType, d.HandlerName); err == nil {
		return clierr.Newf(clierr.CodeMethodAlreadyExists,
			"controller %s already has handler %s — pick a different name",
			controllerType, d.HandlerName)
	}
	astpatch.EnsureImport(cf, "net/http")
	if err := astpatchAppendFuncDeclFn(cf, buildEndpointHandlerStub(d, controllerType)); err != nil {
		return err
	}
	return writeBackOrRecord(cf,
		fmt.Sprintf("add %s handler to %s", d.HandlerName, controllerType))
}

// patchEndpointRoutes inserts the chi route registration line into the
// resource's routes function (string surgery — the file's regex-style
// route syntax is easier to splice than to AST-rewrite).
func patchEndpointRoutes(d EndpointData) error {
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
	return writeBytesOrRecord(d.RoutesFile, patched,
		fmt.Sprintf("register %s %s in %sRoutes", d.HTTPMethod, d.Path, d.Resource))
}

// patchEndpointService extends the resource's service interface with a
// matching method declaration AND appends a stub implementation to the
// service struct. Patching only the interface would break compilation:
// the service (and every generated mock) would stop satisfying it.
// Each half is skipped independently when already present, so a
// half-applied earlier run heals instead of erroring.
func patchEndpointService(d EndpointData) error {
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
	return patchEndpointServiceImpl(d)
}

// patchEndpointServiceImpl appends the "not implemented" stub to the
// service struct — same stub shape `g method` produces, same exported
// receiver the scaffold declares.
func patchEndpointServiceImpl(d EndpointData) error {
	implStruct := d.Resource + "Service"
	implFile, err := astpatch.Parse(d.ServiceImplFile)
	if err != nil {
		return err
	}
	if _, err := astpatch.FindFunc(implFile, implStruct, d.HandlerName); err == nil {
		return nil
	}
	astpatch.EnsureImport(implFile, "context")
	astpatch.EnsureImport(implFile, "fmt")
	stub := buildMethodImplStub(MethodData{
		MethodName:     d.HandlerName,
		InterfaceName:  d.Resource + "ServiceInterface",
		ImplStructName: implStruct,
		Returns:        []string{"error"},
	})
	if err := astpatchAppendFuncDeclFn(implFile, stub); err != nil {
		return err
	}
	return writeBackOrRecord(implFile,
		fmt.Sprintf("add %s impl stub to %s", d.HandlerName, implStruct))
}

func endpointDataDefaults(d EndpointData) EndpointData {
	// Normalize the method ONCE so every downstream consumer — name
	// derivation, chi verb mapping, swagger annotations, route
	// idempotency regex — sees the same spelling. Previously `post` and
	// `POST` took different branches in deriveHandlerName and produced
	// different handler names.
	d.HTTPMethod = strings.ToUpper(d.HTTPMethod)
	if d.Snake == "" && d.Resource != "" {
		d.Snake = toSnakeCase(d.Resource)
	}
	if d.HandlerName == "" {
		d.HandlerName = deriveHandlerName(d.HTTPMethod, d.Path, d.Resource)
	}
	if d.ControllerFile == "" || d.RoutesFile == "" || d.ServiceFile == "" || d.ServiceImplFile == "" {
		lo := layout.Detect()
		if d.ControllerFile == "" {
			d.ControllerFile = lo.ControllerFile(d.Snake)
		}
		if d.RoutesFile == "" {
			d.RoutesFile = lo.RoutesFile(d.Snake)
		}
		if d.ServiceFile == "" {
			d.ServiceFile = lo.SvcIfaceFile(d.Snake)
		}
		if d.ServiceImplFile == "" {
			d.ServiceImplFile = lo.SvcImplFile(d.Snake)
		}
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
		// First non-placeholder from the end is our verb — but only
		// when it isn't the resource's own collection segment. A
		// single-segment path ("POST /orders") has no action segment,
		// so it falls through to the method-based fallback below
		// (POST → Create, not the nonsense "OrdersOrder").
		if i > 0 {
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
// (w http.ResponseWriter, r *http.Request) error, consumed by
// httputil.Handle in the routes file.
//
// With --with-service (the default) the handler delegates to the
// service method this same command declared, so the endpoint is live
// end-to-end the moment the service stub is filled in. Without a
// service method there is nothing to call — the body stays a TODO for
// the developer.
func buildEndpointHandlerStub(d EndpointData, controllerType string) string {
	receiver := "c"
	body := `	// TODO: implement
	return nil`
	if d.WithService {
		body = fmt.Sprintf(`	if err := %s.svc.%s(r.Context()); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil`, receiver, d.HandlerName)
	}
	return fmt.Sprintf(`// %s handles %s %s.
//
// @Summary  %s
// @Tags     %s
// @Accept   json
// @Produce  json
// @Router   %s [%s]
func (%s *%s) %s(w http.ResponseWriter, r *http.Request) error {
%s
}`,
		d.HandlerName, d.HTTPMethod, d.Path,
		d.HandlerName, strings.ToLower(toSnakeCase(d.Resource)),
		d.Path, strings.ToLower(d.HTTPMethod),
		receiver, controllerType, d.HandlerName, body)
}

// endpointRouteRegistered tells whether a route registration for the
// METHOD + path combo already exists in the routes file (regex match
// against the chi `r.<Verb>(...)` lines).
func endpointRouteRegistered(body []byte, httpMethod, path string) bool {
	verb := toChiVerb(httpMethod)
	// %q would inject Go-style escapes; the regex needs literal quotes
	// around the regex-quoted path, so the explicit "%s" form is correct.
	// The `(?:\.With\([^)]*\))?` group makes the optional `.With(...)`
	// chain match — `g middleware` wraps existing routes that way, and
	// the route should still be considered "registered" after wrapping.
	//nolint:gocritic // sprintfQuotedString is a false positive here — the literal quotes are regex metacharacters, not Go string escapes.
	pattern := fmt.Sprintf(`\br(?:\.With\([^)]*\))?\.%s\("%s"`, verb, regexp.QuoteMeta(path))
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
