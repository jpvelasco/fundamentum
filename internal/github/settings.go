// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"fmt"
	"net/http"
)

// ApplyGeneralSettings enables auto-delete of head branches. It does not
// change the repository default branch.
func (c *Client) ApplyGeneralSettings(owner, repo string) error {
	resp, err := c.patch(repoPath(owner, repo), map[string]any{
		"delete_branch_on_merge": true,
	})
	if err != nil {
		return fmt.Errorf("apply general settings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus("apply general settings", resp, http.StatusOK); err != nil {
		return err
	}
	return nil
}
