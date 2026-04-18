package generate

import (
	"bytes"
	"os"
	"text/template"
	"time"

	"github.com/gofastadev/cli/internal/termcolor"
)

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
