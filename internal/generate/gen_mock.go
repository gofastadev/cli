// gen_mock.go — testify/mock generator. Walks Go source for interface
// declarations, builds a method-by-method mock that satisfies the
// interface, and writes it to testutil/mocks/<snake>_mock.go.
//
// Two modes:
//
//	gofasta g mock <InterfaceName>   — find one interface by exact name
//	gofasta g mock --all             — refresh every mock under
//	                                   app/services/interfaces and
//	                                   app/repositories/interfaces
//
// The generator is purely additive — it never modifies non-mock files.
// File overwrite of testutil/mocks/<...>_mock.go IS the expected behavior
// (mocks are derived artifacts); use --check to flag drift without
// rewriting (suitable for CI).
package generate

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	goimports "golang.org/x/tools/imports"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/layout"
)

// MockData is what GenMock needs to produce one mock file.
type MockData struct {
	// Interface name as written in source (e.g. "OrderService").
	Interface string
	// Source file the interface was found in. Used to compute the mock's
	// package import (we re-import the interfaces package so the mock can
	// satisfy it).
	SourcePath string
	// ModulePath is the project's Go module path, read from go.mod once.
	ModulePath string
	// PackageImport is the import path of the interface's package
	// (e.g. "irodata/app/services/interfaces").
	PackageImport string
	// PackageAlias is the local name to use for that import inside the
	// mock file (e.g. "ifaces"). Computed so that two imports with the
	// same final segment don't collide.
	PackageAlias string
	// OutPath is where the mock will land.
	OutPath string
	// Methods is the resolved method set.
	Methods []MockMethod
	// ExtraImports are packages referenced by the methods (e.g. "context")
	// that aren't the interface's own package. Each is "alias -> path".
	ExtraImports []MockImport
}

// MockMethod is one method's compiled signature, ready for templating.
type MockMethod struct {
	Name       string
	Params     []MockParam
	Returns    []MockParam
	HasContext bool // first param is a context.Context — convention helps test-style choices.
}

// MockParam is one parameter or return. Name may be empty (return values
// commonly are). TypeExpr is the AST-printed type literal.
type MockParam struct {
	Name string
	Type string
}

// MockImport pairs an alias with an import path.
type MockImport struct {
	Alias string
	Path  string
}

// GenMockOpts tweaks generator behavior at runtime.
type GenMockOpts struct {
	// Check, when true, doesn't write — it returns CodeMockDrift if the
	// generated content differs from the on-disk file.
	Check bool
	// All, when true, walks both interface dirs and regenerates every
	// mock. Interface argument is ignored.
	All bool
}

// GenMock is the main entry point. Resolves the interface (or every
// interface in --all mode), then writes / checks the mock file(s).
func GenMock(interfaceName string, opts GenMockOpts) error {
	module, err := readModulePathForMock()
	if err != nil {
		return err
	}

	if opts.All {
		return regenAllMocks(module, opts)
	}
	if interfaceName == "" {
		return clierr.New(clierr.CodeInvalidName, "interface name required (or pass --all)")
	}
	target, err := findInterface(interfaceName)
	if err != nil {
		return err
	}
	return writeMockForTarget(target, module, opts)
}

// regenAllMocks walks the standard interfaces directories and regenerates
// every mock file. Errors on individual interfaces don't kill the run —
// each is reported via the normal cliout channels by writeMockForTarget.
// Directories scanned depend on layout (layered scans
// app/{services,repositories}/interfaces; feature walks per-resource
// app/<resource>/ for *_iface.go).
func regenAllMocks(module string, opts GenMockOpts) error {
	dirs := layout.Detect().InterfaceDirs()
	anyHit := false
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			targets, err := scanFileForInterfaces(path)
			if err != nil {
				continue
			}
			for _, t := range targets {
				if err := writeMockForTarget(t, module, opts); err != nil {
					return err
				}
				anyHit = true
			}
		}
	}
	if !anyHit {
		scanned := "the project's interface directories"
		if len(dirs) > 0 {
			scanned = strings.Join(dirs, ", ")
		}
		return clierr.Newf(clierr.CodeInterfaceNotFound,
			"no interfaces found under %s", scanned)
	}
	return nil
}

