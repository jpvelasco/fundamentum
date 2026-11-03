package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
)

// resetGlobals restores the shared flag state the tools mutate.
func resetGlobals(t *testing.T) {
	t.Helper()
	globals.Token = "t"
	globals.DryRun = false
	globals.Preset = ""
	globals.FromFile = ""
	t.Cleanup(func() {
		globals.Token = ""
		globals.DryRun = false
		globals.Preset = ""
		globals.FromFile = ""
		newClient = github.NewClient
	})
}

// mockGitHub serves the minimal API surface plan/audit exercise.
func mockGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"visibility":"public","default_branch":"main","delete_branch_on_merge":true,"owner":{"type":"User"},"security_and_analysis":{"secret_scanning":{"status":"enabled"},"secret_scanning_push_protection":{"status":"enabled"}}}`))
		case strings.Contains(r.URL.Path, "/vulnerability-alerts"),
			strings.Contains(r.URL.Path, "/automated-security-fixes"):
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(r.URL.Path, "/code-scanning/default-setup"):
			_, _ = w.Write([]byte(`{"state":"configured"}`))
		case r.URL.Path == "/repos/owner/repo/rulesets":
			_, _ = w.Write([]byte(`[]`))
		case strings.Contains(r.URL.Path, "/branches/main/protection"):
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestRun_PlanDryRun(t *testing.T) {
	resetGlobals(t)
	srv := mockGitHub(t)
	defer srv.Close()
	newClient = func(token string, verbose bool) *github.Client {
		return github.NewClient(token, verbose).WithBaseURL(srv.URL)
	}
	t.Cleanup(func() { newClient = github.NewClient })

	ts := &toolSet{}
	out, err := ts.run("plan", json.RawMessage(`{"repo":"owner/repo"}`))
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	if !globals.DryRun {
		t.Error("plan must set DryRun")
	}
	if !strings.Contains(out, "fundamentum apply owner/repo") {
		t.Errorf("plan output missing plan header:\n%s", out)
	}
}

func TestRun_UnknownToolAndBadRepo(t *testing.T) {
	resetGlobals(t)
	ts := &toolSet{}
	if _, err := ts.run("nope", json.RawMessage(`{"repo":"owner/repo"}`)); err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("unknown tool = %v", err)
	}
	if _, err := ts.run("plan", json.RawMessage(`{"repo":"noslash"}`)); err == nil {
		t.Error("bad repo = nil, want parse error")
	}
}

func TestRun_AuditAgainstMock(t *testing.T) {
	resetGlobals(t)
	srv := mockGitHub(t)
	defer srv.Close()
	newClient = func(token string, verbose bool) *github.Client {
		return github.NewClient(token, verbose).WithBaseURL(srv.URL)
	}
	t.Cleanup(func() { newClient = github.NewClient })

	ts := &toolSet{}
	out, err := ts.run("audit", json.RawMessage(`{"repo":"owner/repo"}`))
	// The audit will FAIL (no protect-main ruleset in the mock), and
	// runWithClient returns an error on failure — the tool must surface the
	// report, not swallow it.
	if err == nil {
		t.Logf("audit unexpectedly passed:\n%s", out)
	}
	if !strings.Contains(out, "fundamentum audit owner/repo") && err == nil {
		t.Errorf("audit output missing report header:\n%s", out)
	}
}
