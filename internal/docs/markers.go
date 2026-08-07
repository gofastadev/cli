package docs

import (
	"fmt"
	"sort"
	"strings"
)

// Marker syntax: <!-- gofasta:begin <id> --> … <!-- gofasta:end <id> -->.
// Content between a pair is generated — `facts sync` rewrites it wholesale,
// `facts check` verifies it matches what would be generated.

func markerBegin(id string) string { return fmt.Sprintf("<!-- gofasta:begin %s -->", id) }
func markerEnd(id string) string   { return fmt.Sprintf("<!-- gofasta:end %s -->", id) }

// ReplaceBlocks swaps every block's body in src. Every id in blocks must
// exist exactly once in src (both markers, in order) or an error is
// returned naming the problem. Ids are processed in sorted order so error
// messages are deterministic.
func ReplaceBlocks(src string, blocks map[string]string) (string, error) {
	ids := make([]string, 0, len(blocks))
	for id := range blocks {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := src
	for _, id := range ids {
		var err error
		out, err = replaceBlock(out, id, blocks[id])
		if err != nil {
			return "", err
		}
	}
	return out, nil
}

func replaceBlock(src, id, body string) (string, error) {
	begin := markerBegin(id)
	end := markerEnd(id)

	beginIdx := strings.Index(src, begin)
	if beginIdx == -1 {
		return "", fmt.Errorf("missing marker %q", begin)
	}
	if strings.Contains(src[beginIdx+len(begin):], begin) {
		return "", fmt.Errorf("duplicate marker %q", begin)
	}
	endIdx := strings.Index(src, end)
	if endIdx == -1 {
		return "", fmt.Errorf("missing marker %q", end)
	}
	if endIdx < beginIdx {
		return "", fmt.Errorf("marker %q appears before its begin marker", end)
	}

	body = strings.TrimSuffix(body, "\n")
	return src[:beginIdx+len(begin)] + "\n" + body + "\n" + src[endIdx:], nil
}

// BlockMismatches regenerates every block in memory and reports which ones
// differ from src, with the first differing line pair for each. Marker
// errors surface as mismatches too, so check mode aggregates everything.
func BlockMismatches(src string, blocks map[string]string) []string {
	updated, err := ReplaceBlocks(src, blocks)
	if err != nil {
		return []string{err.Error()}
	}
	if updated == src {
		return nil
	}

	// Attribute the difference to specific blocks for a useful message.
	var out []string
	ids := make([]string, 0, len(blocks))
	for id := range blocks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		current := extractBlock(src, id)
		want := strings.TrimSuffix(blocks[id], "\n")
		if current == want {
			continue
		}
		gotLine, wantLine := firstDiffLines(current, want)
		out = append(out, fmt.Sprintf("block %q is stale\n      have: %s\n      want: %s", id, gotLine, wantLine))
	}
	if len(out) == 0 {
		// Difference outside any block body (e.g. trailing whitespace churn).
		out = append(out, "generated blocks differ from the on-disk content")
	}
	return out
}

func extractBlock(src, id string) string {
	begin := markerBegin(id)
	end := markerEnd(id)
	beginIdx := strings.Index(src, begin)
	endIdx := strings.Index(src, end)
	if beginIdx == -1 || endIdx == -1 || endIdx < beginIdx {
		return ""
	}
	return strings.Trim(src[beginIdx+len(begin):endIdx], "\n")
}

func firstDiffLines(got, want string) (gotLine, wantLine string) {
	gotLines := strings.Split(got, "\n")
	wantLines := strings.Split(want, "\n")
	for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
		g, w := "<missing>", "<missing>"
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			return g, w
		}
	}
	return "", ""
}
