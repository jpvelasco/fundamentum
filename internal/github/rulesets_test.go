package github

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateBranchRuleset(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/rulesets" {
			called = true
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":42}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	if err := c.CreateBranchRuleset("owner", "repo", []string{"Test / ubuntu"}, BranchProtectionOptions{Solo: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected POST /repos/owner/repo/rulesets")
	}
}

func TestCreateTagRuleset(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			called = true
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":43}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	if err := c.CreateTagRuleset("owner", "repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected POST for tag ruleset")
	}
}
