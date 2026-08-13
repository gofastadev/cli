package docs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// ValidateInvocations checks every extracted invocation against the facts
// command tree: the subcommand path must exist (aliases resolve) and every
// --flag must be declared on the resolved command, an ancestor's persistent
// set, or the global flags. Returned strings are human-readable problems.
func ValidateInvocations(f Facts, invocations []Invocation) []string {
	var problems []string
	for _, inv := range invocations {
		if p := validateInvocation(f, inv); p != "" {
			problems = append(problems, fmt.Sprintf("%s:%d: %s", inv.File, inv.Line, p))
		}
	}
	return problems
}

func validateInvocation(f Facts, inv Invocation) string {
	tokens := inv.Tokens[1:] // drop the binary name

	current := f.Commands // candidate children at this depth
	var resolved *Command // deepest resolved command
	var available []Flag  // accumulated persistent flags along the path
	available = append(available, f.GlobalFlags...)

	descending := true
	i := 0
	for ; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == "--" {
			return "" // everything after is opaque
		}
		if isPlaceholder(tok) {
			// Placeholder ends path descent and disables further checks —
			// docs use <ResourceName>, [flags], … freely.
			return ""
		}
		if strings.HasPrefix(tok, "-") {
			// Flags do NOT stop path descent — cobra allows them anywhere
			// (`gofasta --json refactor feature --all` is valid).
			if p := checkFlag(tok, resolved, available); p != "" {
				return p
			}
			// A non-flag token following a value-taking flag is its value;
			// only '-'-prefixed tokens are ever flag-checked, so values
			// need no special handling.
			continue
		}
		if !descending {
			continue // positional argument
		}
		child := findChild(current, tok)
		if child == nil {
			if resolved == nil {
				return fmt.Sprintf("unknown command %q (root has no positional args — typo or removed command?)", tok)
			}
			// Positional argument territory from here on.
			descending = false
			continue
		}
		resolved = child
		for _, fl := range child.Flags {
			if fl.Persistent {
				available = append(available, fl)
			}
		}
		current = child.Children
	}
	return ""
}

func findChild(cmds []Command, name string) *Command {
	for i := range cmds {
		c := &cmds[i]
		if c.Name == name || slices.Contains(c.Aliases, name) {
			return c
		}
	}
	return nil
}

func checkFlag(tok string, resolved *Command, inherited []Flag) string {
	name := strings.TrimLeft(tok, "-")
	if eq := strings.Index(name, "="); eq != -1 {
		name = name[:eq]
	}
	if name == "" || isPlaceholder(name) {
		return ""
	}
	// Cobra adds --help/-h to every command implicitly, and --version/-v to
	// the root — neither appears in the walked flag sets.
	if name == "help" || name == "h" {
		return ""
	}
	if resolved == nil && (name == "version" || name == "v") {
		return ""
	}

	var local []Flag
	cmdPath := "gofasta"
	if resolved != nil {
		local = resolved.Flags
		cmdPath = resolved.Path
	}

	if strings.HasPrefix(tok, "--") {
		if flagKnown(name, local, inherited) {
			return ""
		}
		return fmt.Sprintf("flag --%s does not exist on `%s`", name, cmdPath)
	}
	// Shorthand(s): every character must be a known shorthand.
	for _, c := range name {
		if !shorthandKnown(string(c), local, inherited) {
			return fmt.Sprintf("shorthand -%c does not exist on `%s`", c, cmdPath)
		}
	}
	return ""
}

func flagKnown(name string, local, inherited []Flag) bool {
	for _, f := range local {
		if f.Name == name {
			return true
		}
	}
	for _, f := range inherited {
		if f.Name == name {
			return true
		}
	}
	return false
}

func shorthandKnown(sh string, local, inherited []Flag) bool {
	for _, f := range local {
		if f.Shorthand == sh {
			return true
		}
	}
	for _, f := range inherited {
		if f.Shorthand == sh {
			return true
		}
	}
	return false
}

// isPlaceholder reports whether a token is documentation shorthand rather
// than a literal argument: <Name>, [field:type ...], …, ellipses, or shell
// substitutions.
func isPlaceholder(tok string) bool {
	return strings.ContainsAny(tok, "<>[]…*`") ||
		strings.Contains(tok, "...") ||
		strings.Contains(tok, "$")
}

// CheckGoreleaserPlatforms asserts the build-time platform constants match
// the goos/goarch lists in .goreleaser.yaml. Parsing is a section-bounded
// line scan (no YAML dependency); it fails loudly when the lists cannot be
// located so a restructured YAML can't silently pass.
func CheckGoreleaserPlatforms(repoRoot string, goos, goarch []string) []string {
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".goreleaser.yaml"))
	if err != nil {
		return []string{fmt.Sprintf("reading .goreleaser.yaml: %v", err)}
	}

	var problems []string
	for key, want := range map[string][]string{"goos": goos, "goarch": goarch} {
		got, err := yamlListItems(string(raw), key)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if !equalStringSets(got, want) {
			problems = append(problems, fmt.Sprintf(
				".goreleaser.yaml %s list %v does not match the constants in internal/commands/platforms.go %v — update both together (and the install docs)",
				key, got, want))
		}
	}
	return problems
}

// yamlListItems extracts the dash-list items following a `key:` line.
func yamlListItems(raw, key string) ([]string, error) {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != key+":" {
			continue
		}
		var items []string
		for j := i + 1; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if item, found := strings.CutPrefix(t, "- "); found {
				items = append(items, strings.TrimSpace(item))
				continue
			}
			break
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("could not locate list items under %q in .goreleaser.yaml — the docs-check parser needs updating", key)
		}
		return items, nil
	}
	return nil, fmt.Errorf("could not locate %q list in .goreleaser.yaml — the docs-check parser needs updating", key)
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		if !set[s] {
			return false
		}
	}
	return true
}

var (
	makefileLintRe = regexp.MustCompile(`GOLANGCI_LINT_VERSION\s*:=\s*(v[0-9.]+)`)
	ciLintRe       = regexp.MustCompile(`version:\s*(v[0-9.]+)`)
)

// CheckLintVersionParity asserts the golangci-lint version pinned in the
// Makefile matches the one in .github/workflows/ci.yml — the pre-existing
// comment-only rule, now enforced.
func CheckLintVersionParity(repoRoot string) []string {
	makefile, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		return []string{fmt.Sprintf("reading Makefile: %v", err)}
	}
	ci, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "ci.yml"))
	if err != nil {
		return []string{fmt.Sprintf("reading ci.yml: %v", err)}
	}

	mfMatch := makefileLintRe.FindSubmatch(makefile)
	if mfMatch == nil {
		return []string{"could not locate GOLANGCI_LINT_VERSION in Makefile — the docs-check parser needs updating"}
	}
	ciMatch := ciLintRe.FindSubmatch(ci)
	if ciMatch == nil {
		return []string{"could not locate golangci-lint version in ci.yml — the docs-check parser needs updating"}
	}
	if !bytes.Equal(mfMatch[1], ciMatch[1]) {
		return []string{fmt.Sprintf("golangci-lint version drift: Makefile pins %s, ci.yml pins %s — update both in the same commit", mfMatch[1], ciMatch[1])}
	}
	return nil
}
