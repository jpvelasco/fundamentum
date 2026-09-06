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

func TestBranchRulesetDrift_AllOwnedFields(t *testing.T) {
	base := &Ruleset{
		Name:        "protect-main",
		Target:      "branch",
		Enforcement: "active",
		Conditions: map[string]any{
			"ref_name": map[string]any{"include": []string{"~DEFAULT_BRANCH"}},
		},
		Rules: []map[string]any{
			{"type": "deletion"},
			{"type": "non_fast_forward"},
			{"type": "pull_request", "parameters": map[string]any{
				"required_review_thread_resolution": true,
				"require_code_owner_review":         false,
				"dismiss_stale_reviews_on_push":     false,
			}},
			{"type": "required_status_checks", "parameters": map[string]any{
				"strict_required_status_checks_policy": true,
				"required_status_checks":               []map[string]any{{"context": "Lint"}},
			}},
		},
	}
	if got := BranchRulesetDrift(nil, []string{"Lint"}, BranchProtectionOptions{Solo: true}); !sameStrings(got, []string{"missing"}) {
		t.Errorf("nil = %v, want [missing]", got)
	}
	if got := BranchRulesetDrift(base, []string{"Lint"}, BranchProtectionOptions{Solo: true}); len(got) != 0 {
		t.Errorf("match = %v, want none", got)
	}
	empty := &Ruleset{Name: "protect-main", Enforcement: "active"}
	got := BranchRulesetDrift(empty, []string{"Lint"}, BranchProtectionOptions{Solo: true})
	for _, field := range []string{"conditions", "deletion", "non_fast_forward", "pull_request", "required_status_checks"} {
		if !containsString(got, field) {
			t.Errorf("emptied ruleset missing drift %q: %v", field, got)
		}
	}
	loose := cloneRuleset(base)
	loose.Rules[3]["parameters"] = map[string]any{
		"strict_required_status_checks_policy": false,
		"required_status_checks":               []map[string]any{{"context": "Lint"}},
	}
	if got := BranchRulesetDrift(loose, []string{"Lint"}, BranchProtectionOptions{Solo: true}); !containsString(got, "required_status_checks") {
		t.Errorf("non-strict checks = %v", got)
	}
	if got := BranchRulesetDrift(base, []string{"Lint"}, BranchProtectionOptions{Solo: true, SkipStatusChecks: true}); len(got) != 0 {
		t.Errorf("SkipStatusChecks = %v, want none", got)
	}
}

func TestTagRulesetDrift_Emptied(t *testing.T) {
	got := TagRulesetDrift(&Ruleset{Name: "protect-version-tags", Enforcement: "active"})
	for _, field := range []string{"conditions", "deletion", "non_fast_forward"} {
		if !containsString(got, field) {
			t.Errorf("emptied tag ruleset missing drift %q: %v", field, got)
		}
	}
}

func TestIntendedChecksAndHelpers(t *testing.T) {
	if got := intendedChecks(nil, BranchProtectionOptions{SkipStatusChecks: true}); got != nil {
		t.Errorf("SkipStatusChecks = %v, want nil", got)
	}
	if got := intendedChecks(nil, BranchProtectionOptions{}); len(got) != len(DefaultStatusChecks) {
		t.Errorf("nil checks = %d, want defaults", len(got))
	}
	if inferSolo(nil) || inferSolo(&Ruleset{}) {
		t.Error("inferSolo without PR rule must be false")
	}
	if matchingPRRule(nil, BranchProtectionOptions{}) {
		t.Error("matchingPRRule(nil) must be false")
	}
	if !hasAllChecks(&Ruleset{}, nil) {
		t.Error("hasAllChecks empty want must be true")
	}
	if hasAllChecks(&Ruleset{}, []string{"Lint"}) {
		t.Error("hasAllChecks missing rule must be false")
	}
	if ruleByType(nil, "deletion") != nil {
		t.Error("ruleByType(nil) must be nil")
	}
	if includesRef(nil, "~DEFAULT_BRANCH") {
		t.Error("includesRef(nil) must be false")
	}
	if anySlice(1) != nil {
		t.Error("anySlice(non-slice) must be nil")
	}
	if mapBool(nil, "x") {
		t.Error("mapBool(nil) must be false")
	}
}

func cloneRuleset(in *Ruleset) *Ruleset {
	out := *in
	out.Rules = append([]map[string]any{}, in.Rules...)
	return &out
}

func containsString(got []string, want string) bool {
	for _, s := range got {
		if s == want {
			return true
		}
	}
	return false
}

func TestGetRuleset_Paths(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		}), nil, func(c *Client) {
			got, err := c.GetRuleset("owner", "repo", "protect-main")
			if err != nil || got != nil {
				t.Fatalf("GetRuleset() = (%v, %v), want (nil, nil)", got, err)
			}
		}, nil)
	})
	t.Run("listed without id", func(t *testing.T) {
		testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"name":"protect-main"}]`))
		}), nil, func(c *Client) {
			if _, err := c.GetRuleset("owner", "repo", "protect-main"); err == nil {
				t.Fatal("expected listed-without-id error")
			}
		}, nil)
	})
	t.Run("detail 404", func(t *testing.T) {
		testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/repos/owner/repo/rulesets" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[{"id":7,"name":"protect-main"}]`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}), nil, func(c *Client) {
			got, err := c.GetRuleset("owner", "repo", "protect-main")
			if err != nil || got != nil {
				t.Fatalf("GetRuleset() = (%v, %v), want (nil, nil)", got, err)
			}
		}, nil)
	})
	t.Run("list 404", func(t *testing.T) {
		testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}), nil, func(c *Client) {
			got, err := c.GetRuleset("owner", "repo", "protect-main")
			if err != nil || got != nil {
				t.Fatalf("GetRuleset() = (%v, %v), want (nil, nil)", got, err)
			}
		}, nil)
	})
}

func TestPlanAndEnsureTagDrift(t *testing.T) {
	var putPath string
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/rulesets":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id":9,"name":"protect-version-tags"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/rulesets/9":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":9,"name":"protect-version-tags","target":"tag","enforcement":"disabled","conditions":{"ref_name":{"include":["refs/tags/v*"]}},"rules":[{"type":"deletion"},{"type":"non_fast_forward"}]}`))
		case r.Method == http.MethodPut && r.URL.Path == "/repos/owner/repo/rulesets/9":
			putPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":9}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}), nil, func(c *Client) {
		plan, err := c.PlanTagRuleset("owner", "repo")
		if err != nil {
			t.Fatalf("PlanTagRuleset() error: %v", err)
		}
		if !plan.Exists || !containsString(plan.Drift, "enforcement") {
			t.Fatalf("plan = %+v, want exists+enforcement drift", plan)
		}
		if err := c.EnsureTagRuleset("owner", "repo"); err != nil {
			t.Fatalf("EnsureTagRuleset() error: %v", err)
		}
	}, func(t *testing.T) {
		if putPath != "/repos/owner/repo/rulesets/9" {
			t.Errorf("expected PUT /rulesets/9, got %q", putPath)
		}
	})
}

func TestPlanBranchRuleset_Missing(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}), nil, func(c *Client) {
		plan, err := c.PlanBranchRuleset("owner", "repo", nil, BranchProtectionOptions{})
		if err != nil || plan.Exists {
			t.Fatalf("PlanBranchRuleset() = (%+v, %v), want missing", plan, err)
		}
	}, nil)
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
