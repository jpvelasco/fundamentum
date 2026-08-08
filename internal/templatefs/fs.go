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
	FS = mustSub(raw, "templates")
}

// mustSub roots subFS at dir within fsys, panicking if the dir is missing.
// Extracted from init so the failure branch is testable.
func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(errors.New("templatefs: cannot embed templates: " + err.Error()))
	}
	return sub
}
