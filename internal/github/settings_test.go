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
			// Default (non-bot-managed) body must be exactly the single key.
			if strings.Contains(string(body), "allow_squash_merge") || strings.Contains(string(body), "allow_merge_commit") || strings.Contains(string(body), "allow_rebase_merge") {
				t.Errorf("default body must not touch merge methods, body=%s", body)
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if err := c.ApplyGeneralSettings("owner", "repo", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected PATCH /repos/owner/repo to be called")
	}
}

func TestApplyGeneralSettings_BotManaged(t *testing.T) {
	var body string
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if err := c.ApplyGeneralSettings("owner", "repo", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		`"delete_branch_on_merge":true`,
		`"allow_squash_merge":true`,
		`"allow_merge_commit":false`,
		`"allow_rebase_merge":false`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("bot-managed body missing %s: %s", want, body)
		}
	}
}
