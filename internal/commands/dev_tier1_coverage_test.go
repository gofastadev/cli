package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tier 1 coverage — pure-function gaps that can be closed with direct
// invocation. No exec stubs, no terminal stubs, no goroutines. Each
// test names the specific uncovered branch it targets.

// removeService — never invoked from any test, so 0% before this.
func TestRemoveService_FiltersTarget(t *testing.T) {
	got := removeService([]string{"a", "b", "c"}, "b")
	assert.Equal(t, []string{"a", "c"}, got)
}

func TestRemoveService_TargetAbsent(t *testing.T) {
	got := removeService([]string{"a", "c"}, "b")
	assert.Equal(t, []string{"a", "c"}, got)
}

func TestRemoveService_EmptyInput(t *testing.T) {
	got := removeService(nil, "b")
	assert.Empty(t, got)
}

// startServices early-return for empty names — line 277-278 was already
// reachable, but the "skip empty profile entry" branch (line 282-283)
// is not. Empty-name short-circuit included for symmetry.
func TestStartServices_EmptyNamesShortCircuit(t *testing.T) {
	require.NoError(t, startServices(nil, []string{"x"}))
}

func TestStartServices_SkipsEmptyProfileEntries(t *testing.T) {
	// One profile is "", one is "p1". Only p1 should become --profile p1.
	// The fake exec also exits 0 so the call returns nil.
	withFakeExec(t, 0)
	require.NoError(t, startServices([]string{"db"}, []string{"", "p1"}))
}

// detectComposeServices skip-empty-profile branch (line 101-102).
func TestDetectComposeServices_SkipsEmptyProfiles(t *testing.T) {
	fakeExecOutput(t, `{"services":{"db":{}}}`, 0)
	_, _, err := detectComposeServices([]string{""}, false)
	require.NoError(t, err)
}

// detectComposeProfiles — covers blank-line skip (239), append branch
// (249) which fires once we feed valid profile names. The JSON-skip
// (246-247) branch fires when a line contains `{`, `[`, etc.
func TestDetectComposeProfiles_ParsesAndSkipsBlanksAndJSON(t *testing.T) {
	fakeExecOutput(t, "p1\n\n{not-a-profile}\np2\n", 0)
	got, err := detectComposeProfiles()
	require.NoError(t, err)
	assert.Equal(t, []string{"p1", "p2"}, got)
}

// findLocalReplaces — non-IsNotExist error (line 56). The simplest way
// to force this is to point at a path whose parent is a regular file,
// so os.Open returns ENOTDIR (which is not IsNotExist).
func TestFindLocalReplaces_NonNotExistError(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file, then treat it as a directory in the open
	// path. /file/go.mod cannot exist because /file is not a directory,
	// and the resulting error is ENOTDIR, not "no such file or directory".
	file := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	_, err := findLocalReplaces(filepath.Join(file, "go.mod"))
	require.Error(t, err)
	// ENOTDIR is NOT IsNotExist, so we must have taken the non-NotExist branch.
	assert.False(t, errors.Is(err, os.ErrNotExist) && os.IsNotExist(err),
		"error should not be IsNotExist for this case")
}

// printKeyboardBanner — never invoked; just call it once. The function
// writes a single termcolor.PrintStep line.
func TestPrintKeyboardBanner_DoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() { printKeyboardBanner() })
}

// splitManagedBlock — line 104-106 (empty input → nil, nil) and
// 127-130 (started block but never closed → fold collected into rest).
func TestSplitManagedBlock_EmptyInput(t *testing.T) {
	managed, rest := splitManagedBlock("")
	assert.Nil(t, managed)
	assert.Nil(t, rest)
}

func TestSplitManagedBlock_UnclosedBlock(t *testing.T) {
	// Begin marker without a matching end. The function should return
	// nil managed + everything (incl. the collected lines) in `rest`.
	input := managedBlockBegin + "\nFOO=1\nBAR=2\n"
	managed, rest := splitManagedBlock(input)
	assert.Nil(t, managed)
	// The fold copies the collected lines into rest; they appear as-is.
	assert.Contains(t, strings.Join(rest, "\n"), "FOO=1")
	assert.Contains(t, strings.Join(rest, "\n"), "BAR=2")
}

