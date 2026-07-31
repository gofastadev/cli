// Package astpatch is the shared AST helper that powers gofasta's
// modify-aware generators: g method, g field, g endpoint, g middleware,
// g repo-method, g relation, g rename.
//
// Why dst over stdlib go/ast: dst (decorated syntax tree) preserves
// comment attachments and blank lines through the parse → modify → print
// round-trip. Stdlib go/ast loses that information, so a "modify one
// method" edit ends up reformatting the whole file in a way that makes
// the diff unreadable.
//
// The helpers in this package are intentionally small and composable —
// each generator owns its own template fragment of "new code to insert,"
// while astpatch handles the surgery (find the target, splice in the
// new node, write back, gofmt).
package astpatch

import (
	"bytes"
	"fmt"
	"go/format"
	"go/token"
	"os"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/gofastadev/cli/internal/clierr"
)

// File bundles a parsed dst.File with its decorator so callers can
// modify nodes and write back without re-parsing.
type File struct {
	Path string
	Dst  *dst.File
	Dec  *decorator.Decorator
}

// Parse reads path, parses it with dst, and returns the wrapper. Returns
// CodeASTParseFailed wrapped around the underlying error on syntax
// problems — the user gets a useful "the file has a syntax error" hint.
func Parse(path string) (*File, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, clierr.Wrap(clierr.CodeFileIO, err, "reading "+path)
	}
	dec := decorator.NewDecorator(token.NewFileSet())
	df, err := dec.Parse(src)
	if err != nil {
		return nil, clierr.Wrapf(clierr.CodeASTParseFailed, err, "parsing %s", path)
	}
	return &File{Path: path, Dst: df, Dec: dec}, nil
}

// restorerFprintFn is a package-level seam over decorator.NewRestorer().Fprint
// so tests can inject a failure into the otherwise-unreachable
// "restoring dst file" error branch.
var restorerFprintFn = func(w *bytes.Buffer, df *dst.File) error {
	return decorator.NewRestorer().Fprint(w, df)
}

// Render restores the dst.File to bytes and runs gofmt. Returns the
// rendered body without writing anywhere — useful for plan-mode previews
// and tests.
func Render(f *File) ([]byte, error) {
	var buf bytes.Buffer
	if err := restorerFprintFn(&buf, f.Dst); err != nil {
		return nil, clierr.Wrap(clierr.CodeASTPatchFailed, err, "restoring dst file")
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		// A gofmt failure here means the patched tree renders to invalid
		// Go — writing it back would corrupt the user's file, so fail
		// loudly instead of handing unformatted bytes to the caller.
		return nil, clierr.Wrap(clierr.CodeASTPatchFailed, err, "gofmt of patched "+f.Path)
	}
	return formatted, nil
}

// FindInterface walks decls looking for an interface declaration named
// name. Returns CodeASTPatchFailed when the file has no such declaration.
func FindInterface(f *File, name string) (*dst.InterfaceType, error) {
	for _, decl := range f.Dst.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*dst.TypeSpec)
			if !ok || ts.Name.Name != name {
				continue
			}
			if it, ok := ts.Type.(*dst.InterfaceType); ok {
				return it, nil
			}
		}
	}
	return nil, clierr.Newf(clierr.CodeASTPatchFailed,
		"no interface named %q in %s", name, f.Path)
}

// FindStruct walks decls looking for a struct type declaration. Mirror
// of FindInterface for the modify-aware field-add generator.
func FindStruct(f *File, name string) (*dst.StructType, error) {
	for _, decl := range f.Dst.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*dst.TypeSpec)
			if !ok || ts.Name.Name != name {
				continue
			}
			if st, ok := ts.Type.(*dst.StructType); ok {
				return st, nil
			}
		}
	}
	return nil, clierr.Newf(clierr.CodeASTPatchFailed,
		"no struct named %q in %s", name, f.Path)
}

// FindFunc returns the top-level (or method) function declaration matching
// recv + name. Pass "" for recv to match a package-level function.
func FindFunc(f *File, recv, name string) (*dst.FuncDecl, error) {
	for _, decl := range f.Dst.Decls {
		fd, ok := decl.(*dst.FuncDecl)
		if !ok || fd.Name.Name != name {
			continue
		}
		if recv == "" {
			if fd.Recv == nil {
				return fd, nil
			}
			continue
		}
		if fd.Recv == nil || len(fd.Recv.List) == 0 {
			continue
		}
		got := receiverTypeName(fd.Recv.List[0].Type)
		if got == recv {
			return fd, nil
		}
	}
	return nil, clierr.Newf(clierr.CodeASTPatchFailed,
		"no func %s.%s in %s", recv, name, f.Path)
}

