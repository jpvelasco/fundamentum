// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// AnyFileExists checks whether any of the given paths exists in the repo.
// Use this to detect case-variant or path-variant duplicates before writing.
func (c *Client) AnyFileExists(owner, repo string, paths []string) (bool, error) {
	for _, path := range paths {
		resp, err := c.get(fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, path))
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
	apiPath := fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, path)
	resp, err := c.get(apiPath)
	if err != nil {
		return "", fmt.Errorf("check file %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		var existing struct {
			Content string `json:"content"`
		}
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("read existing file %s: %w", path, err)
		}
		if err := json.Unmarshal(raw, &existing); err != nil {
			return "", fmt.Errorf("parse existing file %s: %w", path, err)
		}
		existingClean := bytes.ReplaceAll([]byte(existing.Content), []byte("\n"), nil)
		if bytes.Equal(existingClean, []byte(base64.StdEncoding.EncodeToString(content))) {
			return "skip", nil
		}
		return "update", nil
	case http.StatusNotFound:
		return "create", nil
	default:
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("check file %s: %s: %s", path, resp.Status, b)
	}
}

// UpsertFile creates or updates a file in the repo via the Contents API.
// Returns "created", "updated", or "skipped" (content unchanged).
func (c *Client) UpsertFile(owner, repo, path string, content []byte) (string, error) {
	apiPath := fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, path)

	resp, err := c.get(apiPath)
	if err != nil {
		return "", fmt.Errorf("check file %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := map[string]any{
		"message": "chore: add " + path + " via fundamentum",
		"content": base64.StdEncoding.EncodeToString(content),
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
			return "", fmt.Errorf("read existing file %s: %w", path, err)
		}
		if err := json.Unmarshal(raw, &existing); err != nil {
			return "", fmt.Errorf("parse existing file %s: %w", path, err)
		}
		existingClean := bytes.ReplaceAll([]byte(existing.Content), []byte("\n"), nil)
		newEncoded := []byte(base64.StdEncoding.EncodeToString(content))
		if bytes.Equal(existingClean, newEncoded) {
			return "skipped", nil
		}
		body["sha"] = existing.SHA
		body["message"] = "chore: update " + path + " via fundamentum"
		action = "updated"

	case http.StatusNotFound:
		action = "created"

	default:
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("check file %s: %s: %s", path, resp.Status, b)
	}

	putResp, err := c.do(http.MethodPut, apiPath, body)
	if err != nil {
		return "", fmt.Errorf("upsert file %s: %w", path, err)
	}
	defer func() { _ = putResp.Body.Close() }()
	if putResp.StatusCode != http.StatusCreated && putResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(putResp.Body)
		return "", fmt.Errorf("upsert file %s: %s: %s", path, putResp.Status, b)
	}
	return action, nil
}
