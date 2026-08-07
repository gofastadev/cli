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

func TestBlockMismatches_MarkerErrorSurfaces(t *testing.T) {
	problems := BlockMismatches("no markers here", map[string]string{"one": "x\n"})
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "missing marker")
}

func TestBlockMismatches_OnlyStaleBlocksReported(t *testing.T) {
	src := "<!-- gofasta:begin one -->\ncurrent\n<!-- gofasta:end one -->\n" +
		"<!-- gofasta:begin two -->\nstale\n<!-- gofasta:end two -->\n"
	problems := BlockMismatches(src, map[string]string{"one": "current\n", "two": "fresh\n"})
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], `block "two" is stale`)
}

func TestBlockMismatches_WhitespaceOnlyDrift(t *testing.T) {
	// Extra blank lines inside the block trim away when comparing bodies, so
	// no block is individually stale — the generic fallback message fires.
	src := "<!-- gofasta:begin one -->\n\ncurrent\n\n<!-- gofasta:end one -->\n"
	problems := BlockMismatches(src, map[string]string{"one": "current\n"})
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "generated blocks differ from the on-disk content")
}

func TestExtractBlock_MissingOrReversedMarkers(t *testing.T) {
	assert.Empty(t, extractBlock("plain prose", "one"))
	assert.Empty(t, extractBlock("<!-- gofasta:begin one -->\n", "one"))
	assert.Empty(t, extractBlock("<!-- gofasta:end one -->\n<!-- gofasta:begin one -->\n", "one"))
}

func TestFirstDiffLines_Identical(t *testing.T) {
	gotLine, wantLine := firstDiffLines("same\nlines", "same\nlines")
	assert.Empty(t, gotLine)
	assert.Empty(t, wantLine)
}
