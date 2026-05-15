package generate

import (
	"bytes"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/gofastadev/cli/internal/termcolor"
)

// tokenPair is one __TOKEN__ → value substitution for renderAndEmit.
// A slice of these (rather than a map) makes substitution order explicit
// at the call site — important because Go's map iteration is randomized,
// and if any future token were a substring of another, undefined order
// would produce non-deterministic output.
type tokenPair struct {
	token, value string
}

// renderAndEmit substitutes __TOKEN__-style placeholders in tmpl with the
// supplied substitutions and writes the result to path. If path already
// exists it prints a skip message and returns nil — generators are
// idempotent and never overwrite hand-edited files.
//
// Substitutions are applied in slice order; pass smaller tokens before
// larger ones if they could appear as substrings.
func renderAndEmit(path, tmpl string, substitutions []tokenPair) error {
	if _, err := os.Stat(path); err == nil {
		termcolor.PrintSkip(path, "exists")
		return nil
	}
	content := tmpl
	for _, p := range substitutions {
		content = strings.ReplaceAll(content, p.token, p.value)
	}
	return writeOrRecordCreate(path, []byte(content))
}

// WriteTemplate renders a Go template and writes it to path. Skips when
// the file already exists. In dry-run mode (see planner.go) the render
// still happens — so template errors surface identically — but the file
// is recorded in the plan instead of written to disk.
func WriteTemplate(path, name, tmpl string, data ScaffoldData) error {
	if _, err := os.Stat(path); err == nil {
		termcolor.PrintSkip(path, "exists")
		return nil
	}
	funcMap := template.FuncMap{
		"timestamp": func() string { return time.Now().Format(time.RFC3339) },
		"lbrace":    func() string { return "{" },
		"rbrace":    func() string { return "}" },
	}
	t, err := template.New(name).Funcs(funcMap).Parse(tmpl)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return err
	}
	return writeOrRecordCreate(path, buf.Bytes())
}
