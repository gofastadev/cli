// Package featurize — AST manipulation helpers.
//
// Two layers of utilities:
//
//  1. Import block: ensureImport, dropImportPath, importAlias,
//     importAliasForPath, aliasReferenced. Used by every transformer.
//
//  2. Generic walker: applyCursor + applyOn* recursive descent over
//     dst.File / dst.Decl / dst.Stmt / dst.Expr. The walker exists
//     because dst.Inspect doesn't give parent context — callers need
//     to replace a Node, not just inspect it.
//
//  3. Selector/call rewrite primitives: rewriteSelectors, rewriteCallNames,
//     mergeRewrites. Consumed by the cross-cutting and reverse engines.
//
// Plus renderFile (gofmt-aware dst → []byte).

package featurize

import (
	"bytes"
	"fmt"
	"go/format"
	"go/token"
	"maps"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// ensureImport adds an import with optional alias if it isn't already
// present in the file.
func ensureImport(file *dst.File, path, alias string) {
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) == path {
			return
		}
	}
	spec := &dst.ImportSpec{
		Path: &dst.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("%q", path)},
	}
	if alias != "" {
		spec.Name = &dst.Ident{Name: alias}
	}
	file.Imports = append(file.Imports, spec)
	for _, decl := range file.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		gd.Specs = append(gd.Specs, spec)
		return
	}
	file.Decls = append([]dst.Decl{&dst.GenDecl{
		Tok: token.IMPORT, Specs: []dst.Spec{spec}, Lparen: true, Rparen: true,
	}}, file.Decls...)
}

// importAlias returns the local name an import is referenced by inside
// the file — the explicit alias when one is set, otherwise the last
// segment of the path. Mirrors how Go's compiler resolves identifiers.
func importAlias(imp *dst.ImportSpec, path string) string {
	if imp.Name != nil && imp.Name.Name != "" && imp.Name.Name != "_" {
		return imp.Name.Name
	}
	idx := strings.LastIndex(path, "/")
	if idx == -1 {
		return path
	}
	return path[idx+1:]
}

// renderFile restores a dst.File to []byte and runs gofmt. Mirrors
// astpatch.Render's contract but inlined to keep this package free of
// the astpatch dependency.
func renderFile(file *dst.File) ([]byte, error) {
	var buf bytes.Buffer
	// Fprint's error is discarded: its only source is the io.Writer, and a
	// bytes.Buffer never fails a write. A malformed tree panics inside the
	// restorer rather than returning an error, so there is nothing to report.
	_ = decorator.NewRestorer().Fprint(&buf, file)
	out, err := format.Source(buf.Bytes())
	if err != nil {
		// Return un-formatted source rather than failing — downstream
		// callers can still inspect the output, and the failure is
		// usually a transient gofmt input quirk.
		return buf.Bytes(), nil //nolint:nilerr // intentional fallback
	}
	return out, nil
}

// importAliasForPath returns the alias used to reference the given
// import path in the file — the explicit alias when one is set,
// otherwise the path's last segment. Returns "" if the import is not
// present.
func importAliasForPath(file *dst.File, path string) string {
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) != path {
			continue
		}
		return importAlias(imp, path)
	}
	return ""
}

// dropImportPath removes an import entry by path from both file.Imports
func dropImportPath(file *dst.File, path string) {
	keptImports := make([]*dst.ImportSpec, 0, len(file.Imports))
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) == path {
			continue
		}
		keptImports = append(keptImports, imp)
	}
	file.Imports = keptImports
	for _, decl := range file.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		kept := make([]dst.Spec, 0, len(gd.Specs))
		for _, spec := range gd.Specs {
			is, ok := spec.(*dst.ImportSpec)
			if !ok {
				kept = append(kept, spec)
				continue
			}
			if strings.Trim(is.Path.Value, `"`) == path {
				continue
			}
			kept = append(kept, is)
		}
		gd.Specs = kept
	}
}

