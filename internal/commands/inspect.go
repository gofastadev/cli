package commands

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/spf13/cobra"
)

var inspectCmd = &cobra.Command{
	Use:   "inspect <ResourceName>",
	Short: "Show the full composition of a resource — model fields, routes, service methods, files — as structured output",
	Long: `Inspect a generated resource and emit a structured description of
everything that belongs to it. Parses Go source files with the stdlib
go/parser, not regex, so the output stays accurate even when file
formatting varies.

Intended for AI agents and humans who need to understand a resource's
shape before modifying it — one command replaces opening six files and
squinting at field names and method signatures.

The resource name is the PascalCase model type name. Example:

  gofasta inspect User
  gofasta inspect Product --json

Checks the standard gofasta layout:
  - app/models/<snake>.model.go       — GORM model fields
  - app/dtos/<snake>.dtos.go          — request/response DTOs
  - app/services/interfaces/<snake>_service.go  — service contract
  - app/rest/controllers/<snake>.controller.go — HTTP handler methods
  - app/rest/routes/<snake>.routes.go — registered REST routes

Missing files are reported as null fields in the JSON payload and
omitted from the text output — the command reports what it finds, not
what it expects.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInspect(args[0])
	},
}

func init() {
	rootCmd.AddCommand(inspectCmd)
}

// inspectedResource is the full structured payload returned by the
// inspect command. The JSON shape is the stable contract agents read;
// adding fields is safe, renaming is a breaking change.
type inspectedResource struct {
	Name           string            `json:"name"`
	Snake          string            `json:"snake"`
	Model          *modelInfo        `json:"model,omitempty"`
	DTOs           []dtoInfo         `json:"dtos,omitempty"`
	Routes         []routeEntry      `json:"routes,omitempty"`
	ServiceMethods []methodSignature `json:"service_methods,omitempty"`
	ControllerMeth []methodSignature `json:"controller_methods,omitempty"`
	Files          []string          `json:"files"`
}

type modelInfo struct {
	File   string       `json:"file"`
	Fields []fieldEntry `json:"fields"`
}

type fieldEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Tag  string `json:"tag,omitempty"`
}

type dtoInfo struct {
	File   string       `json:"file"`
	Name   string       `json:"name"`
	Fields []fieldEntry `json:"fields"`
}

type methodSignature struct {
	Name string `json:"name"`
	Sig  string `json:"signature"`
}

// runInspect is the entry point. Builds an inspectedResource by parsing
// each of the standard-layout files, emits it via cliout.
func runInspect(name string) error {
	if name == "" {
		return clierr.New(clierr.CodeInvalidName, "resource name cannot be empty")
	}
	snake := toSnakeLower(name)
	out := inspectedResource{Name: name, Snake: snake}

	// Each lookup is best-effort: missing files are a valid outcome
	// (you might inspect a resource that has a model but no controller).
	if m, ok := tryParseModel(name, snake); ok {
		out.Model = m
		out.Files = append(out.Files, m.File)
	}
	out.DTOs = append(out.DTOs, tryParseDTOs(snake)...)
	if dtoFile := filepath.Join("app", "dtos", snake+".dtos.go"); fileExists(dtoFile) {
		out.Files = append(out.Files, dtoFile)
	}
	if methods, ok := tryParseInterfaceMethods(
		filepath.Join("app", "services", "interfaces", snake+"_service.go"),
		name+"ServiceInterface",
	); ok {
		out.ServiceMethods = methods
		out.Files = append(out.Files, filepath.Join("app", "services", "interfaces", snake+"_service.go"))
	}
	if methods, ok := tryParseControllerMethods(
		filepath.Join("app", "rest", "controllers", snake+".controller.go"),
		name+"Controller",
	); ok {
		out.ControllerMeth = methods
		out.Files = append(out.Files, filepath.Join("app", "rest", "controllers", snake+".controller.go"))
	}
	if routes := tryParseRoutesForResource(snake); len(routes) > 0 {
		out.Routes = routes
		out.Files = append(out.Files, filepath.Join("app", "rest", "routes", snake+".routes.go"))
	}

	if len(out.Files) == 0 {
		return clierr.Newf(clierr.CodeInvalidName,
			"no files found for resource %q — checked app/models, app/dtos, app/services/interfaces, app/rest/controllers, app/rest/routes",
			name)
	}

	cliout.Print(out, func(w io.Writer) { renderInspectText(w, &out) })
	return nil
}

func renderInspectText(w io.Writer, r *inspectedResource) {
	fprintf(w, "Resource: %s (%s)\n", r.Name, r.Snake)
	fprintln(w)

	if r.Model != nil {
		fprintf(w, "Model (%s)\n", r.Model.File)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, f := range r.Model.Fields {
			fprintf(tw, "  %s\t%s\n", f.Name, f.Type)
		}
		_ = tw.Flush()
		fprintln(w)
	}

	if len(r.DTOs) > 0 {
		fprintln(w, "DTOs")
		for _, d := range r.DTOs {
			fprintf(w, "  %s (%d field(s))\n", d.Name, len(d.Fields))
		}
		fprintln(w)
	}

	if len(r.ServiceMethods) > 0 {
		fprintln(w, "Service methods")
		for _, m := range r.ServiceMethods {
			fprintf(w, "  %s\n", m.Sig)
		}
		fprintln(w)
	}

	if len(r.ControllerMeth) > 0 {
		fprintln(w, "Controller methods")
		for _, m := range r.ControllerMeth {
			fprintf(w, "  %s\n", m.Sig)
		}
		fprintln(w)
	}

	if len(r.Routes) > 0 {
		fprintln(w, "Routes")
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, rt := range r.Routes {
			fprintf(tw, "  %s\t%s\n", rt.Method, rt.Path)
		}
		_ = tw.Flush()
		fprintln(w)
	}

	fprintln(w, "Files")
	for _, f := range r.Files {
		fprintf(w, "  %s\n", f)
	}
}

// --- AST parsers ------------------------------------------------------------

// tryParseModel reads app/models/<snake>.model.go, finds the struct
// with typeName, and returns its fields. Zero-values + false when the
// file doesn't exist or parse fails.
func tryParseModel(typeName, snake string) (*modelInfo, bool) {
	path := filepath.Join("app", "models", snake+".model.go")
	file, err := parseGoFile(path)
	if err != nil {
		return nil, false
	}
	fields := findStructFields(file, typeName)
	if fields == nil {
		return nil, false
	}
	return &modelInfo{File: path, Fields: fields}, true
}

// tryParseDTOs walks every struct declared in app/dtos/<snake>.dtos.go.
// Each struct becomes one dtoInfo entry — the file typically defines
// several (create, update, response, filters, etc.).
func tryParseDTOs(snake string) []dtoInfo {
	path := filepath.Join("app", "dtos", snake+".dtos.go")
	file, err := parseGoFile(path)
	if err != nil {
		return nil
	}
	return extractDTOsFromAST(file, path)
}

// extractDTOsFromAST is the AST-walking half of tryParseDTOs,
// factored out so tests can feed in synthetic ast.File values to
// exercise the defensive "not a TypeSpec" branch (unreachable with
// real Go source but possible with manually-constructed ASTs).
func extractDTOsFromAST(file *ast.File, path string) []dtoInfo {
	var out []dtoInfo
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok.String() != "type" {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			out = append(out, dtoInfo{
				File:   path,
				Name:   ts.Name.Name,
				Fields: readStructFields(st),
			})
		}
	}
	return out
}

// tryParseInterfaceMethods reads a file, finds the interface with the
// given name, and returns its method signatures. Used for service and
// repository contract files.
func tryParseInterfaceMethods(path, ifaceName string) ([]methodSignature, bool) {
	file, err := parseGoFile(path)
	if err != nil {
		return nil, false
	}
	var methods []methodSignature
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok.String() != "type" {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != ifaceName {
				continue
			}
			iface, ok := ts.Type.(*ast.InterfaceType)
			if !ok {
				continue
			}
			for _, m := range iface.Methods.List {
				ft, ok := m.Type.(*ast.FuncType)
				if !ok || len(m.Names) == 0 {
					continue
				}
				methods = append(methods, methodSignature{
					Name: m.Names[0].Name,
					Sig:  m.Names[0].Name + exprToString(ft),
				})
			}
		}
	}
	if methods == nil {
		return nil, false
	}
	return methods, true
}

// tryParseControllerMethods returns every method on the controller
// struct (typeName = "UserController"). Receiver methods with public
// names only — private helpers are hidden from agents.
func tryParseControllerMethods(path, typeName string) ([]methodSignature, bool) {
	file, err := parseGoFile(path)
	if err != nil {
		return nil, false
	}
	var methods []methodSignature
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 {
			continue
		}
		// Receiver like (c *UserController) — strip the pointer.
		recvName := exprToString(fd.Recv.List[0].Type)
		recvName = strings.TrimPrefix(recvName, "*")
		if recvName != typeName {
			continue
		}
		if !ast.IsExported(fd.Name.Name) {
			continue
		}
		methods = append(methods, methodSignature{
			Name: fd.Name.Name,
			Sig:  fd.Name.Name + exprToString(fd.Type),
		})
	}
	if methods == nil {
		return nil, false
	}
	return methods, true
}

// tryParseRoutesForResource reads app/rest/routes/<snake>.routes.go and
// reuses the existing regex-based route extractor to pick out registered
// routes. Cheap and consistent with `gofasta routes` output.
func tryParseRoutesForResource(snake string) []routeEntry {
	path := filepath.Join("app", "rest", "routes", snake+".routes.go")
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	// Routes files register under the apiPrefix that the index file
	// sets via r.Mount("/api/v1", api). We read that once.
	apiPrefix := ""
	if index, err := os.ReadFile(filepath.Join("app", "rest", "routes", "index.routes.go")); err == nil {
		if matches := mountRe.FindSubmatch(index); len(matches) > 1 {
			apiPrefix = string(matches[1])
		}
	}
	return extractRoutes(string(content), apiPrefix, snake+".routes.go")
}

// --- AST helpers ------------------------------------------------------------

// parseGoFile wraps parser.ParseFile with a doc-keeping config and file
// non-existence treatment as a clean error.
func parseGoFile(path string) (*ast.File, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	return parser.ParseFile(fset, path, nil, parser.ParseComments)
}

// findStructFields returns the fields of the named struct type, or nil
// if no such struct is declared in file.
func findStructFields(file *ast.File, name string) []fieldEntry {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok.String() != "type" {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != name {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			return readStructFields(st)
		}
	}
	return nil
}

// readStructFields extracts every field from a struct AST node as
// structured data. Embedded fields (anonymous) use the type as their
// name so downstream consumers can tell them apart from named fields.
func readStructFields(st *ast.StructType) []fieldEntry {
	var fields []fieldEntry
	for _, field := range st.Fields.List {
		typeStr := exprToString(field.Type)
		tag := ""
		if field.Tag != nil {
			tag = strings.Trim(field.Tag.Value, "`")
		}
		if len(field.Names) == 0 {
			// Embedded field.
			fields = append(fields, fieldEntry{
				Name: typeStr,
				Type: typeStr,
				Tag:  tag,
			})
			continue
		}
		for _, n := range field.Names {
			fields = append(fields, fieldEntry{
				Name: n.Name,
				Type: typeStr,
				Tag:  tag,
			})
		}
	}
	return fields
}

