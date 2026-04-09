package main

import "github.com/gofastadev/cli/internal/commands"

// Version is set at build time via ldflags.
var Version = "dev"

func main() {
	commands.Execute(Version)
}
