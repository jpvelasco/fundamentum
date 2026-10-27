package audit

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/cmd/util"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/steps"
	"github.com/jpvelasco/fundamentum/internal/templates"
)

// stubGhTokenFailure makes token resolution deterministic on dev machines
// with an authenticated gh CLI (the fallback would otherwise provide a token).
func stubGhTokenFailure(t *testing.T) {
	t.Helper()
	restore := util.GhAuthToken
	util.GhAuthToken = func() (string, error) { return "", errors.New("no gh") }
	t.Cleanup(func() { util.GhAuthToken = restore })
}

func resetAuditGlobals(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		globals.SkipSteps = nil
		globals.OnlySteps = nil
		globals.Octopus = false
		globals.CodacyInstructions = false
		globals.BranchUpToDate = true
		globals.BranchUpToDateSet = false
		globals.Strict = false
		globals.AdvancedSecurity = false
		globals.Preset = ""
		globals.FromFile = ""
		globals.BotManaged = false
		globals.PolicyChecks = nil
	})
}

func TestNewCmd(t *testing.T) {
	cmd := NewCmd()
	if cmd.Use != "audit OWNER/REPO" {
		t.Errorf("Use = %q, want audit OWNER/REPO", cmd.Use)
	}
}

func TestRun_MissingToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Cleanup(func() { globals.Token = "" })
	globals.Token = ""
	stubGhTokenFailure(t)
	cmd := NewCmd()
	cmd.SetArgs([]string{"owner/repo"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "GitHub token is required") {
		t.Fatalf("expected missing-token error, got %v", err)
	}
}

func TestRun_InvalidArg(t *testing.T) {
	cmd := NewCmd()
	cmd.SetArgs([]string{"norepo"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected invalid OWNER/REPO error")
	}
}

// passingAuditHandler serves a repo that fully matches the baseline: the
// files are served with exactly the content the audit's rendered templates
// expect (the handler renders the same data), rulesets are at-intent, and
// security features are on.
func passingAuditHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		writePassingAudit(t, w, r, true, true)
	}
}

func TestAudit_PassingRepo(t *testing.T) {
	resetAuditGlobals(t)
	srv := httptest.NewServer(passingAuditHandler(t))
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)

	var out strings.Builder
	if err := runWithClient(c, "owner", "repo", &out); err != nil {
		t.Fatalf("runWithClient() error: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "PASS") {
		t.Errorf("expected PASS rows, got:\n%s", got)
	}
	if strings.Contains(got, "FAIL") {
		t.Errorf("expected no FAIL rows, got:\n%s", got)
	}
	if strings.Contains(got, "content differs") {
		t.Errorf("matching content must not warn, got:\n%s", got)
	}
}

func TestAudit_DriftFails(t *testing.T) {
	resetAuditGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writePassingAudit(t, w, r, false, true)
	}))
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)

	var out strings.Builder
	if err := runWithClient(c, "owner", "repo", &out); err == nil {
		t.Fatal("expected non-zero exit on protect-main drift")
	}
	if !strings.Contains(out.String(), "FAIL") {
		t.Errorf("expected FAIL in report, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "weaker than baseline") {
		t.Errorf("expected weaker-than-baseline detail, got:\n%s", out.String())
	}
}

func TestAudit_StricterRulesetPasses(t *testing.T) {
	// A hand-tuned ruleset that is STRICTER than the baseline (higher approval
	// count, extra unowned rule, extra required checks) must PASS.
	resetAuditGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/rulesets/1") {
			_, _ = w.Write([]byte(`{"id":1,"name":"protect-main","target":"branch","enforcement":"active","conditions":{"ref_name":{"include":["~DEFAULT_BRANCH"]}},"rules":[{"type":"required_linear_history"},{"type":"deletion"},{"type":"non_fast_forward"},{"type":"pull_request","parameters":{"required_approving_review_count":2,"dismiss_stale_reviews_on_push":true,"require_code_owner_review":true,"require_last_push_approval":true,"required_review_thread_resolution":true}},{"type":"required_status_checks","parameters":{"strict_required_status_checks_policy":true,"required_status_checks":[{"context":"Lint","integration_id":99},{"context":"Vulnerability scan"},{"context":"Build (ubuntu-latest)"},{"context":"Build (windows-latest)"},{"context":"Test (ubuntu-latest)"},{"context":"Test (windows-latest)"},{"context":"gosec"},{"context":"Trivy"},{"context":"Codacy","integration_id":77}]}}]}`))
			return
		}
		writePassingAudit(t, w, r, true, true)
	}))
	defer srv.Close()

	var out strings.Builder
	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	if err := runWithClient(c, "owner", "repo", &out); err != nil {
		t.Fatalf("a stricter ruleset must pass, got: %v\n%s", err, out.String())
	}
}

