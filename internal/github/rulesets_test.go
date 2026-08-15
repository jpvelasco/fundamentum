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
