// gen_method.go — `gofasta g method <Resource> <Method> [param:type ...]`
//
// Adds a method to an existing service:
//
//   - appends the signature to app/services/interfaces/<snake>_service.go
//   - appends an impl stub to app/services/<snake>.service.go
//
// Uses astpatch (dst-based) so the existing file's formatting + comments
// are preserved through the modify → write-back round trip — no marker
// comments left behind in user-edited service files.
package generate

import (
	"fmt"
	"os"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/generate/astpatch"
	"github.com/gofastadev/cli/internal/layout"
)

// MethodData is the resolved input for the method generator.
type MethodData struct {
	Resource   string  // PascalCase ("Order")
	Snake      string  // snake_case ("order")
	MethodName string  // PascalCase ("Archive")
	Args       []Field // parsed name:type pairs (may be empty)
	// InterfaceName is "<Resource>Service" by default; callers can pass a
	// different interface (e.g. "OrderRepository" for g repo-method).
	InterfaceName string
	// ImplStructName is the receiver type for the impl file; default is
	// the lowercase of Resource + "Service" (e.g. "orderService").
	ImplStructName string
	// InterfaceFile / ImplFile let callers override the default paths
	// when generating for repositories instead of services.
	InterfaceFile string
	ImplFile      string
	// Returns is the method's result list (e.g. ["*models.Order",
	// "error"]). Defaults to ["error"]. The stub returns zero values
	// for every non-error result; a trailing error gets the
	// "not implemented" sentinel.
	Returns []string
}

// GenMethod is the entry point invoked by the Cobra command.
func GenMethod(d MethodData) error {
	d = methodDataDefaults(d)

	if err := ensureExists(d.InterfaceFile); err != nil {
		return err
	}
	if err := ensureExists(d.ImplFile); err != nil {
		return err
	}

	// Step 1: patch the interface.
	ifaceFile, err := astpatch.Parse(d.InterfaceFile)
	if err != nil {
		return err
	}
	iface, err := astpatch.FindInterface(ifaceFile, d.InterfaceName)
	if err != nil {
		return err
	}
	if astpatch.InterfaceHasMethod(iface, d.MethodName) {
		return clierr.Newf(clierr.CodeMethodAlreadyExists,
			"interface %s already declares method %s — pick a different name",
			d.InterfaceName, d.MethodName)
	}
	sig := buildMethodSignature(d)
	if err := astpatch.AppendInterfaceMethod(iface, sig); err != nil {
		return err
	}
	// context is the conventional first argument — make sure the import
	// is present even if the file didn't have it before.
	astpatch.EnsureImport(ifaceFile, "context")
	ensureArgTypeImports(ifaceFile, d.Args)
	ensureReturnTypeImports(ifaceFile, d.Returns)
	if err := writeBackOrRecord(ifaceFile,
		fmt.Sprintf("add %s to %s", d.MethodName, d.InterfaceName)); err != nil {
		return err
	}

	// Step 2: append an impl stub to the service file.
	implFile, err := astpatch.Parse(d.ImplFile)
	if err != nil {
		return err
	}
	astpatch.EnsureImport(implFile, "context")
	// The stub body returns fmt.Errorf when the method has a trailing
	// error — scaffolded impl files import fmt already, but hand-written
	// or trimmed ones may not.
	if d.Returns[len(d.Returns)-1] == "error" {
		astpatch.EnsureImport(implFile, "fmt")
	}
	ensureArgTypeImports(implFile, d.Args)
	ensureReturnTypeImports(implFile, d.Returns)
	stub := buildMethodImplStub(d)
	if err := astpatch.AppendFuncDecl(implFile, stub); err != nil {
		return err
	}
	return writeBackOrRecord(implFile,
		fmt.Sprintf("add %s impl stub to %s", d.MethodName, d.ImplStructName))
}

// methodDataDefaults fills in the conventional names + paths so callers
// only need to pass Resource + MethodName for the common case.
func methodDataDefaults(d MethodData) MethodData {
	if len(d.Returns) == 0 {
		d.Returns = []string{"error"}
	}
	if d.Resource == "" {
		return d
	}
	if d.Snake == "" {
		d.Snake = toSnakeCase(d.Resource)
	}
	if d.InterfaceName == "" {
		// gofasta's scaffold names service interfaces "<Name>ServiceInterface"
		// (see internal/generate/templates/svc_interface.go). Honor that.
		d.InterfaceName = d.Resource + "ServiceInterface"
	}
	// The scaffold declares the exported `type <Name>Service struct`
	// (templates/svc.go) — the stub's receiver must match it or the
	// patched file doesn't compile.
	if d.ImplStructName == "" {
		d.ImplStructName = d.Resource + "Service"
	}
	if d.InterfaceFile == "" || d.ImplFile == "" {
		lo := layout.Detect()
		if d.InterfaceFile == "" {
			d.InterfaceFile = lo.SvcIfaceFile(d.Snake)
		}
		if d.ImplFile == "" {
			d.ImplFile = lo.SvcImplFile(d.Snake)
		}
	}
	return d
}

// ensureArgTypeImports adds the imports the parsed field types need
// (uuid/time) to a patched file. Same mapping gen_field uses when it
// appends a field to a model.
func ensureArgTypeImports(f *astpatch.File, args []Field) {
	for _, a := range args {
		switch a.GoType {
		case "time.Time":
			astpatch.EnsureImport(f, "time")
		case "uuid.UUID":
			astpatch.EnsureImport(f, "github.com/google/uuid")
		}
	}
}

