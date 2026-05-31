package github

import (
	"fmt"
	"io"
	"net/http"
)

// ClassicProtectionExists returns true if classic branch protection is set on main.
func (c *Client) ClassicProtectionExists(owner, repo string) (bool, error) {
	resp, err := c.get(fmt.Sprintf("/repos/%s/%s/branches/main/protection", owner, repo))
	if err != nil {
		return false, fmt.Errorf("check classic protection: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK, nil
}

// ApplyClassicBranchProtection sets branch protection on main using the classic API.
// Works on all repos including private free-tier (unlike rulesets).
func (c *Client) ApplyClassicBranchProtection(owner, repo string) error {
	body := map[string]any{
		"required_status_checks": nil,
		"enforce_admins":         true,
		"required_pull_request_reviews": map[string]any{
			"dismiss_stale_reviews":           true,
			"require_code_owner_reviews":      true,
			"required_approving_review_count": 0,
		},
		"restrictions":       nil,
		"allow_force_pushes": false,
		"allow_deletions":    false,
	}
	resp, err := c.do(http.MethodPut, fmt.Sprintf("/repos/%s/%s/branches/main/protection", owner, repo), body)
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

// RemoveClassicBranchProtection removes classic branch protection from main.
func (c *Client) RemoveClassicBranchProtection(owner, repo string) error {
	resp, err := c.do(http.MethodDelete, fmt.Sprintf("/repos/%s/%s/branches/main/protection", owner, repo), nil)
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
