// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// dedup removes duplicate strings while preserving order.
func dedup(s []string) []string {
	seen := make(map[string]struct{}, len(s))
	out := make([]string, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

// DefaultStatusChecks are the status checks added to branch protection by
// default. They match job names shipped in the public and private CI
// templates that report on pull requests — not third-party apps (Codacy,
// Socket) that may be missing and deadlock merges.
var DefaultStatusChecks = []string{
	"Lint",
	"Vulnerability scan",
	"Build (ubuntu-latest)",
	"Build (windows-latest)",
	"Test (ubuntu-latest)",
	"Test (windows-latest)",
	"gosec",
	"Trivy",
}

// BranchProtectionOptions controls how strictly the branch ruleset is configured.
type BranchProtectionOptions struct {
	// Solo disables CODEOWNERS review requirement and stale review dismissal,
	// which would deadlock a solo maintainer who can't approve their own PRs.
	Solo bool
	// SkipCodeOwners disables CODEOWNERS review when the shipped CODEOWNERS
	// file cannot name a valid owner (organization without a team).
	SkipCodeOwners bool
	// SkipStatusChecks omits required status checks. Used in --pr / 409
	// fallback so the open harden PR is not blocked by checks that only
	// exist after that PR merges (Codacy, shipped CI jobs).
	SkipStatusChecks bool
}

func (o BranchProtectionOptions) requireCodeOwners() bool {
	return !o.Solo && !o.SkipCodeOwners
}

// Ruleset is the subset of a GitHub repository ruleset used to detect drift.
type Ruleset struct {
	ID          int64            `json:"id"`
	Name        string           `json:"name"`
	Target      string           `json:"target"`
	Enforcement string           `json:"enforcement"`
	Conditions  map[string]any   `json:"conditions"`
	Rules       []map[string]any `json:"rules"`
}

// RulesetPlan is the apply/audit view of a managed ruleset.
type RulesetPlan struct {
	Exists bool
	ID     int64
	Drift  []string
	// Solo is inferred from the existing pull_request rule so reconcile
	// restores enforcement/checks without flipping solo ↔ team.
	Solo bool
}

// RulesetExists returns true if a ruleset with the given name already exists.
// Only 404 counts as missing — auth, rate-limit, and server errors are returned.
func (c *Client) RulesetExists(owner, repo, name string) (bool, error) {
	_, found, err := c.rulesetID(owner, repo, name)
	return found, err
}

// GetRuleset returns the named repository ruleset, or nil if it is missing.
func (c *Client) GetRuleset(owner, repo, name string) (*Ruleset, error) {
	id, found, err := c.rulesetID(owner, repo, name)
	if err != nil || !found {
		return nil, err
	}
	if id == 0 {
		return nil, fmt.Errorf("get ruleset %s: listed without id", name)
	}
	resp, err := c.get(fmt.Sprintf("%s/rulesets/%d", repoPath(owner, repo), id))
	if err != nil {
		return nil, fmt.Errorf("get ruleset %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus("get ruleset "+name, resp, http.StatusOK, http.StatusNotFound); err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	var got Ruleset
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return nil, fmt.Errorf("decode ruleset %s: %w", name, err)
	}
	return &got, nil
}

func (c *Client) rulesetID(owner, repo, name string) (int64, bool, error) {
	resp, err := c.get(repoPath(owner, repo) + "/rulesets")
	if err != nil {
		return 0, false, fmt.Errorf("list rulesets: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus("list rulesets", resp, http.StatusOK, http.StatusNotFound); err != nil {
		return 0, false, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return 0, false, nil
	}
	var rulesets []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rulesets); err != nil {
		return 0, false, fmt.Errorf("decode rulesets: %w", err)
	}
	for _, r := range rulesets {
		if r.Name == name {
			return r.ID, true, nil
		}
	}
	return 0, false, nil
}

// PlanBranchRuleset reports whether protect-main exists and which owned fields drifted.
func (c *Client) PlanBranchRuleset(owner, repo string, statusChecks []string, opts BranchProtectionOptions) (RulesetPlan, error) {
	got, err := c.GetRuleset(owner, repo, "protect-main")
	if err != nil || got == nil {
		return RulesetPlan{}, err
	}
	if !opts.SkipCodeOwners {
		opts.Solo = inferSolo(got)
	}
	return RulesetPlan{Exists: true, ID: got.ID, Drift: BranchRulesetDrift(got, statusChecks, opts), Solo: opts.Solo}, nil
}

// PlanTagRuleset reports whether protect-version-tags exists and which owned fields drifted.
func (c *Client) PlanTagRuleset(owner, repo string) (RulesetPlan, error) {
	got, err := c.GetRuleset(owner, repo, "protect-version-tags")
	if err != nil || got == nil {
		return RulesetPlan{}, err
	}
	return RulesetPlan{Exists: true, ID: got.ID, Drift: TagRulesetDrift(got)}, nil
}

// EnsureBranchRuleset creates protect-main, or updates it when owned fields have drifted.
func (c *Client) EnsureBranchRuleset(owner, repo string, statusChecks []string, opts BranchProtectionOptions) error {
	plan, err := c.PlanBranchRuleset(owner, repo, statusChecks, opts)
	if err != nil {
		return err
	}
	if !plan.Exists {
		return c.CreateBranchRuleset(owner, repo, statusChecks, opts)
	}
	if len(plan.Drift) == 0 {
		return nil
	}
	if !opts.SkipCodeOwners {
		opts.Solo = plan.Solo
	}
	return c.updateRuleset(owner, repo, plan.ID, branchRulesetBody(statusChecks, opts), "update branch ruleset")
}

// EnsureTagRuleset creates protect-version-tags, or updates it when owned fields have drifted.
func (c *Client) EnsureTagRuleset(owner, repo string) error {
	plan, err := c.PlanTagRuleset(owner, repo)
	if err != nil {
		return err
	}
	if !plan.Exists {
		return c.CreateTagRuleset(owner, repo)
	}
	if len(plan.Drift) == 0 {
		return nil
	}
	return c.updateRuleset(owner, repo, plan.ID, tagRulesetBody(), "update tag ruleset")
}

// CreateBranchRuleset creates the protect-main branch ruleset.
// statusChecks replace DefaultStatusChecks when non-nil. Pass nil to use
// the shipped CI defaults; pass an empty slice to require no checks.
func (c *Client) CreateBranchRuleset(owner, repo string, statusChecks []string, opts BranchProtectionOptions) error {
	resp, err := c.post(repoPath(owner, repo)+"/rulesets", branchRulesetBody(statusChecks, opts))
	if err != nil {
		return fmt.Errorf("create branch ruleset: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return expectStatus("create branch ruleset", resp, http.StatusCreated)
}

// CreateTagRuleset creates the protect-version-tags tag ruleset.
func (c *Client) CreateTagRuleset(owner, repo string) error {
	resp, err := c.post(repoPath(owner, repo)+"/rulesets", tagRulesetBody())
	if err != nil {
		return fmt.Errorf("create tag ruleset: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return expectStatus("create tag ruleset", resp, http.StatusCreated)
}

func (c *Client) updateRuleset(owner, repo string, id int64, body map[string]any, action string) error {
	resp, err := c.do(http.MethodPut, fmt.Sprintf("%s/rulesets/%d", repoPath(owner, repo), id), body)
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return expectStatus(action, resp, http.StatusOK)
}

func branchRulesetBody(statusChecks []string, opts BranchProtectionOptions) map[string]any {
	allChecks := intendedChecks(statusChecks, opts)
	checks := make([]map[string]any, len(allChecks))
	for i, name := range allChecks {
		checks[i] = map[string]any{"context": name}
	}
	prParams := map[string]any{
		"required_approving_review_count":   0,
		"dismiss_stale_reviews_on_push":     !opts.Solo,
		"require_code_owner_review":         opts.requireCodeOwners(),
		"require_last_push_approval":        false,
		"required_review_thread_resolution": true,
	}
	rules := []map[string]any{
		{"type": "deletion"},
		{"type": "non_fast_forward"},
		{"type": "pull_request", "parameters": prParams},
	}
	if len(checks) > 0 {
		rules = append(rules, map[string]any{
			"type": "required_status_checks",
			"parameters": map[string]any{
				"strict_required_status_checks_policy": true,
				"do_not_enforce_on_create":             false,
				"required_status_checks":               checks,
			},
		})
	}
	return map[string]any{
		"name":        "protect-main",
		"target":      "branch",
		"enforcement": "active",
		"conditions": map[string]any{
			"ref_name": map[string]any{
				"include": []string{"~DEFAULT_BRANCH"},
				"exclude": []string{},
			},
		},
		"rules": rules,
	}
}

func tagRulesetBody() map[string]any {
	return map[string]any{
		"name":        "protect-version-tags",
		"target":      "tag",
		"enforcement": "active",
		"conditions": map[string]any{
			"ref_name": map[string]any{
				"include": []string{"refs/tags/v*"},
				"exclude": []string{},
			},
		},
		"rules": []map[string]any{
			{"type": "deletion"},
			{"type": "non_fast_forward"},
		},
	}
}

// BranchRulesetDrift returns Fundamentum-owned fields that differ from the intended baseline.
func BranchRulesetDrift(got *Ruleset, statusChecks []string, opts BranchProtectionOptions) []string {
	if got == nil {
		return []string{"missing"}
	}
	var drift []string
	if got.Enforcement != "active" {
		drift = append(drift, "enforcement")
	}
	if !includesRef(got.Conditions, "~DEFAULT_BRANCH") {
		drift = append(drift, "conditions")
	}
	if !hasRuleType(got, "deletion") {
		drift = append(drift, "deletion")
	}
	if !hasRuleType(got, "non_fast_forward") {
		drift = append(drift, "non_fast_forward")
	}
	if pr := ruleByType(got, "pull_request"); !matchingPRRule(pr, opts) {
		drift = append(drift, "pull_request")
	}
	if !opts.SkipStatusChecks && !hasAllChecks(got, intendedChecks(statusChecks, opts)) {
		drift = append(drift, "required_status_checks")
	}
	return drift
}

// TagRulesetDrift returns owned tag-ruleset fields that differ from the baseline.
func TagRulesetDrift(got *Ruleset) []string {
	if got == nil {
		return []string{"missing"}
	}
	var drift []string
	if got.Enforcement != "active" {
		drift = append(drift, "enforcement")
	}
	if !includesRef(got.Conditions, "refs/tags/v*") {
		drift = append(drift, "conditions")
	}
	if !hasRuleType(got, "deletion") {
		drift = append(drift, "deletion")
	}
	if !hasRuleType(got, "non_fast_forward") {
		drift = append(drift, "non_fast_forward")
	}
	return drift
}

// ResolveRequiredChecks normalizes --require-checks. nil means the shipped
// CI defaults; an empty list requires no status checks.
func ResolveRequiredChecks(in []string) []string {
	if in == nil {
		return append([]string{}, DefaultStatusChecks...)
	}
	return dedup(trimNonEmpty(in))
}

func trimNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, name := range in {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func intendedChecks(statusChecks []string, opts BranchProtectionOptions) []string {
	if opts.SkipStatusChecks {
		return nil
	}
	if statusChecks == nil {
		return append([]string{}, DefaultStatusChecks...)
	}
	return dedup(statusChecks)
}

func inferSolo(got *Ruleset) bool {
	pr := ruleByType(got, "pull_request")
	if pr == nil {
		return false
	}
	params, _ := pr["parameters"].(map[string]any)
	return !mapBool(params, "require_code_owner_review") && !mapBool(params, "dismiss_stale_reviews_on_push")
}

func matchingPRRule(rule map[string]any, opts BranchProtectionOptions) bool {
	if rule == nil {
		return false
	}
	params, _ := rule["parameters"].(map[string]any)
	return mapBool(params, "required_review_thread_resolution") &&
		mapBool(params, "require_code_owner_review") == opts.requireCodeOwners() &&
		mapBool(params, "dismiss_stale_reviews_on_push") == !opts.Solo
}

func hasAllChecks(got *Ruleset, want []string) bool {
	if len(want) == 0 {
		return true
	}
	rule := ruleByType(got, "required_status_checks")
	if rule == nil {
		return false
	}
	params, _ := rule["parameters"].(map[string]any)
	if !mapBool(params, "strict_required_status_checks_policy") {
		return false
	}
	have := map[string]struct{}{}
	for _, item := range anySlice(params["required_status_checks"]) {
		m, _ := item.(map[string]any)
		if ctx, ok := m["context"].(string); ok {
			have[ctx] = struct{}{}
		}
	}
	for _, name := range want {
		if _, ok := have[name]; !ok {
			return false
		}
	}
	return true
}

func hasRuleType(got *Ruleset, typ string) bool {
	return ruleByType(got, typ) != nil
}

func ruleByType(got *Ruleset, typ string) map[string]any {
	if got == nil {
		return nil
	}
	for _, rule := range got.Rules {
		if rule["type"] == typ {
			return rule
		}
	}
	return nil
}

func includesRef(conditions map[string]any, want string) bool {
	ref, _ := conditions["ref_name"].(map[string]any)
	for _, item := range anySlice(ref["include"]) {
		if s, ok := item.(string); ok && s == want {
			return true
		}
	}
	return false
}

func anySlice(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case []string:
		out := make([]any, len(t))
		for i, s := range t {
			out[i] = s
		}
		return out
	case []map[string]any:
		out := make([]any, len(t))
		for i, m := range t {
			out[i] = m
		}
		return out
	default:
		return nil
	}
}

func mapBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	b, _ := m[key].(bool)
	return b
}
