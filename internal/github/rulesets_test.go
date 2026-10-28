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
		Rules: []map[string]any{{"type": "creation"}, {"type": "deletion"}, {"type": "non_fast_forward"}},
	}
	if got := TagRulesetDrift(ok); len(got) != 0 {
		t.Errorf("TagRulesetDrift(ok) = %v, want none", got)
	}
	// A tag ruleset without the creation rule is weaker: anyone with push can
	// create a v* tag.
	noCreation := cloneRuleset(ok)
	noCreation.Rules = []map[string]any{{"type": "deletion"}, {"type": "non_fast_forward"}}
	if got := TagRulesetDrift(noCreation); !sameStrings(got, []string{"creation"}) {
		t.Errorf("TagRulesetDrift(no creation) = %v, want [creation]", got)
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
	for _, field := range []string{"conditions", "creation", "deletion", "non_fast_forward"} {
		if !containsString(got, field) {
			t.Errorf("emptied tag ruleset missing drift %q: %v", field, got)
		}
	}
}

func TestResolveRequiredChecks(t *testing.T) {
	if got := ResolveRequiredChecks(nil); len(got) != len(DefaultStatusChecks) {
		t.Fatalf("nil = %#v", got)
	}
	if got := ResolveRequiredChecks([]string{}); len(got) != 0 {
		t.Fatalf("empty = %#v", got)
	}
	got := ResolveRequiredChecks([]string{" Lint ", "", "Lint"})
	if len(got) != 1 || got[0] != "Lint" {
		t.Fatalf("trim/dedup = %#v", got)
	}
}

func TestResolveRequiredChecksForPack(t *testing.T) {
	if got := ResolveRequiredChecksForPack(nil, "generic"); len(got) != 2 || got[0] != "CI" || got[1] != "Trivy" {
		t.Fatalf("generic defaults = %#v", got)
	}
	if got := ResolveRequiredChecksForPack(nil, "none"); got == nil || len(got) != 0 {
		t.Fatalf("none defaults = %#v, want empty non-nil", got)
	}
	if got := ResolveRequiredChecksForPack(nil, "go"); len(got) != len(DefaultStatusChecks) {
		t.Fatalf("go defaults = %#v", got)
	}
	if got := ResolveRequiredChecksForPack(nil, "node"); len(got) != 3 || got[0] != "Lint" || got[2] != "Trivy" {
		t.Fatalf("node defaults = %#v", got)
	}
	got := ResolveRequiredChecksForPack([]string{"Lint"}, "generic")
	if len(got) != 1 || got[0] != "Lint" {
		t.Fatalf("explicit overrides pack = %#v", got)
	}
	explicit := ResolveRequiredChecksForPack([]string{"CI"}, "none")
	if len(explicit) != 1 || explicit[0] != "CI" {
		t.Fatalf("explicit overrides none = %#v", explicit)
	}
}

func TestCreateBranchRuleset_CINoneRequiresNoChecks(t *testing.T) {
	tests := []struct {
		name         string
		statusChecks []string
		wantRule     bool
		wantContexts []string
	}{
		{
			name:         "pack none posts no required_status_checks rule",
			statusChecks: ResolveRequiredChecksForPack(nil, "none"),
			wantRule:     false,
		},
		{
			name:         "nil statusChecks still posts Go defaults",
			statusChecks: nil,
			wantRule:     true,
			wantContexts: append([]string{}, DefaultStatusChecks...),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var postBody map[string]any
			testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&postBody); err != nil {
					t.Errorf("decode ruleset body: %v", err)
				}
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
			}), nil, func(c *Client) {
				if err := c.CreateBranchRuleset("owner", "repo", tt.statusChecks, BranchProtectionOptions{}); err != nil {
					t.Fatalf("CreateBranchRuleset() error: %v", err)
				}
			}, func(t *testing.T) {
				rule := requiredStatusChecksRule(postBody)
				if tt.wantRule != (rule != nil) {
					t.Fatalf("required_status_checks present = %v, want %v", rule != nil, tt.wantRule)
				}
				if !tt.wantRule {
					return
				}
				got := statusCheckContexts(rule)
				if !sameStrings(got, tt.wantContexts) {
					t.Fatalf("contexts = %#v, want %#v", got, tt.wantContexts)
				}
				if !containsString(got, "Lint") {
					t.Fatalf("nil statusChecks missing Lint: %#v", got)
				}
			})
		})
	}
}

func requiredStatusChecksRule(body map[string]any) map[string]any {
	rules, _ := body["rules"].([]any)
	for _, rule := range rules {
		rm, ok := rule.(map[string]any)
		if ok && rm["type"] == "required_status_checks" {
			return rm
		}
	}
	return nil
}

