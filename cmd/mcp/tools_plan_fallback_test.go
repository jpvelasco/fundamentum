package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/internal/github"
)

// When the repo is not reachable, plan falls back to the init dry-run plan
// instead of erroring.
func TestRun_PlanFallsBackToInitPlan(t *testing.T) {
	resetGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	newClient = func(token string, verbose bool) *github.Client {
		return github.NewClient(token, verbose).WithBaseURL(srv.URL)
	}
	t.Cleanup(func() { newClient = github.NewClient })

	out, err := (&toolSet{}).run("plan", json.RawMessage(`{"repo":"owner/repo"}`))
	if err != nil {
		t.Fatalf("plan fallback error: %v", err)
	}
	if !strings.Contains(out, "init dry-run plan") {
		t.Errorf("plan fallback output missing init note:\n%s", out)
	}
	if !strings.Contains(out, "fundamentum apply owner/repo") {
		t.Errorf("plan fallback output missing plan header:\n%s", out)
	}
}

// plan must surface an argument error before any GitHub call.
func TestRun_PlanInvalidArgs(t *testing.T) {
	resetGlobals(t)
	if _, err := (&toolSet{}).run("plan", json.RawMessage(`{"repo":123}`)); err == nil {
		t.Fatal("plan with non-string repo must error")
	}
}
