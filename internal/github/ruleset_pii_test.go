package github

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPIICheckReconcile(t *testing.T) {
	live := liveBranchRuleset()
	opts := BranchProtectionOptions{ShippedChecks: []string{"pii-gate"}}
	body := mergeBranchRulesetBody(live, []string{"pii-gate"}, opts)
	params := ruleFromBody(body, "required_status_checks")["parameters"].(map[string]any)
	checks := anySlice(params["required_status_checks"])
	found := false
	for _, raw := range checks {
		check := raw.(map[string]any)
		switch check["context"] {
		case "pii-gate":
			found = true
			if check["integration_id"] != GitHubActionsAppID {
				t.Fatal("new baseline gate must be Actions pinned")
			}
		case "Codacy":
			if check["integration_id"] != 9999.0 {
				t.Fatal("custom identity changed")
			}
		}
	}
	if !found {
		t.Fatal("missing gate")
	}
	bytes, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var landed Ruleset
	if err := json.Unmarshal(bytes, &landed); err != nil {
		t.Fatal(err)
	}
	next, err := json.Marshal(mergeBranchRulesetBody(&landed, []string{}, opts))
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	_ = json.Unmarshal(bytes, &a)
	_ = json.Unmarshal(next, &b)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("opt-out/deferred intent must preserve already-required checks")
	}
}
