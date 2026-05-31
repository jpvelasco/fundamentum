// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"fmt"
	"io"
	"net/http"
)

// CreateRepo creates a new GitHub repository for the authenticated user.
func (c *Client) CreateRepo(name string, private bool) error {
	resp, err := c.post("/user/repos", map[string]any{
		"name":    name,
		"private": private,
	})
	if err != nil {
		return fmt.Errorf("create repo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create repo: %s: %s", resp.Status, body)
	}
	return nil
}
