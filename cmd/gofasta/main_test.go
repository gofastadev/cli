package main

import (
	"testing"
)

func TestMain_BuildsSuccessfully(t *testing.T) {
	// This test verifies the main package compiles correctly.
	// The main() function calls os.Exit, so we only test that
	// the package builds without errors.
	// The actual CLI is tested through the commands package.
}
