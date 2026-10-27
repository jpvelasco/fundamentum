// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"fmt"
	"net/http"
)

// ApplyGeneralSettings enables auto-delete of head branches. It does not
// change the repository default branch. When botManaged is set it also locks
// merges down to squash-only (merge commits and rebase merges off) so an
// automation pipeline's PRs land with a single, linear commit shape; with
// botManaged off the request body is exactly {"delete_branch_on_merge":true}.
func (c *Client) ApplyGeneralSettings(owner, repo string, botManaged bool) error {
	body := map[string]any{
		"delete_branch_on_merge": true,
	}
	if botManaged {
		body["allow_squash_merge"] = true
		body["allow_merge_commit"] = false
		body["allow_rebase_merge"] = false
	}
	resp, err := c.patch(repoPath(owner, repo), body)
	if err != nil {
		return fmt.Errorf("apply general settings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus("apply general settings", resp, http.StatusOK); err != nil {
		return err
	}
	return nil
}
