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
