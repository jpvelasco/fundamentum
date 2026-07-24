// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"fmt"
	"net/http"
	"net/url"
)

// contentsPath constructs a safe API path for repo content operations,
// URL-escaping each component to prevent path traversal.
func contentsPath(owner, repo, path string) string {
	return fmt.Sprintf("/repos/%s/%s/contents/%s",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(path))
}

// AnyFileExists checks whether any of the given paths exists in the repo.
// Use this to detect case-variant or path-variant duplicates before writing.
func (c *Client) AnyFileExists(owner, repo string, paths []string) (bool, error) {
	for _, path := range paths {
		resp, err := c.get(contentsPath(owner, repo, path))
		if err != nil {
			return false, fmt.Errorf("check file %s: %w", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return true, nil
		}
	}
	return false, nil
}

// FileStatus returns "create", "update", or "skip" for a file without writing.
func (c *Client) FileStatus(owner, repo, path string, content []byte) (string, error) {
	action, _, err := c.checkExistingFile(contentsPath(owner, repo, path), path, content)
	if err != nil {
		return "", err
	}
	switch action {
	case fileActionUpdate:
		return "update", nil
	case fileActionCreate:
		return "create", nil
	default:
		return "skip", nil
	}
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
		getPath = fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s",
			url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(path), url.PathEscape(branch))
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
	return c.putFileContents(contentsPath(owner, repo, path), body, wasUpdate, describe)
}
