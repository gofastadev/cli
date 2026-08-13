package generate

import (
	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenRepoInterface writes the repository interface file for the scaffolded resource.
func GenRepoInterface(d ScaffoldData) error {
	return WriteTemplate(d.L().RepoIfaceFile(d.SnakeName), "repo_iface", templates.RepoInterface, d)
}

// GenRepo writes the repository implementation file for the scaffolded resource.
func GenRepo(d ScaffoldData) error {
	return WriteTemplate(d.L().RepoImplFile(d.SnakeName), "repo", templates.Repo, d)
}
