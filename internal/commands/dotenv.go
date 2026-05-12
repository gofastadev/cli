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

// writeManagedBlock atomically rewrites the managed block in `path`
// with the given KVs. Any existing managed block is replaced; content
// outside the block is preserved byte-for-byte. The file is created
// (with 0o644 perms) if it does not exist.
//
// Writes are atomic via os.WriteFile + os.Rename: we materialize the
// new content in `<path>.tmp` first, then rename. A crash mid-write
// leaves either the old content or the new content intact, never a
// half-written file.
//
// Keys are emitted in sorted order so the block diffs cleanly across
// saves — important because the user can see the block in git, and a
// stable order avoids noisy "reordered keys" diffs.
//
// Passing an empty `kvs` map removes the managed block entirely
// (leaving the file otherwise untouched), so the menu's "revert"
// affordance has a clean primitive.
func writeManagedBlock(path string, kvs map[string]string) error {
	var existing []byte
	if data, err := os.ReadFile(path); err == nil {
		existing = data
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	_, rest := splitManagedBlock(string(existing))

	var out strings.Builder
	for _, line := range rest {
		out.WriteString(line)
		out.WriteByte('\n')
	}

	if len(kvs) > 0 {
		// Ensure a blank line between user content and the managed
		// block, but only if the user content didn't already end with
		// one. Cosmetic; keeps diffs tidy.
		if len(rest) > 0 && strings.TrimSpace(rest[len(rest)-1]) != "" {
			out.WriteByte('\n')
		}
		out.WriteString(managedBlockBegin)
		out.WriteByte('\n')

		keys := make([]string, 0, len(kvs))
		for k := range kvs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out.WriteString(k)
			out.WriteByte('=')
			out.WriteString(quoteDotEnvValue(kvs[k]))
			out.WriteByte('\n')
		}
		out.WriteString(managedBlockEnd)
		out.WriteByte('\n')
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(out.String()), 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename tmp: %w", err)
	}
	return nil
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
