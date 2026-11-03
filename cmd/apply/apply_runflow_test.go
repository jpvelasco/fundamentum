package apply

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/templates"
)

func writeGoModPresent(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/contents/go.mod") {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"sha":"gomod"}`))
		return true
	}
	return false
}

// newRunFlowServer returns a test server that mocks the full run() flow for a
// public repo with no existing protection or files: visibility check, ruleset
// checks, classic protection, file status/upsert, settings, and security.

func newRunFlowServer() *httptest.Server {
	return httptest.NewServer(runFlowHandler())
}

func runFlowHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if writeGoModPresent(w, r) {
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"visibility":"public"}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rulesets"):
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/branches/main/protection"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/branches/main"):
			// PR-mode flow: CreatePRBranch reads the branch head SHA
			// (ordered AFTER the protection probe — the probe's longer path
			// must match first).
			_, _ = w.Write([]byte(`{"commit":{"sha":"abc123"}}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/refs") && !strings.Contains(r.URL.Path, "/heads/"):
			// PR-mode flow: CreatePRBranch creates the harden branch.
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/harden-"):
			// PR-mode flow: CommitFiles reads the harden branch head.
			_, _ = w.Write([]byte(`{"object":{"sha":"abc123"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/commits/"):
			// PR-mode flow: CommitFiles reads the base commit's tree.
			_, _ = w.Write([]byte(`{"tree":{"sha":"treesha"}}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/blobs"):
			// PR-mode flow: CommitFiles uploads blobs for non-workflow files.
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"blobsha"}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/trees"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"newtree"}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/commits"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"newcommit"}`))
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/git/refs/heads/harden-"):
			// PR-mode flow: CommitFiles fast-forwards the harden branch.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/pulls"):
			// PR-mode flow: the batch PR is opened.
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":42}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"content":{}}`))
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/vulnerability-alerts"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/automated-security-fixes"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/owner/repo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/rulesets"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func existingRulesetHandler() http.HandlerFunc {
	flow := runFlowHandler()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			switch r.URL.Path {
			case "/repos/owner/repo/rulesets":
				_, _ = w.Write([]byte(`[{"id":1,"name":"protect-main"},{"id":2,"name":"protect-version-tags"}]`))
				return
			case "/repos/owner/repo/rulesets/1":
				_, _ = w.Write([]byte(`{"id":1,"name":"protect-main","target":"branch","enforcement":"active","conditions":{"ref_name":{"include":["~DEFAULT_BRANCH"]}},"rules":[{"type":"deletion"},{"type":"non_fast_forward"},{"type":"pull_request","parameters":{"required_review_thread_resolution":true,"require_code_owner_review":false,"dismiss_stale_reviews_on_push":false}},{"type":"required_status_checks","parameters":{"strict_required_status_checks_policy":true,"required_status_checks":[{"context":"Lint"},{"context":"Vulnerability scan"},{"context":"Build (ubuntu-latest)"},{"context":"Build (windows-latest)"},{"context":"Test (ubuntu-latest)"},{"context":"Test (windows-latest)"},{"context":"gosec"},{"context":"Trivy"}]}}]}`))
				return
			case "/repos/owner/repo/rulesets/2":
				_, _ = w.Write([]byte(`{"id":2,"name":"protect-version-tags","target":"tag","enforcement":"active","conditions":{"ref_name":{"include":["refs/tags/v*"]}},"rules":[{"type":"deletion"},{"type":"non_fast_forward"}]}`))
				return
			}
		}
		flow.ServeHTTP(w, r)
	}
}

// TestRunWithClient_ExistingRulesetInfersSolo verifies an existing ruleset
// suppresses the solo/team prompt.

func TestRunWithClient_ExistingRulesetInfersSolo(t *testing.T) {
	srv := httptest.NewServer(existingRulesetHandler())
	defer srv.Close()

	var out strings.Builder
	err := runWithClient(newTestClient(srv), "owner", "repo", newLineReader("y\n"), &out)
	if err != nil {
		t.Fatalf("runWithClient() error: %v", err)
	}
	if strings.Contains(out.String(), "Project type?") {
		t.Error("existing ruleset must not prompt for solo/team")
	}
}

func TestRunWithClient_FullFlow(t *testing.T) {
	srv := newRunFlowServer()
	defer srv.Close()
	c := newTestClient(srv)

	var out strings.Builder
	err := runWithClient(c, "owner", "repo", newLineReader("solo\ny\n"), &out)
	if err != nil {
		t.Fatalf("runWithClient() error: %v", err)
	}
	if !strings.Contains(out.String(), "✓ Done — https://github.com/owner/repo") {
		t.Errorf("expected done message, got:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Dry run complete") {
		t.Error("unexpected dry-run message in live flow")
	}
}

// TestRunWithClient_InteractiveDecline verifies that declining the defaults
// prompt still applies the (interactively accepted) plan, including Done.

func TestRunWithClient_InteractiveDecline(t *testing.T) {
	srv := newRunFlowServer()
	defer srv.Close()
	c := newTestClient(srv)

	var out strings.Builder
	// Decline defaults; empty answers to per-item prompts mean accept.
	err := runWithClient(c, "owner", "repo", newLineReader("solo\nn\n"), &out)
	if err != nil {
		t.Fatalf("runWithClient() error: %v", err)
	}
	if !strings.Contains(out.String(), "Apply all defaults? [Y/n]") {
		t.Errorf("expected defaults prompt, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "✓ Done") {
		t.Errorf("expected apply after interactive selection, got:\n%s", out.String())
	}
}

func TestRunWithClient_InteractiveViaPR(t *testing.T) {
	t.Cleanup(func() { globals.ViaPR = false })
	globals.ViaPR = true

	var createdPR bool
	flow := runFlowHandler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/pulls") {
			createdPR = true
		}
		flow.ServeHTTP(w, r)
	}))
	defer srv.Close()
	c := newTestClient(srv)

	input := "solo\nn\n" + strings.Repeat("y\n", 40)
	var out strings.Builder
	err := runWithClient(c, "owner", "repo", newLineReader(input), &out)
	if err != nil {
		t.Fatalf("runWithClient() error: %v", err)
	}
	if !createdPR {
		t.Error("expected --pr to open a pull request after interactive selection")
	}
	if !strings.Contains(out.String(), "--pr: file changes go in a pull request") {
		t.Errorf("live --pr apply must print the live-settings notice, got:\n%s", out.String())
	}
}

