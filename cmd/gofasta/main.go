package main

import "github.com/gofastadev/cli/internal/commands"

// Version is set at build time via ldflags.
var Version = "dev"

// run is a package-level seam so tests can replace the entrypoint.
var run = commands.Execute

func main() {
	run(Version)
}