func TestAudit_TagDriftWarnsOnly(t *testing.T) {
	// Optional drift (tag ruleset) is a WARN: no failure without --strict,
	// failure with it.
	resetAuditGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writePassingAudit(t, w, r, true, false)
	}))
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)

	var out strings.Builder
	if err := runWithClient(c, "owner", "repo", &out); err != nil {
		t.Fatalf("tag drift must not fail without --strict: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "WARN") {
		t.Errorf("expected WARN for tag drift, got:\n%s", out.String())
	}

	globals.Strict = true
	out.Reset()
	if err := runWithClient(c, "owner", "repo", &out); err == nil {
		t.Fatal("--strict must fail when an optional check drifts")
	}
}

func TestAudit_ContentDriftWarns(t *testing.T) {
	// A shipped file whose content drifted from the template is a WARN with
	// a diffstat — noticed, but only --strict escalates it.
	resetAuditGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/contents/.github/CODEOWNERS") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"` + base64.StdEncoding.EncodeToString([]byte("* @someone-else\n")) + `","sha":"s"}`))
			return
		}
		writePassingAudit(t, w, r, true, true)
	}))
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)

	var out strings.Builder
	if err := runWithClient(c, "owner", "repo", &out); err != nil {
		t.Fatalf("content drift must not fail without --strict: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "content differs from template") {
		t.Errorf("expected content-drift WARN, got:\n%s", out.String())
	}

	globals.Strict = true
	out.Reset()
	if err := runWithClient(c, "owner", "repo", &out); err == nil {
		t.Fatal("--strict must fail on content drift")
	}
}

func TestAudit_MissingFileFails(t *testing.T) {
	resetAuditGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/contents/.github/CODEOWNERS") ||
			strings.HasSuffix(r.URL.Path, "/contents/CODEOWNERS") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writePassingAudit(t, w, r, true, true)
	}))
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)

	var out strings.Builder
	if err := runWithClient(c, "owner", "repo", &out); err == nil {
		t.Fatal("a missing baseline file must fail the audit")
	}
	if !strings.Contains(out.String(), "CODEOWNERS") {
		t.Errorf("expected CODEOWNERS row, got:\n%s", out.String())
	}
}

func TestAudit_AliasFileCountsAsPresent(t *testing.T) {
	// The canonical .github/CODEOWNERS is missing but the root CODEOWNERS
	// alias exists — audit must report PASS with "alias path in use".
	resetAuditGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/contents/.github/CODEOWNERS"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/contents/CODEOWNERS"):
			_, _ = w.Write([]byte(`{"content":"","sha":"x"}`))
		default:
			writePassingAudit(t, w, r, true, true)
		}
	}))
	defer srv.Close()

	var out strings.Builder
	if err := runWithClient(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", &out); err != nil {
		t.Fatalf("root CODEOWNERS alias must satisfy audit: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "alias path in use") {
		t.Errorf("expected alias PASS row, got:\n%s", out.String())
	}
}

func TestAudit_FromMissingBaseline(t *testing.T) {
	t.Cleanup(func() { globals.FromFile = "" })
	globals.FromFile = t.TempDir() + "/missing.json"
	srv := httptest.NewServer(passingAuditHandler(t))
	defer srv.Close()
	err := runWithClient(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "read baseline") {
		t.Fatalf("error = %v, want read baseline", err)
	}
}

