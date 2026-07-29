// Coverage for planner.go — the write/patch chokepoint, including the
// containment check that keeps generators inside the project root.

package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteOrRecordPatch_RefusesOutOfTreePath covers the defense-in-depth net
// that stops a resource name which slipped past validateIdentifier from
// patching a file outside the project root.
func TestWriteOrRecordPatch_RefusesOutOfTreePath(t *testing.T) {
	setupTempProject(t)

	for _, path := range []string{"../escape.go", "/etc/passwd", ".."} {
		t.Run(path, func(t *testing.T) {
			err := writeOrRecordPatch(path, "attempted patch", []byte("package x\n"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "refusing to write outside the project root")
		})
	}
}
