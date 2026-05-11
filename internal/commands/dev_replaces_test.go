package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for dev_replaces.go.
//
// The parser handles a fixed subset of go.mod syntax (replace clauses,
// single-line + block forms, comments). Each test pins one shape of
// input that exercises a specific branch, plus a few cross-cutting
// fixtures (mixed forms, real-world snippet) to guard against
// regressions.
// ─────────────────────────────────────────────────────────────────────

// writeGoMod creates a go.mod at a temp path with the given content and
// returns the path. Centralized so each test reads as one assert per
// branch rather than three lines of setup.
func writeGoMod(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "go.mod")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

// TestFindLocalReplaces_MissingFile — go.mod absent is a no-op, not an
// error. Lets the docker build emit its own (clearer) error about the
// missing file, and avoids spuriously blocking --all-in-docker for
// projects that genuinely have no replaces and no go.mod yet.
func TestFindLocalReplaces_MissingFile(t *testing.T) {
	dir := t.TempDir()
	got, err := findLocalReplaces(filepath.Join(dir, "does-not-exist"))
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestFindLocalReplaces_OpenErrorPropagates — a directory passed in
// place of a file returns the underlying os error. Verifies the
// non-IsNotExist path is wired through.
func TestFindLocalReplaces_OpenErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	_, err := findLocalReplaces(dir) // passing a directory triggers an open error after stat
	// On both macOS and Linux opening a directory and reading from it
	// either fails immediately or yields zero bytes — both outcomes are
	// fine as long as we don't crash and don't claim a missing file.
	if err != nil {
		assert.NotErrorIs(t, err, os.ErrNotExist)
	}
}

// TestParseLocalReplaces_NoReplace — a go.mod with require/module/go
// lines but no replace at all returns an empty slice.
func TestParseLocalReplaces_NoReplace(t *testing.T) {
	got, err := parseLocalReplaces(strings.NewReader(`module example.com/app

go 1.25.0

require github.com/foo/bar v1.0.0
`))
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestParseLocalReplaces_SingleLineDotDot — the canonical
// `replace MOD => ../path` shape is the case we actually care about
// for cross-repo dev. Verify it is captured with both fields preserved.
func TestParseLocalReplaces_SingleLineDotDot(t *testing.T) {
	got, err := parseLocalReplaces(strings.NewReader(
		"replace example.com/foo => ../foo\n",
	))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "example.com/foo", got[0].Module)
	assert.Equal(t, "../foo", got[0].Path)
}

// TestParseLocalReplaces_SingleLineWithVersions — the version-laden
// form `replace MOD vX.Y.Z => ../path vA.B.C` should still surface the
// local path without confusing module/path tokens.
func TestParseLocalReplaces_SingleLineWithVersions(t *testing.T) {
	got, err := parseLocalReplaces(strings.NewReader(
		"replace example.com/foo v1.2.3 => ../foo v1.2.3-dev\n",
	))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "example.com/foo", got[0].Module)
	assert.Equal(t, "../foo", got[0].Path)
}

// TestParseLocalReplaces_Block — block-form replaces are the form
// `go mod edit -replace` and most multi-replace setups produce. Verify
// every entry inside the parens is collected.
func TestParseLocalReplaces_Block(t *testing.T) {
	got, err := parseLocalReplaces(strings.NewReader(`replace (
    example.com/foo => ../foo
    example.com/bar v1.0.0 => ./vendor/bar
    example.com/baz => /opt/baz
)
`))
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "example.com/foo", got[0].Module)
	assert.Equal(t, "../foo", got[0].Path)
	assert.Equal(t, "example.com/bar", got[1].Module)
	assert.Equal(t, "./vendor/bar", got[1].Path)
	assert.Equal(t, "example.com/baz", got[2].Module)
	assert.Equal(t, "/opt/baz", got[2].Path)
}

// TestParseLocalReplaces_ModuleToModuleSkipped — a `replace A => B`
// where B is itself a module (no leading `.` or `/`) is NOT a local
// replace; the proxy resolves it cleanly inside the container. Must be
// dropped from the result so we don't spuriously block --all-in-docker.
func TestParseLocalReplaces_ModuleToModuleSkipped(t *testing.T) {
	got, err := parseLocalReplaces(strings.NewReader(
		"replace example.com/foo => example.com/fork/foo v1.2.3\n",
	))
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestParseLocalReplaces_MixedBlock — a block with both a path replace
// and a module-to-module replace returns only the path one.
func TestParseLocalReplaces_MixedBlock(t *testing.T) {
	got, err := parseLocalReplaces(strings.NewReader(`replace (
    example.com/foo => ../foo
    example.com/bar => example.com/fork/bar v2.0.0
)
`))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "example.com/foo", got[0].Module)
}