// ensureReturnTypeImports adds the imports the return types need
// (uuid/time — both for the type itself and its stub zero value).
// Types from other packages (e.g. models.Order) assume the target file
// already imports them, which holds for every scaffolded service/repo.
func ensureReturnTypeImports(f *astpatch.File, returns []string) {
	for _, r := range returns {
		switch {
		case strings.Contains(r, "uuid.UUID"):
			astpatch.EnsureImport(f, "github.com/google/uuid")
		case strings.Contains(r, "time.Time"):
			astpatch.EnsureImport(f, "time")
		}
	}
}

// ensureExists returns CodeResourceNotFound when the file is missing.
// The check guards both interface + impl since the generator can only
// patch existing layout — fresh resources should go through g scaffold.
func ensureExists(path string) error {
	if _, err := os.Stat(path); err != nil {
		return clierr.Newf(clierr.CodeResourceNotFound,
			"%s not found — generate the resource first with `gofasta g scaffold <Name>`", path)
	}
	return nil
}

// buildMethodSignature produces the interface-method line:
//
//	Archive(ctx context.Context, id string) error
//	FindByOwner(ctx context.Context, ownerId uuid.UUID) (*models.Order, error)
//
// context.Context is always first; user-supplied args follow.
func buildMethodSignature(d MethodData) string {
	params := make([]string, 0, 1+len(d.Args))
	params = append(params, "ctx context.Context")
	for _, a := range d.Args {
		params = append(params, fmt.Sprintf("%s %s", toCamelCase(a.Name), a.GoType))
	}
	return fmt.Sprintf("%s(%s) %s", d.MethodName, strings.Join(params, ", "), renderReturns(d.Returns))
}

// renderReturns formats a result list: a single type stays bare, two or
// more get the parenthesized tuple form.
func renderReturns(returns []string) string {
	if len(returns) == 1 {
		return returns[0]
	}
	return "(" + strings.Join(returns, ", ") + ")"
}

// numericGoTypes are the built-in types whose zero value is 0.
var numericGoTypes = map[string]bool{
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"uintptr": true, "byte": true, "rune": true,
	"float32": true, "float64": true,
	"complex64": true, "complex128": true,
}

// zeroValueFor returns the Go literal for a type's zero value, used by
// the generated stub bodies. Named/qualified types fall back to the
// composite-literal zero (`T{}`), which is correct for structs — the
// common case for repo/service return types like models.Order.
func zeroValueFor(goType string) string {
	t := strings.TrimSpace(goType)
	switch {
	case strings.HasPrefix(t, "*"), strings.HasPrefix(t, "[]"),
		strings.HasPrefix(t, "map["), strings.HasPrefix(t, "chan "),
		strings.HasPrefix(t, "func("),
		t == "any", t == "error", t == "interface{}":
		return "nil"
	case t == "string":
		return `""`
	case t == "bool":
		return "false"
	case t == "uuid.UUID":
		return "uuid.Nil"
	case t == "time.Time":
		return "time.Time{}"
	case numericGoTypes[t]:
		return "0"
	default:
		return t + "{}"
	}
}

// buildMethodImplStub produces the impl body that returns zero values
// for every result, with a trailing error carrying the "not implemented"
// sentinel. Stubbing rather than panicking keeps `go test` green out of
// the box; the user can hollow it out as they fill the method in.
func buildMethodImplStub(d MethodData) string {
	params := make([]string, 0, 1+len(d.Args))
	params = append(params, "ctx context.Context")
	for _, a := range d.Args {
		params = append(params, fmt.Sprintf("%s %s", toCamelCase(a.Name), a.GoType))
	}
	results := make([]string, 0, len(d.Returns))
	for i, r := range d.Returns {
		if i == len(d.Returns)-1 && r == "error" {
			results = append(results,
				fmt.Sprintf("fmt.Errorf(%q)", d.InterfaceName+"."+d.MethodName+": not implemented"))
			continue
		}
		results = append(results, zeroValueFor(r))
	}
	return fmt.Sprintf(`// %s is a generated stub. Replace with the real implementation.
func (s *%s) %s(%s) %s {
	return %s
}`, d.MethodName, d.ImplStructName, d.MethodName, strings.Join(params, ", "),
		renderReturns(d.Returns), strings.Join(results, ", "))
}

// astpatchRenderFn / astpatchAppendFuncDeclFn are package-level seams
// over astpatch.Render and astpatch.AppendFuncDecl so tests can drive
// the defensive error branches that wrap them.
var (
	astpatchRenderFn         = astpatch.Render
	astpatchAppendFuncDeclFn = astpatch.AppendFuncDecl
)

// writeBackOrRecord is the same chokepoint the rest of this package uses
// to honor dry-run mode. We can't reuse writeOrRecordPatch directly here
// because astpatch already produced the body — we need to record a
// patch action with that body's size.
func writeBackOrRecord(f *astpatch.File, detail string) error {
	body, err := astpatchRenderFn(f)
	if err != nil {
		return err
	}
	if GetDryRun() {
		recordPatch(f.Path, detail, len(body))
		return nil
	}
	if err := os.WriteFile(f.Path, body, 0o644); err != nil {
		return clierr.Wrap(clierr.CodeFileIO, err, "writing "+f.Path)
	}
	return nil
}