func statusCheckContexts(rule map[string]any) []string {
	params, _ := rule["parameters"].(map[string]any)
	raw, _ := params["required_status_checks"].([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		m, _ := item.(map[string]any)
		if ctx, ok := m["context"].(string); ok {
			out = append(out, ctx)
		}
	}
	return out
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

func TestNextLink(t *testing.T) {
	got := nextLink(`<https://api.github.com/repos/o/r/rulesets?page=2>; rel="next", <https://api.github.com/repos/o/r/rulesets?page=3>; rel="last"`)
	if got != "/repos/o/r/rulesets?page=2" {
		t.Errorf("nextLink() = %q", got)
	}
	if nextLink("") != "" || nextLink(`<https://example/x>; rel="last"`) != "" {
		t.Error("expected empty next when no rel=next")
	}
}

func TestNextLink_MalformedAndPathOnly(t *testing.T) {
	if nextLink(`rel="next"`) != "" {
		t.Error("malformed next must be empty")
	}
	if got := nextLink(`</repos/o/r/rulesets>; rel="next"`); got != "/repos/o/r/rulesets" {
		t.Errorf("path-only next = %q", got)
	}
}

func TestRulesetID_List404(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}), nil, func(c *Client) {
		id, found, err := c.rulesetID("owner", "repo", "protect-main")
		if err != nil || found || id != 0 {
			t.Fatalf("rulesetID() = (%d, %v, %v), want missing", id, found, err)
		}
	}, nil)
}

func TestRulesetID_Paginates(t *testing.T) {
	pages := 0
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		page := r.URL.Query().Get("page")
		if page == "" || page == "1" {
			w.Header().Set("Link", `<https://api.github.com/repos/owner/repo/rulesets?page=2>; rel="next"`)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id":1,"name":"other"}]`))
			return
		}
		if page != "2" {
			t.Errorf("unexpected page %q", page)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":99,"name":"protect-main"}]`))
	}), nil, func(c *Client) {
		id, found, err := c.rulesetID("owner", "repo", "protect-main")
		if err != nil {
			t.Fatalf("rulesetID() error: %v", err)
		}
		if !found || id != 99 {
			t.Fatalf("rulesetID() = (%d, %v), want (99, true)", id, found)
		}
	}, func(t *testing.T) {
		if pages != 2 {
			t.Errorf("pages fetched = %d, want 2", pages)
		}
	})
}

func TestNewStatusCheck_PinsShippedChecksOnly(t *testing.T) {
	for _, pack := range []string{"go", "generic", "node", "python", "rust", "none"} {
		shipped := ResolveRequiredChecksForPack(nil, pack)
		for _, context := range []string{"Lint", "CI", "Test", "gosec", "custom-check"} {
			got := newStatusCheck(context, shipped)
			_, pinned := got["integration_id"]
			want := containsString(shipped, context)
			if pinned != want || (pinned && got["integration_id"] != GitHubActionsAppID) {
				t.Errorf("pack %s, context %s: pin = %v, want pinned=%t", pack, context, got, want)
			}
			// A custom context remains unpinned even when its name collides
			// with a check produced by one of the starter packs.
			if _, pinned := newStatusCheck(context, nil)["integration_id"]; pinned {
				t.Errorf("custom context %s was pinned", context)
			}
		}
	}
}

func TestStatusChecksRule_PinsNewShippedChecks(t *testing.T) {
	rule := statusChecksRule([]string{"Lint", "Codacy Static Code Analysis"}, BranchProtectionOptions{Solo: true, ShippedChecks: []string{"Lint"}})
	params, _ := rule["parameters"].(map[string]any)
	raw, _ := params["required_status_checks"].([]map[string]any)
	byContext := map[string]map[string]any{}
	for _, c := range raw {
		byContext[c["context"].(string)] = c
	}
	if byContext["Lint"]["integration_id"] != GitHubActionsAppID {
		t.Errorf("Lint must be pinned to the GitHub Actions app: %v", byContext["Lint"])
	}
	if _, pinned := byContext["Codacy Static Code Analysis"]["integration_id"]; pinned {
		t.Errorf("third-party Codacy check must not be pinned: %v", byContext["Codacy Static Code Analysis"])
	}
}