func TestAudit_InvalidPreset(t *testing.T) {
	resetAuditGlobals(t)
	t.Cleanup(func() { globals.Preset = "" })
	globals.Preset = "enterprise"
	srv := httptest.NewServer(passingAuditHandler(t))
	defer srv.Close()
	err := runWithClient(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "invalid --preset") {
		t.Fatalf("error = %v, want invalid --preset", err)
	}
}

func TestAudit_InvalidCIPack(t *testing.T) {
	t.Cleanup(func() { globals.CIPack = "" })
	globals.CIPack = "java"
	srv := httptest.NewServer(passingAuditHandler(t))
	defer srv.Close()
	err := runWithClient(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "invalid --ci") {
		t.Fatalf("error = %v, want invalid --ci", err)
	}
}

func TestAudit_GoModDetectError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/contents/go.mod") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		writePassingAudit(t, w, r, true, true)
	}))
	defer srv.Close()
	err := runWithClient(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "detect go.mod") {
		t.Fatalf("error = %v, want detect go.mod", err)
	}
}

func TestAudit_PreflightErrors(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		status int
		want   string
	}{
		{"ruleset", "/repos/owner/repo/rulesets", http.StatusForbidden, "check branch ruleset"},
		{"tag ruleset", "/repos/owner/repo/rulesets/2", http.StatusForbidden, "check tag ruleset"},
		{"classic", "/repos/owner/repo/branches/main/protection", http.StatusForbidden, "check classic protection"},
		{"dependabot", "/repos/owner/repo/vulnerability-alerts", http.StatusForbidden, "check dependabot"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetAuditGlobals(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tt.path {
					w.WriteHeader(tt.status)
					return
				}
				writePassingAudit(t, w, r, true, true)
			}))
			defer srv.Close()
			err := runWithClient(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", &strings.Builder{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

// TestAudit_RulesetsUnavailableOnFreeTier verifies a 403 "upgrade to GitHub
// Pro" on the ruleset pre-flight does not abort the audit: the report shows
// SKIP (plan-gated) rows instead of failing on "missing".
func TestAudit_RulesetsUnavailableOnFreeTier(t *testing.T) {
	resetAuditGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/rulesets" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Upgrade to GitHub Pro or make this repository public to enable this feature.","status":"403"}`))
			return
		}
		if r.URL.Path == "/repos/owner/repo/contents/"+github.FreeTierGuardPath {
			// The free-tier guard is present on this repo.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"","sha":"g"}`))
			return
		}
		writePassingAudit(t, w, r, true, true)
	}))
	defer srv.Close()

	var out strings.Builder
	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	err := runWithClient(c, "owner", "repo", &out)
	got := out.String()
	if !strings.Contains(got, "protect-main") || !strings.Contains(got, "protect-version-tags") {
		t.Errorf("expected report with both ruleset rows, got:\n%s", got)
	}
	// Plan-gated protection is judged by the free-tier Actions guard: with
	// the guard present (the mock serves it), protect-main PASSes via the
	// guard and the tag ruleset SKIPs as plan-unavailable.
	if !strings.Contains(got, "enforced via free-tier Actions guard") {
		t.Errorf("plan-gated protection must PASS via the guard, got:\n%s", got)
	}
	if !strings.Contains(got, "SKIP") {
		t.Errorf("tag ruleset must report SKIP, got:\n%s", got)
	}
	if err != nil {
		t.Fatalf("plan-gated protection with the guard must not fail the audit: %v", err)
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

func TestAudit_SkipStepsReportSKIP(t *testing.T) {
	resetAuditGlobals(t)
	globals.SkipSteps = []string{"protect-version-tags", "security"}
	srv := httptest.NewServer(passingAuditHandler(t))
	defer srv.Close()

	var out strings.Builder
	if err := runWithClient(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", &out); err != nil {
		t.Fatalf("skipped steps must not fail: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "SKIP") {
		t.Errorf("expected SKIP rows, got:\n%s", got)
	}
	if strings.Contains(got, "protect-version-tags  PASS") {
		t.Errorf("skipped tag ruleset must not PASS, got:\n%s", got)
	}
}

func TestAudit_SkipFileReportsSKIP(t *testing.T) {
	resetAuditGlobals(t)
	globals.SkipSteps = []string{"socket.yml"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/contents/socket.yml") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writePassingAudit(t, w, r, true, true)
	}))
	defer srv.Close()

	var out strings.Builder
	if err := runWithClient(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", &out); err != nil {
		t.Fatalf("an opted-out file must not fail even when missing: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "socket.yml") || !strings.Contains(out.String(), "SKIP") {
		t.Errorf("expected socket.yml SKIP row, got:\n%s", out.String())
	}
}

func TestAudit_SecretScanningPrivateSkips(t *testing.T) {
	resetAuditGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo" {
			_, _ = w.Write([]byte(`{"visibility":"private","default_branch":"main","delete_branch_on_merge":true,"owner":{"type":"User"},"security_and_analysis":{"secret_scanning":{"status":"enabled"},"secret_scanning_push_protection":{"status":"enabled"}}}`))
			return
		}
		writePassingAuditVisibility(t, w, r, true, true, "private")
	}))
	defer srv.Close()

	var out strings.Builder
	if err := runWithClient(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", &out); err != nil {
		t.Fatalf("private repo audit error: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "secret scanning") || !strings.Contains(out.String(), "SKIP") {
		t.Errorf("secret scanning on a private repo must SKIP, got:\n%s", out.String())
	}
}

