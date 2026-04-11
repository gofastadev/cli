package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMain_InvokesRunWithVersion(t *testing.T) {
	var captured string
	orig := run
	run = func(v string) { captured = v }
	t.Cleanup(func() { run = orig })

	main()

	assert.Equal(t, Version, captured)
}
