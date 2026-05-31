// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"fmt"
	"io"
	"net/http"
)

// CreateBranchRuleset creates the protect-main branch ruleset.
// statusChecks is the list of required CI job names (can be empty; update after first CI run).
func (c *Client) CreateBranchRuleset(owner, repo string, statusChecks []string) error {
	checks := make([]map[string]any, len(statusChecks))
	for i, name := range statusChecks {
		checks[i] = map[string]any{"context": name}
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
		"rules": []map[string]any{
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
			{
				"type": "required_status_checks",
				"parameters": map[string]any{
					"strict_required_status_checks_policy": true,
					"do_not_enforce_on_create":             false,
					"required_status_checks":               checks,
				},
			},
		},
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
