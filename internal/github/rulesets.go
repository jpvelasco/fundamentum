// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// dedup removes duplicate strings while preserving order.
func dedup(s []string) []string {
	seen := make(map[string]struct{}, len(s))
	out := make([]string, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

// DefaultStatusChecks are the status checks added to branch protection by default.
// Codacy is always configured by fundamentum (.codacy.yml), so its check is safe to require.
// Socket is a GitHub App that may not be installed on all accounts — add it via the
// statusChecks parameter on CreateBranchRuleset / ApplyClassicBranchProtection if available.
var DefaultStatusChecks = []string{
	"Codacy Static Code Analysis",
}

// BranchProtectionOptions controls how strictly the branch ruleset is configured.
type BranchProtectionOptions struct {
	// Solo disables CODEOWNERS review requirement and stale review dismissal,
	// which would deadlock a solo maintainer who can't approve their own PRs.
	Solo bool
}

// RulesetExists returns true if a ruleset with the given name already exists.
func (c *Client) RulesetExists(owner, repo, name string) (bool, error) {
	resp, err := c.get(repoPath(owner, repo) + "/rulesets")
	if err != nil {
		return false, fmt.Errorf("list rulesets: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	var rulesets []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rulesets); err != nil {
		return false, fmt.Errorf("decode rulesets: %w", err)
	}
	for _, r := range rulesets {
		if r.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// EnsureBranchRuleset creates the protect-main branch ruleset if it doesn't exist.
func (c *Client) EnsureBranchRuleset(owner, repo string, statusChecks []string, opts BranchProtectionOptions) error {
	exists, err := c.RulesetExists(owner, repo, "protect-main")
	if err != nil || exists {
		return err
	}
	return c.CreateBranchRuleset(owner, repo, statusChecks, opts)
}

// EnsureTagRuleset creates the protect-version-tags ruleset if it doesn't exist.
func (c *Client) EnsureTagRuleset(owner, repo string) error {
	exists, err := c.RulesetExists(owner, repo, "protect-version-tags")
	if err != nil || exists {
		return err
	}
	return c.CreateTagRuleset(owner, repo)
}

// CreateBranchRuleset creates the protect-main branch ruleset.
// statusChecks are additional checks on top of DefaultStatusChecks (Codacy).
// Pass nil to use only the defaults.
func (c *Client) CreateBranchRuleset(owner, repo string, statusChecks []string, opts BranchProtectionOptions) error {
	allChecks := dedup(append(append([]string{}, DefaultStatusChecks...), statusChecks...))
	checks := make([]map[string]any, len(allChecks))
	for i, name := range allChecks {
		checks[i] = map[string]any{"context": name}
	}
	prParams := map[string]any{
		"required_approving_review_count":   0,
		"dismiss_stale_reviews_on_push":     !opts.Solo,
		"require_code_owner_review":         !opts.Solo,
		"require_last_push_approval":        false,
		"required_review_thread_resolution": true,
	}
	rules := []map[string]any{
		{"type": "deletion"},
		{"type": "non_fast_forward"},
		{"type": "pull_request", "parameters": prParams},
	}
	if len(checks) > 0 {
		rules = append(rules, map[string]any{
			"type": "required_status_checks",
			"parameters": map[string]any{
				"strict_required_status_checks_policy": true,
				"do_not_enforce_on_create":             false,
				"required_status_checks":               checks,
			},
		})
	}
	body := map[string]any{
		"name":        "protect-main",
		"target":      "branch",
		"enforcement": "active",
		"conditions": map[string]any{
			"ref_name": map[string]any{
				"include": []string{"~DEFAULT_BRANCH"},
				"exclude": []string{},
			},
		},
		"rules": rules,
	}
	resp, err := c.post(repoPath(owner, repo)+"/rulesets", body)
	if err != nil {
		return fmt.Errorf("create branch ruleset: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create branch ruleset: %s: %s", resp.Status, b)
	}
	return nil
}

// CreateTagRuleset creates the protect-version-tags tag ruleset.
func (c *Client) CreateTagRuleset(owner, repo string) error {
	body := map[string]any{
		"name":        "protect-version-tags",
		"target":      "tag",
		"enforcement": "active",
		"conditions": map[string]any{
			"ref_name": map[string]any{
				"include": []string{"refs/tags/v*"},
				"exclude": []string{},
			},
		},
		"rules": []map[string]any{
			{"type": "deletion"},
			{"type": "non_fast_forward"},
		},
	}
	resp, err := c.post(repoPath(owner, repo)+"/rulesets", body)
	if err != nil {
		return fmt.Errorf("create tag ruleset: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create tag ruleset: %s: %s", resp.Status, b)
	}
	return nil
}