// exprToString is a minimal AST printer for the types we care about:
// identifiers, selectors, stars, arrays, slices, maps, and func types.
// Not a full Go formatter — but covers every shape the scaffold emits.
func exprToString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprToString(t.X)
	case *ast.SelectorExpr:
		return exprToString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprToString(t.Elt)
	case *ast.MapType:
		return "map[" + exprToString(t.Key) + "]" + exprToString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "(" + fieldListToString(t.Params) + ") " + fieldListToString(t.Results)
	case *ast.Ellipsis:
		return "..." + exprToString(t.Elt)
	default:
		return "?"
	}
}

func fieldListToString(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	var parts []string
	for _, f := range fl.List {
		t := exprToString(f.Type)
		if len(f.Names) == 0 {
			parts = append(parts, t)
			continue
		}
		for _, n := range f.Names {
			parts = append(parts, n.Name+" "+t)
		}
	}
	return strings.Join(parts, ", ")
}

// --- Local helpers ----------------------------------------------------------

// toSnakeLower converts "UserProfile" to "user_profile". Mirrors the
// generate package's toSnakeCase but scoped to commands to avoid
// importing the generate package (which pulls in every template).
func toSnakeLower(s string) string {
	var out []byte
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				out = append(out, '_')
			}
			out = append(out, byte(r+32))
		} else {
			out = append(out, byte(r))
		}
	}
	return string(out)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
