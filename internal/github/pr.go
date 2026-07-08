package github

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// FileChange describes a file to be created or updated.
type FileChange struct {
	Path    string
	Content []byte
}

// UpsertFileOnBranch creates or updates a file on a specific branch via the Contents API.
func (c *Client) UpsertFileOnBranch(owner, repo, branch, path string, content []byte) (string, error) {
	apiPath := fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s", owner, repo, path, branch)

	resp, err := c.get(apiPath)
	if err != nil {
		return "", fmt.Errorf("check file %s on %s: %w", path, branch, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := map[string]any{
		"message": "chore: add " + path,
		"content": base64.StdEncoding.EncodeToString(content),
		"branch":  branch,
	}

	var action string
	switch resp.StatusCode {
	case http.StatusOK:
		var existing struct {
			Content string `json:"content"`
			SHA     string `json:"sha"`
		}
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("read existing file %s on %s: %w", path, branch, err)
		}
		if err := json.Unmarshal(raw, &existing); err != nil {
			return "", fmt.Errorf("parse existing file %s on %s: %w", path, branch, err)
		}
		existingClean := strings.ReplaceAll(existing.Content, "\n", "")
		newEncoded := base64.StdEncoding.EncodeToString(content)
		if existingClean == newEncoded {
			return "skipped", nil
		}
		body["sha"] = existing.SHA
		body["message"] = "chore: update " + path
		action = "updated"

	case http.StatusNotFound:
		action = "created"

	default:
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("check file %s on %s: %s: %s", path, branch, resp.Status, b)
	}

	// PUT doesn't support ?ref= — use the branch in the request body instead.
	putPath := fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, path)
	putResp, err := c.do(http.MethodPut, putPath, body)
	if err != nil {
		return "", fmt.Errorf("upsert file %s on %s: %w", path, branch, err)
	}
	defer func() { _ = putResp.Body.Close() }()
	if putResp.StatusCode != http.StatusCreated && putResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(putResp.Body)
		return "", fmt.Errorf("upsert file %s on %s: %s: %s", path, branch, putResp.Status, b)
	}
	return action, nil
}

// CreatePRBranch creates a new branch from the default branch.
func (c *Client) CreatePRBranch(owner, repo, branch, baseBranch string) error {
	resp, err := c.get(fmt.Sprintf("/repos/%s/%s/branches/%s", owner, repo, baseBranch))
	if err != nil {
		return fmt.Errorf("get branch %s: %w", baseBranch, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var branchInfo struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&branchInfo); err != nil {
		return fmt.Errorf("decode branch %s: %w", baseBranch, err)
	}

	putResp, err := c.do(http.MethodPost, fmt.Sprintf("/repos/%s/%s/git/refs", owner, repo), map[string]any{
		"ref": "refs/heads/" + branch,
		"sha": branchInfo.Commit.SHA,
	})
	if err != nil {
		return fmt.Errorf("create branch %s: %w", branch, err)
	}
	defer func() { _ = putResp.Body.Close() }()
	if putResp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(putResp.Body)
		return fmt.Errorf("create branch %s: %s: %s", branch, putResp.Status, b)
	}
	return nil
}

// CreatePullRequest opens a PR from head to base. Returns the PR number.
func (c *Client) CreatePullRequest(owner, repo, title, body, head, base string) (int, error) {
	resp, err := c.post(fmt.Sprintf("/repos/%s/%s/pulls", owner, repo), map[string]any{
		"title": title,
		"body":  body,
		"head":  head,
		"base":  base,
	})
	if err != nil {
		return 0, fmt.Errorf("create PR: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("create PR: %s: %s", resp.Status, b)
	}
	var result struct {
		Number int `json:"number"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode PR: %w", err)
	}
	return result.Number, nil
}

// ApplyViaPR creates a feature branch, pushes all file changes, and opens a PR.
// Returns the PR number on success.
func (c *Client) ApplyViaPR(owner, repo, defaultBranch string, changes []FileChange) (int, error) {
	branch := fmt.Sprintf("harden-%s-%d", defaultBranch, time.Now().Unix())

	if err := c.CreatePRBranch(owner, repo, branch, defaultBranch); err != nil {
		return 0, fmt.Errorf("create branch: %w", err)
	}

	for _, ch := range changes {
		action, err := c.UpsertFileOnBranch(owner, repo, branch, ch.Path, ch.Content)
		if err != nil {
			return 0, fmt.Errorf("upsert %s: %w", ch.Path, err)
		}
		if action != "skipped" {
			fmt.Printf("  %-45s  ✓ (%s)\n", ch.Path, action)
		}
	}

	title := "feat: harden repo — community files, settings, security"
	body := "Applied repo hardening: branch protection, security features, community health files, and quality gates."

	prNum, err := c.CreatePullRequest(owner, repo, title, body, branch, defaultBranch)
	if err != nil {
		return 0, err
	}
	return prNum, nil
}

// IsConflict409 returns true if the error is a 409 from branch protection rules.
func IsConflict409(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// "409" from resp.Status is stable; "rule violations" or "GH013" from the JSON body.
	return strings.Contains(msg, "409") && (strings.Contains(msg, "rule violations") || strings.Contains(msg, "GH013"))
}

// IsForbidden403 returns true if the error contains a 403 Forbidden status.
// Used to detect when rulesets are unavailable on free-tier private repos.
func IsForbidden403(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "403")
}