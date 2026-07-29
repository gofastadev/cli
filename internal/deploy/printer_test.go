package deploy

import (
	"testing"
)

func TestPrintStep(t *testing.T) {
	// Just verify it doesn't panic
	PrintStep(1, 5, "Test step")
	PrintSuccess("Success")
	PrintWarning("Warning")
	PrintError("Error")
	PrintInfo("Info")
}
