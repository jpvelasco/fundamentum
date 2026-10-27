// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// This file backs the bot-managed audit: read-only probes that report how far
// a repo is from the profile's hardening posture. Nothing here mutates.

// RepoSecretCount returns how many Actions secrets the repository holds. The
// bot-managed audit reports the count (never the names or values) so the
// operator can see how much secret surface exists.
func (c *Client) RepoSecretCount(owner, repo string) (int, error) {
	var out struct {
		TotalCount int `json:"total_count"`
	}
	if err := c.getDecode(repoPath(owner, repo)+"/actions/secrets?per_page=1", "list actions secrets", &out); err != nil {
		return 0, err
	}
	return out.TotalCount, nil
}

// EnvironmentBranchPolicy describes one environment's deployment-branch
// restriction, or that it has none.
type EnvironmentBranchPolicy struct {
	Name string
	// Restricted reports whether the environment limits which branches can
	// deploy to it. An unrestricted environment lets any branch (including a
	// PR branch) reach its secrets.
	Restricted bool
	// DefaultBranchOnly reports whether the restriction is limited to exactly
	// the repo's default branch.
	DefaultBranchOnly bool
	// Patterns are the allowed branch patterns (for reporting).
	Patterns []string
}

// EnvironmentBranchPolicies lists each environment's deployment-branch
// posture. Environments the token cannot read are skipped only on 404; any
// other failure is an error (fail closed).
func (c *Client) EnvironmentBranchPolicies(owner, repo, defaultBranch string) ([]EnvironmentBranchPolicy, error) {
	var list struct {
		Environments []struct {
			Name string `json:"name"`
		} `json:"environments"`
	}
	if err := c.getDecode(repoPath(owner, repo)+"/environments?per_page=100", "list environments", &list); err != nil {
		return nil, err
	}
	var out []EnvironmentBranchPolicy
	for _, env := range list.Environments {
		pol, err := c.environmentBranchPolicy(owner, repo, env.Name, defaultBranch)
		if err != nil {
			return nil, err
		}
		out = append(out, pol)
	}
	return out, nil
}

// environmentBranchPolicy reads one environment's branch protection rule.
func (c *Client) environmentBranchPolicy(owner, repo, name, defaultBranch string) (EnvironmentBranchPolicy, error) {
	pol := EnvironmentBranchPolicy{Name: name}
	var env struct {
		ProtectionRules []struct {
			Type                    string `json:"type"`
			ProtectedBranches       bool   `json:"protected_branches"`
			CustomBranchPolicies    bool   `json:"custom_branch_policies"`
			BranchNamePatternPolicy []struct {
				Name string `json:"name"`
			} `json:"branch_name_pattern_policy,omitempty"`
		} `json:"protection_rules"`
	}
	resp, err := c.get(repoPath(owner, repo) + "/environments/" + url.PathEscape(name))
	if err != nil {
		return pol, fmt.Errorf("get environment %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus("get environment "+name, resp, http.StatusOK); err != nil {
		return pol, err
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return pol, fmt.Errorf("decode environment %s: %w", name, err)
	}
	for _, rule := range env.ProtectionRules {
		if rule.Type != "branch_policy" {
			continue
		}
		pol.Restricted = true
		for _, bp := range rule.BranchNamePatternPolicy {
			pol.Patterns = append(pol.Patterns, bp.Name)
		}
	}
	pol.DefaultBranchOnly = pol.Restricted && len(pol.Patterns) == 1 && pol.Patterns[0] == defaultBranch
	return pol, nil
}

// WorkflowFinding is one bot-managed concern found in a workflow file.
type WorkflowFinding struct {
	Path   string
	Detail string
}

// ScanWorkflows fetches every .github/workflows file and reports bot-managed
// concerns: privileged triggers (workflow_run / pull_request_target), local
// (./) actions, and write scopes granted outside a default-branch-only
// environment. The scan is text-based — enough signal for an audit without a
// full YAML parser.
func (c *Client) ScanWorkflows(owner, repo, defaultBranch string, defaultBranchOnlyEnvs map[string]bool) ([]WorkflowFinding, error) {
	paths, err := c.listWorkflowPaths(owner, repo)
	if err != nil {
		return nil, err
	}
	var findings []WorkflowFinding
	for _, path := range paths {
		content, ok, err := c.ReadFileContents(owner, repo, path)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		findings = append(findings, AnalyzeWorkflow(path, string(content), defaultBranchOnlyEnvs)...)
	}
	return findings, nil
}

// listWorkflowPaths lists the file paths under .github/workflows.
func (c *Client) listWorkflowPaths(owner, repo string) ([]string, error) {
	resp, err := c.get(contentsPath(owner, repo, ".github/workflows"))
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus("list workflows", resp, http.StatusOK, http.StatusNotFound); err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	var entries []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode workflows listing: %w", err)
	}
	var out []string
	for _, e := range entries {
		if e.Type == "file" && (strings.HasSuffix(e.Path, ".yml") || strings.HasSuffix(e.Path, ".yaml")) {
			out = append(out, e.Path)
		}
	}
	return out, nil
}

// AnalyzeWorkflow reports bot-managed concerns in one workflow body. It is a
// pure function so the audit logic is unit-testable without a server.
// defaultBranchOnlyEnvs names the environments restricted to the default
// branch; a job running in any OTHER environment (or none) with a write scope
// is flagged.
func AnalyzeWorkflow(path, content string, defaultBranchOnlyEnvs map[string]bool) []WorkflowFinding {
	var findings []WorkflowFinding
	if strings.Contains(content, "workflow_run:") {
		findings = append(findings, WorkflowFinding{path, "uses privileged workflow_run trigger"})
	}
	if strings.Contains(content, "pull_request_target:") {
		findings = append(findings, WorkflowFinding{path, "uses privileged pull_request_target trigger"})
	}
	if usesLocalAction(content) {
		findings = append(findings, WorkflowFinding{path, "uses a local (./) action"})
	}
	for _, scope := range writeScopes(content) {
		// security-events: write on CodeQL is the one write scope the profile
		// accepts (CodeQL cannot report without it).
		if strings.HasPrefix(scope, "security-events:") {
			continue
		}
		if !runsInDefaultBranchOnlyEnv(content, defaultBranchOnlyEnvs) {
			findings = append(findings, WorkflowFinding{path, "grants a write scope (" + strings.TrimSpace(scope) + ") outside a default-branch-only environment"})
		}
		break // one write-scope finding per workflow is enough signal
	}
	return findings
}

// usesLocalAction reports whether the workflow references a local ./ action.
func usesLocalAction(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "uses: ./") {
			return true
		}
	}
	return false
}

// writeScopes returns the "*: write" permission grants in the workflow.
func writeScopes(content string) []string {
	var out []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasSuffix(trimmed, ": write") {
			out = append(out, trimmed)
		}
	}
	return out
}

// runsInDefaultBranchOnlyEnv reports whether every job that declares an
// environment uses one restricted to the default branch. A workflow with no
// environment at all is not restricted (its write scopes are reachable by any
// branch), so it returns false.
func runsInDefaultBranchOnlyEnv(content string, defaultBranchOnlyEnvs map[string]bool) bool {
	sawEnv := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "environment:") {
			continue
		}
		sawEnv = true
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, "environment:"))
		name = strings.Trim(name, `"'`)
		if !defaultBranchOnlyEnvs[name] {
			return false
		}
	}
	return sawEnv
}
