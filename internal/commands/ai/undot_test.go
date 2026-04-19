package ai

import "testing"

func TestUndotPrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"dot-claude/settings.json", ".claude/settings.json"},
		{"dot-claude/hooks/pre-commit.sh", ".claude/hooks/pre-commit.sh"},
		{"dot-cursor/rules/gofasta.mdc", ".cursor/rules/gofasta.mdc"},
		{"dot-windsurfrules", ".windsurfrules"},
		{"dot-aider.conf.yml", ".aider.conf.yml"},
		{"dot-aider/CONVENTIONS.md", ".aider/CONVENTIONS.md"},
		// Any segment starting with "dot-" is transformed regardless of
		// depth — the convention is symmetric at every directory level.
		{"configs/dot-this/file", "configs/.this/file"},
		// "dot-" appearing mid-segment is NOT a prefix, so it stays.
		{"this-is-not-dot-", "this-is-not-dot-"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := undotPrefix(tc.in)
			if got != tc.want {
				t.Errorf("undotPrefix(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