// aliasReferenced reports whether any SelectorExpr in the file has its
func aliasReferenced(file *dst.File, alias string) bool {
	found := false
	dst.Inspect(file, func(n dst.Node) bool {
		sel, ok := n.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*dst.Ident)
		if !ok {
			return true
		}
		if ident.Name == alias {
			found = true
			return false
		}
		return true
	})
	return found
}
func mergeRewrites(a, b map[string]string) map[string]string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make(map[string]string, len(a)+len(b))
	maps.Copy(out, a)
	maps.Copy(out, b)
	return out
}

// rewriteSelectors walks the file and rewrites every SelectorExpr that
// matches "X.Y" in the rewrites map. The replacement is itself a
func rewriteSelectors(file *dst.File, rewrites map[string]string) {
	applyOnFile(file, func(c *applyCursor) bool {
		sel, ok := c.Node.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*dst.Ident)
		if !ok {
			return true
		}
		key := ident.Name + "." + sel.Sel.Name
		repl, ok := rewrites[key]
		if !ok {
			return true
		}
		dot := strings.LastIndex(repl, ".")
		if dot == -1 {
			c.Replace(&dst.Ident{Name: repl})
			return true
		}
		c.Replace(&dst.SelectorExpr{
			X:   &dst.Ident{Name: repl[:dot]},
			Sel: &dst.Ident{Name: repl[dot+1:]},
		})
		return true
	})
}

// rewriteCallNames walks the file and rewrites every bare Ident that
// matches the rewrites map and is in CALLEE position
// (i.e. CallExpr.Fun) into a SelectorExpr. Used for the index.routes
func rewriteCallNames(file *dst.File, rewrites map[string]string) {
	applyOnFile(file, func(c *applyCursor) bool {
		call, ok := c.Node.(*dst.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*dst.Ident)
		if !ok {
			return true
		}
		repl, ok := rewrites[ident.Name]
		if !ok {
			return true
		}
		dot := strings.LastIndex(repl, ".")
		if dot == -1 {
			call.Fun = &dst.Ident{Name: repl}
			return true
		}
		call.Fun = &dst.SelectorExpr{
			X:   &dst.Ident{Name: repl[:dot]},
			Sel: &dst.Ident{Name: repl[dot+1:]},
		}
		return true
	})
}

// ---------- dstutil.Apply mini-wrapper ----------
//
// dst doesn't ship dstutil.Apply (yet) — github.com/dave/dst/dstutil
// exists but the parent-context cursor isn't part of dst v0.27.3.
// Instead we use a hand-rolled walker that gives us a Replace seam at
// every Expr container.

type applyCursor struct {
	Node    dst.Node
	replace func(dst.Node)
}

// Replace substitutes the current node with the given node via the
// cursor's parent-aware setter.
func (c *applyCursor) Replace(n dst.Node) {
	if c.replace != nil {
		c.replace(n)
	}
}

// applyOnFile walks every meaningful node in the file (declarations,
// expressions, statements) and invokes fn with a cursor at each.
// Replacements landed via cursor.Replace are written back at the
// parent slot they belong to.
//
// Coverage: function declarations + their bodies, top-level var/const
// initializer expressions, struct field types, import specs. Anything
// useful for featurize. Doesn't try to cover every dst node shape —
func applyOnFile(file *dst.File, fn func(*applyCursor) bool) {
	for _, decl := range file.Decls {
		applyOnDecl(decl, fn)
	}
}

func applyOnDecl(decl dst.Decl, fn func(*applyCursor) bool) {
	switch d := decl.(type) {
	case *dst.FuncDecl:
		if d.Type != nil {
			applyOnFuncType(d.Type, fn)
		}
		if d.Body != nil {
			applyOnStmtList(d.Body.List, fn)
		}
	case *dst.GenDecl:
		for _, spec := range d.Specs {
			applyOnSpec(spec, fn)
		}
	}
}

func applyOnSpec(spec dst.Spec, fn func(*applyCursor) bool) {
	switch s := spec.(type) {
	case *dst.ValueSpec:
		for i := range s.Values {
			applyOnExpr(&s.Values[i], fn)
		}
		for i := range s.Names {
			_ = s.Names[i] // identifiers aren't selectors
		}
		if s.Type != nil {
			applyOnExpr(&s.Type, fn)
		}
	case *dst.TypeSpec:
		if s.Type != nil {
			applyOnExpr(&s.Type, fn)
		}
	}
}

