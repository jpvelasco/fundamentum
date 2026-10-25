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