// TestRunWithClient_DryRun verifies the dry-run path is non-interactive
// (no prompts) and reports the plan counts without making changes.

func TestRunWithClient_DryRun(t *testing.T) {
	t.Cleanup(func() { globals.DryRun = false })
	globals.DryRun = true

	srv := newRunFlowServer()
	defer srv.Close()
	c := newTestClient(srv)

	var out strings.Builder
	// No input at all: dry-run must not wait for prompts.
	err := runWithClient(c, "owner", "repo", strings.NewReader(""), &out)
	if err != nil {
		t.Fatalf("runWithClient() error: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "Dry run complete — ") || !strings.Contains(outStr, "no changes made.") {
		t.Errorf("expected dry-run summary, got:\n%s", outStr)
	}
	if strings.Contains(outStr, "Apply all defaults?") {
		t.Errorf("dry-run must not prompt for defaults, got:\n%s", outStr)
	}
	if strings.Contains(outStr, "Project type?") {
		t.Errorf("dry-run must not prompt for project type, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "would create") {
		t.Errorf("expected dry-run labels in plan, got:\n%s", outStr)
	}
	if strings.Contains(outStr, "--pr: file changes go in a pull request") {
		t.Errorf("dry-run without --pr must not print the PR-mode notice, got:\n%s", outStr)
	}
}

func TestRunWithClient_DryRunPRModeNotice(t *testing.T) {
	t.Cleanup(func() {
		globals.DryRun = false
		globals.ViaPR = false
	})
	globals.DryRun = true
	globals.ViaPR = true

	srv := newRunFlowServer()
	defer srv.Close()
	var out strings.Builder
	if err := runWithClient(newTestClient(srv), "owner", "repo", strings.NewReader(""), &out); err != nil {
		t.Fatalf("runWithClient() error: %v", err)
	}
	if !strings.Contains(out.String(), "--pr: file changes go in a pull request") {
		t.Errorf("expected --pr live-settings notice, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "still apply live via the API") {
		t.Errorf("expected live-API wording, got:\n%s", out.String())
	}
}

func TestRunWithClient_PresetSkipsPrompts(t *testing.T) {
	t.Cleanup(func() { globals.Preset = "" })
	globals.Preset = "oss"

	srv := newRunFlowServer()
	defer srv.Close()
	var out strings.Builder
	// Empty stdin: a leftover prompt would hang or take a default after EOF.
	if err := runWithClient(newTestClient(srv), "owner", "repo", strings.NewReader(""), &out); err != nil {
		t.Fatalf("runWithClient() error: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "Project type?") || strings.Contains(got, "Apply all defaults?") {
		t.Errorf("--preset must skip wizard prompts, got:\n%s", got)
	}
	if !strings.Contains(got, "✓ Done") {
		t.Errorf("expected done after --preset apply, got:\n%s", got)
	}
}

func TestRunWithClient_FromBaseline(t *testing.T) {
	t.Cleanup(func() {
		globals.FromFile = ""
		globals.Preset = ""
	})
	path := t.TempDir() + "/baseline.json"
	if err := os.WriteFile(path, []byte(`{"version":1,"preset":"oss"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	globals.FromFile = path

	srv := newRunFlowServer()
	defer srv.Close()
	var out strings.Builder
	if err := runWithClient(newTestClient(srv), "owner", "repo", strings.NewReader(""), &out); err != nil {
		t.Fatalf("runWithClient() error: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "Project type?") || strings.Contains(got, "Apply all defaults?") {
		t.Errorf("--from oss baseline must skip wizard prompts, got:\n%s", got)
	}
	if !strings.Contains(got, "✓ Done") {
		t.Errorf("expected done after --from apply, got:\n%s", got)
	}
}

func TestRunWithClient_FromMissingBaseline(t *testing.T) {
	t.Cleanup(func() { globals.FromFile = "" })
	globals.FromFile = t.TempDir() + "/missing.json"
	srv := newRunFlowServer()
	defer srv.Close()
	err := runWithClient(newTestClient(srv), "owner", "repo", strings.NewReader(""), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "read baseline") {
		t.Fatalf("error = %v, want read baseline", err)
	}
}

func TestRunWithClient_InvalidPreset(t *testing.T) {
	t.Cleanup(func() { globals.Preset = "" })
	globals.Preset = "enterprise"
	srv := newRunFlowServer()
	defer srv.Close()
	err := runWithClient(newTestClient(srv), "owner", "repo", strings.NewReader(""), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "invalid --preset") {
		t.Fatalf("error = %v, want invalid --preset", err)
	}
}

func TestRunWithClient_PresetPrivateSkipsGHAS(t *testing.T) {
	t.Cleanup(func() {
		globals.Preset = ""
		globals.AdvancedSecurity = false
	})
	globals.Preset = "private"

	inner := newRunFlowServer()
	defer inner.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo" {
			_, _ = w.Write([]byte(`{"visibility":"private","default_branch":"main","owner":{"type":"User"}}`))
			return
		}
		inner.Config.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	var out strings.Builder
	if err := runWithClient(newTestClient(srv), "owner", "repo", strings.NewReader(""), &out); err != nil {
		t.Fatalf("runWithClient() error: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "Enable GitHub Advanced Security") {
		t.Errorf("--preset private must not prompt for GHAS, got:\n%s", got)
	}
	if strings.Contains(got, "Security (secret scanning") {
		t.Errorf("--preset private must not enable secret scanning, got:\n%s", got)
	}
}

func TestRunWithClient_PrivatePromptsAdvancedSecurity(t *testing.T) {
	t.Cleanup(func() { globals.AdvancedSecurity = false })
	inner := newRunFlowServer()
	defer inner.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo" {
			_, _ = w.Write([]byte(`{"visibility":"private","default_branch":"main","owner":{"type":"User"}}`))
			return
		}
		inner.Config.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	var out strings.Builder
	err := runWithClient(newTestClient(srv), "owner", "repo", newLineReader("solo\ny\ny\n"), &out)
	if err != nil {
		t.Fatalf("runWithClient() error: %v", err)
	}
	if !strings.Contains(out.String(), "Enable GitHub Advanced Security") {
		t.Errorf("live private apply must prompt for GHAS, got:\n%s", out.String())
	}
}

func TestRunWithClient_DryRunPrivateShowsAdvancedSecurity(t *testing.T) {
	t.Cleanup(func() {
		globals.DryRun = false
		globals.AdvancedSecurity = false
	})
	globals.DryRun = true

	flow := runFlowHandler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo" {
			_, _ = w.Write([]byte(`{"visibility":"private","default_branch":"main","owner":{"type":"User"}}`))
			return
		}
		flow.ServeHTTP(w, r)
	}))
	defer srv.Close()

	var out strings.Builder
	err := runWithClient(newTestClient(srv), "owner", "repo", strings.NewReader(""), &out)
	if err != nil {
		t.Fatalf("runWithClient() error: %v", err)
	}
	if !strings.Contains(out.String(), "secret scanning") {
		t.Errorf("private dry-run must show offered GHAS plan lines, got:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Enable GitHub Advanced Security") {
		t.Errorf("dry-run must not prompt for GHAS, got:\n%s", out.String())
	}
}

func TestRunWithClient_InvalidCIPack(t *testing.T) {
	t.Cleanup(func() { globals.CIPack = "" })
	globals.CIPack = "java"
	srv := newRunFlowServer()
	defer srv.Close()
	err := runWithClient(newTestClient(srv), "owner", "repo", newLineReader("solo\ny\n"), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "invalid --ci") {
		t.Fatalf("error = %v, want invalid --ci", err)
	}
}

func TestRunWithClient_GoModDetectError(t *testing.T) {
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"visibility":"public","default_branch":"main"}`))
		case strings.HasSuffix(r.URL.Path, "/contents/go.mod"):
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	err := runWithClient(c, "owner", "repo", newLineReader("solo\ny\n"), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "detect go.mod") {
		t.Fatalf("error = %v, want detect go.mod", err)
	}
}

// TestRunWithClient_VisibilityError verifies visibility detection failures
// surface a wrapped error.

func TestRunWithClient_VisibilityError(t *testing.T) {
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	err := runWithClient(c, "owner", "repo", newLineReader("solo\ny\n"), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "detect repo visibility") {
		t.Errorf("expected visibility error, got: %v", err)
	}
}

// TestRunWithClient_RulesetError verifies ruleset pre-flight failures surface
// a wrapped error.

func TestRunWithClient_RulesetError(t *testing.T) {
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/rulesets") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not json`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"visibility":"public"}`))
	}))
	defer srv.Close()

	err := runWithClient(c, "owner", "repo", newLineReader("solo\ny\n"), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "check branch ruleset") {
		t.Errorf("expected ruleset error, got: %v", err)
	}
}

// TestRunWithClient_TagRulesetError verifies the protect-version-tags
// pre-flight failure surfaces a wrapped error.

func TestRunWithClient_TagRulesetError(t *testing.T) {
	rulesetCalls := 0
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/rulesets") {
			rulesetCalls++
			if rulesetCalls == 1 {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not json`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"visibility":"public"}`))
	}))
	defer srv.Close()

	err := runWithClient(c, "owner", "repo", newLineReader("solo\ny\n"), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "check tag ruleset") {
		t.Errorf("expected tag ruleset error, got: %v", err)
	}
}

