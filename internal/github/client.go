// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
func (c *Client) WithBaseURL(baseURL string) *Client {
	c.baseURL = baseURL
	return c
}

func (c *Client) base() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return defaultBase
}

func (c *Client) do(method, path string, body any) (*http.Response, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
	}
	req, err := http.NewRequest(method, c.base()+path, &buf)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Verbose {
		fmt.Printf("+ %s %s\n", method, c.base()+path)
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
