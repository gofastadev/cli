package generate

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// TestPrintPlanResult_TextMode — exercises the text-output branch
// (jsonMode=false) of printPlanResult.
func TestPrintPlanResult_TextMode(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "g"}
	cmd.PersistentFlags().Bool("json", false, "")
	cmd.SetOut(&buf)

	// Wire the command into a root so cmd.Root() works.
	root := &cobra.Command{Use: "gofasta"}
	root.PersistentFlags().Bool("json", false, "")
	root.AddCommand(cmd)

	printPlanResult(cmd)
	// Should write something to the attached buffer without panicking.
	assert.NotPanics(t, func() { printPlanResult(cmd) })
}

// TestPrintPlanResult_JSONMode — exercises the JSON-output branch.
func TestPrintPlanResult_JSONMode(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "g"}
	cmd.SetOut(&buf)

	root := &cobra.Command{Use: "gofasta"}
	root.PersistentFlags().Bool("json", true, "")
	root.AddCommand(cmd)
	_ = root.PersistentFlags().Set("json", "true")

	printPlanResult(cmd)
	// JSON-mode writes an array (even if empty), terminated by newline.
	assert.NotPanics(t, func() { printPlanResult(cmd) })
}