func TestStatusChecksRuleMerged_KeepsLivePinsAddsNewPins(t *testing.T) {
	// A live ruleset with an existing Lint pinned to a DIFFERENT integration
	// (e.g. hand-tuned) and an unpinned custom check. The merge must keep both
	// verbatim, and pin only the newly-added shipped check (gosec).
	liveRule := map[string]any{
		"type": "required_status_checks",
		"parameters": map[string]any{
			"strict_required_status_checks_policy": true,
			"required_status_checks": []any{
				map[string]any{"context": "Lint", "integration_id": 999},
				map[string]any{"context": "my-custom-check"},
			},
		},
	}
	rule := statusChecksRuleMerged(liveRule, []string{"Lint", "gosec"}, BranchProtectionOptions{Solo: true, ShippedChecks: []string{"Lint", "gosec"}})
	params, _ := rule["parameters"].(map[string]any)
	raw := anySlice(params["required_status_checks"])
	byContext := map[string]map[string]any{}
	for _, item := range raw {
		m, _ := item.(map[string]any)
		byContext[m["context"].(string)] = m
	}
	// Live Lint pin is preserved verbatim (999), not rewritten to the Actions app.
	if byContext["Lint"]["integration_id"] != 999 {
		t.Errorf("live Lint pin must be kept untouched: %v", byContext["Lint"])
	}
	// Live custom check stays unpinned.
	if _, pinned := byContext["my-custom-check"]["integration_id"]; pinned {
		t.Errorf("live custom check must stay unpinned: %v", byContext["my-custom-check"])
	}
	// Newly-added shipped check (gosec) is pinned to the Actions app.
	if byContext["gosec"]["integration_id"] != GitHubActionsAppID {
		t.Errorf("new gosec check must be pinned to the GitHub Actions app: %v", byContext["gosec"])
	}
}

func TestTagRulesetBody_CreationRuleAndAdminBypass(t *testing.T) {
	body := tagRulesetBody()
	rules := rulesFromBody(body)
	var hasCreation bool
	for _, r := range rules {
		if r["type"] == "creation" {
			hasCreation = true
		}
	}
	if !hasCreation {
		t.Error("tag ruleset body must include a creation rule")
	}
	actors, _ := body["bypass_actors"].([]map[string]any)
	if len(actors) != 1 {
		t.Fatalf("tag ruleset bypass_actors = %v, want exactly the admin role", actors)
	}
	a := actors[0]
	if a["actor_type"] != "RepositoryRole" || a["actor_id"] != int64(RepositoryRoleAdminID) || a["bypass_mode"] != "always" {
		t.Errorf("admin bypass actor = %v, want RepositoryRole id %d always", a, RepositoryRoleAdminID)
	}
}

func TestMergeTagRulesetBody_MergesBypassActors(t *testing.T) {
	// A live ruleset missing the creation rule but with a hand-tuned Team
	// bypass actor. The merge must add the creation rule, keep the Team actor,
	// and add the admin role — never removing existing actors.
	teamID := int64(12345)
	live := &Ruleset{
		Name:        "protect-version-tags",
		Target:      "tag",
		Enforcement: "active",
		Conditions:  map[string]any{"ref_name": map[string]any{"include": []any{"refs/tags/v*"}}},
		Rules:       []map[string]any{{"type": "deletion"}, {"type": "non_fast_forward"}},
		BypassActors: []BypassActor{
			{ActorID: &teamID, ActorType: "Team", BypassMode: "always"},
		},
	}
	body := mergeTagRulesetBody(live)
	rules := rulesFromBody(body)
	var hasCreation bool
	for _, r := range rules {
		if r["type"] == "creation" {
			hasCreation = true
		}
	}
	if !hasCreation {
		t.Error("merged tag ruleset must add the creation rule")
	}
	actors, _ := body["bypass_actors"].([]map[string]any)
	var sawTeam, sawAdmin bool
	for _, a := range actors {
		if a["actor_type"] == "Team" && a["actor_id"] == teamID {
			sawTeam = true
		}
		if a["actor_type"] == "RepositoryRole" && a["actor_id"] == int64(RepositoryRoleAdminID) {
			sawAdmin = true
		}
	}
	if !sawTeam {
		t.Error("merged bypass actors must keep the existing Team actor")
	}
	if !sawAdmin {
		t.Error("merged bypass actors must add the admin role")
	}

	// Idempotent: a live ruleset that already grants the admin role must not
	// get a duplicate.
	adminID := int64(RepositoryRoleAdminID)
	live.BypassActors = append(live.BypassActors, BypassActor{ActorID: &adminID, ActorType: "RepositoryRole", BypassMode: "always"})
	body = mergeTagRulesetBody(live)
	actors, _ = body["bypass_actors"].([]map[string]any)
	adminCount := 0
	for _, a := range actors {
		if a["actor_type"] == "RepositoryRole" && a["actor_id"] == int64(RepositoryRoleAdminID) {
			adminCount++
		}
	}
	if adminCount != 1 {
		t.Errorf("admin role must appear exactly once after merge, got %d", adminCount)
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
