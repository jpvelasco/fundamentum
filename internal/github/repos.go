// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"encoding/json"
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

// GetRepoVisibility returns the repository visibility: "public" or "private".
func (c *Client) GetRepoVisibility(owner, repo string) (string, error) {
	resp, err := c.get(fmt.Sprintf("/repos/%s/%s", owner, repo))
	if err != nil {
		return "", fmt.Errorf("get repo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("get repo: %s: %s", resp.Status, body)
	}
	var result struct {
		Visibility string `json:"visibility"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode repo visibility: %w", err)
	}
	return result.Visibility, nil
}