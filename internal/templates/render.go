// Package templates renders embedded community health file templates.
package templates

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
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
	Visibility    string // "public" or "private"
}

// Render renders all embedded templates and returns RenderedFiles with target
// paths (dotgithub/ → .github/, dotcodacy.yml → .codacy.yml).
// Templates with a "public_" filename prefix are only included for public repos.
// Templates with a "private_" filename prefix are only included for private repos.
func Render(data RepoData) ([]RenderedFile, error) {
	var files []RenderedFile
	err := fs.WalkDir(templatefs.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !shouldInclude(path, data.Visibility) {
			return nil
		}
		raw, err := fs.ReadFile(templatefs.FS, path)
		if err != nil {
			return fmt.Errorf("read template %s: %w", path, err)
		}

		var rendered string
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

		target := resolveTarget(path)
		files = append(files, RenderedFile{Path: target, Content: rendered})
		return nil
	})
	return files, err
}

// shouldInclude returns false for visibility-gated templates that don't match.
// "public_" prefix → only public repos. "private_" prefix → only private repos.
func shouldInclude(path, visibility string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, "public_") {
		return visibility == "public"
	}
	if strings.HasPrefix(base, "private_") {
		return visibility == "private"
	}
	return true
}

// resolveTarget converts embedded template paths to target paths.
// "public_" and "private_" prefixes are stripped from the filename.
func resolveTarget(path string) string {
	target := strings.Replace(path, "dotgithub/", ".github/", 1)
	target = strings.Replace(target, "dotcodacy.yml", ".codacy.yml", 1)
	// Strip visibility prefix from filename.
	if idx := strings.LastIndex(target, "/"); idx >= 0 {
		dir := target[:idx+1]
		base := target[idx+1:]
		base = stripVisibilityPrefix(base)
		target = dir + base
	} else {
		target = stripVisibilityPrefix(target)
	}
	return target
}

func stripVisibilityPrefix(base string) string {
	if strings.HasPrefix(base, "public_") {
		return strings.TrimPrefix(base, "public_")
	}
	if strings.HasPrefix(base, "private_") {
		return strings.TrimPrefix(base, "private_")
	}
	return base
}