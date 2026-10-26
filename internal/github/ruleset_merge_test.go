package github

import (
	"encoding/json"
	"net/http"
	"testing"
)

// liveBranchRuleset is a protect-main shaped live ruleset with an extra
// unowned rule, an app-provided check carrying integration_id, and stricter
// review settings than the baseline. It is at-intent (no drift) apart from
// being stricter: ~DEFAULT_BRANCH is included and the required rules exist.
func liveBranchRuleset() *Ruleset {
	return &Ruleset{
		ID:          7,
		Name:        "protect-main",
		Target:      "branch",
		Enforcement: "active",
		Conditions: map[string]any{
			"ref_name": map[string]any{
				"include": []any{"~DEFAULT_BRANCH", "refs/heads/release/*"},
				"exclude": []any{},
			},
		},
		Rules: []map[string]any{
			{"type": "required_linear_history"},
			{"type": "deletion"},
			{"type": "non_fast_forward"},
			{"type": "pull_request", "parameters": map[string]any{
				"required_approving_review_count":   1.0,
				"dismiss_stale_reviews_on_push":     true,
				"require_code_owner_review":         true,
				"require_last_push_approval":        true,
				"required_review_thread_resolution": true,
			}},
			{"type": "required_status_checks", "parameters": map[string]any{
				"strict_required_status_checks_policy": true,
				"do_not_enforce_on_create":             false,
				"required_status_checks": []any{
					map[string]any{"context": "Codacy", "integration_id": 9999.0},
					map[string]any{"context": "Lint"},
				},
			}},
		},
	}
}

func TestMergeBranchRulesetBody_PreservesIntegrationID(t *testing.T) {
	body := mergeBranchRulesetBody(liveBranchRuleset(), []string{"Lint", "Trivy"}, BranchProtectionOptions{Solo: true})
	rule := ruleFromBody(body, "required_status_checks")
	params, _ := rule["parameters"].(map[string]any)
	checks := anySlice(params["required_status_checks"])

	foundCodacy := false
	for _, item := range checks {
		m, _ := item.(map[string]any)
		if m["context"] == "Codacy" {
			foundCodacy = true
			if _, ok := m["integration_id"]; !ok {
				t.Error("existing check lost its integration_id — any app could satisfy it")
			}
		}
	}
	if !foundCodacy {
		t.Error("existing Codacy check was dropped instead of merged")
	}
	for _, want := range []string{"Trivy"} {
		if !checkContextPresent(checks, want) {
			t.Errorf("missing intended check %q after merge", want)
		}
	}
}

func TestMergeBranchRulesetBody_KeepsUnownedRules(t *testing.T) {
	body := mergeBranchRulesetBody(liveBranchRuleset(), []string{"Lint"}, BranchProtectionOptions{Solo: true})
	rules, _ := body["rules"].([]map[string]any)
	found := false
	for _, rule := range rules {
		if rule["type"] == "required_linear_history" {
			found = true
		}
	}
	if !found {
		t.Error("unowned rule required_linear_history must survive a reconcile")
	}
}

func TestMergeBranchRulesetBody_NeverWeakensStricterSettings(t *testing.T) {
	body := mergeBranchRulesetBody(liveBranchRuleset(), []string{"Lint"}, BranchProtectionOptions{Solo: true})
	pr := ruleFromBody(body, "pull_request")
	params, _ := pr["parameters"].(map[string]any)
	if got := mapInt(params, "required_approving_review_count"); got != 1 {
		t.Errorf("approving count = %d, want live stricter value 1", got)
	}
	if !mapBool(params, "require_last_push_approval") {
		t.Error("live require_last_push_approval=true must not be weakened")
	}
	if !mapBool(params, "dismiss_stale_reviews_on_push") {
		t.Error("live dismiss_stale_reviews_on_push=true must not be weakened")
	}
	if !mapBool(params, "require_code_owner_review") {
		t.Error("live require_code_owner_review=true must not be weakened")
	}
}

