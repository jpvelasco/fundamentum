package revert

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/internal/github"
)

func TestExecute_ErrorOnSettingsRestore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && r.URL.Path == "/repos/owner/repo" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.Contains(r.URL.Path, "/contents/"+BaselinePath) {
			snapJSON, _ := json.Marshal(testSnapshot())
			_, _ = w.Write([]byte(`{"content":"` + base64.StdEncoding.EncodeToString(snapJSON) + `","sha":"s1"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeSettings, false, &out); err == nil {
		t.Fatal("settings restore failure must error")
	}
}

func TestExecute_ErrorOnSecurityRestore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/vulnerability-alerts") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.Contains(r.URL.Path, "/contents/"+BaselinePath) {
			snapJSON, _ := json.Marshal(testSnapshot())
			_, _ = w.Write([]byte(`{"content":"` + base64.StdEncoding.EncodeToString(snapJSON) + `","sha":"s1"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeSettings, false, &out); err == nil {
		t.Fatal("security restore failure must error")
	}
}

func TestExecute_ErrorOnRulesetRestore(t *testing.T) {
	snap := testSnapshot()
	snap.Rulesets.ProtectMain.Existed = true
	snap.Rulesets.ProtectMain.Body = map[string]any{"name": "protect-main"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/rulesets") && !strings.Contains(r.URL.Path, "/rulesets/") {
			_, _ = w.Write([]byte(`[{"id":1,"name":"protect-main"}]`))
			return
		}
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/rulesets/1") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeFull, false, &out); err == nil {
		t.Fatal("ruleset restore failure must error")
	}
}