func TestBranchCheck(t *testing.T) {
	selector := steps.New(nil, nil)
	if got := branchCheck(github.RulesetPlan{Exists: true}, false, false, false, selector); got.Status != statusPass {
		t.Errorf("at-intent ruleset = %+v, want PASS", got)
	}
	if got := branchCheck(github.RulesetPlan{Exists: true, Drift: []string{"enforcement"}}, false, false, false, selector); got.Status != statusFail || got.Detail == "" {
		t.Errorf("weaker ruleset = %+v, want FAIL with detail", got)
	}
	if got := branchCheck(github.RulesetPlan{}, true, false, false, selector); got.Status != statusFail || got.Detail != "classic protection only" {
		t.Errorf("classic = %+v, want FAIL classic protection only", got)
	}
	if got := branchCheck(github.RulesetPlan{}, false, false, false, selector); got.Status != statusFail || got.Detail != "missing" {
		t.Errorf("missing = %+v, want FAIL missing", got)
	}
	// Plan-gated + guard = PASS via the Actions guard.
	if got := branchCheck(github.RulesetPlan{}, false, true, true, selector); got.Status != statusPass || !strings.Contains(got.Detail, "free-tier Actions guard") {
		t.Errorf("plan-gated with guard = %+v, want PASS via guard", got)
	}
	// Plan-gated without the guard = FAIL with re-apply guidance.
	if got := branchCheck(github.RulesetPlan{}, false, true, false, selector); got.Status != statusFail || !strings.Contains(got.Detail, "re-apply") {
		t.Errorf("plan-gated without guard = %+v, want FAIL with guidance", got)
	}
	skipSel := steps.New([]string{"protect-main"}, nil)
	if got := branchCheck(github.RulesetPlan{}, false, false, false, skipSel); got.Status != statusSkip {
		t.Errorf("opted out = %+v, want SKIP", got)
	}
}

func TestTagCheck(t *testing.T) {
	selector := steps.New(nil, nil)
	if got := tagCheck(github.RulesetPlan{Exists: true}, false, selector); got.Status != statusPass {
		t.Errorf("match = %+v, want PASS", got)
	}
	if got := tagCheck(github.RulesetPlan{}, false, selector); got.Status != statusWarn || got.Detail != "missing" {
		t.Errorf("missing = %+v, want WARN missing", got)
	}
	if got := tagCheck(github.RulesetPlan{Exists: true, Drift: []string{"enforcement"}}, false, selector); got.Status != statusWarn {
		t.Errorf("drift = %+v, want WARN", got)
	}
}

