// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// rulesetExists returns true if a ruleset with the given name already exists.
func (c *Client) rulesetExists(owner, repo, name string) (bool, error) {
	resp, err := c.get(fmt.Sprintf("/repos/%s/%s/rulesets", owner, repo))
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
func (c *Client) EnsureBranchRuleset(owner, repo string, statusChecks []string) error {
	exists, err := c.rulesetExists(owner, repo, "protect-main")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return c.CreateBranchRuleset(owner, repo, statusChecks)
}

// EnsureTagRuleset creates the protect-version-tags ruleset if it doesn't exist.
func (c *Client) EnsureTagRuleset(owner, repo string) error {
	exists, err := c.rulesetExists(owner, repo, "protect-version-tags")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return c.CreateTagRuleset(owner, repo)
}

// CreateBranchRuleset creates the protect-main branch ruleset.
// statusChecks is the list of required CI job names (can be empty; add after first CI run).
func (c *Client) CreateBranchRuleset(owner, repo string, statusChecks []string) error {
	checks := make([]map[string]any, len(statusChecks))
	for i, name := range statusChecks {
		checks[i] = map[string]any{"context": name}
	}
	rules := []map[string]any{
		{"type": "deletion"},
		{"type": "non_fast_forward"},
		{
			"type": "pull_request",
			"parameters": map[string]any{
				"required_approving_review_count":   0,
				"dismiss_stale_reviews_on_push":     true,
				"require_code_owner_review":         true,
				"require_last_push_approval":        false,
				"required_review_thread_resolution": true,
			},
		},
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
	resp, err := c.post(fmt.Sprintf("/repos/%s/%s/rulesets", owner, repo), body)
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
	resp, err := c.post(fmt.Sprintf("/repos/%s/%s/rulesets", owner, repo), body)
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
