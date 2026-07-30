// gen_repo_method.go — `gofasta g repo-method <Resource> <Method> [param:type ...]`
//
// The repository-layer twin of `g method`. Same astpatch pattern,
// different default paths: targets
//   - app/repositories/interfaces/<snake>_repository.go
//   - app/repositories/<snake>.repository.go
//
// gofasta's scaffold names repository interfaces "<Name>RepositoryInterface"
// (see internal/generate/templates/repo_interface.go) — we match that
// convention by default.
package generate

import (
	"github.com/gofastadev/cli/internal/layout"
)

// GenRepoMethod is a thin wrapper that fills in repo-specific defaults
// and delegates to the shared GenMethod implementation. Keeping it as a
// separate entry point makes the CLI command surface explicit even
// though the implementation is identical.
func GenRepoMethod(d MethodData) error {
	if d.Resource == "" {
		return GenMethod(d) // let GenMethod surface the missing-name error
	}
	snake := toSnakeCase(d.Resource)
	if d.InterfaceName == "" {
		d.InterfaceName = d.Resource + "RepositoryInterface"
	}
	// The scaffold declares the exported `type <Name>Repository struct`
	// (templates/repo.go) — the stub's receiver must match it or the
	// patched file doesn't compile.
	if d.ImplStructName == "" {
		d.ImplStructName = d.Resource + "Repository"
	}
	if d.InterfaceFile == "" || d.ImplFile == "" {
		lo := layout.Detect()
		if d.InterfaceFile == "" {
			d.InterfaceFile = lo.RepoIfaceFile(snake)
		}
		if d.ImplFile == "" {
			d.ImplFile = lo.RepoImplFile(snake)
		}
	}
	d.Snake = snake
	return GenMethod(d)
}