// rulesetsUnavailableHandler mocks a free-tier private repo whose plan offers
// neither rulesets nor classic branch protection: both probe endpoints respond
// with the GitHub "Upgrade to GitHub Pro" 403.

func rulesetsUnavailableHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo" {
		_, _ = w.Write([]byte(`{"visibility":"private","default_branch":"main","owner":{"type":"User"}}`))
		return
	}
	if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rulesets") {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Upgrade to GitHub Pro or make this repository public to enable this feature.","documentation_url":"https://docs.github.com/rest/repos/rulesets#get-all-repository-rulesets","status":"403"}`))
		return
	}
	if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/protection") {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Upgrade to GitHub Pro or make this repository public to enable this feature.","documentation_url":"https://docs.github.com/rest/branches/branch-protection#get-branch-protection","status":"403"}`))
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

// TestRunWithClient_RulesetsUnavailableOnFreeTier verifies a 403 "upgrade to
// GitHub Pro" on the ruleset pre-flight still builds the rest of the plan,
// but does not claim dry-run would create protection that cannot land.

func TestRunWithClient_RulesetsUnavailableOnFreeTier(t *testing.T) {
	t.Cleanup(func() { globals.DryRun = false; globals.ViaPR = false })
	globals.DryRun = true

	srv := httptest.NewServer(http.HandlerFunc(rulesetsUnavailableHandler))
	defer srv.Close()

	var out strings.Builder
	err := runWithClient(newTestClient(srv), "owner", "repo", strings.NewReader(""), &out)
	if err != nil {
		t.Fatalf("runWithClient() error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Dry run complete — ") {
		t.Errorf("expected dry-run summary, got:\n%s", got)
	}
	// The free-tier guard ships to private repos, so plan-gated protection
	// points at the Actions guard instead of an impossible ruleset create.
	row := summaryRow(t, got, "Branch protection (free-tier Actions guard)")
	if !strings.Contains(row, github.FreeTierGuardPath) {
		t.Errorf("guard row must name the guard workflow, got %q", row)
	}
	tagRow := summaryRow(t, got, "Tag ruleset (protect-version-tags)")
	if strings.Contains(tagRow, "would create") || strings.Contains(tagRow, "already exists") {
		t.Errorf("tag ruleset row must not create or claim existing, got %q", tagRow)
	}
	if !strings.Contains(tagRow, "unavailable") {
		t.Errorf("tag ruleset row = %q, want unavailable", tagRow)
	}
	// The guard reverts direct pushes, so the plan forces PR mode with the
	// guard notice — even without --pr.
	if !strings.Contains(got, "--free-tier-guard") {
		t.Errorf("expected the free-tier guard PR-mode notice, got:\n%s", got)
	}
	if strings.Contains(got, "already exist") {
		t.Errorf("summary must not count unavailable protection as already exist:\n%s", got)
	}
}

// summaryRow returns the plan-table line whose setting name is name.

func summaryRow(t *testing.T, out, name string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, name) {
			return line
		}
	}
	t.Fatalf("missing plan row %q in:\n%s", name, out)
	return ""
}

