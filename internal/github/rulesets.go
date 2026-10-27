// Package github provides a GitHub API client with authenticated HTTP operations.
package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
// default for the Go CI pack. They match job names shipped in public_ci.yml
// / private_ci.yml that report on pull requests — not third-party apps
// (Codacy, Socket) that may be missing and deadlock merges.
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

// GenericStatusChecks are the required contexts for the generic CI pack
// (generic_ci.yml). Keep this in sync with that workflow's job names.
var GenericStatusChecks = []string{
	"CI",
	"Trivy",
}

// LanguageStatusChecks are the required contexts for node/python/rust packs.
var LanguageStatusChecks = []string{
	"Lint",
	"Test",
	"Trivy",
}

// GitHubActionsAppID is the integration_id of the GitHub Actions app — the
// app that posts the check runs produced by a repository's own workflows.
// Pinning a required status check to this app means only a GitHub Actions
// check run with that exact context satisfies it; a third-party app posting
// an identically-named check cannot. It is a var (not a const) so operators
// embedding fundamentum can override it if GitHub ever changes the id.
var GitHubActionsAppID = 15368

// RepositoryRoleAdminID is the bypass-actor actor_id for the repository
// "admin" role in a ruleset (actor_type "RepositoryRole"). The tag ruleset's
// creation rule restricts tag creation to bypass actors; granting the admin
// role lets repository admins still cut version tags while everyone else is
// blocked. It is a var so operators can override it if their GitHub
// deployment reports a different role id.
var RepositoryRoleAdminID = 5

// shippedCheckContexts is the set of status-check contexts produced by
// Fundamentum's own shipped CI workflows (DefaultStatusChecks for the Go
// pack, GenericStatusChecks for generic, LanguageStatusChecks for
// node/python/rust). Only these are eligible for an integration_id pin —
// checks from any other app are never pinned.
func shippedCheckContexts() map[string]bool {
	set := make(map[string]bool, len(DefaultStatusChecks)+len(GenericStatusChecks)+len(LanguageStatusChecks))
	for _, name := range DefaultStatusChecks {
		set[name] = true
	}
	for _, name := range GenericStatusChecks {
		set[name] = true
	}
	for _, name := range LanguageStatusChecks {
		set[name] = true
	}
	return set
}

// newStatusCheck builds one required_status_checks entry for a check being
// ADDED (not already present live). When the context is one Fundamentum's
// shipped workflows produce, it is pinned to the GitHub Actions app so only
// that app can satisfy it; any other (third-party) context is left unpinned.
func newStatusCheck(context string) map[string]any {
	if shippedCheckContexts()[context] {
		return map[string]any{"context": context, "integration_id": GitHubActionsAppID}
	}
	return map[string]any{"context": context}
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
	// StrictChecks is the intended strict_required_status_checks_policy
	// ("branches must be up to date before merging"). Default true; a create
	// always uses it.
	StrictChecks bool
	// StrictChecksSet records that StrictChecks was chosen explicitly (flag
	// or baseline). A reconcile enforces an explicitly-chosen value; when
	// unset it keeps the ruleset's live policy instead of forcing it back.
	StrictChecksSet bool
	// BotManaged enables the bot-managed profile for the branch ruleset:
	// solo-style PR params, squash-only allowed_merge_methods, shipped checks
	// pinned to the GitHub Actions app, optional policy checks, and an
	// explicit empty bypass_actors list.
	BotManaged bool
	// PolicyChecks are external policy checks required on protect-main in
	// bot-managed mode. Each must carry the integration (app) id that posts
	// it — a policy check without an app id is refused.
	PolicyChecks []PolicyCheck
}

// PolicyCheck is one external policy status check: a context plus the
// integration (app) id that must post it.
type PolicyCheck struct {
	Context       string `json:"context"`
	IntegrationID int64  `json:"integration_id"`
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
	// BypassActors are the actors allowed to bypass the ruleset. GitHub only
	// returns this when the caller has write access to the ruleset, so it may
	// be nil on read.
	BypassActors []BypassActor `json:"bypass_actors"`
}

