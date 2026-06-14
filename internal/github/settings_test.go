package github

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApplyGeneralSettings(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && r.URL.Path == "/repos/owner/repo" {
			called = true
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	if err := c.ApplyGeneralSettings("owner", "repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected PATCH /repos/owner/repo to be called")
	}
}
