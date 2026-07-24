// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

const defaultBase = "https://api.github.com"

// Client makes authenticated requests to the GitHub API.
type Client struct {
	Token   string
	Verbose bool
	baseURL string
	client  *http.Client
}

// NewClient creates a Client, falling back to GITHUB_TOKEN env var if token is empty.
func NewClient(token string, verbose bool) *Client {
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	return &Client{
		Token:   token,
		Verbose: verbose,
		baseURL: defaultBase,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// WithBaseURL sets the base URL for the client, useful for testing.
// Rejects URLs with non-HTTPS schemes (except localhost/127.0.0.1 for testing)
// to prevent SSRF and credential leakage.
func (c *Client) WithBaseURL(baseURL string) *Client {
	if baseURL == "" {
		return c
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return c
	}
	// Only allow localhost for testing or HTTPS for production.
	if u.Scheme != "https" && !strings.HasPrefix(u.Host, "localhost") && !strings.HasPrefix(u.Host, "127.0.0.1") {
		return c
	}
	c.baseURL = u.String()
	return c
}

func (c *Client) base() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return defaultBase
}

// repoPath constructs a safe API path for repo-level operations,
// URL-escaping owner and repo to prevent path traversal.
func repoPath(owner, repo string) string {
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
}

func (c *Client) do(method, path string, body any) (*http.Response, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
	}

	url := c.base() + path
	if c.Verbose {
		fmt.Printf("+ %s %s\n", method, url)
	}

	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.client.Do(req)
}

func (c *Client) get(path string) (*http.Response, error) {
	return c.do(http.MethodGet, path, nil)
}

func (c *Client) patch(path string, body any) (*http.Response, error) {
	return c.do(http.MethodPatch, path, body)
}

func (c *Client) post(path string, body any) (*http.Response, error) {
	return c.do(http.MethodPost, path, body)
}

func (c *Client) put(path string) (*http.Response, error) {
	return c.do(http.MethodPut, path, nil)
}

// expectStatus returns a wrapped error naming action if resp's status code is
// not one of want. Does not close resp.Body — callers still own that.
func expectStatus(action string, resp *http.Response, want ...int) error {
	if slices.Contains(want, resp.StatusCode) {
		return nil
	}
	b, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("%s: %s: %s", action, resp.Status, b)
}

// existingContentsFile is the subset of the Contents API GET response used to
// detect whether new content differs from what's already committed.
type existingContentsFile struct {
	Content string `json:"content"`
	SHA     string `json:"sha"`
}

// decodeExistingContentsFile reads and parses a Contents API GET response body.
func decodeExistingContentsFile(resp *http.Response, path string) (existingContentsFile, error) {
	var existing existingContentsFile
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return existing, fmt.Errorf("read existing file %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &existing); err != nil {
		return existing, fmt.Errorf("parse existing file %s: %w", path, err)
	}
	return existing, nil
}

// contentsUnchanged reports whether existingContent (base64, possibly
// newline-wrapped by the GitHub API) matches newContent once re-encoded.
func contentsUnchanged(existingContent string, newContent []byte) bool {
	existingClean := strings.ReplaceAll(existingContent, "\n", "")
	newEncoded := base64.StdEncoding.EncodeToString(newContent)
	return existingClean == newEncoded
}

// fileAction indicates whether a file needs to be created, updated, or left alone.
type fileAction int

const (
	fileActionCreate fileAction = iota
	fileActionUpdate
	fileActionSkip
)

// checkExistingFile GETs getPath and determines whether content differs from
// what's already committed at that path. sha is the existing file's SHA
// (needed for the update request), empty when the file doesn't exist yet.
func (c *Client) checkExistingFile(getPath, path string, content []byte) (action fileAction, sha string, err error) {
	resp, err := c.get(getPath)
	if err != nil {
		return fileActionSkip, "", fmt.Errorf("check file %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		existing, err := decodeExistingContentsFile(resp, path)
		if err != nil {
			return fileActionSkip, "", err
		}
		if contentsUnchanged(existing.Content, content) {
			return fileActionSkip, "", nil
		}
		return fileActionUpdate, existing.SHA, nil
	case http.StatusNotFound:
		return fileActionCreate, "", nil
	default:
		return fileActionSkip, "", expectStatus("check file "+path, resp, http.StatusOK, http.StatusNotFound)
	}
}

// upsertBody builds the Contents API request body for creating or updating
// path with content. sha (required for updates) is empty for a create.
func upsertBody(path string, content []byte, sha string, wasUpdate bool) map[string]any {
	body := map[string]any{
		"message": "chore: add " + path,
		"content": base64.StdEncoding.EncodeToString(content),
	}
	if wasUpdate {
		body["sha"] = sha
		body["message"] = "chore: update " + path
	}
	return body
}

// putFileContents PUTs body to putPath and interprets the result as an
// upsert outcome ("created"/"updated"/"skipped"), given wasUpdate (whether
// the caller determined this is an update rather than a create) and a
// describe function for error messages (e.g. "upsert file %s" or
// "upsert file %s on %s").
func (c *Client) putFileContents(putPath string, body map[string]any, wasUpdate bool, describe func() string) (string, error) {
	putResp, err := c.do(http.MethodPut, putPath, body)
	if err != nil {
		return "", fmt.Errorf("%s: %w", describe(), err)
	}
	defer func() { _ = putResp.Body.Close() }()

	// GitHub Actions locks workflow files — PUT returns 404 when trying to
	// overwrite an existing workflow via the Contents API.
	if putResp.StatusCode == http.StatusNotFound && wasUpdate {
		return "skipped", fmt.Errorf("%s: %w", describe(), ErrWorkflowLocked)
	}
	if err := expectStatus(describe(), putResp, http.StatusCreated, http.StatusOK); err != nil {
		return "", err
	}
	if wasUpdate {
		return "updated", nil
	}
	return "created", nil
}