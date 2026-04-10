package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GenEmailTemplate generates an HTML email template file in templates/emails/.
// Unlike other generators, this writes raw HTML (not Go-template-executed)
// because the output itself contains Go template directives for the email renderer.
func GenEmailTemplate(d ScaffoldData) error {
	path := fmt.Sprintf("templates/emails/%s.html", d.SnakeName)
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("  skip (exists): %s\n", path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	// Replace placeholders manually instead of using text/template
	// (because the output itself contains {{...}} directives for the email renderer)
	content := emailTemplateContent
	content = strings.ReplaceAll(content, "__SNAKE_NAME__", d.SnakeName)
	content = strings.ReplaceAll(content, "__NAME__", d.Name)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Printf("  create: %s\n", path)
	return nil
}

const emailTemplateContent = `{{template "base.html" .}}
{{define "content"}}
<h2>{{.Title}}</h2>
<p>Hi {{.Name}},</p>
<p>This is the <strong>__SNAKE_NAME__</strong> email template. Customize this content for your needs.</p>
<p><a href="{{.ActionURL}}" class="btn">Take Action</a></p>
{{end}}
`
