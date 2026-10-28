package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRepoMergeSettingsDrift(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		squash, merge, rebase bool
		want                  string
	}{
		{"compliant", true, false, false, ""},
		{"all settings drift", false, true, true, "allow_squash_merge=false,allow_merge_commit=true,allow_rebase_merge=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]bool{
					"allow_squash_merge": tc.squash, "allow_merge_commit": tc.merge, "allow_rebase_merge": tc.rebase,
				})
			}))
			defer srv.Close()
			repo, err := client.GetRepo("owner", "repo")
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(repo.SquashOnlyDrift(), ","); got != tc.want {
				t.Errorf("merge settings drift = %q, want %q", got, tc.want)
			}
		})
	}
}

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
