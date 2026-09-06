// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"fmt"
	"net/http"
)

// SecurityOptions selects which security features EnableSecurity turns on.
type SecurityOptions struct {
	Visibility     string
	AdvancedCodeQL bool // skip default-setup CodeQL when the advanced workflow ships
	PaidFeatures   bool // secret scanning + push protection (GHAS on private/internal)
}

// IsPublicVisibility reports whether visibility is GitHub's free public tier.
func IsPublicVisibility(visibility string) bool {
	return visibility == "public"
}

// DependabotAlertsEnabled reports whether Dependabot alerts are on.
// GitHub returns 204 when enabled and 404 when disabled.
func (c *Client) DependabotAlertsEnabled(owner, repo string) (bool, error) {
	resp, err := c.get(repoPath(owner, repo) + "/vulnerability-alerts")
	if err != nil {
		return false, fmt.Errorf("check dependabot alerts: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus("check dependabot alerts", resp, http.StatusNoContent, http.StatusNotFound); err != nil {
		return false, err
	}
	return resp.StatusCode == http.StatusNoContent, nil
}

// EnableSecurity enables Dependabot alerts and security updates for every repo.
// Secret scanning and push protection are free on public repos; on private or
// internal repos they require GitHub Advanced Security and are skipped unless
// PaidFeatures is set. CodeQL default setup stays public-only.
func (c *Client) EnableSecurity(owner, repo string, opts SecurityOptions) error {
	base := repoPath(owner, repo)

	for _, path := range []string{
		base + "/vulnerability-alerts",
		base + "/automated-security-fixes",
	} {
		resp, err := c.put(path)
		if err != nil {
			return fmt.Errorf("enable security %s: %w", path, err)
		}
		statusErr := expectStatus("enable security "+path, resp, http.StatusOK, http.StatusNoContent)
		_ = resp.Body.Close()
		if statusErr != nil {
			return statusErr
		}
	}

	if IsPublicVisibility(opts.Visibility) || opts.PaidFeatures {
		if err := c.enableSecretScanning(base); err != nil {
			return err
		}
	}

	if IsPublicVisibility(opts.Visibility) && !opts.AdvancedCodeQL {
		if err := c.enableCodeQL(base); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) enableSecretScanning(base string) error {
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
	return expectStatus("enable secret scanning", resp, http.StatusOK)
}

// enableCodeQL enables CodeQL scanning. Only available for public repos
// (private repos require GitHub Advanced Security, which is paid).
func (c *Client) enableCodeQL(base string) error {
	resp, err := c.patch(base+"/code-scanning/default-setup", map[string]any{
		"state":       "configured",
		"query_suite": "default",
	})
	if err != nil {
		return fmt.Errorf("enable CodeQL: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return expectStatus("enable CodeQL", resp, http.StatusOK, http.StatusAccepted)
}
