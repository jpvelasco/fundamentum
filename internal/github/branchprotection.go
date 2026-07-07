package github

import (
	"fmt"
	"io"
	"net/http"
)

// ClassicProtectionExists returns true if classic branch protection is set on the given branch.
func (c *Client) ClassicProtectionExists(owner, repo, branch string) (bool, error) {
	resp, err := c.get(fmt.Sprintf("/repos/%s/%s/branches/%s/protection", owner, repo, branch))
	if err != nil {
		return false, fmt.Errorf("check classic protection: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK, nil
}

// ApplyClassicBranchProtection sets branch protection using the classic API.
// Works on all repos including private free-tier (unlike rulesets).
// statusChecks are the required CI/status check names; use DefaultStatusChecks for Codacy,
// or append additional checks (e.g., Socket) as needed.
func (c *Client) ApplyClassicBranchProtection(owner, repo, branch string, statusChecks []string, opts BranchProtectionOptions) error {
	var checksReq any
	if len(statusChecks) > 0 {
		checksReq = map[string]any{
			"strict":   true,
			"contexts": statusChecks,
		}
	}
	body := map[string]any{
		"required_status_checks": checksReq,
		"enforce_admins":         true,
		"required_pull_request_reviews": map[string]any{
			"dismiss_stale_reviews":           !opts.Solo,
			"require_code_owner_reviews":      !opts.Solo,
			"required_approving_review_count": 0,
		},
		"restrictions":       nil,
		"allow_force_pushes": false,
		"allow_deletions":    false,
	}
	resp, err := c.do(http.MethodPut, fmt.Sprintf("/repos/%s/%s/branches/%s/protection", owner, repo, branch), body)
	if err != nil {
		return fmt.Errorf("apply classic branch protection: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("apply classic branch protection: %s: %s", resp.Status, b)
	}
	return nil
}

// RemoveClassicBranchProtection removes classic branch protection.
func (c *Client) RemoveClassicBranchProtection(owner, repo, branch string) error {
	resp, err := c.do(http.MethodDelete, fmt.Sprintf("/repos/%s/%s/branches/%s/protection", owner, repo, branch), nil)
	if err != nil {
		return fmt.Errorf("remove classic branch protection: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("remove classic branch protection: %s: %s", resp.Status, b)
	}
	return nil
}