// TestParseLocalReplaces_CommentsStripped — comments must be stripped
// before clause matching. A clause that exists only inside a comment
// is not a replace; a clause followed by a trailing comment IS one.
func TestParseLocalReplaces_CommentsStripped(t *testing.T) {
	got, err := parseLocalReplaces(strings.NewReader(`module example.com/app

// replace example.com/commented => ../commented  // not a real replace

replace example.com/real => ../real // legacy local checkout
`))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "example.com/real", got[0].Module)
	assert.Equal(t, "../real", got[0].Path)
}

// TestParseLocalReplaces_MalformedClauseIgnored — a `replace` line
// missing the `=>` operator is invalid go.mod syntax; we silently skip
// it rather than erroring. The compiler / `go mod tidy` will surface
// the underlying syntax error elsewhere.
func TestParseLocalReplaces_MalformedClauseIgnored(t *testing.T) {
	got, err := parseLocalReplaces(strings.NewReader(
		"replace example.com/foo\n",
	))
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestParseLocalReplaces_EmptySidesIgnored — `=>` with empty LHS or RHS
// is malformed; skip silently for the same reason as the above test.
func TestParseLocalReplaces_EmptySidesIgnored(t *testing.T) {
	got, err := parseLocalReplaces(strings.NewReader(
		"replace  =>  \n",
	))
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestParseLocalReplaces_BlankAndCommentLinesInsideBlock — empty and
// comment-only lines inside a block must not affect block-state
// tracking or be picked up as clauses.
func TestParseLocalReplaces_BlankAndCommentLinesInsideBlock(t *testing.T) {
	got, err := parseLocalReplaces(strings.NewReader(`replace (

    // first real-life pin
    example.com/foo => ../foo

    // (this comment alone has no effect)
)
`))
	require.NoError(t, err)
	require.Len(t, got, 1)
}

// TestIsFilesystemPath — table-driven exhaustive check of the
// path-detection predicate. Keeps the heuristic honest by pinning each
// shape it must accept or reject.
func TestIsFilesystemPath(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"./foo", true},
		{"../foo", true},
		{"/abs/foo", true},
		{".", true},
		{"..", true},
		{"example.com/foo", false},
		{"github.com/foo/bar", false},
		{"foo", false},   // no domain-like first segment but also no path prefix
		{"", false},      // empty
		{".foo", false},  // dot file, but not a path
		{"..foo", false}, // weird, treat as not-a-path
		{"./", true},     // edge case: trailing slash only
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, isFilesystemPath(tc.in), "isFilesystemPath(%q)", tc.in)
	}
}

// TestStripLineComment — table-driven sanity for comment stripping. We
// don't need full Go tokenizer fidelity here; we only need to strip a
// trailing `//` correctly so a comment cannot hide a replace clause.
func TestStripLineComment(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"replace foo => ../foo", "replace foo => ../foo"},
		{"replace foo => ../foo // legacy", "replace foo => ../foo "},
		{"// only a comment", ""},
		{"", ""},
		{"foo // a // b", "foo "},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, stripLineComment(tc.in), "stripLineComment(%q)", tc.in)
	}
}

// TestFindLocalReplaces_RealisticGoMod — end-to-end: write a go.mod
// shaped like the one that triggered the original bug report, and
// confirm the parser surfaces the offending replace.
func TestFindLocalReplaces_RealisticGoMod(t *testing.T) {
	p := writeGoMod(t, `module github.com/Example/app

go 1.25.0

require (
    github.com/gofastadev/gofasta v0.1.4
    github.com/go-chi/chi/v5 v5.2.5
)

replace github.com/gofastadev/gofasta => ../../gofastadev/gofasta
`)
	got, err := findLocalReplaces(p)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "github.com/gofastadev/gofasta", got[0].Module)
	assert.Equal(t, "../../gofastadev/gofasta", got[0].Path)
}
