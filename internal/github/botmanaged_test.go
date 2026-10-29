package github

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestBotManagedReconcilesRequiredCheckPins(t *testing.T) {
	for _, context := range []string{"Lint", "external-review"} {
		for _, pin := range []any{nil, float64(0), float64(999), float64(4242), float64(GitHubActionsAppID)} {
			t.Run(fmt.Sprintf("%s/pin=%v", context, pin), func(t *testing.T) {
				opts := BranchProtectionOptions{
					BotManaged:    true,
					Solo:          true,
					PolicyChecks:  []PolicyCheck{{Context: "external-review", IntegrationID: 4242}},
					ShippedChecks: []string{"Lint"},
				}
				body := branchRulesetBody([]string{"Lint"}, opts)
				data, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				var live Ruleset
				if err := json.Unmarshal(data, &live); err != nil {
					t.Fatal(err)
				}
				params := ruleByType(&live, "required_status_checks")["parameters"].(map[string]any)
				checks := anySlice(params["required_status_checks"])
				for _, item := range checks {
					entry := item.(map[string]any)
					if entry["context"] == context {
						entry["integration_id"] = pin
						if pin == nil {
							delete(entry, "integration_id")
						}
						entry["extra"] = "preserved"
					}
				}
				params["required_status_checks"] = append(checks, map[string]any{"context": "unowned", "integration_id": float64(777)})
				before, err := json.Marshal(&live)
				if err != nil {
					t.Fatal(err)
				}
				wantPin := int64(4242)
				if context == "Lint" {
					wantPin = int64(GitHubActionsAppID)
				}
				if pin != float64(wantPin) && len(BranchRulesetDrift(&live, []string{"Lint"}, opts)) == 0 {
					t.Error("incorrect pin must cause drift before reconciliation")
				}
				merged := mergeBranchRulesetBody(&live, []string{"Lint"}, opts)
				after, err := json.Marshal(&live)
				if err != nil || string(after) != string(before) {
					t.Error("merge mutated the live ruleset")
				}
				data, err = json.Marshal(merged)
				if err != nil {
					t.Fatal(err)
				}
				var applied Ruleset
				if err := json.Unmarshal(data, &applied); err != nil {
					t.Fatal(err)
				}
				if drift := BranchRulesetDrift(&applied, []string{"Lint"}, opts); len(drift) != 0 {
					t.Errorf("drift after apply = %v", drift)
				}
				result := anySlice(ruleByType(&applied, "required_status_checks")["parameters"].(map[string]any)["required_status_checks"])
				if len(result) != 3 {
					t.Fatalf("merged checks = %v, want exactly 3", result)
				}
				for _, item := range result {
					entry := item.(map[string]any)
					switch entry["context"] {
					case context:
						if int64(mapInt(entry, "integration_id")) != wantPin || entry["extra"] != "preserved" {
							t.Errorf("reconciled check = %v", entry)
						}
					case "unowned":
						if mapInt(entry, "integration_id") != 777 {
							t.Errorf("unowned pin changed: %v", entry)
						}
					}
				}
				next, err := json.Marshal(mergeBranchRulesetBody(&applied, []string{"Lint"}, opts))
				if err != nil || string(data) != string(next) {
					t.Errorf("reconcile is not idempotent: %v", err)
				}
			})
		}
	}
}

// TestBranchBodyBotManagedOffIsBaseline is the golden test for the API body
// contract: with BotManaged false, the protect-main request body is identical
// to the un-profiled baseline — no bypass_actors key, no allowed_merge_methods
// in the pull_request params, and no policy checks. Turning the profile on
// must be the only thing that changes the body.
func TestBranchBodyBotManagedOffIsBaseline(t *testing.T) {
	checks := []string{"Lint", "gosec"}

	off := branchRulesetBody(checks, BranchProtectionOptions{Solo: true})
	if _, present := off["bypass_actors"]; present {
		t.Error("baseline (BotManaged off) body must not carry bypass_actors")
	}
	pr := ruleByTypeBody(off, "pull_request")
	params, _ := pr["parameters"].(map[string]any)
	if _, present := params["allowed_merge_methods"]; present {
		t.Error("baseline pull_request params must not set allowed_merge_methods")
	}
	sc := ruleByTypeBody(off, "required_status_checks")
	scParams, _ := sc["parameters"].(map[string]any)
	raw := anySlice(scParams["required_status_checks"])
	if len(raw) != len(checks) {
		t.Errorf("baseline required checks = %d, want %d (no policy checks)", len(raw), len(checks))
	}

	// BotManaged on: squash-only merge methods, an explicit empty bypass list,
	// and the solo PR shape.
	on := branchRulesetBody(checks, BranchProtectionOptions{BotManaged: true})
	actors, present := on["bypass_actors"]
	if !present {
		t.Error("bot-managed body must carry an explicit empty bypass_actors list")
	}
	if arr, ok := actors.([]any); !ok || len(arr) != 0 {
		t.Errorf("bot-managed bypass_actors = %v, want empty", actors)
	}
	onParams, _ := ruleByTypeBody(on, "pull_request")["parameters"].(map[string]any)
	if mm, _ := onParams["allowed_merge_methods"].([]string); len(mm) != 1 || mm[0] != "squash" {
		t.Errorf("bot-managed allowed_merge_methods = %v, want [squash]", onParams["allowed_merge_methods"])
	}
	if b, _ := onParams["require_code_owner_review"].(bool); b {
		t.Error("bot-managed must not require code-owner review")
	}
	if b, _ := onParams["dismiss_stale_reviews_on_push"].(bool); b {
		t.Error("bot-managed must not dismiss stale reviews (solo shape)")
	}
}

