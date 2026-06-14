// Package templatefs exposes the embedded template files as an fs.FS.
package templatefs

import (
	"embed"
	"errors"
	"io/fs"
)

//go:embed all:templates
var raw embed.FS

// FS is rooted at the templates directory.
var FS fs.FS

func init() {
	var err error
	FS, err = fs.Sub(raw, "templates")
	if err != nil {
		panic(errors.New("templatefs: cannot embed templates: " + err.Error()))
	}
}
