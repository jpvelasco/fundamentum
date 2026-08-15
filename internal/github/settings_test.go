package github

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestApplyGeneralSettings(t *testing.T) {
	called := false
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && r.URL.Path == "/repos/owner/repo" {
			called = true
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"delete_branch_on_merge":true`) {
				t.Errorf("expected delete_branch_on_merge, body=%s", body)
			}
			if strings.Contains(string(body), "default_branch") {
				t.Errorf("must not force default_branch, body=%s", body)
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if err := c.ApplyGeneralSettings("owner", "repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected PATCH /repos/owner/repo to be called")
	}
}
