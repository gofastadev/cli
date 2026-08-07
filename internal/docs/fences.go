package docs

import (
	"strings"
)

// Invocation is one `gofasta …` command line found inside a fenced code
// block of a markdown document.
type Invocation struct {
	File   string
	Line   int // 1-based line of the (first line of the) logical command
	Tokens []string
}

// fence info strings whose contents are shell commands worth validating.
var shellInfoStrings = map[string]bool{
	"": true, "bash": true, "sh": true, "shell": true, "console": true, "terminal": true, "zsh": true,
}

// binary spellings that count as invoking the CLI.
var gofastaSpellings = map[string]bool{
	"gofasta": true, "./bin/gofasta": true, "$(CURDIR)/bin/gofasta": true,
}

// ExtractInvocations scans markdown content and returns every gofasta
// invocation found in shell-ish fenced code blocks. Lines containing Go
// template directives ({{ … }}) are skipped so skeleton templates can be
// scanned too.
func ExtractInvocations(file string, content []byte) []Invocation {
	var out []Invocation

	lines := strings.Split(string(content), "\n")
	inFence := false
	fenceMarker := ""
	shellFence := false
	promptFence := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if marker, info, isFence := fenceLine(trimmed); isFence {
			if !inFence {
				inFence = true
				fenceMarker = marker
				shellFence = shellInfoStrings[strings.ToLower(info)]
				promptFence = fenceUsesPrompts(lines, i+1, marker)
			} else if strings.HasPrefix(trimmed, fenceMarker) {
				inFence = false
			}
			continue
		}
		if !inFence || !shellFence {
			continue
		}
		if strings.Contains(line, "{{") {
			continue
		}
		// Console-style fences mix commands and output; when the fence
		// uses `$ ` prompts, only prompt-prefixed lines are commands.
		if promptFence && !strings.HasPrefix(trimmed, "$") {
			continue
		}

		// Join trailing-backslash continuations into one logical line.
		startLine := i + 1
		logical := line
		for strings.HasSuffix(strings.TrimRight(logical, " \t"), "\\") && i+1 < len(lines) {
			logical = strings.TrimSuffix(strings.TrimRight(logical, " \t"), "\\") + " " + lines[i+1]
			i++
		}

		for _, segment := range splitSegments(logical) {
			tokens := tokenize(segment)
			tokens = stripPromptAndEnv(tokens)
			if len(tokens) == 0 || !gofastaSpellings[tokens[0]] {
				continue
			}
			out = append(out, Invocation{File: file, Line: startLine, Tokens: tokens})
		}
	}
	return out
}

// fenceUsesPrompts reports whether the fence starting at line index start
// contains any `$ `-prompted line before its closing marker.
func fenceUsesPrompts(lines []string, start int, marker string) bool {
	for j := start; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j])
		if strings.HasPrefix(t, marker) {
			return false
		}
		if strings.HasPrefix(t, "$ ") {
			return true
		}
	}
	return false
}

// fenceLine reports whether a trimmed line opens/closes a fence, returning
// the fence marker (``` or ~~~) and the info string.
func fenceLine(trimmed string) (marker, info string, ok bool) {
	for _, m := range []string{"```", "~~~"} {
		if rest, found := strings.CutPrefix(trimmed, m); found {
			return m, strings.TrimSpace(rest), true
		}
	}
	return "", "", false
}

// splitSegments splits a shell line on unquoted && ; | separators and
// drops per-segment trailing comments.
func splitSegments(line string) []string {
	var segments []string
	var cur strings.Builder
	inSingle, inDouble := false, false

	flush := func() {
		s := strings.TrimSpace(cur.String())
		if s != "" {
			segments = append(segments, s)
		}
		cur.Reset()
	}

	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case !inSingle && !inDouble:
			if c == '#' {
				flush()
				return segments
			}
			if c == '&' && i+1 < len(runes) && runes[i+1] == '&' {
				flush()
				i++
				continue
			}
			if c == ';' || c == '|' {
				flush()
				continue
			}
		}
		cur.WriteRune(c)
	}
	flush()
	return segments
}

// tokenize splits a segment on whitespace, honoring single/double quotes.
func tokenize(segment string) []string {
	var tokens []string
	var cur strings.Builder
	inSingle, inDouble := false, false

	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}

	for _, c := range segment {
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case (c == ' ' || c == '\t') && !inSingle && !inDouble:
			flush()
		default:
			cur.WriteRune(c)
		}
	}
	flush()
	return tokens
}

// stripPromptAndEnv removes a leading shell prompt ("$"/">") and NAME=value
// environment prefixes.
func stripPromptAndEnv(tokens []string) []string {
	for len(tokens) > 0 && (tokens[0] == "$" || tokens[0] == ">") {
		tokens = tokens[1:]
	}
	for len(tokens) > 0 && isEnvAssignment(tokens[0]) {
		tokens = tokens[1:]
	}
	return tokens
}

func isEnvAssignment(tok string) bool {
	eq := strings.Index(tok, "=")
	if eq <= 0 {
		return false
	}
	for _, c := range tok[:eq] {
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}
