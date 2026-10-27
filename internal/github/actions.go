// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"fmt"
	"net/http"
)

// ActionsWorkflowPermissions is the repository's default GITHUB_TOKEN
// workflow permissions: what the token can do by default, and whether it can
// approve pull requests.
type ActionsWorkflowPermissions struct {
	DefaultWorkflowPermissions   string `json:"default_workflow_permissions"`
	CanApprovePullRequestReviews bool   `json:"can_approve_pull_request_reviews"`
}

// GetActionsWorkflowPermissions reads the repository's default workflow
// permissions (GET /repos/{owner}/{repo}/actions/permissions/workflow).
func (c *Client) GetActionsWorkflowPermissions(owner, repo string) (ActionsWorkflowPermissions, error) {
	var out ActionsWorkflowPermissions
	if err := c.getDecode(repoPath(owner, repo)+"/actions/permissions/workflow", "get actions workflow permissions", &out); err != nil {
		return ActionsWorkflowPermissions{}, err
	}
	return out, nil
}

// SetActionsWorkflowPermissions locks the default GITHUB_TOKEN down to
// read-only and forbids it from approving pull requests
// (PUT /repos/{owner}/{repo}/actions/permissions/workflow). The bot-managed
// profile applies this so a compromised workflow cannot write to the repo or
// self-approve an automation PR.
func (c *Client) SetActionsWorkflowPermissions(owner, repo string) error {
	resp, err := c.do(http.MethodPut, repoPath(owner, repo)+"/actions/permissions/workflow", map[string]any{
		"default_workflow_permissions":     "read",
		"can_approve_pull_request_reviews": false,
	})
	if err != nil {
		return fmt.Errorf("set actions workflow permissions: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return expectStatus("set actions workflow permissions", resp, http.StatusNoContent, http.StatusOK)
}