// InterfaceHasMethod reports whether the interface already declares a
// method by this name. Used by the modify-aware generators as the
// idempotency check before appending.
func InterfaceHasMethod(it *dst.InterfaceType, name string) bool {
	if it.Methods == nil {
		return false
	}
	for _, fld := range it.Methods.List {
		for _, n := range fld.Names {
			if n.Name == name {
				return true
			}
		}
	}
	return false
}

// StructHasField reports whether the struct already declares a field by
// this name (case-sensitive).
func StructHasField(st *dst.StructType, name string) bool {
	if st.Fields == nil {
		return false
	}
	for _, fld := range st.Fields.List {
		for _, n := range fld.Names {
			if n.Name == name {
				return true
			}
		}
	}
	return false
}

// AppendInterfaceMethod parses methodSrc as one interface method (e.g.
//
//	"Archive(ctx context.Context, id uuid.UUID) error"
//
// ) and appends it to the interface. Returns CodeASTPatchFailed if the
// fragment doesn't parse as a valid method signature.
func AppendInterfaceMethod(it *dst.InterfaceType, methodSrc string) error {
	if it.Methods == nil {
		it.Methods = &dst.FieldList{}
	}
	wrapped := "package x\ntype _t interface { " + methodSrc + " }\n"
	found, err := extractFirstSpec[*dst.InterfaceType](wrapped, methodSrc, "method signature")
	if err != nil {
		return err
	}
	if found.Methods != nil {
		it.Methods.List = append(it.Methods.List, found.Methods.List...)
	}
	return nil
}

// AppendStructField parses fieldSrc and appends it to the struct. Field
// source is a single line like:
//
//	"Archived bool `gorm:\"not null;default:false\"`"
//
// Tags should be backtick-quoted exactly as they would appear in source.
func AppendStructField(st *dst.StructType, fieldSrc string) error {
	if st.Fields == nil {
		st.Fields = &dst.FieldList{}
	}
	wrapped := "package x\ntype _t struct { " + fieldSrc + " }\n"
	found, err := extractFirstSpec[*dst.StructType](wrapped, fieldSrc, "field declaration")
	if err != nil {
		return err
	}
	if found.Fields != nil {
		st.Fields.List = append(st.Fields.List, found.Fields.List...)
	}
	return nil
}

// extractFirstSpec parses wrapped (a synthetic single-decl Go file) and
// returns the first TypeSpec.Type matching T. kind is a human label
// ("method signature", "field declaration") used in the error message
// so callers don't all duplicate the same parse-failure boilerplate.
//
// Deduplicates the two near-identical helpers AppendInterfaceMethod and
// AppendStructField used (separate functions remain because exposing
// generic helpers in a public API is uglier than wrapping them).
func extractFirstSpec[T dst.Expr](wrapped, source, kind string) (T, error) {
	var zero T
	dec := decorator.NewDecorator(nil)
	df, err := dec.Parse([]byte(wrapped))
	if err != nil {
		return zero, clierr.Wrapf(clierr.CodeASTPatchFailed, err,
			"parsing %s %q", kind, strings.TrimSpace(source))
	}
	for _, decl := range df.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*dst.TypeSpec)
			if !ok {
				continue
			}
			if got, ok := ts.Type.(T); ok {
				return got, nil
			}
		}
	}
	return zero, clierr.Newf(clierr.CodeASTPatchFailed,
		"could not extract %s from %q", kind, source)
}

// AppendFuncDecl parses declSrc (a complete top-level function with body)
// and appends it to the file's declarations.
func AppendFuncDecl(f *File, declSrc string) error {
	wrapped := "package " + f.Dst.Name.Name + "\n" + declSrc + "\n"
	dec := decorator.NewDecorator(nil)
	df, err := dec.Parse([]byte(wrapped))
	if err != nil {
		return clierr.Wrapf(clierr.CodeASTPatchFailed, err,
			"parsing function declaration")
	}
	for _, decl := range df.Decls {
		if fd, ok := decl.(*dst.FuncDecl); ok {
			f.Dst.Decls = append(f.Dst.Decls, fd)
			return nil
		}
	}
	return clierr.New(clierr.CodeASTPatchFailed,
		"declSrc did not contain a FuncDecl")
}

