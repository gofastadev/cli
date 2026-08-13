package deploy

import (
	"testing"
)

func TestLookPathSetters(t *testing.T) {
	SetLookPathForTest(func(n string) (string, error) { return "x", nil })
	ResetLookPathForTest()
	SetExecCommandForTest(fakeExecCommand(0, ""))
	ResetExecCommandForTest()
}
