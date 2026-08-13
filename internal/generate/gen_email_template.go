package generate

import (
	"fmt"
	"os"
	"strings"

	"github.com/gofastadev/cli/internal/cliout"
)

// GenEmailTemplate generates an HTML email template file in templates/emails/.
// Unlike other generators, this writes raw HTML (not Go-template-executed)
// because the output itself contains Go template directives for the email renderer.
func GenEmailTemplate(d ScaffoldData) error {
	path := fmt.Sprintf("templates/emails/%s.html", d.SnakeName)
	if _, err := os.Stat(path); err == nil {
		cliout.Skip(path, "exists")
		return nil
	}

	// Replace placeholders manually instead of using text/template
	// (because the output itself contains {{...}} directives for the email renderer)
	content := emailTemplateContent
	content = strings.ReplaceAll(content, "__SNAKE_NAME__", d.SnakeName)
	content = strings.ReplaceAll(content, "__NAME__", d.Name)

	// Through the planner chokepoint: honors dry-run, records the
	// planned create, guards against paths outside the project, and
	// owns the MkdirAll. This generator previously wrote directly to
	// disk and was invisible to `--dry-run` plans.
	return writeOrRecordCreate(path, []byte(content))
}

const emailTemplateContent = `{{template "base.html" .}}
{{define "content"}}
<h2>{{.Title}}</h2>
<p>Hi {{.Name}},</p>
<p>This is the <strong>__SNAKE_NAME__</strong> email template. Customize this content for your needs.</p>
<p><a href="{{.ActionURL}}" class="btn">Take Action</a></p>
{{end}}
`
