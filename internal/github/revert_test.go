package github

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetFileContent_FoundAndMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/contents/ok.yml") {
			_, _ = w.Write([]byte(`{"content":"eA==","sha":"s1"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := NewClient("t", false).WithBaseURL(srv.URL)
	content, sha, ok, err := c.GetFileContent("o", "r", "ok.yml", "")
	if err != nil || !ok || content == "" || sha == "" {
		t.Fatalf("GetFileContent(ok) = %q %q %v %v", content, sha, ok, err)
	}
	_, _, ok, err = c.GetFileContent("o", "r", "missing.yml", "")
	if err != nil || ok {
		t.Fatalf("GetFileContent(missing) = ok=%v err=%v, want ok=false", ok, err)
	}
}

func TestGetRulesetBody_FoundAndMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/rulesets") && !strings.Contains(r.URL.Path, "/rulesets/") {
			_, _ = w.Write([]byte(`[{"id":1,"name":"protect-main"}]`))
			return
		}
		if strings.Contains(r.URL.Path, "/rulesets/1") {
			_, _ = w.Write([]byte(`{"id":1,"name":"protect-main","target":"branch","enforcement":"active","conditions":{},"rules":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := NewClient("t", false).WithBaseURL(srv.URL)
	body, ok, err := c.GetRulesetBody("o", "r", "protect-main")
	if err != nil || !ok || body["name"] != "protect-main" {
		t.Fatalf("GetRulesetBody(protect-main) = %v %v %v", body, ok, err)
	}
	_, ok, err = c.GetRulesetBody("o", "r", "missing")
	if err != nil || ok {
		t.Fatalf("GetRulesetBody(missing) = ok=%v err=%v, want ok=false", ok, err)
	}
}

func TestDeleteRuleset_FoundAndMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/rulesets") && !strings.Contains(r.URL.Path, "/rulesets/") {
			_, _ = w.Write([]byte(`[{"id":1,"name":"protect-main"}]`))
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := NewClient("t", false).WithBaseURL(srv.URL)
	if err := c.DeleteRuleset("o", "r", "protect-main"); err != nil {
		t.Fatalf("DeleteRuleset(protect-main) error: %v", err)
	}
	if err := c.DeleteRuleset("o", "r", "missing"); err != nil {
		t.Fatalf("DeleteRuleset(missing) error: %v", err)
	}
}

func TestUpdateRulesetAndRepoSettingsAndDisableSecurityAndDeleteFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.Contains(path, "/rulesets") && !strings.Contains(path, "/rulesets/"):
			_, _ = w.Write([]byte(`[{"id":1,"name":"protect-main"}]`))
		case strings.Contains(path, "/rulesets/1") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case path == "/repos/o/r" && r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case strings.HasSuffix(path, "/vulnerability-alerts") || strings.HasSuffix(path, "/automated-security-fixes"):
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(path, "/code-scanning/default-setup"):
			_, _ = w.Write([]byte(`{"state":"configured"}`))
		case strings.Contains(path, "/contents/ok.yml") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"content":"eA==","sha":"s1"}`))
		case strings.Contains(path, "/contents/ok.yml") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := NewClient("t", false).WithBaseURL(srv.URL)
	if err := c.UpdateRuleset("o", "r", 1, map[string]any{"name": "protect-main"}); err != nil {
		t.Fatalf("UpdateRuleset error: %v", err)
	}
	if err := c.UpdateRepoSettings("o", "r", SettingsState{DeleteBranchOnMerge: true, AllowSquashMerge: true, AllowMergeCommit: true, AllowRebaseMerge: true}); err != nil {
		t.Fatalf("UpdateRepoSettings error: %v", err)
	}
	if err := c.DisableSecurity("o", "r", SecurityState{DependabotAlerts: true, AutomatedFixes: true, SecretScanning: true, PushProtection: true}); err != nil {
		t.Fatalf("DisableSecurity error: %v", err)
	}
	if err := c.DeleteFile("o", "r", "ok.yml"); err != nil {
		t.Fatalf("DeleteFile error: %v", err)
	}
}

func TestRulesetIDForUpdate_FoundAndMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/rulesets") && !strings.Contains(r.URL.Path, "/rulesets/") {
			_, _ = w.Write([]byte(`[{"id":1,"name":"protect-main"}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := NewClient("t", false).WithBaseURL(srv.URL)
	id, err := c.RulesetIDForUpdate("o", "r", "protect-main")
	if err != nil || id != 1 {
		t.Fatalf("rulesetIDForUpdate(protect-main) = %d %v", id, err)
	}
	if _, err := c.RulesetIDForUpdate("o", "r", "missing"); err == nil {
		t.Fatal("rulesetIDForUpdate(missing) must error")
	}
}

func TestDisabledIf(t *testing.T) {
	if disabledIf(true) != "enabled" || disabledIf(false) != "disabled" {
		t.Error("disabledIf wrong")
	}
}
