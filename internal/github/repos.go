// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// CreateRepo creates a new GitHub repository under owner.
// When owner is the authenticated user, this POSTs /user/repos.
// When owner is an organization the caller can create in, this POSTs /orgs/{org}/repos.
// Other users are rejected with a clear error instead of silently creating
// authenticated-user/name and then applying against owner/name.
// auto_init is always true so the repo has a default-branch ref for apply.
func (c *Client) CreateRepo(owner, name string, private bool) error {
	path, err := c.createRepoEndpoint(owner)
	if err != nil {
		return err
	}
	resp, err := c.post(path, map[string]any{
		"name":      name,
		"private":   private,
		"auto_init": true,
	})
	if err != nil {
		return fmt.Errorf("create repo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return expectStatus("create repo", resp, http.StatusCreated)
}

// createRepoEndpoint chooses the GitHub create-repo path for owner.
func (c *Client) createRepoEndpoint(owner string) (string, error) {
	if owner == "" {
		return "", fmt.Errorf("create repo: owner is required")
	}
	login, err := c.authenticatedLogin()
	if err != nil {
		return "", err
	}
	if strings.EqualFold(owner, login) {
		return "/user/repos", nil
	}
	kind, err := c.accountType(owner)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(kind, "Organization") {
		return "/orgs/" + url.PathEscape(owner) + "/repos", nil
	}
	return "", fmt.Errorf("cannot create repository under %q: owner is not the authenticated user %q and is not an organization", owner, login)
}

// getDecode GETs path and JSON-decodes a 200 body into dest.
func (c *Client) getDecode(path, action string, dest any) error {
	resp, err := c.get(path)
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus(action, resp, http.StatusOK); err != nil {
		return err
	}
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode %s: %w", action, err)
	}
	return nil
}

// authenticatedLogin returns the login of the token owner (GET /user).
func (c *Client) authenticatedLogin() (string, error) {
	var u struct {
		Login string `json:"login"`
	}
	if err := c.getDecode("/user", "get authenticated user", &u); err != nil {
		return "", err
	}
	if u.Login == "" {
		return "", fmt.Errorf("get authenticated user: empty login")
	}
	return u.Login, nil
}

// accountType returns the GitHub account type for login ("User" or "Organization").
func (c *Client) accountType(login string) (string, error) {
	var u struct {
		Type string `json:"type"`
	}
	if err := c.getDecode("/users/"+url.PathEscape(login), "get account "+login, &u); err != nil {
		return "", err
	}
	if u.Type == "" {
		return "", fmt.Errorf("get account %s: empty type", login)
	}
	return u.Type, nil
}

// Repo is the subset of GET /repos/{owner}/{repo} that apply needs.
type Repo struct {
	Visibility    string
	DefaultBranch string
}

// GetRepo returns visibility and default branch. An empty default_branch
// falls back to "main" so callers always have a ref name.
func (c *Client) GetRepo(owner, repo string) (Repo, error) {
	var result struct {
		Visibility    string `json:"visibility"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.getDecode(repoPath(owner, repo), "get repo", &result); err != nil {
		return Repo{}, err
	}
	if result.DefaultBranch == "" {
		result.DefaultBranch = "main"
	}
	return Repo{Visibility: result.Visibility, DefaultBranch: result.DefaultBranch}, nil
}

// GetRepoVisibility returns the repository visibility: "public" or "private".
func (c *Client) GetRepoVisibility(owner, repo string) (string, error) {
	info, err := c.GetRepo(owner, repo)
	if err != nil {
		return "", err
	}
	return info.Visibility, nil
}
