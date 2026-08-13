package clierr

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_PopulatesHintAndDocsFromRegistry(t *testing.T) {
	e := New(CodeWireMissingProvider, "undefined: NewThingProvider")
	if e.Code != string(CodeWireMissingProvider) {
		t.Errorf("Code = %q, want %q", e.Code, CodeWireMissingProvider)
	}
	if e.Hint == "" {
		t.Error("Hint is empty — registry lookup did not populate it")
	}
	if e.Docs == "" {
		t.Error("Docs is empty — registry lookup did not populate it")
	}
}

func TestNew_UnknownCodeStillUsable(t *testing.T) {
	// Unregistered codes must not panic; they simply produce an error
	// without a hint or docs URL.
	e := New(Code("UNREGISTERED_CODE"), "something happened")
	if e.Hint != "" || e.Docs != "" {
		t.Errorf("expected empty hint/docs for unregistered code, got %+v", e)
	}
	if e.Message != "something happened" {
		t.Errorf("Message lost: %q", e.Message)
	}
}

// TestRegistry_EveryCodeHasAHint guards against adding a code constant
// and forgetting to register its hint. If a registered code has an empty
// hint, the test fails — that's a contract with agents/CI.
func TestRegistry_EveryCodeHasAHint(t *testing.T) {
	for code, entry := range registry {
		if entry.Hint == "" && code != CodeInternal {
			t.Errorf("code %q has no Hint — add one to registry in codes.go", code)
		}
	}
}

// TestRegistry_NonEmpty — the code registry enumerates every declared
// code and must include at least the canonical codes.
func TestRegistry_NonEmpty(t *testing.T) {
	require.NotEmpty(t, registry)
	_, foundInternal := registry[CodeInternal]
	_, foundWire := registry[CodeWireMissingProvider]
	assert.True(t, foundInternal, "CodeInternal missing from registry")
	assert.True(t, foundWire, "CodeWireMissingProvider missing from registry")
}

// TestEveryDeclaredCodeIsRegistered parses this package's source and
// asserts every Code-typed constant has a registry entry. The older
// registry-side tests iterate the map, which can never notice a
// constant that was declared but never registered — exactly the drift
// that shipped four refactor codes with empty hints. Enumerating the
// const block from the AST closes that hole for every future code.
func TestEveryDeclaredCodeIsRegistered(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "codes.go", nil, 0)
	require.NoError(t, err)

	var declared []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != "Code" {
				continue
			}
			for _, v := range vs.Values {
				if lit, ok := v.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					declared = append(declared, strings.Trim(lit.Value, `"`))
				}
			}
		}
	}
	require.NotEmpty(t, declared, "AST scan found no Code constants — parser assumptions broke")

	// Docs is deliberately NOT asserted non-empty: several long-standing
	// entries (CodeInternal, the toolchain-failure family) route the
	// user via the hint alone.
	for _, code := range declared {
		m, ok := registry[Code(code)]
		assert.True(t, ok, "Code %q is declared but has no registry entry — the codes.go header says: keep the two lists in sync", code)
		if ok {
			assert.NotEmpty(t, m.Hint, "Code %q registry entry has an empty hint", code)
		}
	}
}
