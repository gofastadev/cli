package docs

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Inline fact annotations pin a prose claim to the facts document:
//
//	<!-- fact: scaffold.createdCount = 18 -->
//
// The annotation sits next to the claim it guards; `facts check` fails
// when the facts value no longer equals the annotated one, forcing the
// adjacent prose to be revisited.
var inlineFactRe = regexp.MustCompile(`<!--\s*fact:\s*([A-Za-z0-9_.\[\]]+)\s*=\s*([^>]+?)\s*-->`)

// CheckInlineFacts scans content for fact annotations and validates each
// against the facts document. Returned strings are human-readable problems
// (empty slice = all good).
func CheckInlineFacts(file string, content []byte, f Facts) []string {
	doc, err := factsAsMap(f)
	if err != nil {
		return []string{fmt.Sprintf("internal error flattening facts: %v", err)}
	}

	var problems []string
	for line := range strings.SplitSeq(string(content), "\n") {
		for _, m := range inlineFactRe.FindAllStringSubmatch(line, -1) {
			path, want := m[1], m[2]
			got, err := resolvePath(doc, path)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: fact %q: %v", file, path, err))
				continue
			}
			if got != want {
				problems = append(problems, fmt.Sprintf("%s: fact %q is stale — annotation says %q, facts say %q (update the surrounding prose too)", file, path, want, got))
			}
		}
	}
	return problems
}

// jsonMarshal is a seam: Facts contains no type json.Marshal can reject, so
// the error branches below are unreachable without stubbing this out.
var jsonMarshal = json.Marshal

func factsAsMap(f Facts) (map[string]any, error) {
	raw, err := jsonMarshal(f)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// resolvePath walks a dotted path (with optional [N] array indexes) through
// the JSON-shaped facts document and renders the leaf as a string.
func resolvePath(doc any, path string) (string, error) {
	cur := doc
	for seg := range strings.SplitSeq(path, ".") {
		name, indexes, err := splitIndexes(seg)
		if err != nil {
			return "", err
		}
		if name != "" {
			m, ok := cur.(map[string]any)
			if !ok {
				return "", fmt.Errorf("segment %q: not an object", name)
			}
			cur, ok = m[name]
			if !ok {
				return "", fmt.Errorf("unknown field %q", name)
			}
		}
		for _, idx := range indexes {
			arr, ok := cur.([]any)
			if !ok {
				return "", fmt.Errorf("segment %q: not an array", seg)
			}
			if idx < 0 || idx >= len(arr) {
				return "", fmt.Errorf("segment %q: index out of range", seg)
			}
			cur = arr[idx]
		}
	}
	switch v := cur.(type) {
	case string:
		return v, nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(v), nil
	default:
		return "", fmt.Errorf("path resolves to a non-scalar value")
	}
}

func splitIndexes(seg string) (name string, indexes []int, err error) {
	name = seg
	for {
		open := strings.Index(name, "[")
		if open == -1 {
			return name, indexes, nil
		}
		closing := strings.Index(name, "]")
		if closing < open {
			return "", nil, fmt.Errorf("malformed index in %q", seg)
		}
		idx, convErr := strconv.Atoi(name[open+1 : closing])
		if convErr != nil {
			return "", nil, fmt.Errorf("malformed index in %q", seg)
		}
		indexes = append(indexes, idx)
		name = name[:open] + name[closing+1:]
	}
}
