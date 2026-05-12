package commands

import "strings"

// ─────────────────────────────────────────────────────────────────────
// Service-name typo suggestion for `--services <name>` validation.
//
// When the user types a service name that isn't declared in
// compose.yaml, we compute Levenshtein distance against every
// declared service and suggest the closest match if the distance is
// small enough to be a likely typo. Otherwise the error just lists
// the available services without "did you mean", because a 5-edit
// distance suggestion is more confusing than helpful.
// ─────────────────────────────────────────────────────────────────────

// suggestionMaxDistance is the highest edit distance we'll consider a
// "likely typo". 2 catches single-char typos, swapped-letter mistakes,
// and one missing/extra character. Anything bigger usually means the
// user typed a real name they expected to exist but doesn't.
const suggestionMaxDistance = 2

// suggestionMinLength is the smallest target name we'll suggest. With
// 2-char names a distance-2 match would match every other 2-char name,
// producing noise.
const suggestionMinLength = 3

// suggestClosest returns the available-services entry closest to typo
// (by Levenshtein distance) when:
//   - the distance is ≤ suggestionMaxDistance, AND
//   - the chosen candidate is ≥ suggestionMinLength
//
// Returns ("", false) otherwise. Empty `available` always returns
// ("", false). Case-insensitive comparison so the user typing "DB"
// when the service is "db" still suggests it.
func suggestClosest(typo string, available []string) (string, bool) {
	if typo == "" || len(available) == 0 {
		return "", false
	}
	lowerTypo := strings.ToLower(typo)

	best := ""
	bestDistance := -1
	for _, candidate := range available {
		if len(candidate) < suggestionMinLength {
			continue
		}
		d := levenshtein(lowerTypo, strings.ToLower(candidate))
		if d > suggestionMaxDistance {
			continue
		}
		if bestDistance == -1 || d < bestDistance {
			best = candidate
			bestDistance = d
		}
	}
	if bestDistance == -1 {
		return "", false
	}
	return best, true
}

// levenshtein computes the edit distance between two strings using
// the standard dynamic-programming approach. O(len(a) * len(b)) time
// and O(min(len(a), len(b))) space — small enough for service names
// (typically < 30 chars).
func levenshtein(a, b string) int {
	if a == "" {
		return len(b)
	}
	if b == "" {
		return len(a)
	}

	// Keep two rows of the DP table; we only ever need the previous
	// row to compute the current one.
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = minOfThree(
				curr[j-1]+1,    // insertion
				prev[j]+1,      // deletion
				prev[j-1]+cost, // substitution
			)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

// minOfThree is a tiny helper to keep the levenshtein body declarative.
func minOfThree(a, b, c int) int {
	return min(min(a, b), c)
}
