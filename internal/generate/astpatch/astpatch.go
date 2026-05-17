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

// WriteBack restores the (possibly modified) dst.File to source, runs
// gofmt over the result, and writes it to disk. Returns the byte body
// and the size; the file is overwritten in place.
//
// Tests that don't want disk writes can call Render instead.
func WriteBack(f *File) ([]byte, error) {
	body, err := Render(f)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(f.Path, body, 0o644); err != nil {
		return nil, clierr.Wrap(clierr.CodeFileIO, err, "writing "+f.Path)
	}
	return body, nil
}

// Render restores the dst.File to bytes and runs gofmt. Returns the
// rendered body without writing anywhere — useful for plan-mode previews
// and tests.
func Render(f *File) ([]byte, error) {
	var buf bytes.Buffer
	if err := decorator.NewRestorer().Fprint(&buf, f.Dst); err != nil {
		return nil, clierr.Wrap(clierr.CodeASTPatchFailed, err, "restoring dst file")
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		// Return unformatted bytes — caller still gets working output,
		// even if a downstream gofmt step will surface the issue.
		return buf.Bytes(), nil
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
