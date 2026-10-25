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
	CIPack        string // go, generic, or none; empty means go (legacy Render callers)
	// Octopus opts in to the third-party Octopus Review workflow (opt_ files).
	Octopus bool
	// CodacyInstructions opts in to .github/instructions/codacy.instructions.md.
	CodacyInstructions bool
	// ReadmePath names the repo's README file (PR-template documentation line).
	ReadmePath string
	// AgentGuideLine is the PR-template checklist line for the repo's AI agent
	// guide (AGENTS.md standard, CLAUDE.md legacy). Empty omits the line.
	AgentGuideLine string
	// NodeLintCommand / NodeTestCommand are the run: lines for the node CI
	// pack, generated from the repo's package.json scripts.
	NodeLintCommand string
	NodeTestCommand string
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
		Owner:              d.Owner,
		RepoName:           d.RepoName,
		DefaultBranch:      d.DefaultBranch,
		Visibility:         visibility,
		CodeOwnerLine:      codeOwnerLine,
		CIPack:             d.CIPack,
		Octopus:            d.Octopus,
		CodacyInstructions: d.CodacyInstructions,
		ReadmePath:         sanitizeFileName(d.ReadmePath, "README.md"),
		AgentGuideLine:     sanitizeAgentGuideLine(d.AgentGuideLine),
		NodeLintCommand:    sanitizeShellLine(d.NodeLintCommand, defaultNodeLintCommand),
		NodeTestCommand:    sanitizeShellLine(d.NodeTestCommand, defaultNodeTestCommand),
	}, nil
}

// sanitizeFileName keeps a doc filename to safe identifier characters,
// falling back to def when empty. Filenames are template-fixed or detected
// from a fixed candidate list, so the charset check is defense-in-depth.
func sanitizeFileName(name, def string) string {
	if !safeNameRe.MatchString(name) {
		return def
	}
	return name
}

var safeNameRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// sanitizeAgentGuideLine validates the PR-template checklist line. The line
// is generated internally by DetectAgentGuideLine from a fixed candidate
// list. Empty or unknown values fall back to the AGENTS.md standard line —
// "if there is no file, use agents.md".
func sanitizeAgentGuideLine(line string) string {
	for _, guide := range agentGuideCandidates {
		if line == agentGuideLine(guide) {
			return line
		}
	}
	return agentGuideLine("AGENTS.md")
}

const agentGuidePrefix = "- [ ] Updated "

// agentGuideCandidates are the agent guide filenames, standard first.
var agentGuideCandidates = []string{"AGENTS.md", "CLAUDE.md"}

// agentGuideLine renders the PR-template checklist line for guide.
func agentGuideLine(guide string) string {
	return agentGuidePrefix + guide + " (if applicable)"
}

// sanitizeShellLine keeps a CI run: line to a safe command charset, falling
// back to def when empty or unsafe. Commands are generated internally.
func sanitizeShellLine(cmd, def string) string {
	if cmd == "" || !safeShellRe.MatchString(cmd) {
		return def
	}
	return cmd
}

// safeShellRe allows letters, digits, spaces, and the common shell punctuation
// the generated CI commands use. Slashes (paths), quotes, and :: annotations
// are required; other metacharacters are rejected.
var safeShellRe = regexp.MustCompile(`^[A-Za-z0-9 ./:_&|=-]+$`)

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
// paths (dotgithub/ → .github/, public_codacy.yml → .codacy.yml).
// Templates with a "public_" filename prefix are only included for public repos.
// Templates with a "private_" filename prefix are only included for private repos.
// Templates with an "opt_" filename prefix are only included when the matching
// RepoData opt-in is set (e.g. opt_octopus.yml → Octopus).
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
		if !templateIncluded(path, data) {
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

// templateIncluded reports whether the template at embedded path ships for
// this repo: visibility prefixes (public_/private_), opt-in prefixes (opt_),
// and the CI-pack mapping all gate inclusion.
func templateIncluded(path string, data RepoData) bool {
	base := filepath.Base(path)
	prefixes, stem := splitPrefixes(base)

	if prefixes["public_"] && data.Visibility != "public" {
		return false
	}
	if prefixes["private_"] && data.Visibility != "private" {
		return false
	}
	if prefixes["opt_"] && !optInAllowed(stem, data) {
		return false
	}
	return packMatches(path, data.CIPack)
}

// optInAllowed reports whether the opt_-gated template's flag is set.
// octopus.yml requires Octopus; codacy.instructions.md requires CodacyInstructions.
func optInAllowed(stem string, data RepoData) bool {
	switch stem {
	case "octopus.yml":
		return data.Octopus
	case "codacy.instructions.md":
		return data.CodacyInstructions
	default:
		return false
	}
}

// templatePrefixes is every known filename prefix, stripped from target names
// and inspected for gating. Order-insensitive: public_node_codacy.yml and
// node_public_codacy.yml both resolve to codacy.yml.
var templatePrefixes = []string{"public_", "private_", "generic_", "node_", "python_", "rust_", "go_", "opt_"}

// splitPrefixes splits a template filename into its known prefix set and the
// remaining stem, stripping prefixes in any order.
func splitPrefixes(base string) (map[string]bool, string) {
	prefixes := map[string]bool{}
	stem := base
	for {
		matched := false
		for _, p := range templatePrefixes {
			if strings.HasPrefix(stem, p) {
				prefixes[p] = true
				stem = strings.TrimPrefix(stem, p)
				matched = true
				break
			}
		}
		if !matched {
			return prefixes, stem
		}
	}
}

// resolveTarget converts embedded template paths to target paths.
// All known filename prefixes are stripped — in any combination — from the
// filename.
func resolveTarget(path string) string {
	target := strings.Replace(path, "dotgithub/", ".github/", 1)

	dir, base := filepath.Split(target)
	_, base = splitPrefixes(base)
	if base == "codacy.yml" {
		// Unlike other root files (codecov.yml), the Codacy config lives at
		// the dot-prefixed .codacy.yml — for every pack variant.
		return dir + ".codacy.yml"
	}
	return dir + base
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
		{"{{.ReadmePath}}", data.ReadmePath},
		{"{{.AgentGuideLine}}", data.AgentGuideLine},
		{"{{.NodeLintCommand}}", data.NodeLintCommand},
		{"{{.NodeTestCommand}}", data.NodeTestCommand},
	}
	out := tmpl
	for _, r := range repls {
		out = strings.ReplaceAll(out, r.old, r.new)
	}
	return out
}