// TestRunWithClient_RulesetsGeneric403StillFails verifies a non-upgrade 403
// (token scope, SSO, IP allow list) on the ruleset pre-flight still aborts.

func TestRunWithClient_RulesetsGeneric403StillFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"visibility":"private","default_branch":"main","owner":{"type":"User"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rulesets"):
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	err := runWithClient(newTestClient(srv), "owner", "repo", strings.NewReader(""), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "check branch ruleset") {
		t.Errorf("error = %v, want check branch ruleset", err)
	}
}

// TestRunWithClient_ClassicProtectionError verifies classic protection
// pre-flight failures surface a wrapped error. A hijacked connection forces a
// transport-level error on the GET /branches/main/protection request.

func TestRunWithClient_ClassicProtectionError(t *testing.T) {
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/rulesets"):
			_, _ = w.Write([]byte(`[]`))
		case strings.Contains(r.URL.Path, "/branches/main/protection"):
			hj, ok := w.(http.Hijacker)
			if !ok {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				return
			}
			_ = conn.Close()
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"visibility":"public"}`))
		}
	}))
	defer srv.Close()

	err := runWithClient(c, "owner", "repo", newLineReader("solo\ny\n"), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "check classic protection") {
		t.Errorf("expected classic protection error, got: %v", err)
	}
}

// TestRunWithClient_ViaPRFailure verifies that an ApplyViaPR failure inside
// the defaults-apply path propagates the wrapped error (auto-fallback stays
// transparent: the PR error is the reported cause).

func TestRunWithClient_UsesDefaultBranch(t *testing.T) {
	t.Cleanup(func() { globals.ViaPR = false })
	globals.ViaPR = true

	var sawDevelop, sawMain bool
	flow := runFlowHandler()
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"visibility":"public","default_branch":"develop"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/branches/develop":
			sawDevelop = true
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/branches/main":
			sawMain = true
			w.WriteHeader(http.StatusNotFound)
		default:
			flow.ServeHTTP(w, r)
		}
	}))
	defer srv.Close()

	err := runWithClient(c, "owner", "repo", newLineReader("solo\ny\n"), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "apply via PR") {
		t.Errorf("expected apply-via-PR error, got: %v", err)
	}
	if !sawDevelop {
		t.Error("expected PR branch lookup against default branch develop")
	}
	if sawMain {
		t.Error("must not look up hardcoded main when default_branch is develop")
	}
}

func TestRunWithClient_ViaPRFailure(t *testing.T) {
	t.Cleanup(func() { globals.ViaPR = false })
	globals.ViaPR = true

	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if writeGoModPresent(w, r) {
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"visibility":"public"}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rulesets"):
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/branches/main/protection"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	var out strings.Builder
	err := runWithClient(c, "owner", "repo", newLineReader("solo\ny\n"), &out)
	if err == nil || !strings.Contains(err.Error(), "apply via PR") {
		t.Errorf("expected apply-via-PR error, got: %v", err)
	}
}

// TestRun_Success exercises the cobra RunE closure end-to-end with an
// injected mock-server client: valid args, prompts answered, done message.

func TestRunWithClient_RenderError(t *testing.T) {
	t.Cleanup(func() { renderTemplates = templates.Render })
	renderTemplates = func(templates.RepoData) ([]templates.RenderedFile, error) {
		return nil, errors.New("boom")
	}

	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"visibility":"public"}`))
	}))
	defer srv.Close()

	err := runWithClient(c, "owner", "repo", newLineReader("solo\ny\n"), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "render templates") {
		t.Errorf("expected render error, got: %v", err)
	}
}

