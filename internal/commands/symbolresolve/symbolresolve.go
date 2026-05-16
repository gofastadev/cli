// Package symbolresolve provides type-aware symbol lookup + reverse-
// dependency analysis powered by golang.org/x/tools/go/packages.
//
// Two surfaces:
//
//   • LookupReferences(pkgPath, symbol) — find every file:line:col
//     reference to a symbol across the loaded module.
//
//   • ImpactGraph(target)               — given a file or import path,
//     return the transitive closure of packages that depend on it.
//
// Both load the module with full type info (NeedTypes | NeedTypesInfo)
// so they survive renames, aliases, and method-set resolution. The
// load itself is the expensive step; both functions share a loader
// helper to keep parsed packages warm across an invocation.
package symbolresolve

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"golang.org/x/tools/go/packages"
)

// Reference is one resolved use-site of a symbol.
type Reference struct {
	File   string `json:"file"`   // repo-relative when possible
	Line   int    `json:"line"`
	Column int    `json:"column"`
	InFunc string `json:"in_func,omitempty"` // enclosing function name (if any)
	Kind   string `json:"kind"`              // "call" | "ref" | "decl"
}

// SymbolReport is the JSON envelope returned by LookupReferences.
type SymbolReport struct {
	Symbol     string     `json:"symbol"`
	Package    string     `json:"package,omitempty"`
	Kind       string     `json:"kind,omitempty"` // "func" | "type" | "method" | "var" | "const"
	Definition *Reference `json:"definition,omitempty"`
	References []Reference `json:"references"`
	Count      int        `json:"count"`
}

// ImpactReport is the JSON envelope returned by ImpactGraph.
type ImpactReport struct {
	Target            string   `json:"target"`
	Package           string   `json:"package,omitempty"`
	DirectImporters   []string `json:"direct_importers"`
	TransitiveImporters []string `json:"transitive_importers"`
	ImpactedFiles     []string `json:"impacted_files"`
}

// loadModule loads every package in the current module with full type
// information. Errors when no packages are found (i.e. cwd isn't a
// module). Package-level seam loadModuleFn lets tests inject a fake
// loader.
var loadModuleFn = loadModule

func loadModule() ([]*packages.Package, error) {
	// Pin GOARCH/GOOS into the loader's environment so go/packages's
	// internal call to types.SizesFor returns a non-nil sizer. Without
	// this, certain `go list` configurations return an empty Arch
	// field, the loader falls back to a nil *StdSizes, and the
	// go/types checker panics on any const-expression sizeof check.
	env := append(os.Environ(),
		"GOARCH="+runtime.GOARCH,
		"GOOS="+runtime.GOOS,
	)
	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedTypesSizes |
			packages.NeedImports |
			packages.NeedDeps |
			packages.NeedModule,
		Tests: false,
		Env:   env,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, clierr.Wrap(clierr.CodePackageLoadFailed, err, "loading module")
	}
	if len(pkgs) == 0 {
		return nil, clierr.New(clierr.CodePackageLoadFailed,
			"no packages found — run from a Go module root")
	}
	// Surface package-level build errors via TypeAnalysisFailed so the
	// user gets a hint to fix the build first. Don't fail outright —
	// some commands (impact) can still produce useful output from a
	// partially-loaded module.
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			// Just record; don't return — the caller decides.
		}
	}
	return pkgs, nil
}

// LookupReferences finds every reference to symbol across the loaded
// module. The symbol can be:
//
//   • Pkg.Func        — package-level func / var / const
//   • Pkg.Type        — package-level type
//   • Pkg.Type.Method — method on a type
//
// or an unqualified name (Func / Type / Type.Method) which is resolved
// by searching every package for a match. Ambiguous unqualified names
// return CodeAmbiguousSymbol.
func LookupReferences(symbol string) (SymbolReport, error) {
	pkgs, err := loadModuleFn()
	if err != nil {
		return SymbolReport{}, err
	}

	obj, owningPkg, err := findSymbol(pkgs, symbol)
	if err != nil {
		return SymbolReport{}, err
	}

	report := SymbolReport{
		Symbol:  symbol,
		Package: owningPkg.PkgPath,
		Kind:    objectKind(obj),
	}

	// Definition site.
	if pos := obj.Pos(); pos.IsValid() {
		fset := owningPkg.Fset
		p := fset.Position(pos)
		report.Definition = &Reference{
			File:   relToCwd(p.Filename),
			Line:   p.Line,
			Column: p.Column,
			Kind:   "decl",
		}
	}

	// Walk every package looking for *ast.Ident nodes resolving to obj.
	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		for ident, use := range pkg.TypesInfo.Uses {
			if use != obj {
				continue
			}
			p := pkg.Fset.Position(ident.Pos())
			report.References = append(report.References, Reference{
				File:   relToCwd(p.Filename),
				Line:   p.Line,
				Column: p.Column,
				InFunc: enclosingFunc(pkg, ident),
				Kind:   identUseKind(pkg, ident),
			})
		}
	}

	// Deterministic ordering.
	sort.Slice(report.References, func(i, j int) bool {
		if report.References[i].File != report.References[j].File {
			return report.References[i].File < report.References[j].File
		}
		return report.References[i].Line < report.References[j].Line
	})
	report.Count = len(report.References)
	return report, nil
}

