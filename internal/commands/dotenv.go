package commands

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Managed-block markers identify a region of .env that the dev
// preflight menu's "save" prompt owns. Anything inside the block can
// be regenerated; anything outside is the user's hand-edited content
// and we never touch it.
//
// The markers are intentionally generic ("dev preflight override")
// rather than tool-branded — a project's .env belongs to the project,
// not to the CLI that scaffolded it. Keeping the markers free of
// "GOFASTA_" matches the wider opt-out-defaults framing: the file
// looks like a normal project file, with one auto-managed section.
const (
	managedBlockBegin = "# >>> auto-managed: dev preflight override (do not edit by hand) >>>"
	managedBlockEnd   = "# <<< auto-managed: dev preflight override <<<"
)

// loadDotEnv reads a .env-style file and sets each KEY=VALUE pair as a
// process environment variable (via os.Setenv), returning the number of
// variables set. Pre-existing env vars are NOT overwritten — the running
// shell always wins, matching the conventional `godotenv` semantics. If
// the file doesn't exist it returns (0, nil) so callers can invoke this
// unconditionally without gating on a Stat.
//
// Supported syntax (intentionally minimal — no interpolation, no inline
// eval, no quoting games):
//
//	KEY=value                       → os.Setenv("KEY", "value")
//	KEY="value with spaces"         → quoted values allowed, quotes stripped
//	KEY='value with spaces'         → single quotes allowed, quotes stripped
//	# comment                       → whole-line comments ignored
//	<blank line>                    → ignored
//
// Any line that doesn't match KEY=VALUE is silently skipped. No escape
// sequence handling — the goal is to cover 99% of dev .env files, not to
// reimplement dotenv fully.
func loadDotEnv(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("open %s: %w", path, err)
	}

	managed, rest := splitManagedBlock(string(data))

	// Apply the managed block FIRST. Both passes respect "shell wins"
	// (only set if not already in env), but applying managed first
	// means within the file the managed values win over the user's
	// hand-edited entries — which is the contract for the persisted-
	// override block: the menu's last "save" decides the current
	// session's defaults, not the unchanged top-of-file entries.
	count := 0
	count += applyDotEnvLines(managed)
	count += applyDotEnvLines(rest)
	return count, nil
}

// applyDotEnvLines walks one or more .env-format lines and sets each
// KEY=VALUE in process env, respecting the "shell wins" rule. Returns
// the number of variables actually set (skipped duplicates not counted).
func applyDotEnvLines(lines []string) int {
	count := 0
	for _, line := range lines {
		key, val, ok := parseDotEnvLine(line)
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, val)
		count++
	}
	return count
}

// splitManagedBlock scans .env content and separates lines INSIDE the
// managed block from lines outside. Marker lines themselves are
// discarded — they're comments, not data. Order within each group is
// preserved.
//
// If the markers are missing or unbalanced (e.g. begin without end),
// no managed block is detected and everything is returned in `rest`.
// This keeps malformed files from silently splitting in surprising
// ways; the next "save" call will re-emit a clean block.
//
// Splits on `\n` rather than using bufio.Scanner so an unusually long
// line (>64KB — the scanner's default token limit) doesn't get
// silently truncated. A user could paste a base64-encoded TLS cert
// or similar; we shouldn't lose it.
func splitManagedBlock(content string) (managed, rest []string) {
	// Strip an optional trailing newline so we don't emit a phantom
	// empty line at the end of `rest`.
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return nil, nil
	}
	inBlock := false
	sawEnd := false
	var collected []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == managedBlockBegin:
			inBlock = true
		case trimmed == managedBlockEnd:
			inBlock = false
			sawEnd = true
		case inBlock:
			collected = append(collected, line)
		default:
			rest = append(rest, line)
		}
	}
	// If we started a block but never closed it, treat the collected
	// lines as plain content — better to ignore the partial block than
	// to grant unmarked lines priority.
	if !sawEnd && len(collected) > 0 {
		rest = append(rest, collected...)
		return nil, rest
	}
	return collected, rest
}

// mergeIntoDotEnv atomically updates `path` so each KEY in `kvs` is
// set to its VALUE, with one occurrence per key and no duplication.
// The merge rules:
//
//  1. If the key already appears in the file, the FIRST occurrence's
//     value is replaced in place; later duplicates of the same key
//     are removed. The line position is preserved so existing
//     comments, ordering, and structure stay intact — the user's
//     `.env` looks like the user wrote it, with new values where
//     applicable.
//  2. If the key does NOT appear, it's appended at the end (in
//     sorted order across all newly-added keys, for diff stability).
//  3. Keys NOT in `kvs` are left untouched — including duplicates,
//     which we deliberately do not "clean up" since they may be
//     intentional.
//  4. Any LEGACY managed-block markers (the prior
//     "# >>> auto-managed" wrappers we used to emit) are stripped
//     and the inner lines fold back into the regular file body.
//     This migrates older `.env` files cleanly the first time they
//     pass through this function.
//
// Writes are atomic via tmp + rename: a crash mid-write leaves
// either the old content or the new content intact, never a
// half-written file.
//
// Passing an empty `kvs` is a no-op (file untouched). Callers that
// want to "revert" should edit `.env` by hand or call this function
// with an explicit set of keys they want changed.
func mergeIntoDotEnv(path string, kvs map[string]string) error {
	if len(kvs) == 0 {
		return nil
	}

	var content string
	if data, err := os.ReadFile(path); err == nil {
		content = string(data)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	// Drop legacy managed-block markers — inner lines stay where
	// they were, just no longer fenced.
	content = stripManagedBlockMarkers(content)

	// Split into lines for line-aware editing.
	trailingNewline := strings.HasSuffix(content, "\n")
	content = strings.TrimRight(content, "\n")
	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
	}

	out, pending := mergeReplaceInPlace(lines, kvs)
	out = mergeAppendPending(out, pending, kvs)

	body := strings.Join(out, "\n")
	if trailingNewline || len(out) > 0 {
		body += "\n"
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename tmp: %w", err)
	}
	return nil
}