func TestMergeBranchRulesetBody_MergesConditions(t *testing.T) {
	body := mergeBranchRulesetBody(liveBranchRuleset(), []string{"Lint"}, BranchProtectionOptions{Solo: true})
	conditions, _ := body["conditions"].(map[string]any)
	if !includesRef(conditions, "~DEFAULT_BRANCH") {
		t.Error("~DEFAULT_BRANCH must be ensured in the merged conditions")
	}
	if !includesRef(conditions, "refs/heads/release/*") {
		t.Error("live extra condition entries must survive the merge")
	}
}

func TestMergeBranchRulesetBody_StrictPolicyKeptWhenUnset(t *testing.T) {
	live := liveBranchRuleset()
	body := mergeBranchRulesetBody(live, []string{"Lint"}, BranchProtectionOptions{Solo: true, StrictChecks: false, StrictChecksSet: false})
	rule := ruleFromBody(body, "required_status_checks")
	p, _ := rule["parameters"].(map[string]any)
	if !mapBool(p, "strict_required_status_checks_policy") {
		t.Error("unset intent must keep the live strict policy (true here)")
	}

	// Explicit false enforces false even though live is true.
	body = mergeBranchRulesetBody(live, []string{"Lint"}, BranchProtectionOptions{Solo: true, StrictChecks: false, StrictChecksSet: true})
	rule = ruleFromBody(body, "required_status_checks")
	p, _ = rule["parameters"].(map[string]any)
	if mapBool(p, "strict_required_status_checks_policy") {
		t.Error("explicit --branch-up-to-date=false must be enforced")
	}
}

func TestMergeBranchRulesetBody_MissingChecksRuleAddsOne(t *testing.T) {
	live := &Ruleset{
		ID:          8,
		Name:        "protect-main",
		Target:      "branch",
		Enforcement: "active",
		Conditions: map[string]any{
			"ref_name": map[string]any{"include": []any{"~DEFAULT_BRANCH"}},
		},
		Rules: []map[string]any{
			{"type": "deletion"},
			{"type": "non_fast_forward"},
		},
	}
	body := mergeBranchRulesetBody(live, []string{"Lint"}, BranchProtectionOptions{Solo: true})
	if ruleFromBody(body, "required_status_checks") == nil {
		t.Error("a live ruleset without checks must gain the intended checks rule")
	}
}

func TestBranchRulesetDrift_StricterSettingsAreNotDrift(t *testing.T) {
	drift := BranchRulesetDrift(liveBranchRuleset(), []string{"Lint"}, BranchProtectionOptions{Solo: true})
	for _, d := range drift {
		if d == "pull_request" {
			t.Errorf("stricter live review settings must not be drift: %v", drift)
		}
		if d == "required_status_checks" && false {
			t.Errorf("superset of checks must not be drift: %v", drift)
		}
	}
	if len(drift) != 0 {
		t.Errorf("a stricter ruleset with all intended checks must not drift, got %v", drift)
	}
}

func TestBranchRulesetDrift_ExplicitStrictPolicyDrifts(t *testing.T) {
	live := liveBranchRuleset()
	// live strict policy is true; explicitly intending false is never drift
	// (never-weaken), but explicitly intending true on a false live is.
	rule := ruleByType(live, "required_status_checks")
	params, _ := rule["parameters"].(map[string]any)
	params["strict_required_status_checks_policy"] = false

	drift := BranchRulesetDrift(live, []string{"Lint"}, BranchProtectionOptions{Solo: true, StrictChecks: true, StrictChecksSet: true})
	if !containsStr(drift, "up-to-date policy") {
		t.Errorf("explicit strict=true with live false must drift, got %v", drift)
	}
	// Unset intent never drifts on this field.
	drift = BranchRulesetDrift(live, []string{"Lint"}, BranchProtectionOptions{Solo: true})
	if containsStr(drift, "up-to-date policy") {
		t.Errorf("unset intent must not drift on the policy, got %v", drift)
	}
}

func TestMergeTagRulesetBody_KeepsExtraRules(t *testing.T) {
	live := &Ruleset{
		ID:          9,
		Name:        "protect-version-tags",
		Target:      "tag",
		Enforcement: "active",
		Conditions: map[string]any{
			"ref_name": map[string]any{"include": []any{"refs/tags/v*"}},
		},
		Rules: []map[string]any{
			{"type": "deletion"},
			{"type": "non_fast_forward"},
			{"type": "required_deployments", "parameters": map[string]any{"required": 1.0}},
		},
	}
	body := mergeTagRulesetBody(live)
	rules, _ := body["rules"].([]map[string]any)
	found := false
	for _, rule := range rules {
		if rule["type"] == "required_deployments" {
			found = true
		}
	}
	if !found {
		t.Error("unowned tag rule required_deployments must survive")
	}
}

