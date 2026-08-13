package docs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func inlineFixture() Facts {
	return Facts{
		SchemaVersion: 1,
		Scaffold:      Scaffold{CreatedCount: 18, PatchedCount: 4},
		Skeleton: Skeleton{
			FileCount:  78,
			Migrations: []DriverFileList{{Driver: "postgres", Count: 5}},
		},
	}
}

func TestCheckInlineFacts_Valid(t *testing.T) {
	md := "Copies ~78 files <!-- fact: skeleton.fileCount = 78 --> into place.\n" +
		"Creates 18 files <!-- fact: scaffold.createdCount = 18 -->.\n" +
		"Postgres ships 5 <!-- fact: skeleton.migrations[0].count = 5 --> migrations.\n"
	assert.Empty(t, CheckInlineFacts("doc.md", []byte(md), inlineFixture()))
}

func TestCheckInlineFacts_Stale(t *testing.T) {
	md := "Creates 11 files <!-- fact: scaffold.createdCount = 11 -->.\n"
	got := CheckInlineFacts("doc.md", []byte(md), inlineFixture())
	require.Len(t, got, 1)
	assert.Contains(t, got[0], `fact "scaffold.createdCount" is stale`)
	assert.Contains(t, got[0], `annotation says "11", facts say "18"`)
}

func TestCheckInlineFacts_UnknownPath(t *testing.T) {
	md := "<!-- fact: scaffold.nope = 1 -->\n"
	got := CheckInlineFacts("doc.md", []byte(md), inlineFixture())
	require.Len(t, got, 1)
	assert.Contains(t, got[0], `unknown field "nope"`)
}

func TestCheckInlineFacts_NoAnnotations(t *testing.T) {
	assert.Empty(t, CheckInlineFacts("doc.md", []byte("plain prose, no annotations"), inlineFixture()))
}

func TestResolvePath_Errors(t *testing.T) {
	doc, err := factsAsMap(inlineFixture())
	require.NoError(t, err)

	_, err = resolvePath(doc, "skeleton.migrations[9].count")
	assert.ErrorContains(t, err, "index out of range")

	_, err = resolvePath(doc, "skeleton.fileCount.deeper")
	assert.ErrorContains(t, err, "not an object")

	_, err = resolvePath(doc, "scaffold")
	assert.ErrorContains(t, err, "non-scalar")

	_, err = resolvePath(doc, "skeleton.migrations[x]")
	assert.ErrorContains(t, err, "malformed index")
}

func TestCheckInlineFacts_StringAndBoolLeaves(t *testing.T) {
	f := inlineFixture()
	md := "Default driver <!-- fact: skeleton.migrations[0].driver = postgres -->.\n"
	assert.Empty(t, CheckInlineFacts("doc.md", []byte(md), f))
}

func TestResolvePath_LeafKindsAndIndexErrors(t *testing.T) {
	doc, err := factsAsMap(inlineFixture())
	require.NoError(t, err)

	// string leaf
	got, err := resolvePath(doc, "skeleton.migrations[0].driver")
	require.NoError(t, err)
	assert.Equal(t, "postgres", got)

	// bool leaf (hand-built doc — the fixture has no bool leaves)
	got, err = resolvePath(map[string]any{"flags": []any{map[string]any{"persistent": true}}}, "flags[0].persistent")
	require.NoError(t, err)
	assert.Equal(t, "true", got)

	// indexing into a non-array
	_, err = resolvePath(doc, "skeleton.fileCount[0]")
	assert.ErrorContains(t, err, "not an array")

	// unclosed index bracket
	_, err = resolvePath(doc, "skeleton.migrations[0")
	assert.ErrorContains(t, err, "malformed index")
}

func TestFactsAsMap_MarshalAndUnmarshalErrors(t *testing.T) {
	orig := jsonMarshal
	t.Cleanup(func() { jsonMarshal = orig })

	jsonMarshal = func(any) ([]byte, error) { return nil, assert.AnError }
	_, err := factsAsMap(inlineFixture())
	require.ErrorIs(t, err, assert.AnError)

	// CheckInlineFacts surfaces the flattening failure as a problem string.
	problems := CheckInlineFacts("doc.md", []byte("x"), inlineFixture())
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "internal error flattening facts")

	// Marshal "succeeds" with bytes json.Unmarshal rejects.
	jsonMarshal = func(any) ([]byte, error) { return []byte("{not json"), nil }
	_, err = factsAsMap(inlineFixture())
	require.Error(t, err)
}
