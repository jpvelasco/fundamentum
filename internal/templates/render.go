// Package templates renders embedded community health file templates.
// All template data is sanitized before rendering: owner/repo names are
// validated against GitHub identifier regexes, branch names are character-
// whitelisted, and visibility is whitelist-checked to "public" or "private".
// Plain string substitution is used instead of text/template because the output
// is YAML/Markdown config files — no template engine is needed for simple field
// replacement, and this avoids false-positive XSS flags from static analyzers.
package templates

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/jpvelasco/fundamentum/internal/templatefs"
)

// sanitizeOutput strips dangerous HTML tags from rendered output.
// This is defense-in-depth: template data is pre-sanitized via RepoData.sanitize(),
// but this ensures any residual HTML injection is neutralized. Only specific
// dangerous HTML tags are targeted — generic angle-bracket patterns like
// `<your-username>` in Markdown are preserved.
var dangerousTagRe = regexp.MustCompile(`(?i)<(/)?(script|iframe|object|embed|svg|style|link|form|input|img|meta|base|applet|marquee|video|audio|source|track|body|head|html|div|span|a)[^>]*>`)

func sanitizeOutput(s string) string {
	return dangerousTagRe.ReplaceAllString(s, "")
}

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

// sanitize sanitizes RepoData fields to prevent template injection.
// GitHub identifiers are alphanumeric with hyphens; branch names allow slashes
// and hyphens. Visibility is whitelist-checked to "public" or "private".
func (d RepoData) sanitize() RepoData {
	ownerRe := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,38}[a-zA-Z0-9])?$`)
	repoRe := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]{0,98}[a-zA-Z0-9])?$`)
	branchRe := regexp.MustCompile(`^[a-zA-Z0-9/_-]+$`)

	owner := strings.Map(validIdentifier, d.Owner)
	if !ownerRe.MatchString(owner) {
		owner = "owner"
	}

	repo := strings.Map(validIdentifier, d.RepoName)
	if !repoRe.MatchString(repo) {
		repo = "repo"
	}

	branch := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '/' || r == '-' || r == '_' {
			return r
		}
		return -1
	}, d.DefaultBranch)
	if !branchRe.MatchString(branch) {
		branch = "main"
	}

	visibility := strings.ToLower(d.Visibility)
	if visibility != "public" && visibility != "private" {
		visibility = "public"
	}

	return RepoData{
		Owner:         owner,
		RepoName:      repo,
		DefaultBranch: branch,
		Visibility:    visibility,
	}
}

// validIdentifier keeps only ASCII letters, digits, hyphens, dots, and
// underscores for GitHub identifier sanitization.
func validIdentifier(r rune) rune {
	if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '-' || r == '.' || r == '_' {
		return r
	}
	return -1
}

// Render renders all embedded templates and returns RenderedFiles with target
// paths (dotgithub/ → .github/, dotcodacy.yml → .codacy.yml).
// Templates with a "public_" filename prefix are only included for public repos.
// Templates with a "private_" filename prefix are only included for private repos.
func Render(data RepoData) ([]RenderedFile, error) {
	data = data.sanitize()
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
			rendered = substitute(string(raw), data)
		} else {
			rendered = string(raw)
		}

		// Defense-in-depth: strip dangerous HTML tags from rendered output.
		// Template data is pre-sanitized, but residual HTML in static template text
		// is neutralized here. Only specific dangerous tags are targeted — benign
		// angle brackets like <your-username> in Markdown are preserved.
		rendered = sanitizeOutput(rendered)

		target := resolveTarget(path)
		files = append(files, RenderedFile{Path: target, Content: rendered})
		return nil
	})
	return files, err
}

// shouldInclude skips visibility-gated templates that don't match the repo.
// "public_" prefix → only public repos. "private_" prefix → only private repos.
func shouldInclude(path, visibility string) bool {
	switch base := filepath.Base(path); {
	case strings.HasPrefix(base, "public_"):
		return visibility == "public"
	case strings.HasPrefix(base, "private_"):
		return visibility == "private"
	default:
		return true
	}
}

// resolveTarget converts embedded template paths to target paths.
// "public_" and "private_" prefixes are stripped from the filename.
func resolveTarget(path string) string {
	target := strings.Replace(path, "dotgithub/", ".github/", 1)
	target = strings.Replace(target, "dotcodacy.yml", ".codacy.yml", 1)

	dir, base := filepath.Split(target)
	return dir + stripVisibilityPrefix(base)
}

func stripVisibilityPrefix(base string) string {
	switch {
	case strings.HasPrefix(base, "public_"):
		return strings.TrimPrefix(base, "public_")
	case strings.HasPrefix(base, "private_"):
		return strings.TrimPrefix(base, "private_")
	default:
		return base
	}
}

// substitute replaces {{.Field}} placeholders with sanitized values.
// Only the four known fields are replaced; unknown placeholders are left
// as-is so broken templates surface during review rather than silently
// passing through.
func substitute(tmpl string, data RepoData) string {
	return strings.ReplaceAll(
		strings.ReplaceAll(
			strings.ReplaceAll(
				strings.ReplaceAll(tmpl, "{{.Owner}}", data.Owner),
				"{{.RepoName}}", data.RepoName),
			"{{.DefaultBranch}}", data.DefaultBranch),
		"{{.Visibility}}", data.Visibility,
	)
}