package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/internal/github"
)

// A failing audit propagates as an error (isError content upstream).
func TestRun_AuditErrorPropagates(t *testing.T) {
	resetGlobals(t)
	srv := mockGitHub(t)
	defer srv.Close()
	newClient = func(token string, verbose bool) *github.Client {
		return github.NewClient(token, verbose).WithBaseURL(srv.URL)
	}
	// The mock repo has no protect-main, so audit fails.
	if _, err := (&toolSet{}).run("audit", json.RawMessage(`{"repo":"owner/repo"}`)); err == nil {
		t.Fatal("audit against unprotected repo must error")
	}
}

// A failing apply (repo not writable / plan fails) propagates as an error.
func TestRun_ApplyErrorPropagates(t *testing.T) {
	resetGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	newClient = func(token string, verbose bool) *github.Client {
		return github.NewClient(token, verbose).WithBaseURL(srv.URL)
	}
	if _, err := (&toolSet{}).run("apply", json.RawMessage(`{"repo":"owner/repo"}`)); err == nil {
		t.Fatal("apply against a 403 repo must error")
	}
}

// Plan against a healthy repo returns the plan table with no fallback note.
func TestRun_PlanSuccess(t *testing.T) {
	resetGlobals(t)
	srv := mockGitHub(t)
	defer srv.Close()
	newClient = func(token string, verbose bool) *github.Client {
		return github.NewClient(token, verbose).WithBaseURL(srv.URL)
	}
	out, err := (&toolSet{}).run("plan", json.RawMessage(`{"repo":"owner/repo"}`))
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	if !strings.Contains(out, "fundamentum apply owner/repo") {
		t.Errorf("plan output missing header:\n%s", out)
	}
}