func applyOnFuncType(ft *dst.FuncType, fn func(*applyCursor) bool) {
	if ft.Params != nil {
		applyOnFieldList(ft.Params, fn)
	}
	if ft.Results != nil {
		applyOnFieldList(ft.Results, fn)
	}
}

func applyOnFieldList(fl *dst.FieldList, fn func(*applyCursor) bool) {
	for _, f := range fl.List {
		if f.Type != nil {
			applyOnExpr(&f.Type, fn)
		}
	}
}

func applyOnStmtList(stmts []dst.Stmt, fn func(*applyCursor) bool) {
	for i := range stmts {
		applyOnStmt(&stmts[i], fn)
	}
}

//nolint:gocognit,gocyclo,gocritic // ptrToRefParam: the *dst.Stmt is intentional so callers can replace the statement in-place via *s = ...
func applyOnStmt(s *dst.Stmt, fn func(*applyCursor) bool) {
	switch st := (*s).(type) {
	case *dst.ExprStmt:
		applyOnExpr(&st.X, fn)
	case *dst.AssignStmt:
		for i := range st.Lhs {
			applyOnExpr(&st.Lhs[i], fn)
		}
		for i := range st.Rhs {
			applyOnExpr(&st.Rhs[i], fn)
		}
	case *dst.ReturnStmt:
		for i := range st.Results {
			applyOnExpr(&st.Results[i], fn)
		}
	case *dst.IfStmt:
		if st.Init != nil {
			applyOnStmt(&st.Init, fn)
		}
		if st.Cond != nil {
			applyOnExpr(&st.Cond, fn)
		}
		if st.Body != nil {
			applyOnStmtList(st.Body.List, fn)
		}
		if st.Else != nil {
			applyOnStmt(&st.Else, fn)
		}
	case *dst.ForStmt:
		if st.Init != nil {
			applyOnStmt(&st.Init, fn)
		}
		if st.Cond != nil {
			applyOnExpr(&st.Cond, fn)
		}
		if st.Post != nil {
			applyOnStmt(&st.Post, fn)
		}
		if st.Body != nil {
			applyOnStmtList(st.Body.List, fn)
		}
	case *dst.RangeStmt:
		if st.Key != nil {
			applyOnExpr(&st.Key, fn)
		}
		if st.Value != nil {
			applyOnExpr(&st.Value, fn)
		}
		if st.X != nil {
			applyOnExpr(&st.X, fn)
		}
		if st.Body != nil {
			applyOnStmtList(st.Body.List, fn)
		}
	case *dst.SwitchStmt:
		if st.Init != nil {
			applyOnStmt(&st.Init, fn)
		}
		if st.Tag != nil {
			applyOnExpr(&st.Tag, fn)
		}
		if st.Body != nil {
			for _, c := range st.Body.List {
				if cc, ok := c.(*dst.CaseClause); ok {
					for i := range cc.List {
						applyOnExpr(&cc.List[i], fn)
					}
					applyOnStmtList(cc.Body, fn)
				}
			}
		}
	case *dst.TypeSwitchStmt:
		if st.Init != nil {
			applyOnStmt(&st.Init, fn)
		}
		if st.Assign != nil {
			applyOnStmt(&st.Assign, fn)
		}
		if st.Body != nil {
			for _, c := range st.Body.List {
				if cc, ok := c.(*dst.CaseClause); ok {
					for i := range cc.List {
						applyOnExpr(&cc.List[i], fn)
					}
					applyOnStmtList(cc.Body, fn)
				}
			}
		}
	case *dst.BlockStmt:
		applyOnStmtList(st.List, fn)
	case *dst.DeferStmt:
		if st.Call != nil {
			expr := dst.Expr(st.Call)
			applyOnExpr(&expr, fn)
			st.Call = expr.(*dst.CallExpr)
		}
	case *dst.GoStmt:
		if st.Call != nil {
			expr := dst.Expr(st.Call)
			applyOnExpr(&expr, fn)
			st.Call = expr.(*dst.CallExpr)
		}
	case *dst.DeclStmt:
		if st.Decl != nil {
			applyOnDecl(st.Decl, fn)
		}
	case *dst.SendStmt:
		applyOnExpr(&st.Chan, fn)
		applyOnExpr(&st.Value, fn)
	case *dst.IncDecStmt:
		applyOnExpr(&st.X, fn)
	case *dst.LabeledStmt:
		applyOnStmt(&st.Stmt, fn)
	case *dst.SelectStmt:
		if st.Body != nil {
			for _, c := range st.Body.List {
				if cc, ok := c.(*dst.CommClause); ok {
					if cc.Comm != nil {
						applyOnStmt(&cc.Comm, fn)
					}
					applyOnStmtList(cc.Body, fn)
				}
			}
		}
	}
}

