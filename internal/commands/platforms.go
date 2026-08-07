package commands

// releaseGoos / releaseGoarch mirror the build matrix in .goreleaser.yaml.
// The YAML is not embedded in the binary, so `gofasta facts` publishes
// these constants instead — and `gofasta facts check` asserts they still
// match the YAML, failing the build when the release matrix changes
// without this file (and therefore the docs) following.
var (
	releaseGoos   = []string{"linux", "darwin", "windows"}
	releaseGoarch = []string{"amd64", "arm64"}
)