// BypassActor is one ruleset bypass actor.
type BypassActor struct {
	ActorID    *int64 `json:"actor_id,omitempty"`
	ActorType  string `json:"actor_type"`
	BypassMode string `json:"bypass_mode,omitempty"`
}

// RulesetPlan is the apply/audit view of a managed ruleset.
type RulesetPlan struct {
	Exists bool
	ID     int64
	Drift  []string
	// Solo is inferred from the existing pull_request rule so reconcile
	// restores enforcement/checks without flipping solo ↔ team.
	Solo bool
	// Live is the fetched ruleset on Exists; reconcile merges into it.
	Live *Ruleset
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
	path := repoPath(owner, repo) + "/rulesets?per_page=100"
	for path != "" {
		resp, err := c.get(path)
		if err != nil {
			return 0, false, fmt.Errorf("list rulesets: %w", err)
		}
		if err := expectStatus("list rulesets", resp, http.StatusOK, http.StatusNotFound); err != nil {
			_ = resp.Body.Close()
			return 0, false, err
		}
		if resp.StatusCode == http.StatusNotFound {
			_ = resp.Body.Close()
			return 0, false, nil
		}
		var rulesets []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		err = json.NewDecoder(resp.Body).Decode(&rulesets)
		next := nextLink(resp.Header.Get("Link"))
		_ = resp.Body.Close()
		if err != nil {
			return 0, false, fmt.Errorf("decode rulesets: %w", err)
		}
		for _, r := range rulesets {
			if r.Name == name {
				return r.ID, true, nil
			}
		}
		path = next
	}
	return 0, false, nil
}

// nextLink returns the request path (including query) from a GitHub Link
// header's rel="next", or empty. Full URLs are reduced to path+query so
// they stay on the client's base URL.
func nextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start < 0 || end <= start {
			continue
		}
		raw := part[start+1 : end]
		if u, err := url.Parse(raw); err == nil && u.Path != "" {
			if u.RawQuery != "" {
				return u.Path + "?" + u.RawQuery
			}
			return u.Path
		}
		return raw
	}
	return ""
}

// PlanBranchRuleset reports whether protect-main exists and which owned fields
// drifted. Drift only lists fields where the live ruleset is WEAKER than the
// intended baseline — a stricter hand-tuned ruleset passes unchanged.
func (c *Client) PlanBranchRuleset(owner, repo string, statusChecks []string, opts BranchProtectionOptions) (RulesetPlan, error) {
	got, err := c.GetRuleset(owner, repo, "protect-main")
	if err != nil || got == nil {
		return RulesetPlan{}, err
	}
	if !opts.SkipCodeOwners {
		opts.Solo = inferSolo(got)
	}
	return RulesetPlan{Exists: true, ID: got.ID, Drift: BranchRulesetDrift(got, statusChecks, opts), Solo: opts.Solo, Live: got}, nil
}

// PlanTagRuleset reports whether protect-version-tags exists and which owned fields drifted.
func (c *Client) PlanTagRuleset(owner, repo string) (RulesetPlan, error) {
	got, err := c.GetRuleset(owner, repo, "protect-version-tags")
	if err != nil || got == nil {
		return RulesetPlan{}, err
	}
	return RulesetPlan{Exists: true, ID: got.ID, Drift: TagRulesetDrift(got), Live: got}, nil
}

// EnsureBranchRuleset creates protect-main, or reconciles it when owned fields
// have drifted. Reconcile MERGES: required checks already present keep their
// integration_id, extra checks are kept, unowned rules survive, and stricter
// live settings are never weakened.
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
	return c.updateRuleset(owner, repo, plan.ID, mergeBranchRulesetBody(plan.Live, statusChecks, opts), "update branch ruleset")
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
	return c.updateRuleset(owner, repo, plan.ID, mergeTagRulesetBody(plan.Live), "update tag ruleset")
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

// branchRulesetBody is the create-path body: owned rules from scratch at the
// intended (not weaker-than-live) settings.
func branchRulesetBody(statusChecks []string, opts BranchProtectionOptions) map[string]any {
	return baseBranchRulesetBody(statusChecks, opts)
}

