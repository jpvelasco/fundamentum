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
	_, err := c.getDecodePage(path, action, dest)
	return err
}

// getDecodePage also returns the next page of a JSON collection, when present.
func (c *Client) getDecodePage(path, action string, dest any) (string, error) {
	resp, err := c.get(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", action, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus(action, resp, http.StatusOK); err != nil {
		return "", err
	}
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return "", fmt.Errorf("decode %s: %w", action, err)
	}
	return nextLink(resp.Header.Get("Link")), nil
}

// authenticatedLogin returns the login of the token owner (GET /user).
func (c *Client) authenticatedLogin() (string, error) {
	login, _, err := c.AuthenticatedUser()
	return login, err
}

// AuthenticatedUser returns the login and account type of the token owner
// (GET /user). The type is "User", "Bot", or "Organization"; an
// installation token (GitHub App) reports the app's bot login.
func (c *Client) AuthenticatedUser() (login, accountType string, err error) {
	var u struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	}
	if err := c.getDecode("/user", "get authenticated user", &u); err != nil {
		return "", "", err
	}
	if u.Login == "" {
		return "", "", fmt.Errorf("get authenticated user: empty login")
	}
	return u.Login, u.Type, nil
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

// IsBotOrAppToken reports whether the client's token belongs to a bot or an
// app rather than a human: the token is a GitHub App installation token
// (ghs_…), or GET /user reports the account type "Bot". The bot-managed
// profile refuses to apply under such a token — its guardrails assume a human
// operator is driving the harden, not the automation it is about to restrict.
func (c *Client) IsBotOrAppToken() (bool, string, error) {
	if strings.HasPrefix(c.Token, "ghs_") {
		return true, "an installation token (GitHub App)", nil
	}
	_, accountType, err := c.AuthenticatedUser()
	if err != nil {
		return false, "", err
	}
	if strings.EqualFold(accountType, "Bot") {
		return true, `the authenticated account's type is "Bot"`, nil
	}
	return false, "", nil
}

// Repo is the subset of GET /repos/{owner}/{repo} that apply and audit need.
type Repo struct {
	Owner                string
	RepoName             string
	Visibility           string
	DefaultBranch        string
	OwnerType            string // "User" or "Organization"
	DeleteBranchOnMerge  bool
	AllowSquashMerge     bool
	AllowMergeCommit     bool
	AllowRebaseMerge     bool
	SecretScanning       bool
	SecretPushProtection bool
}

// GetRepo returns visibility, default branch, owner account type, and
// the settings/security fields audit compares against the baseline.
// An empty default_branch falls back to "main" so callers always have a ref name.
func (c *Client) GetRepo(owner, repo string) (Repo, error) {
	var result struct {
		Name                string `json:"name"`
		Visibility          string `json:"visibility"`
		DefaultBranch       string `json:"default_branch"`
		DeleteBranchOnMerge bool   `json:"delete_branch_on_merge"`
		AllowSquashMerge    bool   `json:"allow_squash_merge"`
		AllowMergeCommit    bool   `json:"allow_merge_commit"`
		AllowRebaseMerge    bool   `json:"allow_rebase_merge"`
		Owner               struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"owner"`
		SecurityAndAnalysis struct {
			SecretScanning struct {
				Status string `json:"status"`
			} `json:"secret_scanning"`
			SecretScanningPushProtection struct {
				Status string `json:"status"`
			} `json:"secret_scanning_push_protection"`
		} `json:"security_and_analysis"`
	}
	if err := c.getDecode(repoPath(owner, repo), "get repo", &result); err != nil {
		return Repo{}, err
	}
	if result.DefaultBranch == "" {
		result.DefaultBranch = "main"
	}
	name := result.Name
	if name == "" {
		name = repo
	}
	login := result.Owner.Login
	if login == "" {
		login = owner
	}
	return Repo{
		Owner:                login,
		RepoName:             name,
		Visibility:           result.Visibility,
		DefaultBranch:        result.DefaultBranch,
		OwnerType:            result.Owner.Type,
		DeleteBranchOnMerge:  result.DeleteBranchOnMerge,
		AllowSquashMerge:     result.AllowSquashMerge,
		AllowMergeCommit:     result.AllowMergeCommit,
		AllowRebaseMerge:     result.AllowRebaseMerge,
		SecretScanning:       result.SecurityAndAnalysis.SecretScanning.Status == "enabled",
		SecretPushProtection: result.SecurityAndAnalysis.SecretScanningPushProtection.Status == "enabled",
	}, nil
}

// GetRepoVisibility returns the repository visibility: "public" or "private".
func (c *Client) GetRepoVisibility(owner, repo string) (string, error) {
	info, err := c.GetRepo(owner, repo)
	if err != nil {
		return "", err
	}
	return info.Visibility, nil
}