// interfaceTarget is one interface located in the project tree, with the
// parsed metadata needed to render its mock.
type interfaceTarget struct {
	Name       string
	SourcePath string
	PkgName    string // declared package name in the source file
	Methods    []MockMethod
	Imports    []MockImport // every import declared in the source file
}

// findInterface walks the standard interfaces dirs and returns the first
// interface declaration matching name. Returns CodeInterfaceNotFound when
// the name isn't found, CodeAmbiguousSymbol when two files define the
// same interface name. Directories scanned depend on layout.
func findInterface(name string) (interfaceTarget, error) {
	dirs := layout.Detect().InterfaceDirs()
	var hits []interfaceTarget
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			targets, err := scanFileForInterfaces(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			for _, t := range targets {
				if t.Name == name {
					hits = append(hits, t)
				}
			}
		}
	}
	switch len(hits) {
	case 0:
		return interfaceTarget{}, clierr.Newf(clierr.CodeInterfaceNotFound,
			"interface %q not found under app/services/interfaces or app/repositories/interfaces", name)
	case 1:
		return hits[0], nil
	default:
		var paths []string
		for _, h := range hits {
			paths = append(paths, h.SourcePath)
		}
		return interfaceTarget{}, clierr.Newf(clierr.CodeAmbiguousSymbol,
			"interface %q is declared in multiple files: %s", name, strings.Join(paths, ", "))
	}
}

// scanFileForInterfaces returns every interface declared in path, fully
// parsed for mock generation.
func scanFileForInterfaces(path string) ([]interfaceTarget, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, clierr.Wrapf(clierr.CodeASTParseFailed, err, "parsing %s", path)
	}
	fileImports := collectFileImports(f)
	var out []interfaceTarget
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			it, ok := ts.Type.(*ast.InterfaceType)
			if !ok {
				continue
			}
			target, err := buildInterfaceTarget(ts.Name.Name, path, f.Name.Name, fileImports, it)
			if err != nil {
				return nil, err
			}
			out = append(out, target)
		}
	}
	return out, nil
}

// collectFileImports flattens f.Imports into the MockImport shape the
// mock template expects (alias + path).
func collectFileImports(f *ast.File) []MockImport {
	imports := make([]MockImport, 0, len(f.Imports))
	for _, imp := range f.Imports {
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		imports = append(imports, MockImport{
			Alias: alias,
			Path:  strings.Trim(imp.Path.Value, `"`),
		})
	}
	return imports
}

// buildInterfaceTarget converts one parsed interface type into an
// interfaceTarget, walking its method list. Embedded interfaces are
// not supported: silently skipping them used to emit a mock that
// doesn't satisfy the interface — a compile error in the user's tests
// with no hint where it came from — so we fail loudly instead.
func buildInterfaceTarget(name, sourcePath, pkgName string, imports []MockImport, it *ast.InterfaceType) (interfaceTarget, error) {
	t := interfaceTarget{
		Name:       name,
		SourcePath: sourcePath,
		PkgName:    pkgName,
		Imports:    imports,
	}
	for _, fld := range it.Methods.List {
		ft, ok := fld.Type.(*ast.FuncType)
		if !ok || len(fld.Names) == 0 {
			return interfaceTarget{}, clierr.Newf(clierr.CodeMockGenFailed,
				"interface %s embeds another interface — embedded interfaces are not supported yet; flatten the embedded methods into %s or write this mock by hand",
				name, name)
		}
		t.Methods = append(t.Methods, buildMockMethod(fld.Names[0].Name, ft, pkgName))
	}
	return t, nil
}

