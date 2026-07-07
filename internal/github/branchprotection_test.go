package github

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClassicProtectionExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/branches/main/protection" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	exists, err := c.ClassicProtectionExists("owner", "repo", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected classic protection to exist")
	}
}

func TestClassicProtectionExists_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	exists, err := c.ClassicProtectionExists("owner", "repo", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected classic protection to not exist")
	}
}

func TestApplyClassicBranchProtection(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/repos/owner/repo/branches/main/protection" {
			called = true
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	if err := c.ApplyClassicBranchProtection("owner", "repo", "main", DefaultStatusChecks, BranchProtectionOptions{Solo: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected PUT to be called")
	}
}

func TestRemoveClassicBranchProtection(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/repos/owner/repo/branches/main/protection" {
			called = true
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	if err := c.RemoveClassicBranchProtection("owner", "repo", "main"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected DELETE to be called")
	}
}
