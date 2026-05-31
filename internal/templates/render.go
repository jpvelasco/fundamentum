// Package templates renders embedded community health file templates.
package templates

import (
	"bytes"
	"fmt"
	"io/fs"
	"strings"
	"text/template"

	"github.com/jpvelasco/fundamentum/internal/templatefs"
)

// RenderedFile is a target path and rendered content ready to commit.
type RenderedFile struct {
	Path    string
	Content string
}

// RepoData holds substitution values for template rendering.
type RepoData struct {
	Owner         string
	RepoName      string
	DefaultBranch string
}

// Render renders all embedded templates and returns RenderedFiles with target
// paths (dotgithub/ → .github/, dotcodacy/ → .codacy/).
func Render(data RepoData) ([]RenderedFile, error) {
	var files []RenderedFile
	err := fs.WalkDir(templatefs.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(templatefs.FS, path)
		if err != nil {
			return fmt.Errorf("read template %s: %w", path, err)
		}

		var rendered string
		// Skip template processing for files without {{. placeholders
		if strings.Contains(string(raw), "{{.") {
			tmpl, err := template.New(path).Parse(string(raw))
			if err != nil {
				return fmt.Errorf("parse template %s: %w", path, err)
			}
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, data); err != nil {
				return fmt.Errorf("render template %s: %w", path, err)
			}
			rendered = buf.String()
		} else {
			rendered = string(raw)
		}

		target := strings.Replace(path, "dotgithub/", ".github/", 1)
		target = strings.Replace(target, "dotcodacy/", ".codacy/", 1)
		files = append(files, RenderedFile{Path: target, Content: rendered})
		return nil
	})
	return files, err
}