// EnsureImport adds an import to the file if it isn't already present.
// Returns true when an import was added (the caller may then mark a
// patch action accordingly).
func EnsureImport(f *File, importPath string) bool {
	for _, imp := range f.Dst.Imports {
		if strings.Trim(imp.Path.Value, `"`) == importPath {
			return false
		}
	}
	newImport := &dst.ImportSpec{
		Path: &dst.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("%q", importPath)},
	}
	// Find an existing import GenDecl to extend; otherwise create one.
	for _, decl := range f.Dst.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		gd.Specs = append(gd.Specs, newImport)
		// A single-line `import "x"` decl has no parens; appending a
		// second spec without them renders invalid Go.
		gd.Lparen = true
		gd.Rparen = true
		f.Dst.Imports = append(f.Dst.Imports, newImport)
		return true
	}
	imp := &dst.GenDecl{
		Tok:    token.IMPORT,
		Specs:  []dst.Spec{newImport},
		Lparen: true,
		Rparen: true,
	}
	f.Dst.Decls = append([]dst.Decl{imp}, f.Dst.Decls...)
	f.Dst.Imports = append(f.Dst.Imports, newImport)
	return true
}

// receiverTypeName returns the type name from a receiver expression like
// "*OrderController" → "OrderController", "OrderController" →
// "OrderController". Used by FindFunc to match by receiver.
func receiverTypeName(expr dst.Expr) string {
	switch e := expr.(type) {
	case *dst.Ident:
		return e.Name
	case *dst.StarExpr:
		if id, ok := e.X.(*dst.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// parseExprFragment parses exprSrc as a single Go expression by wrapping
// it in a synthetic var declaration. Shared by the composite-literal
// helpers below.
func parseExprFragment(exprSrc string) (dst.Expr, error) {
	wrapped := "package x\nvar _ = " + exprSrc + "\n"
	dec := decorator.NewDecorator(nil)
	df, err := dec.Parse([]byte(wrapped))
	if err != nil {
		return nil, clierr.Wrapf(clierr.CodeASTPatchFailed, err,
			"parsing expression %q", strings.TrimSpace(exprSrc))
	}
	for _, decl := range df.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			if vs, ok := spec.(*dst.ValueSpec); ok && len(vs.Values) > 0 {
				return vs.Values[0], nil
			}
		}
	}
	return nil, clierr.Newf(clierr.CodeASTPatchFailed,
		"could not extract expression from %q", exprSrc)
}

// FindVarCompositeLit locates a package-level `var <name> = T{...}`
// declaration and returns its composite literal. Powers the allowlist
// patches (`<lower>SortColumns`, `<lower>FilterColumns`).
func FindVarCompositeLit(f *File, name string) (*dst.CompositeLit, error) {
	for _, decl := range f.Dst.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*dst.ValueSpec)
			if !ok {
				continue
			}
			for i, n := range vs.Names {
				if n.Name != name || i >= len(vs.Values) {
					continue
				}
				if cl, ok := vs.Values[i].(*dst.CompositeLit); ok {
					return cl, nil
				}
			}
		}
	}
	return nil, clierr.Newf(clierr.CodeASTPatchFailed,
		"no composite-literal var %s in %s", name, f.Path)
}

// CompositeLitHasString reports whether lit contains the quoted string
// element val (e.g. an allowlist already carrying "sku").
func CompositeLitHasString(lit *dst.CompositeLit, val string) bool {
	quoted := strconv.Quote(val)
	for _, el := range lit.Elts {
		if bl, ok := el.(*dst.BasicLit); ok && bl.Kind == token.STRING && bl.Value == quoted {
			return true
		}
	}
	return false
}

// AppendStringToCompositeLit appends the quoted string element val to
// lit on its own line.
func AppendStringToCompositeLit(lit *dst.CompositeLit, val string) {
	el := &dst.BasicLit{Kind: token.STRING, Value: strconv.Quote(val)}
	el.Decorations().Before = dst.NewLine
	el.Decorations().After = dst.NewLine
	lit.Elts = append(lit.Elts, el)
}

