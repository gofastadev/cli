package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

func GenRepoInterface(d ScaffoldData) error {
	return WriteTemplate(fmt.Sprintf("app/repositories/interfaces/%s_repository.go", d.SnakeName), "repo_iface", templates.RepoInterface, d)
}

func GenRepo(d ScaffoldData) error {
	return WriteTemplate(fmt.Sprintf("app/repositories/%s.repository.go", d.SnakeName), "repo", templates.Repo, d)
}
