// Shared fixtures for the featurize test suite. Named per the repo's
// convention for helper-only test files (cf. internal/generate/testhelpers_test.go).

package featurize

const testMod = "example.com/myapp"

func userResource() Resource {
	return Resource{Name: "User", Snake: "user", Plural: "Users"}
}

// countOccurrences reports how many times sub appears in s.
func countOccurrences(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}

// nonIdentSelectors is a fragment every transform below embeds. Each transform
// walks SelectorExprs and bails when the receiver is not a bare identifier;
// without a source containing those shapes the guard never runs, and a
// regression that dropped it would go unnoticed until it crashed on a
// user's chained call.
const nonIdentSelectors = `
	_ = outer.inner.Field
	_ = build().Field
	_ = list[0].Field
`
