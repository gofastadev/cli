// gen_method.go — `gofasta g method <Resource> <Method> [param:type ...]`
//
// Adds a method to an existing service:
//
//   • appends the signature to app/services/interfaces/<snake>_service.go
//   • appends an impl stub to app/services/<snake>.service.go
//
// Uses astpatch (dst-based) so the existing file's formatting + comments
// are preserved through the modify → write-back round trip — no marker
// comments left behind in user-edited service files.
package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/generate/astpatch"
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
	ifaceBody, err := writeBackOrRecord(ifaceFile,
		fmt.Sprintf("add %s to %s", d.MethodName, d.InterfaceName))
	if err != nil {
		return err
	}
	_ = ifaceBody // size is captured by the planner

	// Step 2: append an impl stub to the service file.
	implFile, err := astpatch.Parse(d.ImplFile)
	if err != nil {
		return err
	}
	astpatch.EnsureImport(implFile, "context")
	stub := buildMethodImplStub(d)
	if err := astpatch.AppendFuncDecl(implFile, stub); err != nil {
		return err
	}
	if _, err := writeBackOrRecord(implFile,
		fmt.Sprintf("add %s impl stub to %s", d.MethodName, d.ImplStructName)); err != nil {
		return err
	}
	return nil
}

// methodDataDefaults fills in the conventional names + paths so callers
// only need to pass Resource + MethodName for the common case.
func methodDataDefaults(d MethodData) MethodData {
	if d.Resource == "" {
		return d
	}
	if d.Snake == "" {
		d.Snake = toSnakeCase(d.Resource)
	}
	if d.InterfaceName == "" {
		d.InterfaceName = d.Resource + "Service"
	}
	if d.ImplStructName == "" {
		d.ImplStructName = strings.ToLower(d.Resource[:1]) + d.Resource[1:] + "Service"
	}
	if d.InterfaceFile == "" {
		d.InterfaceFile = filepath.Join("app", "services", "interfaces", d.Snake+"_service.go")
	}
	if d.ImplFile == "" {
		d.ImplFile = filepath.Join("app", "services", d.Snake+".service.go")
	}
	return d
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
//
// context.Context is always first; user-supplied args follow.
func buildMethodSignature(d MethodData) string {
	params := []string{"ctx context.Context"}
	for _, a := range d.Args {
		params = append(params, fmt.Sprintf("%s %s", toCamelCase(a.Name), a.GoType))
	}
	return fmt.Sprintf("%s(%s) error", d.MethodName, strings.Join(params, ", "))
}

// buildMethodImplStub produces the impl body that returns a placeholder
// error. Stubbing rather than panicking keeps `go test` green out of the
// box; the user can hollow it out as they fill the method in.
func buildMethodImplStub(d MethodData) string {
	params := []string{"ctx context.Context"}
	for _, a := range d.Args {
		params = append(params, fmt.Sprintf("%s %s", toCamelCase(a.Name), a.GoType))
	}
	return fmt.Sprintf(`// %s is a generated stub. Replace with the real implementation.
func (s *%s) %s(%s) error {
	return fmt.Errorf("%s.%s: not implemented")
}`, d.MethodName, d.ImplStructName, d.MethodName, strings.Join(params, ", "),
		d.InterfaceName, d.MethodName)
}

// writeBackOrRecord is the same chokepoint the rest of this package uses
// to honor dry-run mode. We can't reuse writeOrRecordPatch directly here
// because astpatch already produced the body — we need to record a
// patch action with that body's size.
func writeBackOrRecord(f *astpatch.File, detail string) ([]byte, error) {
	body, err := astpatch.Render(f)
	if err != nil {
		return nil, err
	}
	if GetDryRun() {
		recordPatch(f.Path, detail, len(body))
		return body, nil
	}
	if err := os.WriteFile(f.Path, body, 0o644); err != nil {
		return nil, clierr.Wrap(clierr.CodeFileIO, err, "writing "+f.Path)
	}
	return body, nil
}