// stripManagedBlockMarkers — non-NotExist content path where after
// TrimRight + stripping, body is empty. The "if content == ”" branch
// (line 277-279) is unreachable in practice: the early-return guard
// requires the content to contain at least one marker substring, and
// TrimRight only removes trailing newlines, so content can never be
// empty after the trim if a (non-newline) marker is present. Verified
// by inspection — not testable without removing the defensive guard.
//
// Cover the next-best branch: an input that contains a marker but
// otherwise has only whitespace lines. Filters all marker + blanks.
func TestStripManagedBlockMarkers_MarkerOnly(t *testing.T) {
	// Input has trailing newline so trailingNewline=true and the body is
	// rejoined with that newline at the end after the markers are filtered.
	got := stripManagedBlockMarkers(managedBlockBegin + "\n" + managedBlockEnd + "\n")
	assert.Equal(t, "\n", got)
}

// Markers wrapped around a non-marker line — exercises the append branch
// (line 286 in stripManagedBlockMarkers).
func TestStripManagedBlockMarkers_PreservesNonMarkerLines(t *testing.T) {
	input := managedBlockBegin + "\nFOO=1\n" + managedBlockEnd + "\n"
	got := stripManagedBlockMarkers(input)
	assert.Contains(t, got, "FOO=1")
	assert.NotContains(t, got, ">>> auto-managed")
}

// mergeIntoDotEnv — non-IsNotExist read error (line 170-172). Same
// ENOTDIR trick as findLocalReplaces.
func TestMergeIntoDotEnv_NonNotExistReadError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	// Treat /tmp/.../f as a parent directory; reading /tmp/.../f/.env
	// returns ENOTDIR (not IsNotExist).
	err := mergeIntoDotEnv(filepath.Join(file, ".env"), map[string]string{"K": "v"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read")
}

// mergeIntoDotEnv — WriteFile error on the tmp path (line 195-197). The
// final rename target must exist (or its parent must). Create the parent
// as a read-only directory so WriteFile fails with EACCES.
func TestMergeIntoDotEnv_WriteTmpError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod-based write denial")
	}
	dir := t.TempDir()
	readonly := filepath.Join(dir, "ro")
	require.NoError(t, os.Mkdir(readonly, 0o555))
	t.Cleanup(func() { _ = os.Chmod(readonly, 0o755) })
	err := mergeIntoDotEnv(filepath.Join(readonly, ".env"), map[string]string{"K": "v"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write tmp")
}

// mergeIntoDotEnv — rename error (line 198-201). The function writes
// its tmp file into the same directory as the target and then renames,
// which on POSIX rarely fails in isolation. Drive the failure through
// the osRenameFn seam so the branch is reachable deterministically.
func TestMergeIntoDotEnv_RenameError(t *testing.T) {
	orig := osRenameFn
	osRenameFn = func(string, string) error { return errors.New("synthetic rename failure") }
	t.Cleanup(func() { osRenameFn = orig })

	dir := t.TempDir()
	target := filepath.Join(dir, ".env")
	err := mergeIntoDotEnv(target, map[string]string{"K": "v"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename")
	// Tmp file should have been cleaned up.
	_, statErr := os.Stat(target + ".tmp")
	assert.True(t, os.IsNotExist(statErr), "tmp file should be removed after rename failure")
}

// mergeServices — the body of `for _, s := range base { seen[s] = true }`
// (line 231-233) never runs in existing tests because `base` is always
// empty. Pass a non-empty base AND an `add` that includes a duplicate of
// one base entry plus a new one.
func TestMergeServices_DropsDuplicates(t *testing.T) {
	out := mergeServices([]string{"db"}, []string{"db", "cache"})
	assert.Equal(t, []string{"db", "cache"}, out)
}

// mapFailingDepsToServices — the `case "queue":` (line 526-527) branch
// only fires when a probeResult has Dep=="queue" AND Status==probeUnreachable.
func TestMapFailingDepsToServices_QueueBranch(t *testing.T) {
	results := []probeResult{
		{Dep: "queue", Status: probeUnreachable},
		{Dep: "database", Status: probeOK},         // skipped — not unreachable
		{Dep: "unknown", Status: probeUnreachable}, // skipped — not in the switch
	}
	got := mapFailingDepsToServices(results)
	assert.Equal(t, []string{"queue"}, got)
}
