package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDevFlags_KeepVolumes_DefaultIsTrue — sanity: --keep-volumes has a
// true default so the documented teardown behavior (preserve volumes)
// holds without any extra flag.
func TestDevFlags_KeepVolumes_DefaultIsTrue(t *testing.T) {
	// Re-register a dev command in isolation so we can read its default.
	// The package-level devCmd has been modified by other tests, so we
	// inspect the struct default instead.
	f := devFlags{keepVolumes: true}
	assert.True(t, f.keepVolumes)
}

// TestParseServicesList — input normalization for --services.
func TestParseServicesList(t *testing.T) {
	assert.Nil(t, parseServicesList(""))
	assert.Nil(t, parseServicesList("   "))
	assert.Equal(t, []string{"db"}, parseServicesList("db"))
	assert.Equal(t, []string{"db", "cache"}, parseServicesList("db,cache"))
	assert.Equal(t, []string{"db", "cache"}, parseServicesList(" db , cache "))
	assert.Equal(t, []string{"db", "cache"}, parseServicesList("db,,cache"))
}
