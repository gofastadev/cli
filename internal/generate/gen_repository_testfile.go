package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenRepoTestFile writes the testcontainers-backed repository test
// alongside every generated repo. Covers FindByID, Create,
// UpdateIfVersionMatches (incl. atomicity-under-race),
// SoftDeleteIfDeletable, List pagination.
func GenRepoTestFile(d ScaffoldData) error {
	return WriteTemplate(
		fmt.Sprintf("app/repositories/%s.repository_test.go", d.SnakeName),
		"repo_test", templates.RepoTest, d,
	)
}