// buildMockMethod produces one MockMethod from a method's FuncType,
// flattening parameter lists (multi-name fields like `a, b int` become
// two MockParam entries) and detecting whether ctx.Context is first.
// pkgName qualifies package-local type identifiers (see
// qualifyLocalTypes) so the emitted expressions compile inside
// package mocks.
func buildMockMethod(name string, ft *ast.FuncType, pkgName string) MockMethod {
	m := MockMethod{Name: name}
	if ft.Params != nil {
		m.Params = flattenFuncFieldList(ft.Params, pkgName)
	}
	if ft.Results != nil {
		m.Returns = flattenFuncFieldList(ft.Results, pkgName)
	}
	if len(m.Params) > 0 && strings.HasSuffix(m.Params[0].Type, "context.Context") {
		m.HasContext = true
	}
	return m
}

// flattenFuncFieldList turns Go's grouped-name field list (e.g. a, b int)
// into a flat sequence of MockParam{Name, Type} entries.
func flattenFuncFieldList(fl *ast.FieldList, pkgName string) []MockParam {
	var out []MockParam
	for _, fld := range fl.List {
		typ := exprString(qualifyLocalTypes(fld.Type, pkgName))
		if len(fld.Names) == 0 {
			out = append(out, MockParam{Type: typ})
			continue
		}
		for _, n := range fld.Names {
			out = append(out, MockParam{Name: n.Name, Type: typ})
		}
	}
	return out
}

// qualifyLocalTypes rewrites a type expression so it compiles from
// OUTSIDE the interface's package: a bare exported identifier
// (`InboxAttachment`) resolves to the interface package's own scope in
// the source file, so the mock (package mocks) must say
// `interfaces.InboxAttachment`. Bare exported idents are exactly the
// package-local case — every predeclared type is lowercase, and
// cross-package references are already SelectorExprs. Unexported bare
// idents are left alone: a mock in another package could never
// reference them anyway, and qualifying wouldn't fix that.
//
// The rewrite returns fresh nodes for the paths it touches and never
// mutates the parsed file's AST (other consumers may still read it).
func qualifyLocalTypes(e ast.Expr, pkgName string) ast.Expr {
	if e == nil || pkgName == "" {
		return e
	}
	switch n := e.(type) {
	case *ast.Ident:
		if n.IsExported() {
			return &ast.SelectorExpr{X: ast.NewIdent(pkgName), Sel: ast.NewIdent(n.Name)}
		}
		return n
	case *ast.SelectorExpr:
		return n // already qualified (other package) — leave verbatim
	}
	return qualifyCompositeType(e, pkgName)
}

// qualifyCompositeType handles the composite/recursive type nodes for
// qualifyLocalTypes (pointers, slices, maps, channels, generics, func types).
// Split out from qualifyLocalTypes to keep each type switch small. Struct/
// interface literals and anything exotic pass through verbatim.
func qualifyCompositeType(e ast.Expr, pkgName string) ast.Expr {
	switch n := e.(type) {
	case *ast.StarExpr:
		return &ast.StarExpr{X: qualifyLocalTypes(n.X, pkgName)}
	case *ast.ArrayType:
		return &ast.ArrayType{Len: n.Len, Elt: qualifyLocalTypes(n.Elt, pkgName)}
	case *ast.Ellipsis:
		return &ast.Ellipsis{Elt: qualifyLocalTypes(n.Elt, pkgName)}
	case *ast.MapType:
		return &ast.MapType{
			Key:   qualifyLocalTypes(n.Key, pkgName),
			Value: qualifyLocalTypes(n.Value, pkgName),
		}
	case *ast.ChanType:
		return &ast.ChanType{Dir: n.Dir, Value: qualifyLocalTypes(n.Value, pkgName)}
	case *ast.IndexExpr: // generic instantiation with one type arg
		return &ast.IndexExpr{
			X:     qualifyLocalTypes(n.X, pkgName),
			Index: qualifyLocalTypes(n.Index, pkgName),
		}
	case *ast.IndexListExpr: // generic instantiation with several type args
		indices := make([]ast.Expr, len(n.Indices))
		for i, idx := range n.Indices {
			indices[i] = qualifyLocalTypes(idx, pkgName)
		}
		return &ast.IndexListExpr{X: qualifyLocalTypes(n.X, pkgName), Indices: indices}
	case *ast.FuncType:
		return &ast.FuncType{
			Params:  qualifyFieldList(n.Params, pkgName),
			Results: qualifyFieldList(n.Results, pkgName),
		}
	case *ast.ParenExpr:
		return &ast.ParenExpr{X: qualifyLocalTypes(n.X, pkgName)}
	}
	return e
}