func TestSecretCheck(t *testing.T) {
	selector := steps.New(nil, nil)
	if got := secretCheck(github.Repo{Visibility: "private"}, selector); got.Status != statusSkip {
		t.Errorf("private without GHAS = %+v, want SKIP", got)
	}
	if got := secretCheck(github.Repo{Visibility: "public", SecretScanning: true, SecretPushProtection: true}, selector); got.Status != statusPass {
		t.Errorf("public enabled = %+v, want PASS", got)
	}
	if got := secretCheck(github.Repo{Visibility: "public"}, selector); got.Status != statusWarn {
		t.Errorf("public disabled = %+v, want WARN", got)
	}
}

func TestFailedRequired(t *testing.T) {
	resetAuditGlobals(t)
	if failedRequired([]check{{Status: statusPass}}) {
		t.Error("all pass must not fail")
	}
	if !failedRequired([]check{{Status: statusFail}}) {
		t.Error("FAIL must fail")
	}
	if failedRequired([]check{{Status: statusWarn}}) {
		t.Error("WARN must not fail without --strict")
	}
	globals.Strict = true
	if !failedRequired([]check{{Status: statusWarn}}) {
		t.Error("WARN must fail with --strict")
	}
	if failedRequired([]check{{Status: statusSkip}}) {
		t.Error("SKIP must never fail")
	}
}

func TestFormatReport(t *testing.T) {
	got := formatReport("o", "r", []check{
		{Name: "protect-main", Status: statusPass},
		{Name: "tag ruleset", Status: statusWarn, Detail: "drift: enforcement"},
		{Name: "skipped step", Status: statusSkip, Detail: "opted out (--skip)"},
		{Name: "Dependabot alerts", Status: statusFail, Detail: "not enabled"},
	})
	for _, want := range []string{"PASS", "FAIL", "WARN", "SKIP", "1 failed", "1 warn", "1 skipped"} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q:\n%s", want, got)
		}
	}
}

// writePassingAudit serves a repo that matches the baseline. When t is nil
// the file contents are served as short non-matching bodies (WARN rows) —
// tests that assert on FAIL/SKIP logic tolerate the extra warns. With t set,
// the handler renders the same templates the audit renders and serves exact
// content, so file rows PASS.
func writePassingAudit(t *testing.T, w http.ResponseWriter, r *http.Request, healthyBranch, healthyTag bool) {
	writePassingAuditVisibility(t, w, r, healthyBranch, healthyTag, "public")
}

