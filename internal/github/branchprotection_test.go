package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClassicProtectionExists(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   bool
	}{
		{
			name:   "found",
			status: http.StatusOK,
			want:   true,
		},
		{
			name:   "not found",
			status: http.StatusNotFound,
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo/branches/main/protection" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			c := NewClient("t", false).WithBaseURL(srv.URL)
			exists, err := c.ClassicProtectionExists("owner", "repo", "main")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if exists != tt.want {
				t.Errorf("got %v, want %v", exists, tt.want)
			}
		})
	}
}

func TestClassicProtectionExists_NetworkError(t *testing.T) {
	c := newErroringClient()
	_, err := c.ClassicProtectionExists("owner", "repo", "main")
	if err == nil {
		t.Fatal("expected error on network failure")
	}
}

func TestApplyClassicBranchProtection(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/repos/owner/repo/branches/main/protection" {
			called = true
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{})
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

func TestApplyClassicBranchProtection_NetworkError(t *testing.T) {
	c := newErroringClient()
	err := c.ApplyClassicBranchProtection("owner", "repo", "main", DefaultStatusChecks, BranchProtectionOptions{})
	if err == nil {
		t.Fatal("expected error on network failure")
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

func TestRemoveClassicBranchProtection_NetworkError(t *testing.T) {
	c := newErroringClient()
	err := c.RemoveClassicBranchProtection("owner", "repo", "main")
	if err == nil {
		t.Fatal("expected error on network failure")
	}
}
