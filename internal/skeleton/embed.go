// Package skeleton provides embedded project template files for gofasta new.
package skeleton

import "embed"

//go:embed all:project
var ProjectFS embed.FS