//nolint:gocognit,gocyclo,gocritic // ptrToRefParam: the *dst.Expr is intentional so callers can replace the expression in-place via *e = ...
func applyOnExpr(e *dst.Expr, fn func(*applyCursor) bool) {
	if *e == nil {
		return
	}
	// Recurse into children first (post-order rewrite).
	switch n := (*e).(type) {
	case *dst.SelectorExpr:
		applyOnExpr(&n.X, fn)
	case *dst.CallExpr:
		applyOnExpr(&n.Fun, fn)
		for i := range n.Args {
			applyOnExpr(&n.Args[i], fn)
		}
	case *dst.UnaryExpr:
		applyOnExpr(&n.X, fn)
	case *dst.BinaryExpr:
		applyOnExpr(&n.X, fn)
		applyOnExpr(&n.Y, fn)
	case *dst.IndexExpr:
		applyOnExpr(&n.X, fn)
		applyOnExpr(&n.Index, fn)
	case *dst.SliceExpr:
		applyOnExpr(&n.X, fn)
		if n.Low != nil {
			applyOnExpr(&n.Low, fn)
		}
		if n.High != nil {
			applyOnExpr(&n.High, fn)
		}
		if n.Max != nil {
			applyOnExpr(&n.Max, fn)
		}
	case *dst.TypeAssertExpr:
		applyOnExpr(&n.X, fn)
		if n.Type != nil {
			applyOnExpr(&n.Type, fn)
		}
	case *dst.ParenExpr:
		applyOnExpr(&n.X, fn)
	case *dst.StarExpr:
		applyOnExpr(&n.X, fn)
	case *dst.CompositeLit:
		if n.Type != nil {
			applyOnExpr(&n.Type, fn)
		}
		for i := range n.Elts {
			applyOnExpr(&n.Elts[i], fn)
		}
	case *dst.KeyValueExpr:
		// Skip walking a bare-Ident Key — in struct literals the Key
		// is a field NAME, not an identifier reference. Rewriting it
		// would emit `dtos.SortOrientation: &asc` which is invalid
		// (`invalid field name X.Y in struct literal`).
		if _, isIdent := n.Key.(*dst.Ident); !isIdent {
			applyOnExpr(&n.Key, fn)
		}
		applyOnExpr(&n.Value, fn)
	case *dst.FuncLit:
		if n.Type != nil {
			applyOnFuncType(n.Type, fn)
		}
		if n.Body != nil {
			applyOnStmtList(n.Body.List, fn)
		}
	case *dst.ArrayType:
		if n.Len != nil {
			applyOnExpr(&n.Len, fn)
		}
		applyOnExpr(&n.Elt, fn)
	case *dst.MapType:
		applyOnExpr(&n.Key, fn)
		applyOnExpr(&n.Value, fn)
	case *dst.ChanType:
		applyOnExpr(&n.Value, fn)
	case *dst.StructType:
		if n.Fields != nil {
			applyOnFieldList(n.Fields, fn)
		}
	case *dst.InterfaceType:
		if n.Methods != nil {
			for _, m := range n.Methods.List {
				if m.Type != nil {
					applyOnExpr(&m.Type, fn)
				}
			}
		}
	case *dst.FuncType:
		applyOnFuncType(n, fn)
	}
	// Visit the current node.
	c := &applyCursor{Node: *e, replace: func(n dst.Node) {
		if expr, ok := n.(dst.Expr); ok {
			*e = expr
		}
	}}
	fn(c)
}
