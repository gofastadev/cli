package clierr

import (
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
