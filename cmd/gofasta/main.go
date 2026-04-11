package main

import (
	"runtime/debug"

	"github.com/gofastadev/cli/internal/commands"
)

// Version is overridden at release-build time via
//
//	-ldflags="-X main.Version=<tag>"
//
// so pre-built binaries (from the GitHub Release + install.sh path) show the
// exact release tag. For `go install` users, ldflags is not applied, so this
// default value is used — but we replace it at startup with whatever Go
// stamped into the binary via runtime/debug.ReadBuildInfo(), which reflects
// the module version the user actually installed.
var Version = "dev"

// run is a package-level seam so tests can replace the entrypoint.
var run = commands.Execute

// resolveBuildInfo is a seam so tests can stub debug.ReadBuildInfo.
var resolveBuildInfo = debug.ReadBuildInfo

func resolveVersion() string {
	// If ldflags set Version to something concrete, trust it.
	if Version != "dev" {
		return Version
	}
	// Otherwise ask Go's build metadata what module version this binary came
	// from. `go install github.com/gofastadev/cli/cmd/gofasta@v0.1.2` stamps
	// "v0.1.2" here; `go install ...@latest` stamps the resolved tag; a local
	// `go build` leaves it as "(devel)".
	info, ok := resolveBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return Version
	}
	return info.Main.Version
}

func main() {
	run(resolveVersion())
}