// FirstCompositeLitInFunc returns the first composite literal in fn's
// body in source order. The scaffold's mapper functions (FromModel,
// ToCreateInput, ToPatch, ToFilter, the test fixture builders) each
// build exactly one struct literal, and it is the first composite in
// the body — which makes "first" a stable anchor for field insertion.
func FirstCompositeLitInFunc(fn *dst.FuncDecl) (*dst.CompositeLit, error) {
	var found *dst.CompositeLit
	dst.Inspect(fn.Body, func(n dst.Node) bool {
		if found != nil {
			return false
		}
		if cl, ok := n.(*dst.CompositeLit); ok {
			found = cl
			return false
		}
		return true
	})
	if found == nil {
		return nil, clierr.Newf(clierr.CodeASTPatchFailed,
			"no composite literal inside func %s", fn.Name.Name)
	}
	return found, nil
}

// CompositeLitHasKey reports whether lit already has a Key: value entry
// for the given key identifier.
func CompositeLitHasKey(lit *dst.CompositeLit, key string) bool {
	for _, el := range lit.Elts {
		kv, ok := el.(*dst.KeyValueExpr)
		if !ok {
			continue
		}
		if id, ok := kv.Key.(*dst.Ident); ok && id.Name == key {
			return true
		}
	}
	return false
}

// AppendKeyValueToCompositeLit parses exprSrc and appends `key: <expr>,`
// to lit on its own line.
func AppendKeyValueToCompositeLit(lit *dst.CompositeLit, key, exprSrc string) error {
	val, err := parseExprFragment(exprSrc)
	if err != nil {
		return err
	}
	kv := &dst.KeyValueExpr{Key: dst.NewIdent(key), Value: val}
	kv.Decorations().Before = dst.NewLine
	kv.Decorations().After = dst.NewLine
	lit.Elts = append(lit.Elts, kv)
	return nil
}

// InsertStmtBeforeReturn parses stmtSrc (one or more statements) and
// inserts them immediately before the LAST top-level return in fn's
// body. Powers the AsMap / AsRepoFilter patches, whose shape is a run
// of `if p.X != nil { out["x"] = *p.X }` blocks followed by
// `return out`. Falls back to appending when the body has no return.
func InsertStmtBeforeReturn(fn *dst.FuncDecl, stmtSrc string) error {
	wrapped := "package x\nfunc _f() {\n" + stmtSrc + "\n}\n"
	dec := decorator.NewDecorator(nil)
	df, err := dec.Parse([]byte(wrapped))
	if err != nil {
		return clierr.Wrapf(clierr.CodeASTPatchFailed, err,
			"parsing statement %q", strings.TrimSpace(stmtSrc))
	}
	var stmts []dst.Stmt
	for _, decl := range df.Decls {
		if fd, ok := decl.(*dst.FuncDecl); ok {
			stmts = fd.Body.List
			break
		}
	}
	if len(stmts) == 0 {
		return clierr.Newf(clierr.CodeASTPatchFailed,
			"no statements extracted from %q", stmtSrc)
	}
	retIdx := -1
	for i, s := range fn.Body.List {
		if _, ok := s.(*dst.ReturnStmt); ok {
			retIdx = i
		}
	}
	if retIdx == -1 {
		fn.Body.List = append(fn.Body.List, stmts...)
		return nil
	}
	out := make([]dst.Stmt, 0, len(fn.Body.List)+len(stmts))
	out = append(out, fn.Body.List[:retIdx]...)
	out = append(out, stmts...)
	out = append(out, fn.Body.List[retIdx:]...)
	fn.Body.List = out
	return nil
}

// FuncContainsStringLit reports whether fn's body contains the string
// literal val anywhere. Used as the idempotency probe for statement
// insertions keyed on a column name (`out["sku"] = ...`).
func FuncContainsStringLit(fn *dst.FuncDecl, val string) bool {
	quoted := strconv.Quote(val)
	found := false
	dst.Inspect(fn.Body, func(n dst.Node) bool {
		if found {
			return false
		}
		if bl, ok := n.(*dst.BasicLit); ok && bl.Kind == token.STRING && bl.Value == quoted {
			found = true
			return false
		}
		return true
	})
	return found
}