// mergeReplaceInPlace walks `lines` and, for each line whose key is
// in `kvs`, emits a fresh `KEY=quoted(VALUE)` at the same position;
// LATER duplicates of the same key are dropped so the file ends with
// exactly one occurrence per persisted key. Lines for keys NOT in
// `kvs` pass through untouched (including any duplicates the user
// authored on purpose).
//
// Returns (rewritten lines, pending) where `pending` is the subset
// of `kvs` keys NOT encountered in the file — those get appended by
// mergeAppendPending.
func mergeReplaceInPlace(lines []string, kvs map[string]string) (rewritten []string, pending map[string]bool) {
	pending = make(map[string]bool, len(kvs))
	for k := range kvs {
		pending[k] = true
	}
	seen := make(map[string]bool, len(kvs))
	rewritten = make([]string, 0, len(lines)+len(kvs))
	for _, line := range lines {
		key, _, ok := parseDotEnvLine(line)
		if !ok {
			rewritten = append(rewritten, line) // comment, blank, malformed
			continue
		}
		if _, isTarget := kvs[key]; !isTarget {
			rewritten = append(rewritten, line) // not being persisted — pass through
			continue
		}
		if seen[key] {
			continue // later duplicate of a persisted key — drop
		}
		rewritten = append(rewritten, key+"="+quoteDotEnvValue(kvs[key]))
		seen[key] = true
		delete(pending, key)
	}
	return rewritten, pending
}

// mergeAppendPending tacks any unconsumed `pending` keys onto the
// end of `out`, in sorted order for diff stability. A blank line
// separates the appended block from preceding content (purely
// cosmetic; keeps diffs tidy when the user opens .env).
func mergeAppendPending(out []string, pending map[string]bool, kvs map[string]string) []string {
	if len(pending) == 0 {
		return out
	}
	if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
		out = append(out, "")
	}
	keys := make([]string, 0, len(pending))
	for k := range pending {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, k+"="+quoteDotEnvValue(kvs[k]))
	}
	return out
}

// stripManagedBlockMarkers removes any line equal to the legacy
// managed-block begin/end markers, leaving everything else intact.
// Used by mergeIntoDotEnv to migrate older .env files written by
// the previous writeManagedBlock implementation — once those marker
// lines are gone the file is a clean, normal .env.
//
// No-op for files that never had markers.
func stripManagedBlockMarkers(content string) string {
	if !strings.Contains(content, managedBlockBegin) && !strings.Contains(content, managedBlockEnd) {
		return content
	}
	trailingNewline := strings.HasSuffix(content, "\n")
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return ""
	}
	out := make([]string, 0)
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == managedBlockBegin || trimmed == managedBlockEnd {
			continue
		}
		out = append(out, line)
	}
	body := strings.Join(out, "\n")
	if trailingNewline {
		body += "\n"
	}
	return body
}

// quoteDotEnvValue wraps the value in double quotes ONLY if it
// contains whitespace or characters a naive .env parser would
// mis-interpret (#, =, quotes, newlines). The vast majority of values
// (URLs, hostnames, passwords with letters/digits) need no quoting —
// keeping them unquoted makes the file easy to scan by eye.
func quoteDotEnvValue(v string) string {
	if v == "" {
		return ""
	}
	needsQuote := false
	for _, r := range v {
		if r == ' ' || r == '\t' || r == '#' || r == '"' || r == '\n' || r == '\r' {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return v
	}
	// Escape embedded double quotes; we use double quotes for the
	// wrapper because they're more common in env-file conventions.
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}

// parseDotEnvLine returns (key, val, ok) for a single .env-style line.
// Returns ok=false for blank lines, comments, or malformed entries so the
// caller can skip them uniformly. Extracted from loadDotEnv to keep that
// function's cyclomatic complexity under the linter limit.
func parseDotEnvLine(raw string) (key, val string, ok bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	// Split once on "=". Values containing "=" are preserved. An `=` at
	// position 0 is rejected via the eq<=0 guard (a line that starts with
	// `=` has no key). We intentionally do not re-check for an empty key
	// after TrimSpace(line[:eq]) because `line` is already TrimSpace'd at
	// the top of the function — its first byte is non-whitespace, so
	// line[:eq] always starts with a non-whitespace byte whenever eq>0,
	// and TrimSpace of a string whose first byte is non-whitespace can
	// never produce "".
	eq := strings.IndexByte(line, '=')
	if eq <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:eq])
	val = strings.TrimSpace(line[eq+1:])
	// Strip matching surrounding quotes so `FOO="bar baz"` becomes `bar baz`.
	if len(val) >= 2 {
		first, last := val[0], val[len(val)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			val = val[1 : len(val)-1]
		}
	}
	return key, val, true
}
