// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"fmt"
	"io"
	"net/http"
)

// EnableSecurity enables Dependabot alerts, secret scanning, and push protection.
// CodeQL is only enabled for public repos (private repos require GitHub Advanced Security).
func (c *Client) EnableSecurity(owner, repo, visibility string) error {
	base := fmt.Sprintf("/repos/%s/%s", owner, repo)

	for _, path := range []string{
		base + "/vulnerability-alerts",
		base + "/automated-security-fixes",
	} {
		resp, err := c.put(path)
		if err != nil {
			return fmt.Errorf("enable security %s: %w", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			return fmt.Errorf("enable security %s: unexpected status %s", path, resp.Status)
		}
	}

	resp, err := c.patch(base, map[string]any{
		"security_and_analysis": map[string]any{
			"secret_scanning":                 map[string]any{"status": "enabled"},
			"secret_scanning_push_protection": map[string]any{"status": "enabled"},
		},
	})
	if err != nil {
		return fmt.Errorf("enable secret scanning: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("enable secret scanning: %s: %s", resp.Status, body)
	}

	// CodeQL requires GitHub Advanced Security for private repos (paid).
	// Only enable for public repos.
	if visibility == "public" {
		resp2, err := c.patch(base+"/code-scanning/default-setup", map[string]any{
			"state":       "configured",
			"query_suite": "default",
		})
		if err != nil {
			return fmt.Errorf("enable CodeQL: %w", err)
		}
		defer func() { _ = resp2.Body.Close() }()
		if resp2.StatusCode != http.StatusOK && resp2.StatusCode != http.StatusAccepted {
			body, _ := io.ReadAll(resp2.Body)
			return fmt.Errorf("enable CodeQL: %s: %s", resp2.Status, body)
		}
	}

	return nil
}