package github

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// PRTFinding is one pull_request_target workflow with a risky shape. Fail marks
// a pwn-request: the workflow checks out the untrusted PR head.
type PRTFinding struct {
	Path   string
	Fail   bool
	Detail string
}

// prHeadExprs name the untrusted PR head in a checkout's ref or repository.
var prHeadExprs = []string{"github.event.pull_request.head.", "github.head_ref"}

var secretRefPattern = regexp.MustCompile(`secrets\.([A-Za-z0-9_]+)`)

// ScanPullRequestTargetWorkflows reports risky pull_request_target workflows on
// the default audit path. It applies only the pull_request_target rules, so
// routine workflows with ordinary write scopes never produce findings here.
func (c *Client) ScanPullRequestTargetWorkflows(owner, repo string) ([]PRTFinding, error) {
	var findings []PRTFinding
	err := c.eachWorkflow(owner, repo, func(path, content string) {
		findings = append(findings, AnalyzePullRequestTarget(path, content)...)
	})
	if err != nil {
		return nil, err
	}
	return findings, nil
}

// AnalyzePullRequestTarget reports at most one combined finding per workflow
// whose trigger is pull_request_target. A checkout of the PR head is a FAIL;
// a write scope, a secret other than GITHUB_TOKEN, or a third-party action is a
// WARN. Workflows on other triggers and benign privileged-trigger workflows
// yield nothing. Unparseable YAML is a WARN, never a silent pass.
func AnalyzePullRequestTarget(path, content string) []PRTFinding {
	var workflow workflowAudit
	if err := yaml.Unmarshal([]byte(content), &workflow); err != nil {
		return []PRTFinding{{Path: path, Detail: "could not parse workflow YAML, pull_request_target status unknown: " + err.Error()}}
	}
	if !slices.Contains(workflowTriggers(workflow.On), "pull_request_target") {
		return nil
	}

	var headRef string
	writes := map[string]bool{}
	secrets := map[string]bool{}
	thirdParty := map[string]bool{}
	for _, name := range slices.Sorted(maps.Keys(workflow.Jobs)) {
		job := workflow.Jobs[name]
		for _, scope := range writeScopes(workflow.jobPermissions(job)) {
			writes[scope] = true
		}
		collectSecrets(secrets, job.Env)
		if job.Uses != "" && !trustedActionRef(job.Uses) {
			thirdParty[job.Uses] = true
		}
		for _, step := range job.Steps {
			collectSecrets(secrets, step.Env)
			collectSecrets(secrets, step.With)
			if !trustedActionRef(step.Uses) {
				thirdParty[step.Uses] = true
			}
			if headRef == "" && strings.HasPrefix(step.Uses, "actions/checkout") {
				headRef = prHeadCheckout(step.With)
			}
		}
	}

	var parts []string
	if headRef != "" {
		parts = append(parts, "checks out PR head ("+headRef+")")
	}
	if len(writes) > 0 {
		parts = append(parts, "write scope ("+strings.Join(slices.Sorted(maps.Keys(writes)), ", ")+")")
	}
	if len(secrets) > 0 {
		parts = append(parts, "secret "+strings.Join(slices.Sorted(maps.Keys(secrets)), ", "))
	}
	if len(thirdParty) > 0 {
		parts = append(parts, "third-party action "+strings.Join(slices.Sorted(maps.Keys(thirdParty)), ", "))
	}
	if len(parts) == 0 {
		return nil
	}
	return []PRTFinding{{Path: path, Fail: headRef != "", Detail: "pull_request_target: " + strings.Join(parts, "; ")}}
}

// prHeadCheckout returns the checkout ref or repository that names the PR head,
// or "" when the checkout targets the base repository.
func prHeadCheckout(with map[string]any) string {
	for _, key := range []string{"ref", "repository"} {
		value, _ := with[key].(string)
		for _, expr := range prHeadExprs {
			if strings.Contains(value, expr) {
				return value
			}
		}
	}
	return ""
}

// trustedActionRef reports whether a uses: reference is first-party (actions/*
// or github/*). An empty reference is trusted because it has nothing to run.
func trustedActionRef(uses string) bool {
	return uses == "" || strings.HasPrefix(uses, "actions/") || strings.HasPrefix(uses, "github/")
}

// collectSecrets records each secrets.NAME reference in value, except the
// automatic GITHUB_TOKEN, which every workflow receives.
func collectSecrets(into map[string]bool, value any) {
	for _, match := range secretRefPattern.FindAllStringSubmatch(fmt.Sprint(value), -1) {
		if match[1] != "GITHUB_TOKEN" {
			into["secrets."+match[1]] = true
		}
	}
}