// qualifyFieldList maps qualifyLocalTypes over a func-type field list.
func qualifyFieldList(fl *ast.FieldList, pkgName string) *ast.FieldList {
	if fl == nil {
		return nil
	}
	fields := make([]*ast.Field, len(fl.List))
	for i, f := range fl.List {
		fields[i] = &ast.Field{Names: f.Names, Type: qualifyLocalTypes(f.Type, pkgName)}
	}
	return &ast.FieldList{List: fields}
}

// writeMockForTarget renders and writes the mock for one resolved
// interface. Honors opts.Check (drift detection) and the package-wide
// dry-run state.
func writeMockForTarget(t interfaceTarget, module string, opts GenMockOpts) error {
	d := MockData{
		Interface:    t.Name,
		SourcePath:   t.SourcePath,
		ModulePath:   module,
		Methods:      t.Methods,
		PackageAlias: t.PkgName,
	}
	d.PackageImport = deriveImportPath(module, filepath.Dir(t.SourcePath))
	d.OutPath = filepath.Join("testutil", "mocks", toMockSnake(t.Name)+"_mock.go")

	// Extra imports = every import in the source file except the
	// interface's own package (we re-import that as PackageImport).
	for _, imp := range t.Imports {
		if imp.Path == d.PackageImport {
			continue
		}
		d.ExtraImports = append(d.ExtraImports, imp)
	}
	// Add testify/mock — every mock needs it.
	d.ExtraImports = append(d.ExtraImports, MockImport{Path: "github.com/stretchr/testify/mock"})

	body := renderMock(d)

	if opts.Check {
		existing, err := os.ReadFile(d.OutPath)
		if err != nil {
			return clierr.Newf(clierr.CodeMockDrift,
				"mock for %s is missing — run `gofasta g mock --all` to create", t.Name)
		}
		if !bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(body)) {
			return clierr.Newf(clierr.CodeMockDrift,
				"mock %s differs from interface — run `gofasta g mock %s` to regenerate", d.OutPath, t.Name)
		}
		return nil
	}

	// writeOrRecordCreate skips if the file exists — but for mocks the
	// expected behavior is overwrite. Bypass via direct write (honoring
	// dry-run via the same planner machinery).
	if GetDryRun() {
		recordCreate(d.OutPath, len(body))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(d.OutPath), 0o755); err != nil {
		return clierr.Wrap(clierr.CodeFileIO, err, "mkdir "+filepath.Dir(d.OutPath))
	}
	if err := os.WriteFile(d.OutPath, body, 0o644); err != nil {
		return clierr.Wrap(clierr.CodeFileIO, err, "writing "+d.OutPath)
	}
	return nil
}

