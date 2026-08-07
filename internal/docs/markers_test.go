package docs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const markersFixture = `# Title

<!-- gofasta:begin one -->
old content
<!-- gofasta:end one -->

prose between

<!-- gofasta:begin two -->
<!-- gofasta:end two -->
`

func TestReplaceBlocks(t *testing.T) {
	out, err := ReplaceBlocks(markersFixture, map[string]string{
		"one": "new content\n",
		"two": "filled\n",
	})
	require.NoError(t, err)
	assert.Contains(t, out, "<!-- gofasta:begin one -->\nnew content\n<!-- gofasta:end one -->")
	assert.Contains(t, out, "<!-- gofasta:begin two -->\nfilled\n<!-- gofasta:end two -->")
	assert.Contains(t, out, "prose between")

	// Idempotence: replacing again changes nothing.
	again, err := ReplaceBlocks(out, map[string]string{"one": "new content\n", "two": "filled\n"})
	require.NoError(t, err)
	assert.Equal(t, out, again)
}

func TestReplaceBlocks_Errors(t *testing.T) {
	_, err := ReplaceBlocks("no markers", map[string]string{"one": "x"})
	assert.ErrorContains(t, err, `missing marker "<!-- gofasta:begin one -->"`)

	_, err = ReplaceBlocks("<!-- gofasta:begin one -->\n", map[string]string{"one": "x"})
	assert.ErrorContains(t, err, "missing marker \"<!-- gofasta:end one -->\"")

	dup := "<!-- gofasta:begin one -->\n<!-- gofasta:end one -->\n<!-- gofasta:begin one -->\n<!-- gofasta:end one -->\n"
	_, err = ReplaceBlocks(dup, map[string]string{"one": "x"})
	assert.ErrorContains(t, err, "duplicate marker")

	reversed := "<!-- gofasta:end one -->\n<!-- gofasta:begin one -->\n"
	_, err = ReplaceBlocks(reversed, map[string]string{"one": "x"})
	assert.ErrorContains(t, err, "before its begin marker")
}

func TestBlockMismatches(t *testing.T) {
	assert.Empty(t, BlockMismatches(
		"<!-- gofasta:begin one -->\ncurrent\n<!-- gofasta:end one -->\n",
		map[string]string{"one": "current\n"}))

	problems := BlockMismatches(
		"<!-- gofasta:begin one -->\nstale line\n<!-- gofasta:end one -->\n",
		map[string]string{"one": "fresh line\n"})
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], `block "one" is stale`)
	assert.Contains(t, problems[0], "have: stale line")
	assert.Contains(t, problems[0], "want: fresh line")
}
