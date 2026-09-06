package github

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestCreateRulesets(t *testing.T) {
	tests := []struct {
		name      string
		fn        func(c *Client) error
		pathCheck func(string, string) bool
	}{
		{
			name: "branch ruleset",
			fn: func(c *Client) error {
				return c.CreateBranchRuleset("owner", "repo", []string{"Test / ubuntu"}, BranchProtectionOptions{Solo: true})
			},
			pathCheck: func(method, path string) bool {
				return method == http.MethodPost && path == "/repos/owner/repo/rulesets"
			},
		},
		{
			name: "tag ruleset",
			fn: func(c *Client) error {
				return c.CreateTagRuleset("owner", "repo")
			},
			pathCheck: func(method, _ string) bool {
				return method == http.MethodPost
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.pathCheck(r.Method, r.URL.Path) {
					called = true
				}
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
			}))
			defer srv.Close()
			if err := tt.fn(c); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !called {
				t.Error("expected POST to be called")
			}
		})
	}
}

func TestBranchRulesetDrift(t *testing.T) {
	desired := branchRulesetBody([]string{"Lint"}, BranchProtectionOptions{Solo: true})
	tests := []struct {
		name string
		got  *Ruleset
		want []string
	}{
		{
			name: "match",
			got: &Ruleset{
				Name:        "protect-main",
				Target:      "branch",
				Enforcement: "active",
				Conditions: map[string]any{
					"ref_name": map[string]any{
						"include": []any{"~DEFAULT_BRANCH"},
						"exclude": []any{},
					},
				},
				Rules: []map[string]any{
					{"type": "deletion"},
					{"type": "non_fast_forward"},
					{"type": "pull_request", "parameters": map[string]any{
						"required_approving_review_count":   0.0,
						"dismiss_stale_reviews_on_push":     false,
						"require_code_owner_review":         false,
						"require_last_push_approval":        false,
						"required_review_thread_resolution": true,
					}},
					{"type": "required_status_checks", "parameters": map[string]any{
						"strict_required_status_checks_policy": true,
						"do_not_enforce_on_create":             false,
						"required_status_checks": []any{
							map[string]any{"context": "Lint"},
						},
					}},
				},
			},
		},
		{
			name: "disabled enforcement",
			got: &Ruleset{
				Name:        "protect-main",
				Target:      "branch",
				Enforcement: "disabled",
				Conditions:  desired["conditions"].(map[string]any),
				Rules:       rulesFromBody(desired),
			},
			want: []string{"enforcement"},
		},
		{
			name: "missing required checks",
			got: &Ruleset{
				Name:        "protect-main",
				Target:      "branch",
				Enforcement: "active",
				Conditions:  desired["conditions"].(map[string]any),
				Rules: []map[string]any{
					{"type": "deletion"},
					{"type": "non_fast_forward"},
					{"type": "pull_request", "parameters": map[string]any{
						"required_approving_review_count":   0.0,
						"dismiss_stale_reviews_on_push":     false,
						"require_code_owner_review":         false,
						"require_last_push_approval":        false,
						"required_review_thread_resolution": true,
					}},
				},
			},
			want: []string{"required_status_checks"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BranchRulesetDrift(tt.got, []string{"Lint"}, BranchProtectionOptions{Solo: true})
			if !sameStrings(got, tt.want) {
				t.Errorf("BranchRulesetDrift() = %v, want %v", got, tt.want)
			}
		})
	}
}

func rulesFromBody(body map[string]any) []map[string]any {
	raw, _ := body["rules"].([]map[string]any)
	return raw
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestTagRulesetDrift(t *testing.T) {
	ok := &Ruleset{
		Name:        "protect-version-tags",
		Target:      "tag",
		Enforcement: "active",
		Conditions: map[string]any{
			"ref_name": map[string]any{"include": []any{"refs/tags/v*"}, "exclude": []any{}},
		},
		Rules: []map[string]any{{"type": "deletion"}, {"type": "non_fast_forward"}},
	}
	if got := TagRulesetDrift(ok); len(got) != 0 {
		t.Errorf("TagRulesetDrift(ok) = %v, want none", got)
	}
	ok.Enforcement = "disabled"
	if got := TagRulesetDrift(ok); !sameStrings(got, []string{"enforcement"}) {
		t.Errorf("TagRulesetDrift(disabled) = %v, want [enforcement]", got)
	}
	if got := TagRulesetDrift(nil); !sameStrings(got, []string{"missing"}) {
		t.Errorf("TagRulesetDrift(nil) = %v, want [missing]", got)
	}
}

func TestRequireCodeOwners(t *testing.T) {
	tests := []struct {
		opts BranchProtectionOptions
		want bool
	}{
		{opts: BranchProtectionOptions{}, want: true},
		{opts: BranchProtectionOptions{Solo: true}, want: false},
		{opts: BranchProtectionOptions{SkipCodeOwners: true}, want: false},
		{opts: BranchProtectionOptions{Solo: true, SkipCodeOwners: true}, want: false},
	}
	for _, tt := range tests {
		if got := tt.opts.requireCodeOwners(); got != tt.want {
			t.Errorf("requireCodeOwners(%+v) = %v, want %v", tt.opts, got, tt.want)
		}
	}
}

func TestCreateBranchRuleset_SkipCodeOwners(t *testing.T) {
	var got bool
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Rules []struct {
				Type       string `json:"type"`
				Parameters struct {
					RequireCodeOwnerReview bool `json:"require_code_owner_review"`
				} `json:"parameters"`
			} `json:"rules"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("decode ruleset: %v", err)
		}
		for _, rule := range payload.Rules {
			if rule.Type == "pull_request" {
				got = rule.Parameters.RequireCodeOwnerReview
			}
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
	}))
	defer srv.Close()

	err := c.CreateBranchRuleset("owner", "repo", []string{"Lint"}, BranchProtectionOptions{SkipCodeOwners: true})
	if err != nil {
		t.Fatalf("CreateBranchRuleset() error: %v", err)
	}
	if got {
		t.Error("expected require_code_owner_review false when SkipCodeOwners is set")
	}
}
