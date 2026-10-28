// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
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
// environment. YAML parsing keeps comments and script text out of the scan.
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
	var workflow struct {
		On          any `yaml:"on"`
		Permissions any
		Jobs        map[string]workflowAuditJob
	}
	if err := yaml.Unmarshal([]byte(content), &workflow); err != nil {
		return []WorkflowFinding{{path, "could not parse workflow YAML: " + err.Error()}}
	}
	var findings []WorkflowFinding
	for _, trigger := range workflowTriggers(workflow.On) {
		if trigger == "workflow_run" || trigger == "pull_request_target" {
			findings = append(findings, WorkflowFinding{path, "uses privileged " + trigger + " trigger"})
		}
	}
	for _, name := range slices.Sorted(maps.Keys(workflow.Jobs)) {
		job := workflow.Jobs[name]
		prefix := fmt.Sprintf("job %q: ", name)
		if job.usesLocalAction() {
			findings = append(findings, WorkflowFinding{path, prefix + "uses a local (./) action"})
		}
		permissions := job.Permissions
		if permissions == nil {
			permissions = workflow.Permissions
		}
		envName, _ := job.Environment.(string)
		if env, ok := job.Environment.(map[string]any); ok {
			envName, _ = env["name"].(string)
		}
		if scopes := writeScopes(permissions); len(scopes) > 0 && (envName == "" || !defaultBranchOnlyEnvs[envName]) {
			findings = append(findings, WorkflowFinding{path, prefix + "grants a write scope (" + strings.Join(scopes, ", ") + ") outside a default-branch-only environment"})
		}
	}
	return findings
}

type workflowAuditJob struct {
	Permissions any
	Environment any
	Uses        string
	Steps       []struct{ Uses string }
}

func workflowTriggers(on any) []string {
	switch events := on.(type) {
	case string:
		return []string{events}
	case []any:
		var names []string
		for _, event := range events {
			if name, ok := event.(string); ok {
				names = append(names, name)
			}
		}
		return names
	case map[string]any:
		return slices.Sorted(maps.Keys(events))
	default:
		return nil
	}
}

func (job workflowAuditJob) usesLocalAction() bool {
	if strings.HasPrefix(job.Uses, "./") {
		return true
	}
	for _, step := range job.Steps {
		if strings.HasPrefix(step.Uses, "./") {
			return true
		}
	}
	return false
}

// writeScopes returns effective write grants, except CodeQL's accepted
// security-events scope. An explicit job permissions map replaces, rather
// than extends, the workflow permissions.
func writeScopes(permissions any) []string {
	if permissions == "write-all" {
		return []string{"write-all"}
	}
	grants, _ := permissions.(map[string]any)
	var out []string
	for scope, grant := range grants {
		if grant == "write" && scope != "security-events" {
			out = append(out, scope+": write")
		}
	}
	slices.Sort(out)
	return out
}
