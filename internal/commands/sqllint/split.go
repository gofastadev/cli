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
	st := newSplitState(sql)
	for st.i < len(st.runes) {
		st.advance()
	}
	if err := st.finishError(); err != nil {
		return nil, err
	}
	st.flush()
	return st.out, nil
}

// splitState is the splitter's run-time state. Each top-level branch in
// SplitStatements moved into a dedicated method on splitState so the
// outer loop stays linear and gocognit doesn't trip on the deeply-nested
// switch-with-state-machine pattern.
type splitState struct {
	out      []string
	buf      strings.Builder
	i        int
	runes    []rune
	dollar   string // active dollar-quote tag, or "" outside one
	inString byte   // '\'', '"', or 0
	inLine   bool
	inBlock  bool
}

func newSplitState(sql string) *splitState {
	return &splitState{runes: []rune(sql)}
}

func (s *splitState) flush() {
	stmt := strings.TrimSpace(s.buf.String())
	if stmt != "" {
		s.out = append(s.out, stmt)
	}
	s.buf.Reset()
}

// finishError surfaces unterminated-context bugs to the caller. Called
// once at EOF, before the final flush.
func (s *splitState) finishError() error {
	switch {
	case s.inString != 0:
		return clierr.Newf(clierr.CodeMigrationParseFailed,
			"unterminated string literal (opened with %q)", string(s.inString))
	case s.inBlock:
		return clierr.New(clierr.CodeMigrationParseFailed,
			"unterminated /* ... */ block comment")
	case s.dollar != "":
		return clierr.Newf(clierr.CodeMigrationParseFailed,
			"unterminated dollar-quote block (tag %s)", s.dollar)
	}
	return nil
}

// advance dispatches one rune through the right context handler.
func (s *splitState) advance() {
	switch {
	case s.inLine:
		s.advanceInLineComment()
	case s.inBlock:
		s.advanceInBlockComment()
	case s.inString != 0:
		s.advanceInString()
	case s.dollar != "":
		s.advanceInDollarQuote()
	default:
		s.advanceTopLevel()
	}
}

func (s *splitState) advanceInLineComment() {
	r := s.runes[s.i]
	s.buf.WriteRune(r)
	if r == '\n' {
		s.inLine = false
	}
	s.i++
}

func (s *splitState) advanceInBlockComment() {
	r := s.runes[s.i]
	s.buf.WriteRune(r)
	if r == '*' && s.i+1 < len(s.runes) && s.runes[s.i+1] == '/' {
		s.buf.WriteRune('/')
		s.i += 2
		s.inBlock = false
		return
	}
	s.i++
}

// advanceInString handles characters inside a '...' or "..." literal.
// SQL doubles the quote to escape it (” or ""); a backslash also
// escapes the following character.
func (s *splitState) advanceInString() {
	r := s.runes[s.i]
	s.buf.WriteRune(r)
	if byte(r) == s.inString {
		if s.i+1 < len(s.runes) && byte(s.runes[s.i+1]) == s.inString {
			s.buf.WriteRune(s.runes[s.i+1])
			s.i += 2
			return
		}
		s.inString = 0
	} else if r == '\\' && s.i+1 < len(s.runes) {
		s.buf.WriteRune(s.runes[s.i+1])
		s.i += 2
		return
	}
	s.i++
}

// advanceInDollarQuote handles characters inside an active $$/$tag$
// block — closes at the matching tag.
func (s *splitState) advanceInDollarQuote() {
	r := s.runes[s.i]
	s.buf.WriteRune(r)
	if r == '$' {
		if rest := string(s.runes[s.i:]); strings.HasPrefix(rest, s.dollar) {
			for j := 1; j < len(s.dollar); j++ {
				s.buf.WriteRune(s.runes[s.i+j])
			}
			s.i += len(s.dollar)
			s.dollar = ""
			return
		}
	}
	s.i++
}

// advanceTopLevel is the default dispatch when no special context is
// active. Detects entry into comments / strings / dollar-quotes and
// terminates statements on semicolons.
func (s *splitState) advanceTopLevel() {
	r := s.runes[s.i]
	switch {
	case r == '-' && s.peek(1) == '-':
		s.buf.WriteRune(r)
		s.buf.WriteRune(s.runes[s.i+1])
		s.i += 2
		s.inLine = true
	case r == '/' && s.peek(1) == '*':
		s.buf.WriteRune(r)
		s.buf.WriteRune(s.runes[s.i+1])
		s.i += 2
		s.inBlock = true
	case r == '\'' || r == '"':
		s.buf.WriteRune(r)
		s.inString = byte(r)
		s.i++
	case r == '$' && s.tryEnterDollarQuote():
		// Side-effect handled in tryEnterDollarQuote; no further action.
	case r == ';':
		s.buf.WriteRune(r)
		s.flush()
		s.i++
	default:
		s.buf.WriteRune(r)
		s.i++
	}
}

// peek returns the rune at offset n from the current position, or 0 if
// out of range. Used by advanceTopLevel to detect two-character starts
// ("--", "/*") without bounds-checking inline.
func (s *splitState) peek(n int) rune {
	if s.i+n >= len(s.runes) {
		return 0
	}
	return s.runes[s.i+n]
}

// tryEnterDollarQuote tests whether the current "$" starts a $$ or
// $tag$ dollar-quote block. If yes, emits the opening tag, advances
// past it, and returns true. If no, returns false (and the caller's
// default branch handles "$" as a regular character).
func (s *splitState) tryEnterDollarQuote() bool {
	j := s.i + 1
	for j < len(s.runes) && (unicode.IsLetter(s.runes[j]) || unicode.IsDigit(s.runes[j]) || s.runes[j] == '_') {
		j++
	}
	if j >= len(s.runes) || s.runes[j] != '$' {
		return false
	}
	s.dollar = string(s.runes[s.i : j+1])
	for k := s.i; k <= j; k++ {
		s.buf.WriteRune(s.runes[k])
	}
	s.i = j + 1
	return true
}
