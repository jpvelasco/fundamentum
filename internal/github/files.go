// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// contentsPath constructs a safe API path for repo content operations,
// URL-escaping each component to prevent path traversal.
func contentsPath(owner, repo, path string) string {
	return fmt.Sprintf("/repos/%s/%s/contents/%s",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(path))
}

// IsWorkflowPath reports whether path targets a GitHub Actions workflow
// file. GitHub gates writes to these paths behind the `workflow` token
// scope: with a repo-scoped token the Contents API PUT returns 404 — on
// workflow creates just like on updates — and the git data API refuses
// to create a tree containing such a path the same way. Both refusals
// are classified ErrWorkflowLocked so callers can skip with a warning.
func IsWorkflowPath(path string) bool {
	return strings.HasPrefix(path, ".github/workflows/")
}

// AnyFileExists checks whether any of the given paths exists in the repo.
// Use this to detect case-variant or path-variant duplicates before writing.
// Only 404 counts as "missing" — any other non-200 status (401/403/500) is
// surfaced as an error rather than silently treated as "does not exist".
func (c *Client) AnyFileExists(owner, repo string, paths []string) (bool, error) {
	for _, path := range paths {
		resp, err := c.get(contentsPath(owner, repo, path))
		if err != nil {
			return false, fmt.Errorf("check file %s: %w", path, err)
		}
		statusErr := expectStatus("check file "+path, resp, http.StatusOK, http.StatusNotFound)
		_ = resp.Body.Close()
		if statusErr != nil {
			return false, statusErr
		}
		if resp.StatusCode == http.StatusOK {
			return true, nil
		}
	}
	return false, nil
}

// FileStatus returns "create", "update", or "skip" for a file without writing.
func (c *Client) FileStatus(owner, repo, path string, content []byte) (string, error) {
	action, _, err := c.FileStatusDetail(owner, repo, path, content)
	return action, err
}

// FileStatusDetail is FileStatus plus a human-readable update detail: for
// content drift it returns the line-change summary ("+3/-5") computed
// against the live content. Empty detail means no useful diff (create).
func (c *Client) FileStatusDetail(owner, repo, path string, content []byte) (action, detail string, err error) {
	// One GET decides everything: existence, drift, and (for updates) the
	// live content the diffstat summarizes.
	resp, err := c.get(contentsPath(owner, repo, path))
	if err != nil {
		return "", "", fmt.Errorf("check file %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "create", "", nil
	case resp.StatusCode != http.StatusOK:
		return "", "", expectStatus("check file "+path, resp, http.StatusOK, http.StatusNotFound)
	}

	existing, err := decodeExistingContentsFile(resp, path)
	if err != nil {
		return "", "", err
	}
	if contentsUnchanged(existing.Content, content) {
		return "skip", "", nil
	}
	live, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(existing.Content, "\n", ""))
	if err != nil {
		return "update", "", nil // drift is still drift even if the live bytes are odd
	}
	return "update", DiffStat(live, content), nil
}

// DiffStat summarizes the line difference between old and new content as
// "+added/-removed". Counts are multiset-based (a line changed in place
// counts as one added and one removed) — enough signal for a plan table
// without a full unified diff.
func DiffStat(old, new []byte) string {
	oldCount := lineCount(string(old))
	newCount := lineCount(string(new))
	added, removed := 0, 0
	for line, n := range newCount {
		if d := n - oldCount[line]; d > 0 {
			added += d
		}
	}
	for line, n := range oldCount {
		if d := n - newCount[line]; d > 0 {
			removed += d
		}
	}
	return fmt.Sprintf("+%d/-%d", added, removed)
}

// lineCount counts occurrences of each line (trailing newline ignored).
func lineCount(content string) map[string]int {
	counts := map[string]int{}
	if content == "" {
		return counts
	}
	lines := strings.Split(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for _, line := range lines {
		counts[line]++
	}
	return counts
}

// UpsertFile creates or updates a file in the repo via the Contents API.
// Returns "created", "updated", or "skipped" (content unchanged).
func (c *Client) UpsertFile(owner, repo, path string, content []byte) (string, error) {
	return c.upsertFile(owner, repo, "", path, content)
}

// upsertFile creates or updates path via the Contents API. When branch is
// empty, operates on the default branch (root Contents API, no ?ref=); when
// set, targets that branch (used by UpsertFileOnBranch for PR workflows).
func (c *Client) upsertFile(owner, repo, branch, path string, content []byte) (string, error) {
	getPath := contentsPath(owner, repo, path)
	describe := func() string { return "upsert file " + path }
	if branch != "" {
		q := url.Values{"ref": {branch}}
		getPath = contentsPath(owner, repo, path) + "?" + q.Encode()
		describe = func() string { return fmt.Sprintf("upsert file %s on %s", path, branch) }
	}

	action, sha, err := c.checkExistingFile(getPath, path, content)
	if err != nil {
		return "", err
	}
	if action == fileActionSkip {
		return "skipped", nil
	}

	wasUpdate := action == fileActionUpdate
	body := upsertBody(path, content, sha, wasUpdate)
	if branch != "" {
		body["branch"] = branch
	}

	// PUT doesn't support ?ref= — always target the root Contents path.
	return c.putFileContents(contentsPath(owner, repo, path), body, wasUpdate, IsWorkflowPath(path), describe)
}
