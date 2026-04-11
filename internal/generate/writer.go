package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"time"
)

// WriteTemplate renders a Go template to a file. Skips if the file already exists.
func WriteTemplate(path, name, tmpl string, data ScaffoldData) error {
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("  skip (exists): %s\n", path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
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
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := t.Execute(f, data); err != nil {
		return err
	}
	fmt.Printf("  create: %s\n", path)
	return nil
}
