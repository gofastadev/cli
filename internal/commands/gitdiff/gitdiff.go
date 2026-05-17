// Package gitdiff lists files changed since a git ref and maps them to
// Go packages. It is consumed by `gofasta verify --since=<ref>` to scope
// the quality-gate gauntlet to only the packages affected by the user's
// changeset.
//
// The package shells out to `git` and `go list` rather than using
// libraries: those binaries are universally available and their CLI
// contracts are far more stable than any Go SDK. Both are wrapped behind
// package-level seams (execCommand / execLookPath) so tests can substitute
// fake runners.
package gitdiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
)

// Package-level seams for test injection.
var (
	execCommand  = exec.CommandContext
	execLookPath = exec.LookPath
)

// ChangedFiles returns the set of repo-relative file paths that differ
// between the given ref and the current working tree. The set is the union
// of:
//
//   - committed changes since ref (`git diff --name-only <ref>...HEAD`)
//   - staged changes (`git diff --name-only --cached`)
//   - unstaged tracked-file changes (`git diff --name-only`)
//   - untracked files visible to git (`git ls-files --others --exclude-standard`)
//
// Deleted files are excluded by default (use IncludeDeleted=true to keep
// them — useful for reverse-dep analysis where you want to know which
// packages used to depend on a now-deleted file).
//
// If the directory is not a git repo, returns CodeGitNotAvailable with a
// hint pointing at `git init` or dropping the --since flag. If the ref
// does not resolve, returns CodeGitRefNotFound.
func ChangedFiles(ctx context.Context, ref string, opts Options) ([]string, error) {
	if _, err := execLookPath("git"); err != nil {
		return nil, clierr.Wrap(clierr.CodeGitNotAvailable, err,
			"`git` is not on $PATH")
	}
	if !insideGitRepo(ctx) {
		return nil, clierr.New(clierr.CodeGitNotAvailable,
			"current directory is not inside a git repository")
	}
	if ref != "" && !refResolves(ctx, ref) {
		return nil, clierr.Newf(clierr.CodeGitRefNotFound,
			"git ref %q does not resolve", ref)
	}

	files := newFileSet()

	// Committed changes since the ref (or HEAD~ when ref is empty — which
	// callers normally don't do; --changed always passes "" and gets only
	// staged+working-tree).
	if ref != "" {
		out, err := runGit(ctx, "diff", "--name-status", ref+"...HEAD")
		if err != nil {
			return nil, clierr.Wrap(clierr.CodeGitDiffFailed, err,
				fmt.Sprintf("git diff %s...HEAD failed", ref))
		}
		files.absorbStatus(out, opts.IncludeDeleted)
	}

	// Staged changes.
	if out, err := runGit(ctx, "diff", "--name-status", "--cached"); err == nil {
		files.absorbStatus(out, opts.IncludeDeleted)
	} else {
		return nil, clierr.Wrap(clierr.CodeGitDiffFailed, err, "git diff --cached failed")
	}

	// Unstaged tracked-file changes.
	if out, err := runGit(ctx, "diff", "--name-status"); err == nil {
		files.absorbStatus(out, opts.IncludeDeleted)
	} else {
		return nil, clierr.Wrap(clierr.CodeGitDiffFailed, err, "git diff failed")
	}

	// Untracked files (new files not yet `git add`-ed).
	if !opts.SkipUntracked {
		if out, err := runGit(ctx, "ls-files", "--others", "--exclude-standard"); err == nil {
			for _, line := range nonEmptyLines(out) {
				files.add(line)
			}
		} else {
			return nil, clierr.Wrap(clierr.CodeGitDiffFailed, err, "git ls-files failed")
		}
	}

	return files.sorted(), nil
}

// Options controls ChangedFiles behavior.
type Options struct {
	// IncludeDeleted keeps deleted files in the returned set. Default
	// false (deletes are usually irrelevant for scoping verify steps).
	IncludeDeleted bool
	// SkipUntracked omits files not yet tracked by git. Default false.
	SkipUntracked bool
}

// FilterGoFiles returns the subset of paths whose suffix is `.go`. Useful
// for gofmt scoping, which can take individual files as positional args.
func FilterGoFiles(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if strings.HasSuffix(p, ".go") {
			out = append(out, p)
		}
	}
	return out
}

// UniqueDirs returns the sorted set of unique directories containing the
// given files (relative to the repo root). Used to derive the package list
// to feed `go list`.
func UniqueDirs(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		seen[filepath.Dir(p)] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// ----- internals ---------------------------------------------------------

func insideGitRepo(ctx context.Context) bool {
	cmd := execCommand(ctx, "git", "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

func refResolves(ctx context.Context, ref string) bool {
	cmd := execCommand(ctx, "git", "rev-parse", "--verify", ref+"^{commit}")
	_, err := cmd.Output()
	return err == nil
}

// runGit runs `git <args...>` and returns trimmed stdout. Stderr is folded
// into the returned error for diagnostic value.
func runGit(ctx context.Context, args ...string) (string, error) {
	cmd := execCommand(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return stdout.String(), nil
}

// nonEmptyLines splits output by newline and drops blanks.
func nonEmptyLines(out string) []string {
	lines := strings.Split(out, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, l := range lines {
		if s := strings.TrimSpace(l); s != "" {
			cleaned = append(cleaned, s)
		}
	}
	return cleaned
}

// fileSet deduplicates paths and applies --name-status filtering.
type fileSet struct {
	m map[string]struct{}
}

func newFileSet() *fileSet { return &fileSet{m: make(map[string]struct{})} }

func (s *fileSet) add(path string) {
	if path == "" {
		return
	}
	s.m[path] = struct{}{}
}

// absorbStatus parses `git diff --name-status` output. Each line is
// "<STATUS>\t<PATH>" or "R<score>\t<OLD>\t<NEW>" for renames. We track the
// destination of renames and (depending on includeDeleted) keep or drop
// deletes.
func (s *fileSet) absorbStatus(out string, includeDeleted bool) {
	for _, line := range nonEmptyLines(out) {
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		switch {
		case strings.HasPrefix(status, "R"), strings.HasPrefix(status, "C"):
			if len(parts) >= 3 {
				s.add(parts[2])
			}
		case status == "D":
			if includeDeleted && len(parts) >= 2 {
				s.add(parts[1])
			}
		default:
			s.add(parts[1])
		}
	}
}

func (s *fileSet) sorted() []string {
	out := make([]string, 0, len(s.m))
	for p := range s.m {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
