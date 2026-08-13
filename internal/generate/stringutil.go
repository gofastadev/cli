// stringutil.go — thin delegates over internal/naming, the single
// source of truth for name derivation. Kept as package-local function
// names so the many call sites across the generators read naturally.
package generate

import (
	"strings"

	"github.com/gofastadev/cli/internal/naming"
)

// toPascalCase converts any accepted input shape to initialism-aware
// PascalCase: "api_key" → "APIKey". Resources and fields share the same
// conversion so `g rename`, `g relation`, and the scaffold agree on
// every derived identifier.
func toPascalCase(s string) string { return naming.Pascal(s) }

// fieldPascalCase is an alias of toPascalCase retained for call sites
// that read better with the field-specific name.
func fieldPascalCase(s string) string { return naming.Pascal(s) }

// toSnakeCase is the initialism-aware inverse: "APIKey" → "api_key",
// "OwnerID" → "owner_id". Also normalizes kebab-case ("send-email" →
// "send_email") so generated file names are consistent.
func toSnakeCase(s string) string { return naming.Snake(s) }

// toCamelCase is the PLAIN camel form used for JSON tag names and
// generated parameter names: "owner_id" → "ownerId" (not "ownerID").
// Deliberately not initialism-aware — the wire format's key style is
// lowerCamel with plain word capitalization, and changing it would
// change every generated API's JSON contract.
func toCamelCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' })
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	joined := strings.Join(parts, "")
	if joined == "" {
		return joined
	}
	return strings.ToLower(joined[:1]) + joined[1:]
}

// pluralize returns the English plural, initialism-aware at the tail
// (API → APIs, never APIes).
func pluralize(s string) string { return naming.Pluralize(s) }
