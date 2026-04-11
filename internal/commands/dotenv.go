package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"
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
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, val, ok := parseDotEnvLine(scanner.Text())
		if !ok {
			continue
		}
		// Respect the "shell wins" rule — only set if not already set.
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return count, fmt.Errorf("setenv %s: %w", key, err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("read %s: %w", path, err)
	}
	return count, nil
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
	// Split once on "=". Values containing "=" are preserved.
	eq := strings.IndexByte(line, '=')
	if eq <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:eq])
	val = strings.TrimSpace(line[eq+1:])
	if key == "" {
		return "", "", false
	}
	// Strip matching surrounding quotes so `FOO="bar baz"` becomes `bar baz`.
	if len(val) >= 2 {
		first, last := val[0], val[len(val)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			val = val[1 : len(val)-1]
		}
	}
	return key, val, true
}
