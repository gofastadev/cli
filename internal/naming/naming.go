// Package naming is the single source of truth for every name the CLI
// derives from user input: Go type names, receivers, file paths, SQL
// table/column names, GraphQL SDL names. It is initialism-aware in BOTH
// directions — "api_key" ⇄ "APIKey" — because the generated project's
// own linter (revive var-naming) rejects ApiKey/OwnerId, and a naive
// per-capital snake inverse turns APIKey into a_p_i_key, silently
// orphaning tables, trigger names, and migrations.
//
// The package deliberately imports nothing from the rest of the CLI so
// internal/generate, internal/commands, and internal/featurize can all
// depend on it without cycles.
package naming

import (
	"regexp"
	"strings"
	"unicode"
)

// CommonInitialisms maps lowercase name segments to their Go-idiomatic
// all-caps form. Mirrors revive's var-naming list for the segments that
// plausibly appear in resource and column names.
var CommonInitialisms = map[string]string{
	"api": "API", "cpu": "CPU", "db": "DB", "dns": "DNS", "eof": "EOF",
	"guid": "GUID", "html": "HTML", "http": "HTTP", "https": "HTTPS",
	"id": "ID", "ip": "IP", "json": "JSON", "ram": "RAM", "sku": "SKU",
	"sql": "SQL", "ssh": "SSH", "tcp": "TCP", "tls": "TLS", "ttl": "TTL",
	"udp": "UDP", "ui": "UI", "uid": "UID", "uri": "URI", "url": "URL",
	"utf8": "UTF8", "uuid": "UUID", "vm": "VM", "xml": "XML",
}

// words splits a name into its lowercase word segments. Accepts every
// input shape the CLI sees: snake_case, kebab-case, PascalCase,
// camelCase, and initialism-bearing Pascal (APIKey → [api key],
// OwnerID → [owner id], UTF8Name → [utf8 name]).
//
// Splitting rules:
//   - '_' and '-' are separators.
//   - a lower/digit → Upper boundary starts a new word.
//   - inside an ALL-CAPS run, the last capital starts the next word
//     when a lowercase letter follows (APIKey → API|Key) — EXCEPT when
//     that trailing lowercase is a lone plural 's' ending the word
//     (APIs → [apis], DNSs → [dnss]), which belongs to the initialism.
func words(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.ToLower(string(cur)))
			cur = nil
		}
	}
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-':
			flush()
		case unicode.IsUpper(r):
			prevLowerish := i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]))
			prevUpper := i > 0 && unicode.IsUpper(runes[i-1])
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			splitHere := prevLowerish || (prevUpper && nextLower && !isTrailingPluralS(runes, i+1))
			if splitHere {
				flush()
			}
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return out
}

// isTrailingPluralS reports whether the lowercase run starting at i is
// exactly "s" followed by a word boundary — the plural tail of an
// initialism (APIs, DNSs, URLs).
func isTrailingPluralS(runes []rune, i int) bool {
	if i >= len(runes) || runes[i] != 's' {
		return false
	}
	next := i + 1
	return next >= len(runes) || !unicode.IsLower(runes[next])
}

// pascalWord renders one lowercase word segment in PascalCase, mapping
// known initialisms to their all-caps form.
func pascalWord(w string) string {
	if up, ok := CommonInitialisms[w]; ok {
		return up
	}
	if w == "" {
		return ""
	}
	return strings.ToUpper(w[:1]) + w[1:]
}

// Pascal converts any accepted input shape to initialism-aware
// PascalCase: "api_key" → "APIKey", "owner-id" → "OwnerID",
// "user" → "User". Idempotent on its own output.
func Pascal(s string) string {
	var b strings.Builder
	for _, w := range words(s) {
		writeJoinedWord(&b, w)
	}
	return b.String()
}

// writeJoinedWord appends a word to a Pascal/Camel join. A case join
// cannot mark a boundary before a digit-leading segment (digits have
// no case): "a_00" would collapse to "A00" and Snake could never
// recover the "a_00" the user wrote — so the underscore is kept for
// digit-leading segments. Found by FuzzNamingRoundTrip with "A_00".
func writeJoinedWord(b *strings.Builder, w string) {
	if b.Len() > 0 && w != "" && w[0] >= '0' && w[0] <= '9' {
		b.WriteString("_")
	}
	b.WriteString(pascalWord(w))
}

// Camel is Pascal with the first word fully lowered — the golint camel
// form where a leading initialism drops entirely to lowercase:
// "api_key" → "apiKey", "owner_id" → "ownerID", "user" → "user".
func Camel(s string) string {
	ws := words(s)
	if len(ws) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(ws[0])
	for _, w := range ws[1:] {
		writeJoinedWord(&b, w)
	}
	return b.String()
}

// Snake converts any accepted input shape to snake_case — the
// initialism-aware inverse of Pascal: "APIKey" → "api_key",
// "OwnerID" → "owner_id", "UTF8Name" → "utf8_name". Snake(Pascal(x))
// is identity for snake inputs, with one documented exception:
// adjacent segments that both pascalize to all-caps ("api_id" →
// "APIID") cannot encode their boundary in PascalCase, so the round
// trip yields "apiid". Database columns are unaffected — GORM's
// NamingStrategy makes the same split we do (APIID → api_id).
func Snake(s string) string {
	return strings.Join(words(s), "_")
}

// Pluralize returns the English plural of a PascalCase (or plain) name.
// A trailing all-caps run of length ≥ 2 — an initialism tail — takes a
// bare "s" (API → APIs, DNS → DNSs) instead of the suffix rules that
// would produce DNSes. Otherwise the usual rules apply: s/x/z/ch/sh →
// +es, consonant+y → ies, else +s.
func Pluralize(s string) string {
	if hasInitialismTail(s) {
		return s + "s"
	}
	if strings.HasSuffix(s, "s") || strings.HasSuffix(s, "x") || strings.HasSuffix(s, "z") ||
		strings.HasSuffix(s, "ch") || strings.HasSuffix(s, "sh") {
		return s + "es"
	}
	if strings.HasSuffix(s, "y") && len(s) > 1 {
		c := s[len(s)-2]
		if c != 'a' && c != 'e' && c != 'i' && c != 'o' && c != 'u' {
			return s[:len(s)-1] + "ies"
		}
	}
	return s + "s"
}

// hasInitialismTail reports whether s ends in an all-caps run of
// length ≥ 2 (API, OwnerID, DNS).
func hasInitialismTail(s string) bool {
	runes := []rune(s)
	n := 0
	for i := len(runes) - 1; i >= 0 && unicode.IsUpper(runes[i]); i-- {
		n++
	}
	return n >= 2
}

// resourceNamePattern is the strict allow-list for resource names: no
// hyphens, because feature-layout package aliases (<snake>pkg) and Go
// package directories derive from them and `blog-postpkg` is not a
// valid Go identifier.
var resourceNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// identifierPattern additionally allows hyphens — job, task, and email
// template names become file names and cron identifiers, never Go
// identifiers, and kebab-case is their natural spelling.
var identifierPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// IsResourceName reports whether s is acceptable as a resource name
// (letters, digits, underscores; starts with a letter; NO hyphens).
func IsResourceName(s string) bool {
	return resourceNamePattern.MatchString(s)
}

// IsIdentifier reports whether s is acceptable as a generic generated
// identifier (resource rules plus hyphens — jobs/tasks/templates).
func IsIdentifier(s string) bool {
	return identifierPattern.MatchString(s)
}