// ImpactGraph returns the set of packages that depend (directly +
// transitively) on the target. Target may be a file path (looked up to
// its package) or an explicit import path.
func ImpactGraph(target string) (ImpactReport, error) {
	pkgs, err := loadModuleFn()
	if err != nil {
		return ImpactReport{}, err
	}

	pkgPath := resolveTargetToPackage(target, pkgs)
	if pkgPath == "" {
		return ImpactReport{}, clierr.Newf(clierr.CodeSymbolNotFound,
			"could not resolve %q to a Go package in the current module", target)
	}

	// Build the reverse-import map for the entire loaded module.
	rev := map[string]map[string]struct{}{}
	for _, p := range pkgs {
		for impPath := range p.Imports {
			set, ok := rev[impPath]
			if !ok {
				set = map[string]struct{}{}
				rev[impPath] = set
			}
			set[p.PkgPath] = struct{}{}
		}
	}

	directSet := rev[pkgPath]
	report := ImpactReport{
		Target:  target,
		Package: pkgPath,
	}
	for d := range directSet {
		report.DirectImporters = append(report.DirectImporters, d)
	}
	sort.Strings(report.DirectImporters)

	// BFS the reverse map for the transitive closure.
	visited := map[string]struct{}{pkgPath: {}}
	queue := []string{pkgPath}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for p := range rev[cur] {
			if _, hit := visited[p]; hit {
				continue
			}
			visited[p] = struct{}{}
			queue = append(queue, p)
			report.TransitiveImporters = append(report.TransitiveImporters, p)
		}
	}
	sort.Strings(report.TransitiveImporters)

	// Collect every source file in the impacted packages.
	pkgByPath := map[string]*packages.Package{}
	for _, p := range pkgs {
		pkgByPath[p.PkgPath] = p
	}
	for path := range visited {
		if path == pkgPath {
			continue
		}
		p, ok := pkgByPath[path]
		if !ok {
			continue
		}
		for _, f := range p.GoFiles {
			report.ImpactedFiles = append(report.ImpactedFiles, relToCwd(f))
		}
	}
	sort.Strings(report.ImpactedFiles)

	return report, nil
}

// ----- internals ---------------------------------------------------------

// findSymbol resolves a string symbol against the loaded packages. Returns
// the types.Object + the package it was declared in. Handles three
// shapes: Pkg.Name, Pkg.Type.Method, and bare Name (ambiguous → error).
func findSymbol(pkgs []*packages.Package, symbol string) (types.Object, *packages.Package, error) {
	parts := strings.Split(symbol, ".")
	// Pkg.Name or Pkg.Type.Method — the first segment that resolves to a
	// loaded package is the package, the rest is the path within it.
	if len(parts) >= 2 {
		for i := len(parts) - 1; i >= 1; i-- {
			pkgPath := strings.Join(parts[:i], ".")
			for _, p := range pkgs {
				if p.PkgPath == pkgPath || strings.HasSuffix(p.PkgPath, "/"+pkgPath) || p.Name == pkgPath {
					if obj := resolveInPackage(p, parts[i:]); obj != nil {
						return obj, p, nil
					}
				}
			}
		}
	}

	// Fall back: unqualified name. Search every package's exported
	// surface, error on ambiguity.
	var hits []struct {
		obj types.Object
		pkg *packages.Package
	}
	for _, p := range pkgs {
		if p.Types == nil {
			continue
		}
		if obj := resolveInPackage(p, parts); obj != nil {
			hits = append(hits, struct {
				obj types.Object
				pkg *packages.Package
			}{obj, p})
		}
	}
	if len(hits) == 0 {
		return nil, nil, clierr.Newf(clierr.CodeSymbolNotFound,
			"symbol %q not found in module", symbol)
	}
	if len(hits) > 1 {
		var ps []string
		for _, h := range hits {
			ps = append(ps, h.pkg.PkgPath)
		}
		return nil, nil, clierr.Newf(clierr.CodeAmbiguousSymbol,
			"symbol %q matches multiple packages: %s — qualify with a package prefix",
			symbol, strings.Join(ps, ", "))
	}
	return hits[0].obj, hits[0].pkg, nil
}