func TestCodeOwnerLine(t *testing.T) {
	tests := []struct {
		owner string
		org   bool
		want  string
	}{
		{owner: "jpvelasco", org: false, want: "* @jpvelasco"},
		{owner: "acme", org: true, want: "# Organizations need a team (@org/team), not @acme"},
	}
	for _, tt := range tests {
		if got := codeOwnerLine(tt.owner, tt.org); got != tt.want {
			t.Errorf("codeOwnerLine(%q, %v) = %q, want %q", tt.owner, tt.org, got, tt.want)
		}
	}
}

func TestRunWithClient_OrgOwnerSkipsCodeOwners(t *testing.T) {
	t.Cleanup(func() { renderTemplates = templates.Render })
	var gotLine string
	orig := renderTemplates
	renderTemplates = func(data templates.RepoData) ([]templates.RenderedFile, error) {
		gotLine = data.CodeOwnerLine
		return orig(data)
	}

	var requireReview *bool
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if writeGoModPresent(w, r) {
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/repo":
			_, _ = w.Write([]byte(`{"visibility":"public","default_branch":"main","owner":{"type":"Organization"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rulesets"):
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/branches/main/protection"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"content":{}}`))
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/vulnerability-alerts"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/automated-security-fixes"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/repo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/rulesets"):
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Rules []struct {
					Type       string `json:"type"`
					Parameters struct {
						RequireCodeOwnerReview bool `json:"require_code_owner_review"`
					} `json:"parameters"`
				} `json:"rules"`
			}
			if err := json.Unmarshal(body, &payload); err == nil {
				for _, rule := range payload.Rules {
					if rule.Type == "pull_request" {
						v := rule.Parameters.RequireCodeOwnerReview
						requireReview = &v
					}
				}
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	var out strings.Builder
	err := runWithClient(c, "acme", "repo", newLineReader("team\ny\n"), &out)
	if err != nil {
		t.Fatalf("runWithClient() error: %v", err)
	}
	wantLine := "# Organizations need a team (@org/team), not @acme"
	if gotLine != wantLine {
		t.Errorf("CodeOwnerLine = %q, want %q", gotLine, wantLine)
	}
	if requireReview == nil {
		t.Fatal("expected pull_request ruleset to be created")
	}
	if *requireReview {
		t.Error("org owner must not require CODEOWNERS reviews")
	}
}

// TestRunWithClient_FileStatusErrorFailsPlan verifies the run-level wrap: a
// failed pre-flight status check aborts with a "plan owner/repo" error that
// carries the actionable hint, instead of proceeding with a wrong plan.

func TestRunWithClient_FileStatusErrorFailsPlan(t *testing.T) {
	t.Cleanup(func() { globals.DryRun = false })
	globals.DryRun = false

	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if writeGoModPresent(w, r) {
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/repos/owner/repo"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"visibility":"public","default_branch":"main","owner":{"type":"User"}}`))
		case strings.HasSuffix(r.URL.Path, "/rulesets"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/contents/package.json"),
			strings.HasSuffix(r.URL.Path, "/contents/pyproject.toml"),
			strings.HasSuffix(r.URL.Path, "/contents/requirements.txt"),
			strings.HasSuffix(r.URL.Path, "/contents/Cargo.toml"):
			w.WriteHeader(http.StatusNotFound)
		case strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	err := runWithClient(c, "owner", "repo", newLineReader("\n\n"), &strings.Builder{})
	if err == nil {
		t.Fatal("expected plan error when file status check fails, got nil")
	}
	if !strings.Contains(err.Error(), "plan owner/repo") {
		t.Errorf("expected error to name plan owner/repo, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Contents read") {
		t.Errorf("expected actionable hint in error, got: %v", err)
	}
}
