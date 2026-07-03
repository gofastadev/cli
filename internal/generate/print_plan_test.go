package generate

import (
	"testing"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// TestPrintPlanResult_TextMode — exercises the text-output branch
// (JSON mode off) of printPlanResult.
func TestPrintPlanResult_TextMode(t *testing.T) {
	cliout.SetJSONMode(false)
	cmd := &cobra.Command{Use: "g"}
	assert.NotPanics(t, func() { printPlanResult(cmd) })
}

// TestPrintPlanResult_JSONMode — exercises the JSON-output branch.
func TestPrintPlanResult_JSONMode(t *testing.T) {
	cliout.SetJSONMode(true)
	t.Cleanup(func() { cliout.SetJSONMode(false) })
	cmd := &cobra.Command{Use: "g"}
	assert.NotPanics(t, func() { printPlanResult(cmd) })
}