// mergeBranchRulesetBody is the reconcile-path body: it starts from the live
// ruleset, keeps unowned rules and extra checks (with their integration_id),
// adds any intended check that is missing, and raises any weaker owned field
// to the intended value. Stricter live values are preserved.
func mergeBranchRulesetBody(live *Ruleset, statusChecks []string, opts BranchProtectionOptions) map[string]any {
	if live == nil {
		// Nothing to merge — fall back to a plain create body.
		return branchRulesetBody(statusChecks, opts)
	}
	pr := strictestPRRule(ruleByType(live, "pull_request"), opts)
	body := baseBranchRulesetBody(statusChecks, opts)
	body["rules"] = mergedBranchRules(live, statusChecks, opts, pr)
	body["conditions"] = mergedConditions(live.Conditions, "~DEFAULT_BRANCH")
	return body
}

// RemovedBypassActors returns the live bypass actors a bot-managed reconcile
// would drop (it sends an explicit empty list). Returns nil when the profile
// is off or there is nothing to remove, so callers can print exactly what
// will disappear.
func RemovedBypassActors(live *Ruleset, opts BranchProtectionOptions) []BypassActor {
	if !opts.BotManaged || live == nil || len(live.BypassActors) == 0 {
		return nil
	}
	return append([]BypassActor{}, live.BypassActors...)
}

// DescribeBypassActor renders one bypass actor for the removed-actors notice,
// e.g. "Team#12345 (always)" or "OrganizationAdmin (pull_request)".
func DescribeBypassActor(a BypassActor) string {
	s := a.ActorType
	if a.ActorID != nil {
		s += fmt.Sprintf("#%d", *a.ActorID)
	}
	if a.BypassMode != "" {
		s += " (" + a.BypassMode + ")"
	}
	return s
}

// mergeTagRulesetBody is the reconcile-path tag body: keep unowned rules and
// extra condition entries; ensure the owned ones exist.
func mergeTagRulesetBody(live *Ruleset) map[string]any {
	if live == nil {
		return tagRulesetBody()
	}
	return map[string]any{
		"name":          "protect-version-tags",
		"target":        "tag",
		"enforcement":   "active",
		"conditions":    mergedConditions(live.Conditions, "refs/tags/v*"),
		"rules":         mergeOwnedRules(live, tagRulesetRules()),
		"bypass_actors": mergedTagBypassActors(live),
	}
}

// mergeOwnedRules returns live's rules with owned types replaced by the
// intended ones (present in intended). Unowned rule types survive untouched.
func mergeOwnedRules(live *Ruleset, owned []map[string]any) []map[string]any {
	ownedTypes := map[string]bool{}
	for _, rule := range owned {
		if typ, ok := rule["type"].(string); ok {
			ownedTypes[typ] = true
		}
	}
	merged := make([]map[string]any, 0, len(live.Rules)+len(owned))
	for _, rule := range live.Rules {
		if typ, ok := rule["type"].(string); ok && ownedTypes[typ] {
			continue
		}
		merged = append(merged, rule)
	}
	return append(merged, owned...)
}

// mergedConditions keeps every live ref_name entry and adds want to include
// when missing. Other condition keys are preserved as-is. The returned
// conditions are a copy — the live ruleset must not be mutated.
func mergedConditions(live map[string]any, want string) map[string]any {
	merged := map[string]any{}
	for k, v := range live {
		merged[k] = v
	}
	if includesRef(live, want) {
		return merged
	}
	ref := map[string]any{}
	for k, v := range anyMap(live["ref_name"]) {
		ref[k] = v
	}
	ref["include"] = append(refIncludeStrings(ref), want)
	merged["ref_name"] = ref
	return merged
}

func anyMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func refIncludeStrings(ref map[string]any) []string {
	var out []string
	for _, item := range anySlice(ref["include"]) {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// baseBranchRulesetBody is the shared branch body shape; the rules are
// replaced by create vs merge paths before use. The bot-managed profile also
// sends an explicit empty bypass_actors list; with the profile off the body
// carries no bypass_actors key at all, byte-identical to the default.
func baseBranchRulesetBody(statusChecks []string, opts BranchProtectionOptions) map[string]any {
	body := map[string]any{
		"name":        "protect-main",
		"target":      "branch",
		"enforcement": "active",
		"conditions": map[string]any{
			"ref_name": map[string]any{
				"include": []string{"~DEFAULT_BRANCH"},
				"exclude": []string{},
			},
		},
		"rules": branchRulesetRules(statusChecks, opts, prRuleParams(opts)),
	}
	if opts.BotManaged {
		body["bypass_actors"] = []any{}
	}
	return body
}

func branchRulesetRules(statusChecks []string, opts BranchProtectionOptions, prParams map[string]any) []map[string]any {
	rules := []map[string]any{
		{"type": "deletion"},
		{"type": "non_fast_forward"},
		{"type": "pull_request", "parameters": prParams},
	}
	if checks := statusChecksRule(statusChecks, opts); checks != nil {
		rules = append(rules, checks)
	}
	return rules
}

// prRuleParams is the intended pull_request rule for the create path.
// Bot-managed forces the solo shape (no code-owner review, no stale-review
// dismissal — automation cannot supply either) and locks merges to squash-only.
func prRuleParams(opts BranchProtectionOptions) map[string]any {
	params := map[string]any{
		"required_approving_review_count":   0,
		"dismiss_stale_reviews_on_push":     !opts.Solo,
		"require_code_owner_review":         opts.requireCodeOwners(),
		"require_last_push_approval":        false,
		"required_review_thread_resolution": true,
	}
	if opts.BotManaged {
		params["dismiss_stale_reviews_on_push"] = false
		params["require_code_owner_review"] = false
		params["allowed_merge_methods"] = []string{"squash"}
	}
	return params
}

// strictestPRRule merges a live pull_request rule with the intended one,
// keeping the stricter value for every owned parameter. A live rule that is
// already stricter than the baseline survives a reconcile unchanged.
//
// Fidelity rule: parameters the merge does NOT own — hand-tuned keys like
// allowed_merge_methods, require_extra_approval_for_unattributed_changes,
// or required_reviewers — are passed through untouched, exactly like an
// existing required check keeps its integration_id. Rebuilding the params
// map from the baseline five alone silently strips a hand-tuned repo's
// merge posture (found on fabrica: allowed_merge_methods ["squash"] and
// extra-approval were dropped by an earlier version).
func strictestPRRule(live map[string]any, opts BranchProtectionOptions) map[string]any {
	intended := prRuleParams(opts)
	if live == nil {
		return intended
	}
	params, _ := live["parameters"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}

	// Copy every live parameter first — owned keys are re-merged below with
	// baseline-or-stricter semantics; everything else passes through.
	merged := make(map[string]any, len(params)+len(intended))
	for k, v := range params {
		merged[k] = v
	}
	for k, v := range intended {
		// Owned booleans: stricter of live and intent. (The copy already
		// holds the live value; OR in the intent.)
		if b, isBool := v.(bool); isBool {
			if b || mapBool(params, k) {
				merged[k] = true
			} else {
				merged[k] = false
			}
			continue
		}
		// Owned count: the live value already sits in the copy, and the
		// baseline count is 0, so the copy is the max — nothing to do.
		if k == "required_approving_review_count" {
			if _, present := merged[k]; !present {
				merged[k] = v
			}
			continue
		}
		merged[k] = v
	}
	return merged
}

// ValidatePolicyChecks enforces the bot-managed rule that every required
// policy check names the app that posts it. An unpinned policy check would be
// satisfied by ANY app posting that context — exactly the spoof the pin
// exists to prevent — so it is refused rather than silently required. Callers
// run this before building or diffing a ruleset body.
func ValidatePolicyChecks(checks []PolicyCheck) error {
	for _, pc := range checks {
		if pc.Context == "" {
			return fmt.Errorf("bot-managed policy check with integration_id %d has an empty context", pc.IntegrationID)
		}
		if pc.IntegrationID == 0 {
			return fmt.Errorf("bot-managed policy check %q has no integration_id: every required policy check must pin the app that posts it", pc.Context)
		}
	}
	return nil
}

// policyCheckEntries builds the required_status_checks entries for validated
// policy checks. Callers must run ValidatePolicyChecks first; entries here
// are pinned by construction.
func policyCheckEntries(checks []PolicyCheck) []map[string]any {
	out := make([]map[string]any, 0, len(checks))
	for _, pc := range checks {
		out = append(out, map[string]any{"context": pc.Context, "integration_id": pc.IntegrationID})
	}
	return out
}

// statusChecksRule builds the required_status_checks rule. On the create path
// it lists the intended checks; the merge path merges the live checks (with
// their integration_id) in statusChecksRuleMerged.
func statusChecksRule(statusChecks []string, opts BranchProtectionOptions) map[string]any {
	allChecks := intendedChecks(statusChecks, opts)
	policy := policyCheckEntries(opts.PolicyChecks)
	if len(allChecks) == 0 && len(policy) == 0 {
		return nil
	}
	checks := make([]map[string]any, 0, len(allChecks)+len(policy))
	for _, name := range allChecks {
		checks = append(checks, newStatusCheck(name))
	}
	checks = append(checks, policy...)
	return statusChecksRuleWith(checks, intendedStrictChecks(opts))
}

// intendedStrictChecks is the up-to-date policy for a fresh ruleset: true
// (the shipped baseline) unless the operator explicitly chose otherwise.
func intendedStrictChecks(opts BranchProtectionOptions) bool {
	if opts.StrictChecksSet {
		return opts.StrictChecks
	}
	return true
}

// statusChecksRuleMerged unions the live checks (kept verbatim, preserving
// their integration_id) with any intended check that is missing, and applies
// the strict-policy intent: explicitly set → enforced; unset → keep live.
func statusChecksRuleMerged(liveRule map[string]any, statusChecks []string, opts BranchProtectionOptions) map[string]any {
	params, _ := liveRule["parameters"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}
	liveChecks := anySlice(params["required_status_checks"])
	if liveChecks == nil {
		liveChecks = []any{}
	}
	have := map[string]bool{}
	merged := make([]any, 0, len(liveChecks)+len(statusChecks))
	for _, item := range liveChecks {
		if m, ok := item.(map[string]any); ok {
			if ctx, ok := m["context"].(string); ok {
				have[ctx] = true
				// Keep the entry verbatim — integration_id included, so only
				// the app that posted the check can satisfy it.
				merged = append(merged, m)
			}
		}
	}
	for _, name := range intendedChecks(statusChecks, opts) {
		if !have[name] {
			merged = append(merged, newStatusCheck(name))
		}
	}
	// Bot-managed policy checks: present-and-pinned is required. A policy
	// check already live is kept verbatim; a missing one is added pinned.
	for _, entry := range policyCheckEntries(opts.PolicyChecks) {
		ctx := entry["context"].(string)
		if !have[ctx] {
			merged = append(merged, entry)
		}
	}
	strict := mapBool(params, "strict_required_status_checks_policy")
	if opts.StrictChecksSet {
		strict = opts.StrictChecks
	}
	rule := statusChecksRuleWith(nil, strict)
	rule["parameters"].(map[string]any)["required_status_checks"] = merged
	return rule
}

func statusChecksRuleWith(checks []map[string]any, strict bool) map[string]any {
	params := map[string]any{
		"strict_required_status_checks_policy": strict,
		"do_not_enforce_on_create":             false,
	}
	if checks != nil {
		params["required_status_checks"] = checks
	}
	return map[string]any{
		"type":       "required_status_checks",
		"parameters": params,
	}
}

// mergeBranchRulesetBody needs the merged checks rule when the live ruleset
// has one, so mergeOwnedRules is given the merged variant. This is assembled
// in mergeBranchRulesetBody via mergedBranchRules.
func mergedBranchRules(live *Ruleset, statusChecks []string, opts BranchProtectionOptions, pr map[string]any) []map[string]any {
	owned := []map[string]any{
		{"type": "deletion"},
		{"type": "non_fast_forward"},
		{"type": "pull_request", "parameters": pr},
	}
	liveChecksRule := ruleByType(live, "required_status_checks")
	if liveChecksRule != nil {
		owned = append(owned, statusChecksRuleMerged(liveChecksRule, statusChecks, opts))
	} else if rule := statusChecksRule(statusChecks, opts); rule != nil {
		owned = append(owned, rule)
	}
	return mergeOwnedRules(live, owned)
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
		"rules":         tagRulesetRules(),
		"bypass_actors": tagRulesetBypassActors(),
	}
}

// tagRulesetRules are the owned tag rules. The creation rule restricts tag
// creation to bypass actors (the admin role below), so version tags can only
// be cut by a repository admin — a leaked write token can no longer push a
// malicious v* tag that a release workflow would then build and publish.
func tagRulesetRules() []map[string]any {
	return []map[string]any{
		{"type": "creation"},
		{"type": "deletion"},
		{"type": "non_fast_forward"},
	}
}

// tagRulesetBypassActors grants the repository admin role bypass so admins
// can still create version tags under the creation rule.
func tagRulesetBypassActors() []map[string]any {
	id := int64(RepositoryRoleAdminID)
	return []map[string]any{
		{"actor_id": id, "actor_type": "RepositoryRole", "bypass_mode": "always"},
	}
}

// hasTagAdminBypass reports whether the live ruleset already grants bypass to
// the repository admin role, so a reconcile does not add a duplicate.
func hasTagAdminBypass(live *Ruleset) bool {
	for _, a := range live.BypassActors {
		if a.ActorType == "RepositoryRole" && a.ActorID != nil && *a.ActorID == int64(RepositoryRoleAdminID) {
			return true
		}
	}
	return false
}

// mergedTagBypassActors preserves every existing bypass actor and adds the
// admin role when it is not already present. Normal reconciles never remove
// an actor — only the bot-managed profile sends an explicit empty list.
func mergedTagBypassActors(live *Ruleset) []map[string]any {
	merged := make([]map[string]any, 0, len(live.BypassActors)+1)
	for _, a := range live.BypassActors {
		entry := map[string]any{"actor_type": a.ActorType}
		if a.ActorID != nil {
			entry["actor_id"] = *a.ActorID
		}
		if a.BypassMode != "" {
			entry["bypass_mode"] = a.BypassMode
		}
		merged = append(merged, entry)
	}
	if !hasTagAdminBypass(live) {
		merged = append(merged, tagRulesetBypassActors()...)
	}
	return merged
}

// BranchRulesetDrift returns Fundamentum-owned fields where the live ruleset
// is WEAKER than the intended baseline. Stricter-than-intent settings are
// deliberate hardening by the operator and never count as drift; a reconcile
// must not weaken them.
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
	if pr := ruleByType(got, "pull_request"); prRuleWeaker(pr, opts) {
		drift = append(drift, "pull_request")
	}
	if !opts.SkipStatusChecks {
		if !hasAllChecks(got, intendedChecks(statusChecks, opts)) {
			drift = append(drift, "required_status_checks")
		}
		if strictChecksWeaker(got, opts) {
			drift = append(drift, "up-to-date policy")
		}
	}
	if opts.BotManaged {
		drift = append(drift, botManagedDrift(got, opts)...)
	}
	return drift
}