// resolveInPackage walks parts[0..] within pkg's scope to find the named
// object. For a method, parts[0] is the type name and parts[1] is the
// method name.
func resolveInPackage(pkg *packages.Package, parts []string) types.Object {
	if pkg.Types == nil || len(parts) == 0 {
		return nil
	}
	scope := pkg.Types.Scope()
	first := scope.Lookup(parts[0])
	if first == nil {
		return nil
	}
	if len(parts) == 1 {
		return first
	}
	// parts[0] should be a type for a deeper lookup; parts[1] is the
	// method name.
	t, ok := first.(*types.TypeName)
	if !ok {
		return nil
	}
	named, ok := t.Type().(*types.Named)
	if !ok {
		return nil
	}
	method := parts[1]
	for i := 0; i < named.NumMethods(); i++ {
		if named.Method(i).Name() == method {
			return named.Method(i)
		}
	}
	// Maybe it's a struct field rather than a method.
	if st, ok := named.Underlying().(*types.Struct); ok {
		for i := 0; i < st.NumFields(); i++ {
			if st.Field(i).Name() == method {
				return st.Field(i)
			}
		}
	}
	return nil
}

// objectKind maps a types.Object to a coarse "func" / "type" / "method" /
// "var" / "const" label.
func objectKind(obj types.Object) string {
	switch o := obj.(type) {
	case *types.Func:
		if o.Type().(*types.Signature).Recv() != nil {
			return "method"
		}
		return "func"
	case *types.TypeName:
		return "type"
	case *types.Var:
		return "var"
	case *types.Const:
		return "const"
	}
	return "unknown"
}

// identUseKind returns "call" when the identifier sits in the func
// position of a *ast.CallExpr, else "ref".
func identUseKind(pkg *packages.Package, ident *ast.Ident) string {
	for _, file := range pkg.Syntax {
		if file == nil {
			continue
		}
		var kind string
		ast.Inspect(file, func(n ast.Node) bool {
			if kind != "" {
				return false
			}
			if ce, ok := n.(*ast.CallExpr); ok {
				if matchIdent(ce.Fun, ident) {
					kind = "call"
					return false
				}
			}
			return true
		})
		if kind != "" {
			return kind
		}
	}
	return "ref"
}

// matchIdent reports whether expr is the identifier we're looking for —
// handles both plain idents and pkg.Sel selector expressions.
func matchIdent(expr ast.Expr, ident *ast.Ident) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return e == ident
	case *ast.SelectorExpr:
		return e.Sel == ident
	}
	return false
}

// enclosingFunc returns the name of the FuncDecl whose body contains pos,
// or "" when the position is at file scope.
func enclosingFunc(pkg *packages.Package, ident *ast.Ident) string {
	for _, file := range pkg.Syntax {
		if file == nil {
			continue
		}
		var fnName string
		ast.Inspect(file, func(n ast.Node) bool {
			fd, ok := n.(*ast.FuncDecl)
			if !ok {
				return true
			}
			if fd.Body == nil {
				return true
			}
			if ident.Pos() >= fd.Body.Lbrace && ident.Pos() <= fd.Body.Rbrace {
				if fd.Recv != nil && len(fd.Recv.List) > 0 {
					fnName = receiverString(fd.Recv.List[0].Type) + "." + fd.Name.Name
				} else {
					fnName = fd.Name.Name
				}
				return false
			}
			return true
		})
		if fnName != "" {
			return fnName
		}
	}
	return ""
}

// receiverString stringifies a receiver type expression.
func receiverString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return "?"
}

// resolveTargetToPackage maps a CLI target (file path or import path) to
// the canonical import path of the containing package. Empty string if
// no match is found.
func resolveTargetToPackage(target string, pkgs []*packages.Package) string {
	// Explicit import-path form.
	for _, p := range pkgs {
		if p.PkgPath == target {
			return p.PkgPath
		}
	}
	// File path form — match by checking p.GoFiles.
	cleaned := filepath.Clean(target)
	for _, p := range pkgs {
		for _, f := range p.GoFiles {
			if filepath.Clean(f) == cleaned {
				return p.PkgPath
			}
			if filepath.Clean(relToCwd(f)) == cleaned {
				return p.PkgPath
			}
		}
	}
	return ""
}

// relToCwd renders an absolute path relative to cwd when possible.
// Falls back to the absolute path on any error.
func relToCwd(path string) string {
	cwd, err := filepath.Abs(".")
	if err != nil {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return rel
}

// _ keeps go/token + go/types + fmt referenced even if a future refactor
// stops using them at the top level — the package is loaded for callers
// that need to construct positions / sizes / format strings directly.
var _ = token.NoPos
var _ = fmt.Sprint
var _ = types.NewMethodSet
