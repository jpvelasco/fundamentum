package github

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// commitMessage is the single commit message used when fundamentum batches
// every file change into one commit (the default direct-apply mode).
const commitMessage = "chore: harden repo via fundamentum"

// CommitFiles writes all changes as ONE commit on branch via the git data
// API (blobs → tree → commit → ref update). This replaces the old
// one-commit-per-file Contents API path, which littered the target branch
// with N "add X via fundamentum" commits.
//
// The ref update is a fast-forward by construction (the new commit's parent
// is the ref's current head), so it fails with 409/422 when branch
// protection blocks direct pushes — callers detect that and fall back to
// PR mode.
func (c *Client) CommitFiles(owner, repo, branch string, changes []FileChange) error {
	if len(changes) == 0 {
		return nil
	}
	base, err := c.refHead(owner, repo, branch)
	if err != nil {
		return err
	}
	tree, err := c.commitTree(owner, repo, base)
	if err != nil {
		return err
	}

	entries := make([]map[string]any, 0, len(changes))
	for _, ch := range changes {
		blob, err := c.createBlob(owner, repo, ch.Content)
		if err != nil {
			return err
		}
		entries = append(entries, map[string]any{
			"path": ch.Path,
			"mode": "100644",
			"type": "blob",
			"sha":  blob,
		})
	}
	newTree, err := c.createTree(owner, repo, tree, entries)
	if err != nil {
		return err
	}
	newCommit, err := c.createCommit(owner, repo, commitMessage, newTree, base)
	if err != nil {
		return err
	}
	return c.updateRef(owner, repo, branch, newCommit)
}

// refHead returns the commit SHA a branch currently points at.
func (c *Client) refHead(owner, repo, branch string) (string, error) {
	var result struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.getDecode(repoPath(owner, repo)+"/git/ref/heads/"+branch, "get ref heads/"+branch, &result); err != nil {
		return "", err
	}
	if result.Object.SHA == "" {
		return "", fmt.Errorf("get ref heads/%s: empty sha", branch)
	}
	return result.Object.SHA, nil
}

// commitTree returns the tree SHA of a commit.
func (c *Client) commitTree(owner, repo, sha string) (string, error) {
	var result struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := c.getDecode(fmt.Sprintf("%s/git/commits/%s", repoPath(owner, repo), sha), "get commit "+sha, &result); err != nil {
		return "", err
	}
	if result.Tree.SHA == "" {
		return "", fmt.Errorf("get commit %s: empty tree sha", sha)
	}
	return result.Tree.SHA, nil
}

// createBlob uploads content and returns the blob SHA.
func (c *Client) createBlob(owner, repo string, content []byte) (string, error) {
	resp, err := c.post(repoPath(owner, repo)+"/git/blobs", map[string]any{
		"content":   base64.StdEncoding.EncodeToString(content),
		"encoding": "base64",
	})
	if err != nil {
		return "", fmt.Errorf("create blob: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus("create blob", resp, http.StatusCreated); err != nil {
		return "", err
	}
	var result struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode blob: %w", err)
	}
	if result.SHA == "" {
		return "", fmt.Errorf("create blob: empty sha")
	}
	return result.SHA, nil
}

// createTree posts a tree layered on baseTree with the given entries.
func (c *Client) createTree(owner, repo, baseTree string, entries []map[string]any) (string, error) {
	resp, err := c.post(repoPath(owner, repo)+"/git/trees", map[string]any{
		"base_tree": baseTree,
		"tree":      entries,
	})
	if err != nil {
		return "", fmt.Errorf("create tree: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus("create tree", resp, http.StatusCreated); err != nil {
		return "", err
	}
	var result struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode tree: %w", err)
	}
	if result.SHA == "" {
		return "", fmt.Errorf("create tree: empty sha")
	}
	return result.SHA, nil
}

// createCommit posts a commit and returns its SHA.
func (c *Client) createCommit(owner, repo, message, tree, parent string) (string, error) {
	resp, err := c.post(repoPath(owner, repo)+"/git/commits", map[string]any{
		"message": message,
		"tree":    tree,
		"parents": []string{parent},
	})
	if err != nil {
		return "", fmt.Errorf("create commit: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus("create commit", resp, http.StatusCreated); err != nil {
		return "", err
	}
	var result struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode commit: %w", err)
	}
	if result.SHA == "" {
		return "", fmt.Errorf("create commit: empty sha")
	}
	return result.SHA, nil
}

// updateRef fast-forwards refs/heads/{branch} to sha. A 409/422 is returned
// as an error so callers can fall back (branch protection requires a PR,
// branch moved since the base was read).
func (c *Client) updateRef(owner, repo, branch, sha string) error {
	resp, err := c.do(http.MethodPatch, repoPath(owner, repo)+"/git/refs/heads/"+branch, map[string]any{
		"sha":   sha,
		"force": false,
	})
	if err != nil {
		return fmt.Errorf("update ref heads/%s: %w", branch, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus("update ref heads/"+branch, resp, http.StatusOK); err != nil {
		return err
	}
	return nil
}

// ReadFileContents fetches a file's decoded contents. A missing file is not
// an error — ok=false lets callers treat "absent" and "present" distinctly.
func (c *Client) ReadFileContents(owner, repo, path string) (content []byte, ok bool, err error) {
	resp, err := c.get(contentsPath(owner, repo, path))
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus("read file "+path, resp, http.StatusOK, http.StatusNotFound); err != nil {
		return nil, false, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	existing, err := decodeExistingContentsFile(resp, path)
	if err != nil {
		return nil, false, err
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(existing.Content, "\n", ""))
	if err != nil {
		return nil, false, fmt.Errorf("decode %s: %w", path, err)
	}
	return decoded, true, nil
}