// writePassingAuditVisibility is writePassingAudit with an explicit repo visibility
// so the file content renderer matches the audit's rendered template set.
func writePassingAuditVisibility(t *testing.T, w http.ResponseWriter, r *http.Request, healthyBranch, healthyTag bool, visibility string) {
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
	case r.URL.Path == "/repos/owner/repo/contents/go.mod":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":"bW9kdWxlIGdvCg==","sha":"g"}`))
	case r.URL.Path == "/repos/owner/repo/rulesets":
		_, _ = w.Write([]byte(`[{"id":1,"name":"protect-main"},{"id":2,"name":"protect-version-tags"}]`))
	case r.URL.Path == "/repos/owner/repo/rulesets/1":
		_, _ = w.Write([]byte(`{"id":1,"name":"protect-main","target":"branch","enforcement":"` + enforcement + `","conditions":{"ref_name":{"include":["~DEFAULT_BRANCH"]}},"rules":[{"type":"deletion"},{"type":"non_fast_forward"},{"type":"pull_request","parameters":{"required_review_thread_resolution":true,"require_code_owner_review":false,"dismiss_stale_reviews_on_push":false}},{"type":"required_status_checks","parameters":{"strict_required_status_checks_policy":true,"required_status_checks":[{"context":"Lint"},{"context":"Vulnerability scan"},{"context":"Build (ubuntu-latest)"},{"context":"Build (windows-latest)"},{"context":"Test (ubuntu-latest)"},{"context":"Test (windows-latest)"},{"context":"gosec"},{"context":"Trivy"}]}}]}`))
	case r.URL.Path == "/repos/owner/repo/rulesets/2":
		_, _ = w.Write([]byte(`{"id":2,"name":"protect-version-tags","target":"tag","enforcement":"` + tagEnforcement + `","conditions":{"ref_name":{"include":["refs/tags/v*"]}},"rules":[{"type":"deletion"},{"type":"non_fast_forward"}]}`))
	case strings.Contains(r.URL.Path, "/vulnerability-alerts"):
		w.WriteHeader(http.StatusNoContent)
	case strings.Contains(r.URL.Path, "/automated-security-fixes"):
		w.WriteHeader(http.StatusNoContent)
	case strings.Contains(r.URL.Path, "/code-scanning/default-setup"):
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"state":"configured"}`))
	case strings.Contains(r.URL.Path, "/branches/main/protection"):
		w.WriteHeader(http.StatusNotFound)
	case strings.Contains(r.URL.Path, "/contents/"):
		writeMatchingContent(t, w, r, visibility)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// writeMatchingContent serves the rendered template content for a file path
// so the audit's FileStatusDetail compares equal (PASS). With t nil it
// serves a generic body (drift rows).
func writeMatchingContent(t *testing.T, w http.ResponseWriter, r *http.Request, visibility string) {
	if t == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":"eHg=","sha":"x"}`))
		return
	}
	// The audit renders with defaults: go pack (go.mod served), main branch,
	// README.md and AGENTS.md (both 404 → AGENTS.md default line).
	data := templates.RepoData{
		Owner:          "owner",
		RepoName:       "repo",
		DefaultBranch:  "main",
		Visibility:     visibility,
		CIPack:         "go",
		CodeOwnerLine:  "* @owner",
		ReadmePath:     "README.md",
		AgentGuideLine: templates.DetectAgentGuideLine(false, false),
	}
	files, err := templates.Render(data)
	if err != nil {
		t.Errorf("mock render: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	for _, f := range files {
		if strings.HasSuffix(r.URL.Path, "/contents/"+f.Path) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"` + base64.StdEncoding.EncodeToString([]byte(f.Content)) + `","sha":"x"}`))
			return
		}
	}
	// Not a shipped file (a detection probe): report missing.
	w.WriteHeader(http.StatusNotFound)
}

func TestAudit_FileProbeErrorFailsClosed(t *testing.T) {
	// A file status-check error must FAIL the audit, not WARN: an audit that
	// could not verify a baseline file must not pass silently.
	resetAuditGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/contents/.github/CODEOWNERS" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		writePassingAudit(t, w, r, true, true)
	}))
	defer srv.Close()

	var out strings.Builder
	err := runWithClient(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", &out)
	if err == nil {
		t.Fatalf("a probe 403 must fail the audit:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "status check failed") {
		t.Errorf("report must name the probe failure:\n%s", out.String())
	}
}

func TestAudit_UnknownSkipKeyErrors(t *testing.T) {
	resetAuditGlobals(t)
	globals.SkipSteps = []string{"octopus.yml"} // bare filename: never matches the target path
	srv := httptest.NewServer(passingAuditHandler(t))
	defer srv.Close()

	err := runWithClient(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "unknown --skip/--only key(s): octopus.yml") {
		t.Fatalf("a token that matches nothing must error, got: %v", err)
	}
}

// botManagedProbe is the configurable payload set for the bot-managed audit
// probe endpoints, so the compliant and gaps handlers share one router.
type botManagedProbe struct {
	perms   string
	countBlob   string
	envs         string
	envDetail    string
	workflowList string
	workflowFile string // path → served for the single workflow file, empty for none
	workflowBody string // base64 content for workflowFile
}

// botManagedHandler routes the bot-managed audit probe endpoints from p.
func botManagedHandler(p botManagedProbe) http.HandlerFunc {
	write := func(w http.ResponseWriter, body string) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/actions/permissions/workflow"):
			write(w, p.perms)
		case strings.Contains(r.URL.Path, "/actions/secrets"):
			write(w, p.countBlob)
		case r.URL.Path == "/repos/owner/repo/environments":
			write(w, p.envs)
		case strings.Contains(r.URL.Path, "/environments/"):
			write(w, p.envDetail)
		case p.workflowFile != "" && strings.Contains(r.URL.Path, "/contents/"+p.workflowFile):
			write(w, `{"content":"`+p.workflowBody+`"}`)
		case strings.Contains(r.URL.Path, "/contents/.github/workflows"):
			write(w, p.workflowList)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// compliantBotManagedProbe is a fully-hardened repo: read-only token, no
// secrets, one default-branch-only environment, no workflows.
var compliantBotManagedProbe = botManagedProbe{
	perms:   `{"default_workflow_permissions":"read","can_approve_pull_request_reviews":false}`,
	countBlob:  `{"total_count":0,"secrets":[]}`,
	envs:         `{"environments":[{"name":"codacy"}]}`,
	envDetail:    `{"protection_rules":[{"type":"branch_policy","branch_name_pattern_policy":[{"name":"main"}]}]}`,
	workflowList: `[]`,
}

// gapBotManagedProbe is an unhardened repo: writable self-approving token,
// four secrets, no environments, one pull_request_target workflow.
var gapBotManagedProbe = botManagedProbe{
	perms:   `{"default_workflow_permissions":"write","can_approve_pull_request_reviews":true}`,
	countBlob:  `{"total_count":4,"secrets":[]}`,
	envs:         `{"environments":[]}`,
	workflowList: `[{"type":"file","path":".github/workflows/bad.yml"}]`,
	workflowFile: ".github/workflows/bad.yml",
	// base64 of "on:\n  pull_request_target:\n"
	workflowBody: "b246CiAgcHVsbF9yZXF1ZXN0X3RhcmdldDoK",
}

func TestBotManagedChecks_Compliant(t *testing.T) {
	srv := httptest.NewServer(botManagedHandler(compliantBotManagedProbe))
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	results := botManagedChecks(c, "owner", "repo", "main")
	byName := map[string]check{}
	for _, r := range results {
		byName[r.Name] = r
	}
	if byName["actions default token"].Status != statusPass {
		t.Errorf("compliant actions token must PASS: %+v", byName["actions default token"])
	}
	if byName["repo secrets"].Status != statusPass {
		t.Errorf("zero secrets must PASS: %+v", byName["repo secrets"])
	}
	if byName["environment codacy"].Status != statusPass {
		t.Errorf("default-branch-only env must PASS: %+v", byName["environment codacy"])
	}
	if byName["workflow scan"].Status != statusPass {
		t.Errorf("clean workflow scan must PASS: %+v", byName["workflow scan"])
	}
}

func TestBotManagedChecks_Gaps(t *testing.T) {
	srv := httptest.NewServer(botManagedHandler(gapBotManagedProbe))
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	results := botManagedChecks(c, "owner", "repo", "main")
	byName := map[string]check{}
	for _, r := range results {
		byName[r.Name] = r
	}
	if byName["actions default token"].Status != statusWarn {
		t.Errorf("write+approve token must WARN: %+v", byName["actions default token"])
	}
	if byName["repo secrets"].Status != statusWarn {
		t.Errorf("4 secrets must WARN: %+v", byName["repo secrets"])
	}
	if byName["environments"].Status != statusSkip {
		t.Errorf("no environments must SKIP: %+v", byName["environments"])
	}
	// The pull_request_target workflow must surface as a WARN finding.
	found := false
	for _, r := range results {
		if r.Name == "workflow .github/workflows/bad.yml" && r.Status == statusWarn {
			found = true
		}
	}
	if !found {
		t.Errorf("pull_request_target workflow must WARN: %+v", results)
	}
	// Report-only: nothing in the bot-managed section FAILs the run.
	for _, r := range results {
		if r.Status == statusFail {
			t.Errorf("bot-managed audit is report-only; %q must not FAIL", r.Name)
		}
	}
}