func TestEnsureBranchRuleset_MergesLiveChecks(t *testing.T) {
	var putBody map[string]any
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/rulesets":
			_, _ = w.Write([]byte(`[{"id":7,"name":"protect-main"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/rulesets/7":
			raw, _ := json.Marshal(liveBranchRuleset())
			_, _ = w.Write(raw)
		case r.Method == http.MethodPut && r.URL.Path == "/repos/owner/repo/rulesets/7":
			if err := json.NewDecoder(r.Body).Decode(&putBody); err != nil {
				t.Errorf("decode put body: %v", err)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	if err := c.EnsureBranchRuleset("owner", "repo", []string{"Lint", "Trivy"}, BranchProtectionOptions{Solo: true}); err != nil {
		t.Fatalf("EnsureBranchRuleset() error: %v", err)
	}
	if putBody == nil {
		t.Fatal("reconcile did not PUT")
	}
	rule := ruleFromBody(putBody, "required_status_checks")
	params, _ := rule["parameters"].(map[string]any)
	checks := anySlice(params["required_status_checks"])
	if !checkContextPresent(checks, "Codacy") || !checkContextPresent(checks, "Trivy") {
		t.Errorf("merged checks must union live and intended: %v", checks)
	}
	for _, item := range checks {
		m, _ := item.(map[string]any)
		if m["context"] == "Codacy" {
			if _, ok := m["integration_id"]; !ok {
				t.Error("integration_id dropped on reconcile")
			}
		}
	}
}

func TestEnsureBranchRuleset_StricterLiveSkipsUpdate(t *testing.T) {
	updated := false
	live := liveBranchRuleset()
	// The fixture's conditions lack ~DEFAULT_BRANCH — make them fully
	// compliant so the whole ruleset is at-intent (and stricter in reviews).
	live.Conditions = map[string]any{
		"ref_name": map[string]any{"include": []any{"~DEFAULT_BRANCH", "refs/heads/release/*"}},
	}
	raw, _ := json.Marshal(live)
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/rulesets":
			_, _ = w.Write([]byte(`[{"id":7,"name":"protect-main"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/rulesets/7":
			_, _ = w.Write(raw)
		case r.Method == http.MethodPut:
			updated = true
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	if err := c.EnsureBranchRuleset("owner", "repo", []string{"Lint", "Codacy"}, BranchProtectionOptions{Solo: true}); err != nil {
		t.Fatalf("EnsureBranchRuleset() error: %v", err)
	}
	if updated {
		t.Error("a stricter live ruleset must not be PUT — reconcile would only churn")
	}
}

// ruleFromBody finds a rule by type in a body whose "rules" may be a native
// slice (merge builders) or a JSON-decoded []any (HTTP round-trip).
func ruleFromBody(body map[string]any, typ string) map[string]any {
	for _, item := range anySlice(body["rules"]) {
		rule, _ := item.(map[string]any)
		if rule["type"] == typ {
			return rule
		}
	}
	return nil
}

func checkContextPresent(checks []any, context string) bool {
	for _, item := range checks {
		m, _ := item.(map[string]any)
		if m["context"] == context {
			return true
		}
	}
	return false
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestMergedConditions(t *testing.T) {
	// Live conditions with the intended ref already present: unchanged.
	live := map[string]any{
		"ref_name": map[string]any{"include": []any{"~DEFAULT_BRANCH"}, "exclude": []any{}},
	}
	got := mergedConditions(live, "~DEFAULT_BRANCH")
	if !includesRef(got, "~DEFAULT_BRANCH") {
		t.Error("existing include must survive")
	}
	if len(refIncludeStrings(got["ref_name"].(map[string]any))) != 1 {
		t.Errorf("no duplicate include entries: %v", got["ref_name"])
	}

	// Live conditions missing the intended ref: appended after the extras.
	live = map[string]any{
		"ref_name": map[string]any{"include": []any{"refs/heads/release/*", 42}},
	}
	got = mergedConditions(live, "~DEFAULT_BRANCH")
	if !includesRef(got, "~DEFAULT_BRANCH") || !includesRef(got, "refs/heads/release/*") {
		t.Errorf("merged include must keep extras and add the intent: %v", got["ref_name"])
	}

	// No ref_name at all: created fresh.
	got = mergedConditions(map[string]any{}, "~DEFAULT_BRANCH")
	if !includesRef(got, "~DEFAULT_BRANCH") {
		t.Errorf("fresh conditions must include the intent: %v", got)
	}
}

func TestMergeBranchRulesetBody_NilLiveFallsBack(t *testing.T) {
	// A nil live ruleset must fall back to the plain create body.
	body := mergeBranchRulesetBody(nil, []string{"Lint"}, BranchProtectionOptions{Solo: true})
	if ruleFromBody(body, "deletion") == nil || ruleFromBody(body, "pull_request") == nil {
		t.Errorf("nil live merge must produce the create body: %v", body)
	}
}

func TestStatusChecksRuleMerged_LiveRuleWithoutParams(t *testing.T) {
	// A live required_status_checks rule with no parameters map: merged rule
	// still gets the intended checks and the intended strict default.
	live := &Ruleset{
		Enforcement: "active",
		Conditions:  map[string]any{"ref_name": map[string]any{"include": []any{"~DEFAULT_BRANCH"}}},
		Rules: []map[string]any{
			{"type": "deletion"},
			{"type": "non_fast_forward"},
			{"type": "required_status_checks"},
		},
	}
	body := mergeBranchRulesetBody(live, []string{"Lint"}, BranchProtectionOptions{Solo: true})
	rule := ruleFromBody(body, "required_status_checks")
	if rule == nil {
		t.Fatal("merged body must have a checks rule")
	}
	params, _ := rule["parameters"].(map[string]any)
	checks := anySlice(params["required_status_checks"])
	if !checkContextPresent(checks, "Lint") {
		t.Errorf("intended checks missing: %v", checks)
	}
	if mapBool(params, "strict_required_status_checks_policy") {
		t.Error("unset intent keeps the live strict policy (false in a parameterless rule)")
	}
}

func TestStrictestPRRule_NilParams(t *testing.T) {
	// A live pull_request rule with no parameters: intended params survive.
	got := strictestPRRule(map[string]any{"type": "pull_request"}, BranchProtectionOptions{Solo: true})
	if !mapBool(got, "required_review_thread_resolution") {
		t.Errorf("nil live params must keep the intended thread resolution: %v", got)
	}
	if mapBool(got, "dismiss_stale_reviews_on_push") {
		t.Error("solo intent must keep dismissal off when live has nothing")
	}
}

func TestBranchRulesetRules_SkipStatusChecksOmitsRule(t *testing.T) {
	rules := branchRulesetRules(nil, BranchProtectionOptions{SkipStatusChecks: true, Solo: true}, prRuleParams(BranchProtectionOptions{Solo: true}))
	for _, rule := range rules {
		if rule["type"] == "required_status_checks" {
			t.Error("SkipStatusChecks must omit the required_status_checks rule")
		}
	}
}

func TestIntendedChecks_NoOverwriteDefaults(t *testing.T) {
	if got := intendedChecks(nil, BranchProtectionOptions{}); len(got) == 0 || got[0] != "Lint" {
		t.Errorf("nil checks must fall back to the shipped defaults: %v", got)
	}
	if got := intendedChecks(nil, BranchProtectionOptions{SkipStatusChecks: true}); got != nil {
		t.Errorf("SkipStatusChecks must nil out intended checks: %v", got)
	}
}

func TestMergeTagRulesetBody_NilLive(t *testing.T) {
	body := mergeTagRulesetBody(nil)
	if body["name"] != "protect-version-tags" {
		t.Errorf("nil live tag merge must produce the create body: %v", body)
	}
}

func TestPrRuleWeaker(t *testing.T) {
	team := BranchProtectionOptions{Solo: false}
	solo := BranchProtectionOptions{Solo: true}

	// Missing rule is always weaker.
	if !prRuleWeaker(nil, solo) {
		t.Error("nil rule is weaker")
	}

	// Thread resolution off: weaker for any mode.
	weak := map[string]any{"parameters": map[string]any{"required_review_thread_resolution": false}}
	if !prRuleWeaker(weak, solo) {
		t.Error("thread resolution off is weaker")
	}

	// Team intent, stale dismissal off: weaker.
	teamRule := map[string]any{"parameters": map[string]any{
		"required_review_thread_resolution": true,
		"dismiss_stale_reviews_on_push":     false,
	}}
	if !prRuleWeaker(teamRule, team) {
		t.Error("team intent with dismissal off is weaker")
	}
	// Solo intent tolerates dismissal off (solo mode disables it anyway).
	if prRuleWeaker(teamRule, solo) {
		t.Error("solo intent must not require stale dismissal")
	}

	// Code-owner review required but live off: weaker.
	owner := BranchProtectionOptions{SkipCodeOwners: false}
	rule := map[string]any{"parameters": map[string]any{
		"required_review_thread_resolution": true,
		"dismiss_stale_reviews_on_push":     true,
		"require_code_owner_review":         false,
	}}
	if !prRuleWeaker(rule, owner) {
		t.Error("missing code-owner review is weaker when required")
	}

	// Fully at-intent rule: not weaker.
	rule = map[string]any{"parameters": map[string]any{
		"required_review_thread_resolution": true,
		"dismiss_stale_reviews_on_push":     true,
		"require_code_owner_review":         true,
	}}
	if prRuleWeaker(rule, owner) {
		t.Error("at-intent rule is not weaker")
	}
}

func TestStrictChecksWeaker(t *testing.T) {
	atStrict := &Ruleset{Rules: []map[string]any{
		{"type": "required_status_checks", "parameters": map[string]any{
			"strict_required_status_checks_policy": true,
		}},
	}}
	offStrict := &Ruleset{Rules: []map[string]any{
		{"type": "required_status_checks", "parameters": map[string]any{
			"strict_required_status_checks_policy": false,
		}},
	}}

	// Unset intent never drifts.
	if strictChecksWeaker(offStrict, BranchProtectionOptions{StrictChecksSet: false}) {
		t.Error("unset intent must not drift")
	}
	// Explicit false never drifts (never weaken a stricter repo; and relax
	// intent only applies at enforcement, not drift).
	if strictChecksWeaker(atStrict, BranchProtectionOptions{StrictChecks: false, StrictChecksSet: true}) {
		t.Error("explicit false must not drift on a stricter live repo")
	}
	// Explicit true on a live false: drift.
	if !strictChecksWeaker(offStrict, BranchProtectionOptions{StrictChecks: true, StrictChecksSet: true}) {
		t.Error("explicit true with live false must drift")
	}
	// Explicit true but no checks rule at all: drift.
	if !strictChecksWeaker(&Ruleset{}, BranchProtectionOptions{StrictChecks: true, StrictChecksSet: true}) {
		t.Error("explicit true with no rule must drift")
	}
}

func TestInferSoloHybridRule(t *testing.T) {
	// A solo maintainer with stale-dismissal kept on: CODEOWNERS review off
	// is the solo marker. Inferring team here would make reconcile force
	// code-owner review on and deadlock the maintainer's own PRs.
	hybrid := &Ruleset{Rules: []map[string]any{
		{"type": "pull_request", "parameters": map[string]any{
			"require_code_owner_review":        false,
			"dismiss_stale_reviews_on_push":   true,
			"required_review_thread_resolution": true,
		}},
	}}
	if !inferSolo(hybrid) {
		t.Error("code-owner-off with dismiss-on must infer solo")
	}

	team := &Ruleset{Rules: []map[string]any{
		{"type": "pull_request", "parameters": map[string]any{
			"require_code_owner_review":        true,
			"dismiss_stale_reviews_on_push":     true,
		}},
	}}
	if inferSolo(team) {
		t.Error("code-owner-on must infer team")
	}

	// Drift stays silent for the hybrid (intent matches live).
	drift := BranchRulesetDrift(hybrid, nil, BranchProtectionOptions{Solo: true})
	for _, d := range drift {
		if d == "pull_request" {
			t.Errorf("hybrid solo ruleset must not drift on pull_request: %v", drift)
		}
	}
}

// TestStrictestPRRule_PreservesLiveParams pins the fabrica case: a
// hand-tuned live pull_request rule carries params beyond the baseline five
// (allowed_merge_methods, require_extra_approval_for_unattributed_changes,
// required_reviewers). A reconcile must pass them through untouched — the
// same fidelity rule that keeps existing checks' integration_id.
func TestStrictestPRRule_PreservesLiveParams(t *testing.T) {
	live := &Ruleset{
		Enforcement: "active",
		Conditions:  map[string]any{"ref_name": map[string]any{"include": []any{"~DEFAULT_BRANCH"}}},
		Rules: []map[string]any{
			{"type": "pull_request", "parameters": map[string]any{
				"allowed_merge_methods":                                []any{"squash"},
				"require_extra_approval_for_unattributed_changes":      true,
				"required_reviewers":                                   []any{"jpvelasco"},
				"dismiss_stale_reviews_on_push":                        false,
				"require_code_owner_review":                            false,
				"require_last_push_approval":                           false,
				"required_review_thread_resolution":                    false,
			}},
		},
	}
	got := strictestPRRule(ruleByType(live, "pull_request"), BranchProtectionOptions{Solo: true})

	if methods, ok := got["allowed_merge_methods"].([]any); !ok || len(methods) != 1 || methods[0] != "squash" {
		t.Errorf("allowed_merge_methods must survive the merge, got %v", got["allowed_merge_methods"])
	}
	if !mapBool(got, "require_extra_approval_for_unattributed_changes") {
		t.Error("require_extra_approval_for_unattributed_changes must survive the merge")
	}
	if reviewers, ok := got["required_reviewers"].([]any); !ok || len(reviewers) != 1 || reviewers[0] != "jpvelasco" {
		t.Errorf("required_reviewers must survive the merge, got %v", got["required_reviewers"])
	}
	// Owned keys still get baseline-or-stricter treatment: thread resolution
	// was off (weaker than baseline true) and must be raised.
	if !mapBool(got, "required_review_thread_resolution") {
		t.Error("thread resolution must be raised to the baseline true")
	}
	// The owned solo keys keep their intent.
	if mapBool(got, "dismiss_stale_reviews_on_push") {
		t.Error("solo intent keeps dismiss_stale off")
	}
}

// TestMergeBranchRulesetBody_PreservesLivePRParams runs the full body merge
// against a fabrica-shaped live ruleset and asserts the pass-through end to end.
func TestMergeBranchRulesetBody_PreservesLivePRParams(t *testing.T) {
	live := &Ruleset{
		ID:          42,
		Name:        "protect-main",
		Target:      "branch",
		Enforcement: "active",
		Conditions:  map[string]any{"ref_name": map[string]any{"include": []any{"~DEFAULT_BRANCH"}}},
		Rules: []map[string]any{
			{"type": "deletion"},
			{"type": "pull_request", "parameters": map[string]any{
				"allowed_merge_methods":                                []any{"squash"},
				"require_extra_approval_for_unattributed_changes":      true,
				"required_approving_review_count":                      float64(1),
				"required_review_thread_resolution":                    false,
			}},
		},
	}
	body := mergeBranchRulesetBody(live, []string{"Lint"}, BranchProtectionOptions{Solo: true})
	pr := ruleFromBody(body, "pull_request")
	params, _ := pr["parameters"].(map[string]any)

	if methods, ok := params["allowed_merge_methods"].([]any); !ok || len(methods) != 1 || methods[0] != "squash" {
		t.Errorf("allowed_merge_methods lost in full merge: %v", params["allowed_merge_methods"])
	}
	if !mapBool(params, "require_extra_approval_for_unattributed_changes") {
		t.Error("require_extra_approval_for_unattributed_changes lost in full merge")
	}
	if got := mapInt(params, "required_approving_review_count"); got != 1 {
		t.Errorf("live approving count must survive, got %d", got)
	}
	if !mapBool(params, "required_review_thread_resolution") {
		t.Error("weaker live thread resolution must be raised to baseline")
	}
}
