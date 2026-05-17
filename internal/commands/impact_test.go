package commands

import (
	"bytes"
	"testing"

	"github.com/gofastadev/cli/internal/commands/symbolresolve"
	"github.com/stretchr/testify/require"
)

func TestPrintImpactText_EmptyDeps(t *testing.T) {
	var buf bytes.Buffer
	printImpactText(&buf, symbolresolve.ImpactReport{
		Target:  "app/services/order.service.go",
		Package: "irodata/app/services",
	})
	out := buf.String()
	require.Contains(t, out, "Target:")
	require.Contains(t, out, "Package: irodata/app/services")
	require.Contains(t, out, "No packages depend on this target.")
}

func TestPrintImpactText_AllSections(t *testing.T) {
	var buf bytes.Buffer
	printImpactText(&buf, symbolresolve.ImpactReport{
		Target:              "app/services",
		Package:             "irodata/app/services",
		DirectImporters:     []string{"irodata/app/controllers"},
		TransitiveImporters: []string{"irodata/app/main"},
		ImpactedFiles:       []string{"app/controllers/c.go"},
	})
	out := buf.String()
	require.Contains(t, out, "Direct importers (1):")
	require.Contains(t, out, "Transitive importers (1):")
	require.Contains(t, out, "Impacted files (1)")
	require.Contains(t, out, "irodata/app/controllers")
}

func TestPrintImpactText_NoPackage(t *testing.T) {
	// Empty Package field — skip the "Package: …" line.
	var buf bytes.Buffer
	printImpactText(&buf, symbolresolve.ImpactReport{Target: "x"})
	require.NotContains(t, buf.String(), "Package:")
}

// TestImpactCmd_RunE_PropagatesError — impactGraphFn returns an error;
// the RunE wrapper surfaces it.
func TestImpactCmd_RunE_PropagatesError(t *testing.T) {
	saved := impactGraphFn
	impactGraphFn = func(_ string) (symbolresolve.ImpactReport, error) {
		return symbolresolve.ImpactReport{}, errStub
	}
	t.Cleanup(func() { impactGraphFn = saved })

	err := impactCmd.RunE(impactCmd, []string{"any"})
	require.Error(t, err)
}

// TestImpactCmd_RunE_HappyPath — impactGraphFn returns a populated
// report; the RunE wrapper prints it and returns nil.
func TestImpactCmd_RunE_HappyPath(t *testing.T) {
	saved := impactGraphFn
	impactGraphFn = func(_ string) (symbolresolve.ImpactReport, error) {
		return symbolresolve.ImpactReport{Target: "x", Package: "pkg"}, nil
	}
	t.Cleanup(func() { impactGraphFn = saved })

	require.NoError(t, impactCmd.RunE(impactCmd, []string{"x"}))
}

// errStub is a sentinel test error.
var errStub = stubErr("stub error")

type stubErr string

func (s stubErr) Error() string { return string(s) }
