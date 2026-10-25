// Package steps resolves the --skip / --only step opt-outs shared by apply
// and audit. Steps are addressed by canonical key; files by target path
// (or the "files" key for the whole file set).
package steps

import "strings"

// Canonical step keys accepted by --skip and --only.
const (
	Settings           = "settings"              // general settings (auto-delete branches)
	ProtectMain        = "protect-main"           // branch protection ruleset
	ProtectVersionTags = "protect-version-tags"  // tag ruleset
	Security           = "security"              // Dependabot / secret scanning / CodeQL
	Files              = "files"                  // all community/CI file items
)

// Selector decides whether a step or file should run. An empty only-set means
// "everything"; a non-empty only-set restricts to the listed steps/paths. The
// skip-set then subtracts. File items respond to the "files" key and their
// exact target path; an exact path beats the "files" key for only-selections.
type Selector struct {
	skip map[string]struct{}
	only map[string]struct{}
}

// New builds a Selector from the raw --skip and --only token lists. Tokens are
// trimmed and lowercased so --skip Protect-Main matches.
func New(skip, only []string) Selector {
	return Selector{skip: keySet(skip), only: keySet(only)}
}

// Len reports how many raw constraints (skip + only) are configured. Zero
// means no filtering at all.
func (s Selector) Len() int {
	return len(s.skip) + len(s.only)
}

// IncludeStep reports whether the canonical step should run.
func (s Selector) IncludeStep(step string) bool {
	if _, ok := s.skip[step]; ok {
		return false
	}
	if len(s.only) == 0 {
		return true
	}
	_, ok := s.only[step]
	return ok
}

// ExcludeReason reports which opt-out mechanism excludes an item: "--skip",
// "--only", or "" when the item is included. Used for plan-table labels.
func (s Selector) ExcludeReason(stepOrPath string) string {
	if _, ok := s.skip[stepOrPath]; ok {
		return "--skip"
	}
	if _, ok := s.only[stepOrPath]; ok {
		return "--only"
	}
	if len(s.only) > 0 {
		return "--only"
	}
	return ""
}

// IncludeFile reports whether a file at target path should ship.
func (s Selector) IncludeFile(path string) bool {
	if _, ok := s.skip[Files]; ok {
		return false
	}
	if _, ok := s.skip[path]; ok {
		return false
	}
	if len(s.only) == 0 {
		return true
	}
	if _, ok := s.only[Files]; ok {
		return true
	}
	_, ok := s.only[path]
	return ok
}

func keySet(tokens []string) map[string]struct{} {
	if len(tokens) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" {
			set[t] = struct{}{}
		}
	}
	return set
}
