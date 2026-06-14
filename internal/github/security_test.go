package github

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnableSecurity(t *testing.T) {
	paths := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths[r.Method+":"+r.URL.Path] = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	if err := c.EnableSecurity("owner", "repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !paths["PUT:/repos/owner/repo/vulnerability-alerts"] {
		t.Error("expected vulnerability-alerts PUT")
	}
	if !paths["PUT:/repos/owner/repo/automated-security-fixes"] {
		t.Error("expected automated-security-fixes PUT")
	}
}
