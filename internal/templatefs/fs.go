// Package templatefs exposes the embedded template files as an fs.FS.
package templatefs

import (
	"embed"
	"io/fs"
)

//go:embed all:templates
var raw embed.FS

// FS is rooted at the templates directory.
var FS, _ = fs.Sub(raw, "templates")
