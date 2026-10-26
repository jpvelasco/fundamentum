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

// ErrWorkflowLocked is returned when GitHub refuses a workflow-file write
// through the Contents API (PUT returns 404): the token lacks the `workflow`
// scope — GitHub refuses workflow creates the same way it refuses updates to
// locked workflows. The git data API hits the same gate: creating a tree that
// contains a .github/workflows path 404s for a repo-scoped token (verified
// live on a free-tier private repo), so workflow files cannot ride the
// single-commit git data batch either.
var ErrWorkflowLocked = fmt.Errorf("workflow file locked by GitHub Actions")

// IsWorkflowLocked returns true if the error wraps ErrWorkflowLocked.
func IsWorkflowLocked(err error) bool {
	return errors.Is(err, ErrWorkflowLocked)
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
	if resp.StatusCode != http.StatusOK {
		return expectStatus("get branch "+baseBranch, resp, http.StatusOK)
	}
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
	return expectStatus("create branch "+branch, putResp, http.StatusCreated)
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
	if err := expectStatus("create PR", resp, http.StatusCreated); err != nil {
		return 0, err
	}
	var result struct {
		Number int `json:"number"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode PR: %w", err)
	}
	return result.Number, nil
}

// ApplyViaPR creates a feature branch, commits the non-workflow file changes
// as ONE commit via the git data API, writes workflow files per-file via the
// Contents API, and opens a PR. Returns the PR number on success.
//
// Workflow files (.github/workflows/…) cannot ride the git data batch:
// GitHub refuses to create a tree containing a workflow path when the token
// lacks the workflow scope (404, the same gate that 404s the Contents PUT).
// They go per-file through the Contents API so a scope-refused write is
// classified ErrWorkflowLocked and skipped with a warning instead of failing
// the whole PR — a workflow-scoped token lands them normally, and the bulk
// of the changes still lands as one clean commit on the PR branch.
func (c *Client) ApplyViaPR(owner, repo, defaultBranch string, changes []FileChange, stdout io.Writer) (int, error) {
	// UnixNano keeps consecutive runs in the same second from colliding on a
	// branch name (POST /git/refs 422s when the branch already exists).
	branch := fmt.Sprintf("harden-%s-%d", defaultBranch, time.Now().UnixNano())

	if len(changes) == 0 {
		return 0, nil
	}

	if err := c.CreatePRBranch(owner, repo, branch, defaultBranch); err != nil {
		return 0, fmt.Errorf("create branch: %w", err)
	}

	var batch []FileChange
	var workflows []FileChange
	for _, ch := range changes {
		if IsWorkflowPath(ch.Path) {
			workflows = append(workflows, ch)
			continue
		}
		batch = append(batch, ch)
	}

	wrote := 0
	if len(batch) > 0 {
		if err := c.CommitFiles(owner, repo, branch, batch); err != nil {
			_ = c.deleteBranch(owner, repo, branch)
			return 0, fmt.Errorf("commit %d files: %w", len(batch), err)
		}
		wrote += len(batch)
		_, _ = fmt.Fprintf(stdout, "  %-45s  ✓ committed as one commit (%d files)\n", "files", len(batch))
	}
	for _, ch := range workflows {
		action, err := c.UpsertFileOnBranch(owner, repo, branch, ch.Path, ch.Content)
		if err != nil {
			if IsWorkflowLocked(err) {
				_, _ = fmt.Fprintf(stdout, "  %-45s  ⚠ workflow write refused — token lacks workflow scope; skipped (add the scope and re-run to ship it)\n", ch.Path)
				continue
			}
			_ = c.deleteBranch(owner, repo, branch)
			return 0, fmt.Errorf("upsert %s: %w", ch.Path, err)
		}
		if action != "skipped" {
			wrote++
			_, _ = fmt.Fprintf(stdout, "  %-45s  ✓ (%s)\n", ch.Path, action)
		}
	}
	if wrote == 0 {
		_ = c.deleteBranch(owner, repo, branch)
		return 0, nil
	}

	title := "feat: harden repo — community files, settings, security"
	body := "Applied repo hardening: branch protection, security features, community health files, and quality gates."

	prNum, err := c.CreatePullRequest(owner, repo, title, body, branch, defaultBranch)
	if err != nil {
		_ = c.deleteBranch(owner, repo, branch)
		return 0, err
	}
	return prNum, nil
}

// deleteBranch removes refs/heads/{branch}. Best-effort cleanup for unused harden branches.
func (c *Client) deleteBranch(owner, repo, branch string) error {
	resp, err := c.do(http.MethodDelete, repoPath(owner, repo)+"/git/refs/heads/"+url.PathEscape(branch), nil)
	if err != nil {
		return fmt.Errorf("delete branch %s: %w", branch, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return expectStatus("delete branch "+branch, resp, http.StatusNoContent)
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
func IsForbidden403(err error) bool {
	if err == nil {
		return false
	}
	if hasStatusCode(err, http.StatusForbidden) {
		return true
	}
	return strings.Contains(err.Error(), "403")
}

// IsBranchProtectionUnavailable reports a 403 that means branch protection is
// not offered on this plan (free-tier private). GitHub returns the same
// "Upgrade to GitHub Pro / make this repository public" 403 for both the
// rulesets API and the classic branch-protection API, so the same predicate
// covers pre-flight probes of either. Other 403s — token scope, SSO, IP
// allow lists — must surface as errors instead.
func IsBranchProtectionUnavailable(err error) bool {
	if !IsForbidden403(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"upgrade to github",
		"github pro",
		"make this repository public",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
