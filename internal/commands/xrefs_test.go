package commands

import (
	"bytes"
	"testing"

	"github.com/gofastadev/cli/internal/commands/symbolresolve"
	"github.com/stretchr/testify/require"
)

func TestPrintXrefsText_NoReferences(t *testing.T) {
	var buf bytes.Buffer
	printXrefsText(&buf, symbolresolve.SymbolReport{
		Symbol:  "X",
		Package: "pkg",
		Kind:    "func",
		Count:   0,
	})
	out := buf.String()
	require.Contains(t, out, "func X (pkg)")
	require.Contains(t, out, "no references found")
}

func TestPrintXrefsText_WithDefinitionAndReferences(t *testing.T) {
	var buf bytes.Buffer
	printXrefsText(&buf, symbolresolve.SymbolReport{
		Symbol:     "Y",
		Package:    "pkg",
		Kind:       "method",
		Definition: &symbolresolve.Reference{File: "foo.go", Line: 1, Column: 2, Kind: "decl"},
		References: []symbolresolve.Reference{
			{File: "a.go", Line: 10, Column: 3, Kind: "call", InFunc: "pkg.Run"},
			{File: "b.go", Line: 20, Column: 4, Kind: "call"},
		},
		Count: 2,
	})
	out := buf.String()
	require.Contains(t, out, "method Y (pkg)")
	require.Contains(t, out, "defined at foo.go:1:2")
	require.Contains(t, out, "2 reference(s):")
	require.Contains(t, out, "a.go:10:3 (call) — in pkg.Run")
	require.Contains(t, out, "b.go:20:4 (call)")
}

// TestXrefsCmd_RunE_PropagatesError — lookupReferencesFn returns an
// error; the RunE wrapper surfaces it.
func TestXrefsCmd_RunE_PropagatesError(t *testing.T) {
	saved := lookupReferencesFn
	lookupReferencesFn = func(_ string) (symbolresolve.SymbolReport, error) {
		return symbolresolve.SymbolReport{}, errStub
	}
	t.Cleanup(func() { lookupReferencesFn = saved })

	err := xrefsCmd.RunE(xrefsCmd, []string{"any"})
	require.Error(t, err)
}

// TestXrefsCmd_RunE_HappyPath — lookupReferencesFn returns a populated
// report; the RunE wrapper prints it and returns nil.
func TestXrefsCmd_RunE_HappyPath(t *testing.T) {
	saved := lookupReferencesFn
	lookupReferencesFn = func(_ string) (symbolresolve.SymbolReport, error) {
		return symbolresolve.SymbolReport{Symbol: "X", Package: "pkg", Kind: "func"}, nil
	}
	t.Cleanup(func() { lookupReferencesFn = saved })

	require.NoError(t, xrefsCmd.RunE(xrefsCmd, []string{"X"}))
}
