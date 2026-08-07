package docs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func extract(md string) []Invocation {
	return ExtractInvocations("test.md", []byte(md))
}

func TestExtractInvocations_BasicFence(t *testing.T) {
	invs := extract("```bash\ngofasta new myapp --driver mysql\n```\n")
	require.Len(t, invs, 1)
	assert.Equal(t, []string{"gofasta", "new", "myapp", "--driver", "mysql"}, invs[0].Tokens)
	assert.Equal(t, 2, invs[0].Line)
}

func TestExtractInvocations_ProseIsIgnored(t *testing.T) {
	assert.Empty(t, extract("gofasta generates code.\n\nRun `gofasta new` to start.\n"))
}

func TestExtractInvocations_NonShellFencesSkipped(t *testing.T) {
	assert.Empty(t, extract("```go\n// gofasta new myapp\ncmd := \"gofasta new\"\n```\n"))
	assert.Empty(t, extract("```yaml\nrun: gofasta verify\n```\n"))
}

func TestExtractInvocations_ShellVariants(t *testing.T) {
	md := "```sh\ngofasta verify\n```\n~~~console\n$ gofasta status\n~~~\n```\ngofasta routes\n```\n"
	invs := extract(md)
	require.Len(t, invs, 3)
	assert.Equal(t, []string{"gofasta", "verify"}, invs[0].Tokens)
	assert.Equal(t, []string{"gofasta", "status"}, invs[1].Tokens)
	assert.Equal(t, []string{"gofasta", "routes"}, invs[2].Tokens)
}

func TestExtractInvocations_Continuations(t *testing.T) {
	invs := extract("```bash\ngofasta g scaffold Product \\\n  name:string\n```\n")
	require.Len(t, invs, 1)
	assert.Equal(t, []string{"gofasta", "g", "scaffold", "Product", "name:string"}, invs[0].Tokens)
}

func TestExtractInvocations_SegmentsAndComments(t *testing.T) {
	invs := extract("```bash\ncd myapp && gofasta init   # one-time\ngofasta dev | tee log.txt\n```\n")
	require.Len(t, invs, 2)
	assert.Equal(t, []string{"gofasta", "init"}, invs[0].Tokens)
	assert.Equal(t, []string{"gofasta", "dev"}, invs[1].Tokens)
}

func TestExtractInvocations_EnvPrefixAndQuotes(t *testing.T) {
	invs := extract("```bash\nGOFASTA_NO_BANNER=1 gofasta g job cleanup \"0 0 * * * *\"\n```\n")
	require.Len(t, invs, 1)
	assert.Equal(t, []string{"gofasta", "g", "job", "cleanup", "0 0 * * * *"}, invs[0].Tokens)
}

func TestExtractInvocations_TemplateLinesSkipped(t *testing.T) {
	md := "```bash\ngofasta dev --services db\n{{- if ne .DBDriver \"sqlite\" }}\ngofasta migrate up\n{{- end }}\n```\n"
	invs := extract(md)
	require.Len(t, invs, 2)
	assert.Equal(t, []string{"gofasta", "dev", "--services", "db"}, invs[0].Tokens)
	assert.Equal(t, []string{"gofasta", "migrate", "up"}, invs[1].Tokens)
}

// Console-style fences with `$ ` prompts intermix commands and output —
// only prompted lines are commands ("gofasta v0.1.10" below is output).
func TestExtractInvocations_PromptFencesSkipOutputLines(t *testing.T) {
	md := "```bash\n$ gofasta version\ngofasta v0.1.10\nGo:      go1.25.0\n```\n"
	invs := extract(md)
	require.Len(t, invs, 1)
	assert.Equal(t, []string{"gofasta", "version"}, invs[0].Tokens)
}

func TestExtractInvocations_BinDirSpelling(t *testing.T) {
	invs := extract("```bash\n./bin/gofasta facts check --repo .\n```\n")
	require.Len(t, invs, 1)
	assert.Equal(t, "./bin/gofasta", invs[0].Tokens[0])
}

func TestExtractInvocations_UnterminatedFence(t *testing.T) {
	// Fence never closes: fenceUsesPrompts scans to EOF without finding a
	// prompt or a closing marker, and the command is still extracted.
	invs := extract("```bash\ngofasta verify\n")
	require.Len(t, invs, 1)
	assert.Equal(t, []string{"gofasta", "verify"}, invs[0].Tokens)
}

func TestExtractInvocations_SingleQuotedArgs(t *testing.T) {
	invs := extract("```bash\ngofasta g job cleanup '0 0 * * * *' && echo 'a; b'\n```\n")
	require.Len(t, invs, 1)
	assert.Equal(t, []string{"gofasta", "g", "job", "cleanup", "0 0 * * * *"}, invs[0].Tokens)
}

func TestIsEnvAssignment(t *testing.T) {
	assert.True(t, isEnvAssignment("GOFASTA_NO_BANNER=1"))
	assert.False(t, isEnvAssignment("=value"), "no name before =")
	assert.False(t, isEnvAssignment("plain"), "no = at all")
	assert.False(t, isEnvAssignment("./path=x"), "non-identifier characters in the name")
}