// renderMock produces the mock file's bytes. The output is gofmt-ed
// before return so the result lands cleanly in the working tree.
// gofmt errors are intentionally swallowed — falling back to the raw
// generator output lets the user see what the generator actually
// produced when a downstream build surfaces the issue.
func renderMock(d MockData) []byte {
	var b bytes.Buffer
	fmt.Fprintln(&b, "// Code generated by `gofasta g mock`. DO NOT EDIT.")
	fmt.Fprintln(&b, "// Regenerate by running `gofasta g mock", d.Interface, "` (or `--all`).")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "package mocks")
	fmt.Fprintln(&b)

	// Imports — sort by path for deterministic output.
	imports := append([]MockImport(nil), d.ExtraImports...)
	if d.PackageImport != "" {
		imports = append(imports, MockImport{Alias: d.PackageAlias, Path: d.PackageImport})
	}
	sort.Slice(imports, func(i, j int) bool { return imports[i].Path < imports[j].Path })
	fmt.Fprintln(&b, "import (")
	for _, imp := range imports {
		if imp.Alias != "" {
			fmt.Fprintf(&b, "\t%s %q\n", imp.Alias, imp.Path)
		} else {
			fmt.Fprintf(&b, "\t%q\n", imp.Path)
		}
	}
	fmt.Fprintln(&b, ")")
	fmt.Fprintln(&b)

	mockType := d.Interface + "Mock"
	fmt.Fprintf(&b, "// %s is a testify/mock implementation of %s.%s.\n", mockType, d.PackageAlias, d.Interface)
	fmt.Fprintf(&b, "type %s struct {\n\tmock.Mock\n}\n\n", mockType)

	// Compile-time check: the mock must satisfy the interface.
	fmt.Fprintf(&b, "var _ %s.%s = (*%s)(nil)\n\n", d.PackageAlias, d.Interface, mockType)

	for _, m := range d.Methods {
		emitMockMethod(&b, mockType, m)
	}

	// imports.Process (goimports) both formats and PRUNES unused
	// imports: the mock re-emits every import from the interface's
	// source file, but many (errors for sentinel docs, packages used
	// only by other declarations in that file) never appear in the
	// method signatures. format.Source alone would leave them and the
	// build would fail.
	// On failure return the unformatted bytes: goimports only fails when the
	// source does not parse, and format.Source uses the same parser — so a
	// gofmt fallback would fail on exactly the same input.
	formatted, err := goimports.Process(d.OutPath, b.Bytes(), nil)
	if err != nil {
		return b.Bytes()
	}
	return formatted
}

// emitMockMethod writes one method on the mock type. testify/mock pattern:
// pass call args to m.Called(), then unpack results from the returned
// Arguments via the right typed accessor for each return.
func emitMockMethod(b *bytes.Buffer, mockType string, m MockMethod) {
	paramList := make([]string, 0, len(m.Params))
	callArgs := make([]string, 0, len(m.Params))
	for i, p := range m.Params {
		name := p.Name
		if name == "" {
			name = fmt.Sprintf("arg%d", i)
		}
		paramList = append(paramList, name+" "+p.Type)
		callArgs = append(callArgs, name)
	}

	returnList := make([]string, 0, len(m.Returns))
	for _, r := range m.Returns {
		returnList = append(returnList, r.Type)
	}
	returnSig := ""
	if len(returnList) == 1 {
		returnSig = " " + returnList[0]
	} else if len(returnList) > 1 {
		returnSig = " (" + strings.Join(returnList, ", ") + ")"
	}

	fmt.Fprintf(b, "func (m *%s) %s(%s)%s {\n",
		mockType, m.Name, strings.Join(paramList, ", "), returnSig)

	if len(returnList) == 0 {
		fmt.Fprintf(b, "\tm.Called(%s)\n", strings.Join(callArgs, ", "))
		fmt.Fprintln(b, "}")
		fmt.Fprintln(b)
		return
	}

	// The local holding testify's Arguments must not collide with a
	// parameter name — an interface method is free to declare `args`.
	argsVar := uniqueArgsName(callArgs)

	fmt.Fprintf(b, "\t%s := m.Called(%s)\n", argsVar, strings.Join(callArgs, ", "))
	pieces := make([]string, len(returnList))
	for i, r := range returnList {
		pieces[i] = mockReturnAccessor(argsVar, i, r)
	}
	fmt.Fprintf(b, "\treturn %s\n", strings.Join(pieces, ", "))
	fmt.Fprintln(b, "}")
	fmt.Fprintln(b)
}