// botManagedDrift reports the bot-managed-only weaknesses: merges not locked
// to squash-only, a live bypass actor (the profile grants no standing
// bypass), or a required policy check that is missing or not pinned to its
// app.
func botManagedDrift(got *Ruleset, opts BranchProtectionOptions) []string {
	var drift []string
	if !squashOnlyMerges(got) {
		drift = append(drift, "squash-only merges")
	}
	if len(got.BypassActors) > 0 {
		drift = append(drift, "bypass actors present")
	}
	if missing := unpinnedOrMissingPolicyChecks(got, opts.PolicyChecks); len(missing) > 0 {
		drift = append(drift, "policy checks: "+missing)
	}
	return drift
}

// squashOnlyMerges reports whether the live pull_request rule restricts
// merges to squash only.
func squashOnlyMerges(got *Ruleset) bool {
	pr := ruleByType(got, "pull_request")
	if pr == nil {
		return false
	}
	params, _ := pr["parameters"].(map[string]any)
	mm := anySlice(params["allowed_merge_methods"])
	if len(mm) != 1 {
		return false
	}
	s, ok := mm[0].(string)
	return ok && s == "squash"
}

// unpinnedOrMissingPolicyChecks returns a short description when a required
// policy check is absent or not pinned to its app; empty means all good.
func unpinnedOrMissingPolicyChecks(got *Ruleset, want []PolicyCheck) string {
	if len(want) == 0 {
		return ""
	}
	rule := ruleByType(got, "required_status_checks")
	live := map[string]int64{}
	if rule != nil {
		params, _ := rule["parameters"].(map[string]any)
		for _, item := range anySlice(params["required_status_checks"]) {
			m, _ := item.(map[string]any)
			ctx, _ := m["context"].(string)
			live[ctx] = int64(mapInt(m, "integration_id"))
		}
	}
	var bad []string
	for _, pc := range want {
		id, present := live[pc.Context]
		if !present || id != pc.IntegrationID {
			bad = append(bad, pc.Context)
		}
	}
	if len(bad) == 0 {
		return ""
	}
	return strings.Join(bad, ", ")
}

