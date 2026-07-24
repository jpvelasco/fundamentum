package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// FileChange describes a file to be created or updated.
type FileChange struct {
	Path    string
	Content []byte
}

// ErrWorkflowLocked is returned when GitHub Actions locks a workflow file
// and the Contents API refuses to overwrite it (PUT returns 404).
var ErrWorkflowLocked = fmt.Errorf("workflow file locked by GitHub Actions")

// IsWorkflowLocked returns true if the error wraps ErrWorkflowLocked.
func IsWorkflowLocked(err error) bool {
	return err != nil && strings.Contains(err.Error(), "workflow file locked by GitHub Actions")
}

// UpsertFileOnBranch creates or updates a file on a specific branch via the Contents API.
func (c *Client) UpsertFileOnBranch(owner, repo, branch, path string, content []byte) (string, error) {
	return c.upsertFile(owner, repo, branch, path, content)
}

// CreatePRBranch creates a new branch from the default branch.
func (c *Client) CreatePRBranch(owner, repo, branch, baseBranch string) error {
	resp, err := c.get(repoPath(owner, repo) + "/branches/" + url.PathEscape(baseBranch))
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

	putResp, err := c.do(http.MethodPost, repoPath(owner, repo)+"/git/refs", map[string]any{
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
	resp, err := c.post(repoPath(owner, repo)+"/pulls", map[string]any{
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
			if IsWorkflowLocked(err) {
				fmt.Printf("  %-45s  ⚠ workflow locked by GitHub Actions\n", ch.Path)
				continue
			}
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

// HTTPError wraps an error with the HTTP status code for reliable error checking.
type HTTPError struct {
	StatusCode int
	Msg        string
}

func (e *HTTPError) Error() string {
	return e.Msg
}

// hasStatusCode reports whether err wraps an HTTPError with the given status code.
func hasStatusCode(err error, code int) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.StatusCode == code
}

// IsConflict409 returns true if the error is a 409 from branch protection rules.
func IsConflict409(err error) bool {
	if err == nil {
		return false
	}
	if hasStatusCode(err, http.StatusConflict) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "409") && (strings.Contains(msg, "rule violations") || strings.Contains(msg, "GH013"))
}

// IsForbidden403 returns true if the error contains a 403 Forbidden status.
// Used to detect when rulesets are unavailable on free-tier private repos.
func IsForbidden403(err error) bool {
	if err == nil {
		return false
	}
	if hasStatusCode(err, http.StatusForbidden) {
		return true
	}
	return strings.Contains(err.Error(), "403")
}
