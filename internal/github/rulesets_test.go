package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.pathCheck(r.Method, r.URL.Path) {
					called = true
				}
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
			}))
			defer srv.Close()

			c := NewClient("t", false).WithBaseURL(srv.URL)
			if err := tt.fn(c); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !called {
				t.Error("expected POST to be called")
			}
		})
	}
}