func TestPolicyChecksIgnoredWithoutBotManaged(t *testing.T) {
	for _, checks := range [][]string{nil, {}, {"Lint"}} {
		for _, policy := range []PolicyCheck{
			{Context: "external-review"},
			{Context: "external-review", IntegrationID: 4242},
		} {
			opts := BranchProtectionOptions{Solo: true}
			wantCreate := branchRulesetBody(checks, opts)
			wantMerge := mergeBranchRulesetBody(liveBranchRuleset(), checks, opts)
			opts.PolicyChecks = []PolicyCheck{policy}
			for _, pair := range [][2]map[string]any{
				{wantCreate, branchRulesetBody(checks, opts)},
				{wantMerge, mergeBranchRulesetBody(liveBranchRuleset(), checks, opts)},
			} {
				want, err := json.Marshal(pair[0])
				if err != nil {
					t.Fatal(err)
				}
				got, err := json.Marshal(pair[1])
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != string(want) {
					t.Errorf("profile-off request bytes changed with policy %+v:\ngot %s\nwant %s", policy, got, want)
				}
			}
		}
	}
}

// ruleByTypeBody finds a rule by type in a built body map.
func ruleByTypeBody(body map[string]any, typ string) map[string]any {
	rules, _ := body["rules"].([]map[string]any)
	for _, r := range rules {
		if r["type"] == typ {
			return r
		}
	}
	return nil
}

// TestPolicyCheckEntriesPinAndValidate covers the policy-check pin and the
// refuse-unpinned rule.
func TestPolicyCheckEntriesPinAndValidate(t *testing.T) {
	// Valid: integration_id present → pinned entry.
	entries := policyCheckEntries([]PolicyCheck{{Context: "external-review", IntegrationID: 4242}})
	if len(entries) != 1 || entries[0]["integration_id"] != int64(4242) || entries[0]["context"] != "external-review" {
		t.Errorf("policyCheckEntries = %v", entries)
	}
	// Invalid: missing integration_id → refused.
	if err := ValidatePolicyChecks([]PolicyCheck{{Context: "external-review"}}); err == nil {
		t.Error("ValidatePolicyChecks must refuse an unpinned policy check")
	}
	// Invalid: empty context → refused.
	if err := ValidatePolicyChecks([]PolicyCheck{{IntegrationID: 4242}}); err == nil {
		t.Error("ValidatePolicyChecks must refuse an empty-context policy check")
	}
	// Valid set passes.
	if err := ValidatePolicyChecks([]PolicyCheck{{Context: "a", IntegrationID: 1}, {Context: "b", IntegrationID: 2}}); err != nil {
		t.Errorf("ValidatePolicyChecks valid set: %v", err)
	}
}

// TestStatusChecksRulePolicyChecks verifies policy checks land in the create
// body pinned, alongside pinned shipped checks.
func TestStatusChecksRulePolicyChecks(t *testing.T) {
	opts := BranchProtectionOptions{
		BotManaged:    true,
		PolicyChecks:  []PolicyCheck{{Context: "external-review", IntegrationID: 4242}},
		ShippedChecks: []string{"Lint"},
	}
	rule := statusChecksRule([]string{"Lint"}, opts)
	byContext := requiredChecksByContext(rule)
	if byContext["external-review"]["integration_id"] != int64(4242) {
		t.Errorf("policy check must be pinned: %v", byContext["external-review"])
	}
	if byContext["Lint"]["integration_id"] != GitHubActionsAppID {
		t.Errorf("shipped check must be pinned to the Actions app: %v", byContext["Lint"])
	}
}

// TestRemovedBypassActors covers the bot-managed removed-actors report.
func TestRemovedBypassActors(t *testing.T) {
	teamID := int64(7)
	live := &Ruleset{BypassActors: []BypassActor{{ActorID: &teamID, ActorType: "Team", BypassMode: "always"}}}

	// Off: nothing reported.
	if got := RemovedBypassActors(live, BranchProtectionOptions{}); got != nil {
		t.Errorf("BotManaged off must report no removed actors, got %v", got)
	}
	// On: the live actor is reported.
	got := RemovedBypassActors(live, BranchProtectionOptions{BotManaged: true})
	if len(got) != 1 || got[0].ActorType != "Team" {
		t.Errorf("RemovedBypassActors = %v, want the Team actor", got)
	}
	// DescribeBypassActor formats readably.
	if s := DescribeBypassActor(got[0]); s != "Team#7 (always)" {
		t.Errorf("DescribeBypassActor = %q", s)
	}
	if s := DescribeBypassActor(BypassActor{ActorType: "OrganizationAdmin", BypassMode: "pull_request"}); s != "OrganizationAdmin (pull_request)" {
		t.Errorf("DescribeBypassActor no-id = %q", s)
	}
}

