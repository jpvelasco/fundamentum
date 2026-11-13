package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jpvelasco/fundamentum/cmd/apply"
	"github.com/jpvelasco/fundamentum/cmd/audit"
	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/cmd/util"
	"github.com/jpvelasco/fundamentum/internal/github"
)

// newClient creates the GitHub API client; a package-level var so tests can
// inject a mock-server client.
var newClient = github.NewClient

// toolSet holds the MCP tool definitions and their handlers. The same token
// rule as the CLI applies: GITHUB_TOKEN env var or --token, then `gh auth
// token`. Tokens never appear in tool output.
type toolSet struct{}

// toolDef is one entry of tools/list.
type toolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// ownerRepoSchema is the shared input schema: a required OWNER/REPO string.
func ownerRepoSchema(description string) any {
	return map[string]any{
		"type":     "object",
		"required": []string{"repo"},
		"properties": map[string]any{
			"repo": map[string]any{
				"type":        "string",
				"description": description,
			},
		},
	}
}

// list returns the tool catalog for tools/list.
func (t *toolSet) list() []toolDef {
	return []toolDef{
		{
			Name:        "plan",
			Description: "Dry-run a fundamentum harden of OWNER/REPO and return the plan table. Never changes anything. When the repo does not exist yet, returns the init dry-run plan (visibility comes from --private).",
			InputSchema: ownerRepoSchema("GitHub repository as OWNER/REPO"),
		},
		{
			Name:        "audit",
			Description: "Compare OWNER/REPO against the fundamentum baseline and return the PASS/FAIL/WARN/SKIP report. Read-only; never changes anything.",
			InputSchema: ownerRepoSchema("GitHub repository as OWNER/REPO"),
		},
		{
			Name:        "apply",
			Description: "Harden OWNER/REPO for real (writes files, settings, branch protection, security features, priority labels). This is MUTATING — always run plan first and only call apply when the operator confirmed the plan. Runs with the solo defaults and prompts disabled.",
			InputSchema: ownerRepoSchema("GitHub repository as OWNER/REPO"),
		},
	}
}

// args is the parsed arguments for every tool.
type args struct {
	Repo string `json:"repo"`
}

// run dispatches one tools/call by name.
func (t *toolSet) run(name string, raw json.RawMessage) (string, error) {
	var a args
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	owner, repo, err := util.ParseOwnerRepo(a.Repo)
	if err != nil {
		return "", err
	}
	token, err := util.RequireToken(globals.Token)
	if err != nil {
		return "", err
	}
	if err := globals.LoadSharedFlags(); err != nil {
		return "", err
	}
	client := newClient(token, false)

	switch name {
	case "plan":
		return t.plan(client, owner, repo)
	case "audit":
		return t.audit(client, owner, repo)
	case "apply":
		return t.apply(client, owner, repo)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

// plan dry-runs the harden and returns the plan table. When the repo is not
// reachable as an existing repo, it falls back to the init dry-run plan so an
// agent can preview create+harden without a separate tool.
func (t *toolSet) plan(client *github.Client, owner, repo string) (string, error) {
	globals.DryRun = true
	globals.Preset = globals.PresetOSS
	var out strings.Builder
	err := apply.RunWithClientForMCP(client, owner, repo, &out)
	if err == nil {
		return out.String(), nil
	}
	out.Reset()
	_, _ = fmt.Fprintf(&out, "repo %s/%s not reachable (%v) — init dry-run plan (private):\n\n", owner, repo, err)
	if perr := apply.PlanNewRepo(owner, repo, "private", &out); perr != nil {
		return "", err // surface the original plan error, not the fallback's
	}
	return out.String(), nil
}

// audit runs the read-only baseline check and returns the report.
func (t *toolSet) audit(client *github.Client, owner, repo string) (string, error) {
	globals.DryRun = false
	globals.Preset = globals.PresetOSS
	var out strings.Builder
	if err := audit.RunWithClientForMCP(client, owner, repo, &out); err != nil {
		return "", err
	}
	return out.String(), nil
}

// apply runs the mutating harden. Solo defaults + prompts off so an agent
// run never blocks on stdin.
func (t *toolSet) apply(client *github.Client, owner, repo string) (string, error) {
	globals.DryRun = false
	globals.Preset = globals.PresetOSS
	var out strings.Builder
	if err := apply.RunWithClientForMCP(client, owner, repo, &out); err != nil {
		return "", err
	}
	return out.String(), nil
}