// uniqueArgsName returns "args", or "args" with an underscore prefix repeated
// until it no longer collides with any of the taken names.
func uniqueArgsName(taken []string) string {
	name := "args"
	for {
		collides := false
		for _, t := range taken {
			if t == name {
				collides = true
				break
			}
		}
		if !collides {
			return name
		}
		name = "_" + name
	}
}

// mockReturnAccessor picks the right testify/mock arg accessor for the
// return at position i. error → argsVar.Error(i); built-in → typed
// shortcut; everything else → argsVar.Get(i).(T).
func mockReturnAccessor(argsVar string, i int, t string) string {
	switch strings.TrimSpace(t) {
	case "error":
		return fmt.Sprintf("%s.Error(%d)", argsVar, i)
	case "string":
		return fmt.Sprintf("%s.String(%d)", argsVar, i)
	case "int":
		return fmt.Sprintf("%s.Int(%d)", argsVar, i)
	case "bool":
		return fmt.Sprintf("%s.Bool(%d)", argsVar, i)
	}
	// Anything else may legitimately be unset on the mock call. Guard the
	// type assertion and fall back to the type's zero value: `nil` would be
	// invalid for value types such as uuid.UUID, time.Time or a named
	// struct, all of which are qualified and so reach this branch.
	if strings.HasPrefix(t, "*") || strings.HasPrefix(t, "[]") || strings.HasPrefix(t, "map[") ||
		strings.HasPrefix(t, "chan ") || strings.HasPrefix(t, "func(") || strings.Contains(t, ".") {
		return fmt.Sprintf(
			"func() %s {\n\t\tif v := %s.Get(%d); v != nil {\n\t\t\treturn v.(%s)\n\t\t}\n\t\tvar zero %s\n\t\treturn zero\n\t}()",
			t, argsVar, i, t, t)
	}
	return fmt.Sprintf("%s.Get(%d).(%s)", argsVar, i, t)
}

// ----- helpers -----------------------------------------------------------

// formatNodeFn is a package-level seam over format.Node so tests can
// drive exprString's defensive error branch.
var formatNodeFn = format.Node

// exprString printed-renders an ast.Expr back to source. Used so the
// mock's type expressions match what the interface declared verbatim.
func exprString(e ast.Expr) string {
	var b bytes.Buffer
	if err := formatNodeFn(&b, token.NewFileSet(), e); err != nil {
		return fmt.Sprintf("%v", e)
	}
	return b.String()
}

// readModulePathForMock wraps the package-level readModulePath but
// translates the empty-string sentinel into a structured clierr so the
// mock generator surfaces a useful message when invoked outside a
// gofasta project root.
func readModulePathForMock() (string, error) {
	path := readModulePath()
	if path == "" {
		return "", clierr.New(clierr.CodeNotGofastaProject,
			"go.mod not found or missing module directive")
	}
	return path, nil
}

// deriveImportPath converts a directory relative to the project root
// into a Go import path under the module. Returns "" if the directory
// doesn't sit inside the module's tree.
func deriveImportPath(module, dir string) string {
	clean := filepath.ToSlash(filepath.Clean(dir))
	if clean == "." {
		return module
	}
	return module + "/" + clean
}

// toSnake converts a PascalCase identifier into snake_case. Used for the
// mock file name (e.g. "OrderService" → "order_service").
// toMockSnake is the mock generator's local snake-case helper. The
// shared toSnakeCase already exists in scaffold_data.go but follows
// slightly different rules (e.g. plural handling); this one is
// intentionally simple — PascalCase → snake_case, nothing else.
func toMockSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}
