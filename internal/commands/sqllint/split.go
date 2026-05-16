package sqllint

import (
	"strings"
	"unicode"

	"github.com/gofastadev/cli/internal/clierr"
)

// SplitStatements tokenizes a SQL blob into individual statements, honoring
// quoted strings, line comments, block comments, and Postgres dollar-quote
// blocks. Returns an error wrapping CodeMigrationParseFailed if a quote or
// dollar-quote block is left open at EOF.
//
// The splitter is intentionally simple: it does not parse SQL. It only
// understands the contexts in which a semicolon does NOT terminate a
// statement. That's sufficient for migration files written by humans or
// by gofasta's generators.
func SplitStatements(sql string) ([]string, error) {
	var (
		out      []string
		buf      strings.Builder
		i        int
		runes    = []rune(sql)
		dollar   string // current dollar-quote tag ("$$", "$tag$", or "" when not in one)
		inString byte   // '\'', '"' or 0
		inLine   bool   // -- line comment
		inBlock  bool   // /* block comment */
	)

	flush := func() {
		s := strings.TrimSpace(buf.String())
		if s != "" {
			out = append(out, s)
		}
		buf.Reset()
	}

	for i < len(runes) {
		r := runes[i]

		// Line comment runs to newline.
		if inLine {
			buf.WriteRune(r)
			if r == '\n' {
				inLine = false
			}
			i++
			continue
		}

		// Block comment runs to "*/".
		if inBlock {
			buf.WriteRune(r)
			if r == '*' && i+1 < len(runes) && runes[i+1] == '/' {
				buf.WriteRune('/')
				i += 2
				inBlock = false
				continue
			}
			i++
			continue
		}

		// Inside a string literal — only the matching quote (not preceded
		// by an escape backslash) closes it. SQL doubles ('' or "") also
		// escape the quote; we handle those by peeking ahead.
		if inString != 0 {
			buf.WriteRune(r)
			if byte(r) == inString {
				if i+1 < len(runes) && byte(runes[i+1]) == inString {
					// Doubled quote — write the second and continue inside.
					buf.WriteRune(runes[i+1])
					i += 2
					continue
				}
				inString = 0
			} else if r == '\\' && i+1 < len(runes) {
				// Backslash-escape: consume the next rune verbatim.
				buf.WriteRune(runes[i+1])
				i += 2
				continue
			}
			i++
			continue
		}

		// Inside a dollar-quote block — closes at matching tag.
		if dollar != "" {
			buf.WriteRune(r)
			if r == '$' {
				if rest := string(runes[i:]); strings.HasPrefix(rest, dollar) {
					for j := 1; j < len(dollar); j++ {
						buf.WriteRune(runes[i+j])
					}
					i += len(dollar)
					dollar = ""
					continue
				}
			}
			i++
			continue
		}

		// Top-level: detect entry into a special context first.
		if r == '-' && i+1 < len(runes) && runes[i+1] == '-' {
			buf.WriteRune(r)
			buf.WriteRune(runes[i+1])
			i += 2
			inLine = true
			continue
		}
		if r == '/' && i+1 < len(runes) && runes[i+1] == '*' {
			buf.WriteRune(r)
			buf.WriteRune(runes[i+1])
			i += 2
			inBlock = true
			continue
		}
		if r == '\'' || r == '"' {
			buf.WriteRune(r)
			inString = byte(r)
			i++
			continue
		}
		if r == '$' {
			// Detect dollar-quote tag: $$, $tag$ (tag is letters/digits/_).
			j := i + 1
			for j < len(runes) && (unicode.IsLetter(runes[j]) || unicode.IsDigit(runes[j]) || runes[j] == '_') {
				j++
			}
			if j < len(runes) && runes[j] == '$' {
				dollar = string(runes[i : j+1])
				for k := i; k <= j; k++ {
					buf.WriteRune(runes[k])
				}
				i = j + 1
				continue
			}
		}

		// Statement terminator.
		if r == ';' {
			buf.WriteRune(r)
			flush()
			i++
			continue
		}

		buf.WriteRune(r)
		i++
	}

	// Unterminated contexts are migration bugs — surface them.
	switch {
	case inString != 0:
		return nil, clierr.Newf(clierr.CodeMigrationParseFailed,
			"unterminated string literal (opened with %q)", string(inString))
	case inBlock:
		return nil, clierr.New(clierr.CodeMigrationParseFailed,
			"unterminated /* ... */ block comment")
	case dollar != "":
		return nil, clierr.Newf(clierr.CodeMigrationParseFailed,
			"unterminated dollar-quote block (tag %s)", dollar)
	}

	flush()
	return out, nil
}
