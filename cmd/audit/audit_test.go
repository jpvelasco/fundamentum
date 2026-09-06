package audit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
)

func TestNewCmd(t *testing.T) {
	cmd := NewCmd()
	if cmd.Use != "audit OWNER/REPO" {
		t.Errorf("Use = %q, want audit OWNER/REPO", cmd.Use)
	}
}

func TestRun_InvalidArg(t *testing.T) {
	cmd := NewCmd()
	cmd.SetArgs([]string{"norepo"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected invalid OWNER/REPO error")
	}
}

func TestAudit_PassingRepo(t *testing.T) {
	srv := newAuditServer(true)
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)

	var out strings.Builder
	err := runWithClient(c, "owner", "repo", &out)
	if err != nil {
		t.Fatalf("runWithClient() error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "PASS") || strings.Contains(got, "FAIL") {
		t.Errorf("expected all-pass report, got:\n%s", got)
	}
}

func TestAudit_DriftFails(t *testing.T) {
	srv := newAuditServer(false)
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)

	var out strings.Builder
	err := runWithClient(c, "owner", "repo", &out)
	if err == nil {
		t.Fatal("expected non-zero exit on protect-main drift")
	}
	if !strings.Contains(out.String(), "FAIL") {
		t.Errorf("expected FAIL in report, got:\n%s", out.String())
	}
}

func TestAudit_OptionalOnlyFailsInStrict(t *testing.T) {
	t.Cleanup(func() { globals.Strict = false })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writePassingAudit(w, r, true, false)
	}))
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)

	var out strings.Builder
	if err := runWithClient(c, "owner", "repo", &out); err != nil {
		t.Fatalf("optional tag drift must not fail without --strict: %v\n%s", err, out.String())
	}

	globals.Strict = true
	out.Reset()
	if err := runWithClient(c, "owner", "repo", &out); err == nil {
		t.Fatal("--strict must fail when the tag ruleset drifted")
	}
}

func TestAudit_AliasFileCountsAsPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/contents/.github/CODEOWNERS") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writePassingAudit(w, r, true, true)
	}))
	defer srv.Close()

	var out strings.Builder
	err := runWithClient(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", &out)
	if err != nil {
		t.Fatalf("root CODEOWNERS alias must satisfy audit: %v\n%s", err, out.String())
	}
}

func TestAudit_RepoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	err := runWithClient(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "get repo") {
		t.Fatalf("expected get repo error, got %v", err)
	}
}

func TestBranchCheck(t *testing.T) {
	if got := branchCheck(github.RulesetPlan{Exists: true}, false); !got.Pass {
		t.Error("matching ruleset must pass")
	}
	if got := branchCheck(github.RulesetPlan{Exists: true, Drift: []string{"enforcement"}}, false); got.Pass || got.Detail != "enforcement" {
		t.Errorf("drift = %+v", got)
	}
	if got := branchCheck(github.RulesetPlan{}, true); got.Pass || got.Detail != "classic protection only" {
		t.Errorf("classic = %+v", got)
	}
	if got := branchCheck(github.RulesetPlan{}, false); got.Pass || got.Detail != "missing" {
		t.Errorf("missing = %+v", got)
	}
}

func TestSecretCheck(t *testing.T) {
	t.Cleanup(func() { globals.AdvancedSecurity = false })
	if got := secretCheck(github.Repo{Visibility: "private"}); !got.Pass || !got.Optional {
		t.Errorf("private without GHAS = %+v", got)
	}
	if got := secretCheck(github.Repo{Visibility: "public", SecretScanning: true, SecretPushProtection: true}); !got.Pass {
		t.Errorf("public enabled = %+v", got)
	}
	if got := secretCheck(github.Repo{Visibility: "public"}); got.Pass || got.Detail != "not enabled" {
		t.Errorf("public disabled = %+v", got)
	}
}

func TestFormatReport(t *testing.T) {
	got := formatReport("o", "r", []check{
		{Name: "protect-main", Pass: true},
		{Name: "tag ruleset", Pass: false, Detail: "enforcement", Optional: true},
	})
	if !strings.Contains(got, "PASS") || !strings.Contains(got, "FAIL") || !strings.Contains(got, "enforcement") {
		t.Errorf("formatReport() = %q", got)
	}
}

func newAuditServer(healthyBranch bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writePassingAudit(w, r, healthyBranch, true)
	}))
}

func writePassingAudit(w http.ResponseWriter, r *http.Request, healthyBranch, healthyTag bool) {
	enforcement := "active"
	if !healthyBranch {
		enforcement = "disabled"
	}
	tagEnforcement := "active"
	if !healthyTag {
		tagEnforcement = "disabled"
	}
	switch {
	case r.URL.Path == "/repos/owner/repo" && r.Method == http.MethodGet:
		_, _ = w.Write([]byte(`{"visibility":"public","default_branch":"main","delete_branch_on_merge":true,"owner":{"type":"User"},"security_and_analysis":{"secret_scanning":{"status":"enabled"},"secret_scanning_push_protection":{"status":"enabled"}}}`))
	case r.URL.Path == "/repos/owner/repo/rulesets":
		_, _ = w.Write([]byte(`[{"id":1,"name":"protect-main"},{"id":2,"name":"protect-version-tags"}]`))
	case r.URL.Path == "/repos/owner/repo/rulesets/1":
		_, _ = w.Write([]byte(`{"id":1,"name":"protect-main","target":"branch","enforcement":"` + enforcement + `","conditions":{"ref_name":{"include":["~DEFAULT_BRANCH"]}},"rules":[{"type":"deletion"},{"type":"non_fast_forward"},{"type":"pull_request","parameters":{"required_review_thread_resolution":true,"require_code_owner_review":false,"dismiss_stale_reviews_on_push":false}},{"type":"required_status_checks","parameters":{"strict_required_status_checks_policy":true,"required_status_checks":[{"context":"Lint"},{"context":"Vulnerability scan"},{"context":"Build (ubuntu-latest)"},{"context":"Build (windows-latest)"},{"context":"Test (ubuntu-latest)"},{"context":"Test (windows-latest)"},{"context":"gosec"},{"context":"Trivy"}]}}]}`))
	case r.URL.Path == "/repos/owner/repo/rulesets/2":
		_, _ = w.Write([]byte(`{"id":2,"name":"protect-version-tags","target":"tag","enforcement":"` + tagEnforcement + `","conditions":{"ref_name":{"include":["refs/tags/v*"]}},"rules":[{"type":"deletion"},{"type":"non_fast_forward"}]}`))
	case strings.Contains(r.URL.Path, "/vulnerability-alerts"):
		w.WriteHeader(http.StatusNoContent)
	case strings.Contains(r.URL.Path, "/contents/"):
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"sha":"x"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}