// TestBotManagedDrift covers the bot-managed drift signals: squash-only
// merges, no bypass actors, and pinned policy checks.
func TestBotManagedDrift(t *testing.T) {
	good := baseMainRulesetForTest()
	ruleByType(good, "pull_request")["parameters"].(map[string]any)["allowed_merge_methods"] = []any{"squash"}
	good.Rules = append(good.Rules, map[string]any{
		"type": "required_status_checks",
		"parameters": map[string]any{
			"strict_required_status_checks_policy": true,
			"required_status_checks": []any{
				map[string]any{"context": "Lint", "integration_id": float64(GitHubActionsAppID)},
				map[string]any{"context": "external-review", "integration_id": float64(4242)},
			},
		},
	})
	opts := BranchProtectionOptions{
		BotManaged:    true,
		Solo:          true,
		PolicyChecks:  []PolicyCheck{{Context: "external-review", IntegrationID: 4242}},
		ShippedChecks: []string{"Lint"},
	}
	if got := BranchRulesetDrift(good, []string{"Lint"}, opts); len(got) != 0 {
		t.Errorf("fully-compliant bot-managed ruleset drifted: %v", got)
	}

	// Merge commits still allowed → drift.
	mergeAllowed := cloneRuleset(good)
	for _, r := range mergeAllowed.Rules {
		if r["type"] == "pull_request" {
			r["parameters"].(map[string]any)["allowed_merge_methods"] = []any{"squash", "merge"}
		}
	}
	if got := BranchRulesetDrift(mergeAllowed, []string{"Lint"}, opts); !containsString(got, "squash-only merges") {
		t.Errorf("merge-commit-allowed ruleset must drift on squash-only: %v", got)
	}

	// A live bypass actor → drift.
	withBypass := cloneRuleset(good)
	teamID := int64(7)
	withBypass.BypassActors = []BypassActor{{ActorID: &teamID, ActorType: "Team", BypassMode: "always"}}
	if got := BranchRulesetDrift(withBypass, []string{"Lint"}, opts); !containsString(got, "bypass actors present") {
		t.Errorf("bypass-actor ruleset must drift: %v", got)
	}

	// Policy check present but unpinned → drift.
	unpinned := cloneRuleset(good)
	for _, r := range unpinned.Rules {
		if r["type"] == "required_status_checks" {
			params := r["parameters"].(map[string]any)
			params["required_status_checks"] = []any{
				map[string]any{"context": "Lint", "integration_id": float64(GitHubActionsAppID)},
				map[string]any{"context": "external-review"},
			}
		}
	}
	if got := BranchRulesetDrift(unpinned, []string{"Lint"}, opts); !containsString(got, "required check pins: external-review") {
		t.Errorf("unpinned-policy-check ruleset must drift: %v", got)
	}
}

// TestMergeBranchBodyBotManagedDropsBypass verifies the reconcile path sends
// an empty bypass list (dropping live actors) only in bot-managed mode.
func TestMergeBranchBodyBotManagedDropsBypass(t *testing.T) {
	teamID := int64(7)
	live := baseMainRulesetForTest()
	live.BypassActors = []BypassActor{{ActorID: &teamID, ActorType: "Team", BypassMode: "always"}}

	// Normal reconcile: no bypass_actors key (live actors left untouched).
	normal := mergeBranchRulesetBody(live, []string{"Lint"}, BranchProtectionOptions{Solo: true})
	if _, present := normal["bypass_actors"]; present {
		t.Error("normal reconcile must not send bypass_actors")
	}

	// Bot-managed reconcile: explicit empty list drops the live actor.
	bot := mergeBranchRulesetBody(live, []string{"Lint"}, BranchProtectionOptions{BotManaged: true})
	actors, present := bot["bypass_actors"]
	if !present {
		t.Fatal("bot-managed reconcile must send bypass_actors")
	}
	if arr, ok := actors.([]any); !ok || len(arr) != 0 {
		t.Errorf("bot-managed reconcile bypass_actors = %v, want empty", actors)
	}

	// The two bodies differ ONLY in bypass handling + merge methods, not in
	// the required_status_checks rule shape.
	if !reflect.DeepEqual(ruleByTypeBody(normal, "required_status_checks"), ruleByTypeBody(bot, "required_status_checks")) {
		t.Error("status-check rule differs when both modes add the same check")
	}
}
