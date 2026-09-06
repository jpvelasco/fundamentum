// Package templates renders embedded community health file templates.
// All template data is validated before rendering: owner/repo names must
// match GitHub identifier rules (including the special .github repo),
// branch names must be valid git refs, and visibility is whitelist-checked
// to "public" or "private". Invalid owner, repo, or branch values return
// an error instead of being rewritten. Plain string substitution is used
// instead of text/template because the output is YAML/Markdown config
// files — no template engine is needed for simple field replacement, and
// this avoids false-positive XSS flags from static analyzers.
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

var (
	// GitHub login: 1–39 chars, alphanumeric or hyphen, cannot start/end with hyphen.
	ownerRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,37}[a-zA-Z0-9])?$`)
	// GitHub repo name, plus the reserved .github org-template repository.
	repoRe = regexp.MustCompile(`^(\.github|[a-zA-Z0-9]([a-zA-Z0-9._-]{0,98}[a-zA-Z0-9])?)$`)
	// Git branch charset that GitHub accepts and that is safe in YAML/Markdown
	// templates: letters, digits, slash, dot, underscore, hyphen, plus.
	branchCharRe = regexp.MustCompile(`^[A-Za-z0-9/._+-]+$`)
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
	CodeOwnerLine string // CODEOWNERS body line; user: "* @owner", org: comment
}

// sanitize validates owner/repo/branch without rewriting valid names and
// normalizes visibility plus CODEOWNERS. Invalid identifiers return an
// error so callers cannot silently emit workflows for the wrong repo or branch.
func (d RepoData) sanitize() (RepoData, error) {
	if !ownerRe.MatchString(d.Owner) {
		return RepoData{}, fmt.Errorf("invalid owner %q: use a GitHub login (letters, digits, hyphens; cannot start or end with a hyphen)", d.Owner)
	}
	if !repoRe.MatchString(d.RepoName) {
		return RepoData{}, fmt.Errorf("invalid repository name %q: use a GitHub repo name (letters, digits, dots, hyphens, underscores) or the reserved .github repository", d.RepoName)
	}
	if !validGitBranch(d.DefaultBranch) {
		return RepoData{}, fmt.Errorf("invalid default branch %q: use a git branch name (letters, digits, and /._+-; no leading/trailing slash or dot)", d.DefaultBranch)
	}

	visibility := strings.ToLower(d.Visibility)
	if visibility != "public" && visibility != "private" {
		// internal (GHEC) and unknown values use the private template set —
		// public-only tools (Codecov, Octopus, CodeQL workflow) stay off.
		visibility = "private"
	}

	codeOwnerLine := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("#@/* .-:,_'()", r) {
			return r
		}
		return -1
	}, d.CodeOwnerLine)
	if strings.TrimSpace(codeOwnerLine) == "" {
		codeOwnerLine = "* @" + d.Owner
	}

	return RepoData{
		Owner:         d.Owner,
		RepoName:      d.RepoName,
		DefaultBranch: d.DefaultBranch,
		Visibility:    visibility,
		CodeOwnerLine: codeOwnerLine,
	}, nil
}

// validGitBranch reports whether name is a git branch we will substitute
// into templates. Charset is a subset of git check-ref-format --branch that
// stays safe in YAML/Markdown (no angle brackets or template metacharacters).
func validGitBranch(name string) bool {
	switch {
	case !branchCharRe.MatchString(name):
		return false
	case strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "//"):
		return false
	case strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.Contains(name, ".."):
		return false
	default:
		return true
	}
}

// Render renders all embedded templates and returns RenderedFiles with target
// paths (dotgithub/ → .github/, dotcodacy.yml → .codacy.yml).
// Templates with a "public_" filename prefix are only included for public repos.
// Templates with a "private_" filename prefix are only included for private repos.
func Render(data RepoData) ([]RenderedFile, error) {
	return renderFromFS(templatefs.FS, data)
}

// renderFromFS renders all templates from fsys. Split out from Render so the
// ReadFile error branch is testable with an injecting failing fs.FS.
func renderFromFS(fsys fs.FS, data RepoData) ([]RenderedFile, error) {
	clean, err := data.sanitize()
	if err != nil {
		return nil, err
	}
	data = clean
	var files []RenderedFile
	err = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !shouldInclude(path, data.Visibility) {
			return nil
		}
		raw, err := fs.ReadFile(fsys, path)
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
// Only the known fields are replaced; unknown placeholders are left
// as-is so broken templates surface during review rather than silently
// passing through.
func substitute(tmpl string, data RepoData) string {
	repls := []struct{ old, new string }{
		{"{{.Owner}}", data.Owner},
		{"{{.RepoName}}", data.RepoName},
		{"{{.DefaultBranch}}", data.DefaultBranch},
		{"{{.Visibility}}", data.Visibility},
		{"{{.CodeOwnerLine}}", data.CodeOwnerLine},
	}
	out := tmpl
	for _, r := range repls {
		out = strings.ReplaceAll(out, r.old, r.new)
	}
	return out
}
