package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// GetFileContent returns the raw content and blob SHA of a file at ref.
// ok=false means the file does not exist (404); any other error is returned.
func (c *Client) GetFileContent(owner, repo, path, ref string) (content string, sha string, ok bool, err error) {
	p := contentsPath(owner, repo, path)
	if ref != "" {
		p += "?ref=" + url.QueryEscape(ref)
	}
	resp, err := c.get(p)
	if err != nil {
		return "", "", false, fmt.Errorf("get file %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", "", false, nil
	}
	if err := expectStatus("get file "+path, resp, http.StatusOK); err != nil {
		return "", "", false, err
	}
	var body struct {
		Content  string `json:"content"`
		SHA      string `json:"sha"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", "", false, fmt.Errorf("decode file %s: %w", path, err)
	}
	return body.Content, body.SHA, true, nil
}

// GetRulesetBody returns the full JSON body of a named ruleset and whether it
// exists. Used to snapshot a ruleset before it is changed.
func (c *Client) GetRulesetBody(owner, repo, name string) (map[string]any, bool, error) {
	rs, err := c.GetRuleset(owner, repo, name)
	if err != nil {
		return nil, false, err
	}
	if rs == nil {
		return nil, false, nil
	}
	var m map[string]any
	b, err := json.Marshal(rs)
	if err != nil {
		return nil, false, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, false, err
	}
	return m, true, nil
}

// DeleteRuleset removes a named ruleset. A missing ruleset is not an error
// (revert is idempotent); any other failure is returned.
func (c *Client) DeleteRuleset(owner, repo, name string) error {
	id, ok, err := c.rulesetID(owner, repo, name)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	resp, err := c.do(http.MethodDelete, fmt.Sprintf("%s/rulesets/%d", repoPath(owner, repo), id), nil)
	if err != nil {
		return fmt.Errorf("delete ruleset %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return expectStatus("delete ruleset "+name, resp, http.StatusNoContent, http.StatusNotFound)
}

// UpdateRuleset replaces a ruleset's body. The id must match the current
// ruleset (from GetRulesetBody).
func (c *Client) UpdateRuleset(owner, repo string, id int64, body map[string]any) error {
	return c.updateRuleset(owner, repo, id, body, "update ruleset")
}

// UpdateRepoSettings patches repo-level settings (merge methods + auto-delete).
func (c *Client) UpdateRepoSettings(owner, repo string, s SettingsState) error {
	resp, err := c.patch(repoPath(owner, repo), map[string]any{
		"delete_branch_on_merge": s.DeleteBranchOnMerge,
		"allow_squash_merge":     s.AllowSquashMerge,
		"allow_merge_commit":     s.AllowMergeCommit,
		"allow_rebase_merge":     s.AllowRebaseMerge,
	})
	if err != nil {
		return fmt.Errorf("update repo settings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return expectStatus("update repo settings", resp, http.StatusOK)
}

// DisableSecurity turns off the security features apply enables. Idempotent.
func (c *Client) DisableSecurity(owner, repo string, s SecurityState) error {
	base := repoPath(owner, repo)
	for _, p := range []struct {
		path string
		off  bool
	}{
		{base + "/vulnerability-alerts", !s.DependabotAlerts},
		{base + "/automated-security-fixes", !s.AutomatedFixes},
	} {
		if !p.off {
			continue
		}
		resp, err := c.do(http.MethodDelete, p.path, nil)
		if err != nil {
			return fmt.Errorf("disable security %s: %w", p.path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
			return expectStatus("disable security "+p.path, resp, http.StatusNoContent)
		}
	}
	if !s.SecretScanning || !s.PushProtection {
		resp, err := c.patch(base, map[string]any{
			"security_and_analysis": map[string]any{
				"secret_scanning":                 map[string]any{"status": disabledIf(s.SecretScanning)},
				"secret_scanning_push_protection": map[string]any{"status": disabledIf(s.PushProtection)},
			},
		})
		if err != nil {
			return fmt.Errorf("disable secret scanning: %w", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
			return expectStatus("disable secret scanning", resp, http.StatusOK)
		}
	}
	return nil
}

// disabledIf returns "enabled" when the feature was on, "disabled" otherwise.
func disabledIf(on bool) string {
	if on {
		return "enabled"
	}
	return "disabled"
}

// DeleteFile removes a file at the repo's default branch. A missing file is
// not an error (revert is idempotent).
func (c *Client) DeleteFile(owner, repo, path string) error {
	_, sha, ok, err := c.GetFileContent(owner, repo, path, "")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	resp, err := c.do(http.MethodDelete, contentsPath(owner, repo, path), map[string]any{
		"message": "revert " + path,
		"sha":     sha,
	})
	if err != nil {
		return fmt.Errorf("delete file %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return expectStatus("delete file "+path, resp, http.StatusOK)
}

// RulesetIDForUpdate returns the current ID of a named ruleset for UpdateRuleset.
func (c *Client) RulesetIDForUpdate(owner, repo, name string) (int64, error) {
	id, ok, err := c.rulesetID(owner, repo, name)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("ruleset %s not found", name)
	}
	return id, nil
}

// SettingsState records the repo settings apply changes (for snapshot/revert).
type SettingsState struct {
	DeleteBranchOnMerge bool `json:"delete_branch_on_merge"`
	AllowSquashMerge    bool `json:"allow_squash_merge"`
	AllowMergeCommit    bool `json:"allow_merge_commit"`
	AllowRebaseMerge    bool `json:"allow_rebase_merge"`
}

// SecurityState records the security toggles apply touches (for snapshot/revert).
type SecurityState struct {
	DependabotAlerts   bool `json:"dependabot_alerts"`
	AutomatedFixes     bool `json:"automated_security_fixes"`
	SecretScanning     bool `json:"secret_scanning"`
	PushProtection     bool `json:"push_protection"`
	CodeQLDefaultSetup bool `json:"codeql_default_setup"`
}
