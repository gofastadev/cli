// Package skeleton provides embedded project template files for gofasta new.
package skeleton

import "embed"

// ProjectFS holds the embedded skeleton project used by `gofasta new`.
//
//go:embed all:project
var ProjectFS embed.FS
