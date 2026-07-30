// Package featurize — gqlgen.yml rewriting.
//
// gqlgen.yml is not Go source, so the dst-based engines don't apply.
// The file is comment-heavy and hand-edited by users, so a YAML
// round-trip (which would drop comments and re-indent) is off the
// table too. Instead the rewrite is anchored line edits — the same
// approach refactor's flipLayoutInConfig takes for config.yaml — which
// preserves every byte it doesn't explicitly target and therefore
// round-trips exactly.
//
// What changes between layouts:
//
//   - model.filename: gqlgen's generated models file follows the shared
//     dtos package (app/dtos/ ⇄ app/shared/dtos/), in step with the
//     SharedRelocations() entry for generated-types.dtos.go.
//   - autobind: schema type names (User, TCreateUserDto, …) bind
//     against the packages listed here. Layered, that's the single
//     app/dtos package. Feature, the hand-written per-resource DTOs
//     live in app/<snake>/ — so the list becomes app/shared/dtos plus
//     one entry per feature resource. Without the per-resource entries
//     gqlgen would silently regenerate duplicate models and the
//     preserved resolver bodies would stop compiling.

package featurize

import (
	"strings"
)

const generatedTypesLayered = "app/dtos/generated-types.dtos.go"
const generatedTypesFeature = "app/shared/dtos/generated-types.dtos.go"

// autobindLine renders one autobind list entry with the given
// indentation, e.g. ` - "github.com/acme/x/app/dtos"`.
func autobindLine(indent, importPath string) string {
	return indent + `- "` + importPath + `"`
}

// listEntryIndent returns the leading whitespace of a YAML list line.
func listEntryIndent(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// RewriteGqlgenConfig rewrites gqlgen.yml from the layered to the
// feature shape:
//
//   - model.filename: app/dtos/… → app/shared/dtos/…
//   - autobind: `- "<mod>/app/dtos"` → `- "<mod>/app/shared/dtos"`,
//     followed by one `- "<mod>/app/<snake>"` entry per resource.
//
// Idempotent: running it twice produces the same bytes as running it
// once. Everything not targeted (comments, indentation, the exec /
// resolver / models sections) is preserved verbatim.
func RewriteGqlgenConfig(src []byte, mod string, resources []Resource) []byte {
	lines := strings.Split(string(src), "\n")
	out := make([]string, 0, len(lines)+len(resources))

	present := map[string]bool{}
	for _, line := range lines {
		present[strings.TrimSpace(line)] = true
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// model.filename follows the generated-types relocation.
		if strings.HasPrefix(trimmed, "filename:") && strings.Contains(line, generatedTypesLayered) {
			out = append(out, strings.Replace(line, generatedTypesLayered, generatedTypesFeature, 1))
			continue
		}

		// autobind: flip the dtos entry and append the per-resource
		// feature packages right after it.
		if trimmed == `- "`+mod+`/app/dtos"` {
			indent := listEntryIndent(line)
			out = append(out, autobindLine(indent, mod+"/app/shared/dtos"))
			for _, r := range resources {
				entry := autobindLine(indent, mod+"/app/"+r.Snake)
				if present[strings.TrimSpace(entry)] {
					continue
				}
				out = append(out, entry)
			}
			continue
		}

		// Already-flipped shared entry (idempotent re-run): make sure
		// every resource entry exists after it, without duplicating.
		if trimmed == `- "`+mod+`/app/shared/dtos"` {
			indent := listEntryIndent(line)
			out = append(out, line)
			for _, r := range resources {
				entry := autobindLine(indent, mod+"/app/"+r.Snake)
				if present[strings.TrimSpace(entry)] {
					continue
				}
				out = append(out, entry)
			}
			continue
		}

		out = append(out, line)
	}

	return []byte(strings.Join(out, "\n"))
}

// RewriteGqlgenConfigReverse rewrites gqlgen.yml from the feature shape
// back to layered — the exact inverse of RewriteGqlgenConfig: the
// model.filename flips back, the shared dtos autobind entry becomes
// app/dtos again, and every per-resource autobind entry is removed.
func RewriteGqlgenConfigReverse(src []byte, mod string, resources []Resource) []byte {
	perResource := map[string]bool{}
	for _, r := range resources {
		perResource[`- "`+mod+`/app/`+r.Snake+`"`] = true
	}

	lines := strings.Split(string(src), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "filename:") && strings.Contains(line, generatedTypesFeature) {
			out = append(out, strings.Replace(line, generatedTypesFeature, generatedTypesLayered, 1))
			continue
		}
		if trimmed == `- "`+mod+`/app/shared/dtos"` {
			out = append(out, autobindLine(listEntryIndent(line), mod+"/app/dtos"))
			continue
		}
		if perResource[trimmed] {
			continue // per-resource autobind entries only exist in feature shape
		}

		out = append(out, line)
	}

	return []byte(strings.Join(out, "\n"))
}

// EnsureGqlgenAutobind inserts `- "<mod>/app/<snake>"` into the
// autobind list of a FEATURE-shaped gqlgen.yml, anchored right after
// the `<mod>/app/shared/dtos` entry. Used by `gofasta g` when
// scaffolding a new resource into a feature-layout GraphQL project.
//
// No-op (returns src unchanged) when the entry is already present or
// when the shared-dtos anchor is missing — callers detect "nothing
// changed" by comparing the returned bytes with the input.
func EnsureGqlgenAutobind(src []byte, mod, snake string) []byte {
	entry := `- "` + mod + `/app/` + snake + `"`
	lines := strings.Split(string(src), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == entry {
			return src
		}
	}

	anchor := `- "` + mod + `/app/shared/dtos"`
	out := make([]string, 0, len(lines)+1)
	inserted := false
	for _, line := range lines {
		out = append(out, line)
		if !inserted && strings.TrimSpace(line) == anchor {
			out = append(out, autobindLine(listEntryIndent(line), mod+"/app/"+snake))
			inserted = true
		}
	}
	if !inserted {
		return src
	}
	return []byte(strings.Join(out, "\n"))
}
