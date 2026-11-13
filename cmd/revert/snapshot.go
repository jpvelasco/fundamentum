// Package revert implements the "fundamentum revert" command: undo the last
// managed harden apply using a snapshot taken at apply time.
package revert

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jpvelasco/fundamentum/internal/github"
)

// BaselinePath is the repo-local file the snapshot is written to on apply.
const BaselinePath = ".github/fundamentum-baseline.json"

// Snapshot captures enough pre-apply state to undo the apply: the file
// contents (or absence) apply would touch, the repo settings it flips, the
// security toggles, whether the two managed rulesets existed, and selected
// label changes.
type Snapshot struct {
	Labels   []LabelState  `json:"labels,omitempty"`
	Version  int           `json:"version"` // schema version, currently 1
	Owner    string        `json:"owner"`
	Repo     string        `json:"repo"`
	Branch   string        `json:"branch"`
	Files    []FileState   `json:"files"`
	Settings SettingsState `json:"settings"`
	Security SecurityState `json:"security"`
	Rulesets RulesetsState `json:"rulesets"`
}

// FileState records one managed file's pre-apply state: present+content or absent.
type FileState struct {
	Path    string `json:"path"`
	Present bool   `json:"present"`
	Content string `json:"content,omitempty"` // raw bytes (base64 via JSON), empty when absent
	SHA     string `json:"sha,omitempty"`     // blob sha, used to restore exactly
}

// SettingsState / SecurityState alias the github package types so the
// snapshot JSON shape is defined once and shared with the client helpers.
type SettingsState = github.SettingsState
type SecurityState = github.SecurityState

// RulesetsState records whether the two managed rulesets existed pre-apply.
type RulesetsState struct {
	ProtectMain        RulesetState `json:"protect_main"`
	ProtectVersionTags RulesetState `json:"protect_version_tags"`
}

// RulesetState records one ruleset's pre-apply presence and body.
type RulesetState struct {
	Existed bool           `json:"existed"`
	Body    map[string]any `json:"body,omitempty"` // full ruleset JSON when it existed
}

// CaptureSnapshot reads the current state of owner/repo for the given managed
// file paths so a later revert can restore it.
func CaptureSnapshot(c *github.Client, owner, repo, branch string, filePaths []string) (*Snapshot, error) {
	snap := &Snapshot{
		Version: 1,
		Owner:   owner,
		Repo:    repo,
		Branch:  branch,
	}

	for _, path := range filePaths {
		fs := FileState{Path: path}
		content, sha, ok, err := c.GetFileContent(owner, repo, path, branch)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", path, err)
		}
		fs.Present = ok
		fs.Content = content
		fs.SHA = sha
		snap.Files = append(snap.Files, fs)
	}

	info, err := c.GetRepo(owner, repo)
	if err != nil {
		return nil, fmt.Errorf("snapshot repo: %w", err)
	}
	snap.Settings = SettingsState{
		DeleteBranchOnMerge: info.DeleteBranchOnMerge,
		AllowSquashMerge:    info.AllowSquashMerge,
		AllowMergeCommit:    info.AllowMergeCommit,
		AllowRebaseMerge:    info.AllowRebaseMerge,
	}

	alerts, err := c.DependabotAlertsEnabled(owner, repo)
	if err != nil {
		return nil, fmt.Errorf("snapshot dependabot alerts: %w", err)
	}
	fixes, err := c.AutomatedSecurityFixesEnabled(owner, repo)
	if err != nil {
		return nil, fmt.Errorf("snapshot automated security fixes: %w", err)
	}
	codeql, err := c.DefaultCodeQLSetupEnabled(owner, repo)
	if err != nil {
		return nil, fmt.Errorf("snapshot codeql default setup: %w", err)
	}
	snap.Security = SecurityState{
		DependabotAlerts:   alerts,
		AutomatedFixes:     fixes,
		SecretScanning:     info.SecretScanning,
		PushProtection:     info.SecretPushProtection,
		CodeQLDefaultSetup: codeql,
	}

	mainBody, mainExists, err := c.GetRulesetBody(owner, repo, "protect-main")
	if err != nil {
		return nil, fmt.Errorf("snapshot protect-main ruleset: %w", err)
	}
	snap.Rulesets.ProtectMain = RulesetState{Existed: mainExists, Body: mainBody}
	tagBody, tagExists, err := c.GetRulesetBody(owner, repo, "protect-version-tags")
	if err != nil {
		return nil, fmt.Errorf("snapshot protect-version-tags ruleset: %w", err)
	}
	snap.Rulesets.ProtectVersionTags = RulesetState{Existed: tagExists, Body: tagBody}

	return snap, nil
}

// Encode serializes the snapshot for storage.
func (s *Snapshot) Encode() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// DecodeSnapshot parses a stored snapshot.
func DecodeSnapshot(b []byte) (*Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse snapshot: %w", err)
	}
	seen := make(map[string]bool)
	for _, label := range s.Labels {
		key := strings.ToLower(label.Name)
		if _, managed := github.FindLabel(github.PriorityLabels(), key); !managed {
			return nil, fmt.Errorf("snapshot contains unmanaged label %q", label.Name)
		}
		if seen[key] {
			return nil, fmt.Errorf("snapshot contains duplicate label %q", label.Name)
		}
		seen[key] = true
	}
	return &s, nil
}
