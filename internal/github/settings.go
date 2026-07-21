// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"fmt"
	"io"
	"net/http"
)

// ApplyGeneralSettings enables auto-delete of head branches and sets the default branch to main.
func (c *Client) ApplyGeneralSettings(owner, repo string) error {
	resp, err := c.patch(repoPath(owner, repo), map[string]any{
		"delete_branch_on_merge": true,
		"default_branch":         "main",
	})
	if err != nil {
		return fmt.Errorf("apply general settings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("apply general settings: %s: %s", resp.Status, b)
	}
	return nil
}