// prRuleWeaker reports whether the live pull_request rule is weaker than the
// intended baseline: thread resolution off, code-owner review missing when
// required, or stale-review dismissal missing for team mode.
func prRuleWeaker(rule map[string]any, opts BranchProtectionOptions) bool {
	if rule == nil {
		return true
	}
	params, _ := rule["parameters"].(map[string]any)
	if !mapBool(params, "required_review_thread_resolution") {
		return true
	}
	if opts.requireCodeOwners() && !mapBool(params, "require_code_owner_review") {
		return true
	}
	if !opts.Solo && !mapBool(params, "dismiss_stale_reviews_on_push") {
		return true
	}
	return false
}

// strictChecksWeaker reports whether the up-to-date policy is below the
// explicitly-intended one. An unset intent (keep-live) never drifts.
func strictChecksWeaker(got *Ruleset, opts BranchProtectionOptions) bool {
	if !opts.StrictChecksSet || !opts.StrictChecks {
		return false
	}
	rule := ruleByType(got, "required_status_checks")
	if rule == nil {
		return true
	}
	params, _ := rule["parameters"].(map[string]any)
	return !mapBool(params, "strict_required_status_checks_policy")
}

// TagRulesetDrift returns owned tag-ruleset fields where the live ruleset is
// weaker than the baseline.
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
	if !hasRuleType(got, "creation") {
		drift = append(drift, "creation")
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
// Go CI defaults; an empty list requires no status checks.
func ResolveRequiredChecks(in []string) []string {
	return ResolveRequiredChecksForPack(in, "go")
}

// ResolveRequiredChecksForPack is ResolveRequiredChecks with pack-aware
// defaults. "generic" requires the generic_ci.yml jobs; "none" requires
// nothing. Explicit --require-checks still wins for every pack.
func ResolveRequiredChecksForPack(in []string, pack string) []string {
	if in == nil {
		return DefaultChecksForPack(pack)
	}
	return dedup(trimNonEmpty(in))
}

// DefaultChecksForPack returns the shipped required-check list for pack.
func DefaultChecksForPack(pack string) []string {
	switch pack {
	case "generic":
		return append([]string{}, GenericStatusChecks...)
	case "node", "python", "rust":
		return append([]string{}, LanguageStatusChecks...)
	case "none":
		// Empty, not nil: intendedChecks treats nil as the Go defaults.
		return []string{}
	default:
		return append([]string{}, DefaultStatusChecks...)
	}
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

// inferSolo reports whether the live pull_request rule reads as a solo repo.
// The CODEOWNERS-review flag is THE marker: a solo maintainer must have it
// off (they cannot approve their own PRs), so off means solo regardless of
// the stale-dismissal hygiene flag — a solo maintainer may legitimately
// keep dismiss_stale on. Forcing code-owner review back on for such a
// ruleset would deadlock the maintainer's own PRs.
func inferSolo(got *Ruleset) bool {
	pr := ruleByType(got, "pull_request")
	if pr == nil {
		return false
	}
	params, _ := pr["parameters"].(map[string]any)
	return !mapBool(params, "require_code_owner_review")
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

// mapInt reads an integer parameter that JSON may decode as float64.
func mapInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}
