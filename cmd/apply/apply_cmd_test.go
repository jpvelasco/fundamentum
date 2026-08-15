package apply

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/templates"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

// newBuildItemsTest is a shared setup helper for TestBuildItems_* tests.
// It creates a mock HTTP server with the given handler, renders templates with the specified visibility,
// and calls buildItems, returning the resulting items for assertion in the test.
func newBuildItemsTest(t *testing.T, handler http.HandlerFunc, visibility string) []wizard.Item {
	t.Helper()
	return newBuildItemsTestFull(t, handler, visibility, false, false, false)
}

// newBuildItemsTestFull is newBuildItemsTest with explicit control over the
// rulesetExists/tagExists/classicExists inputs to buildItems.
func newBuildItemsTestFull(t *testing.T, handler http.HandlerFunc, visibility string, rulesetExists, tagExists, classicExists bool) []wizard.Item {
	t.Helper()
	srv, c := newTestServer(handler)
	defer srv.Close()
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: visibility}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	return buildItems(c, "owner", "repo", "main", visibility, rendered, rulesetExists, tagExists, classicExists, github.BranchProtectionOptions{}, false)
}

func TestNewCmd(t *testing.T) {
	cmd := NewCmd()
	if cmd.Use != "apply OWNER/REPO" {
		t.Errorf("expected use 'apply OWNER/REPO', got %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("expected non-empty Short")
	}
}

func TestRun_InvalidArg(t *testing.T) {
	cmd := NewCmd()
	cmd.SetArgs([]string{"norepo"})
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid arg")
	}
}

func TestRun_NoArg(t *testing.T) {
	cmd := NewCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for missing arg")
	}
}

func TestBranchProtectionItem_RulesetExists(t *testing.T) {
	item := branchProtectionItem(nil, "owner", "repo", "main", "public", true, false, github.BranchProtectionOptions{})
	if item.Action != wizard.ActionSkip {
		t.Errorf("expected ActionSkip when ruleset exists, got %v", item.Action)
	}
}

func TestBranchProtectionItem_Creation(t *testing.T) {
	// Test branch protection action determination: ActionUpgrade when classic exists,
	// ActionCreate for new repos (both public and private, public is optional).
	tests := []struct {
		name          string
		visibility    string
		rulesetExists bool
		classicExists bool
		wantAction    wizard.Action
		checkOptional bool
		wantOptional  bool // only checked if checkOptional=true
	}{
		{
			name:          "new public repo",
			visibility:    "public",
			rulesetExists: false,
			classicExists: false,
			wantAction:    wizard.ActionCreate,
			checkOptional: true,
			wantOptional:  true,
		},
		{
			name:          "new private repo",
			visibility:    "private",
			rulesetExists: false,
			classicExists: false,
			wantAction:    wizard.ActionCreate,
			checkOptional: false,
		},
		{
			name:          "upgrade from classic",
			visibility:    "public",
			rulesetExists: false,
			classicExists: true,
			wantAction:    wizard.ActionUpgrade,
			checkOptional: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":1}`))
			}), func(c *github.Client) {
				item := branchProtectionItem(c, "owner", "repo", "main", tt.visibility, tt.rulesetExists, tt.classicExists, github.BranchProtectionOptions{})
				if item.Action != tt.wantAction {
					t.Errorf("expected action %v, got %v", tt.wantAction, item.Action)
				}
				if tt.checkOptional && item.Optional != tt.wantOptional {
					t.Errorf("expected Optional=%v, got %v", tt.wantOptional, item.Optional)
				}
			}, nil)
		})
	}
}

func TestBuildItems_Public(t *testing.T) {
	items := newBuildItemsTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}), "public")

	// Check that CodeQL is in the security item name for public repos
	foundSecurity := false
	for _, item := range items {
		if strings.Contains(item.Name, "CodeQL") {
			foundSecurity = true
			break
		}
	}
	if !foundSecurity {
		t.Error("expected CodeQL in security item for public repo")
	}
}

func TestBuildItems_Private(t *testing.T) {
	items := newBuildItemsTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}), "private")

	foundDependabotOnly := false
	for _, item := range items {
		if strings.Contains(item.Name, "CodeQL") {
			t.Error("expected no CodeQL in security item for private repo")
		}
		if strings.Contains(item.Name, "secret scanning") {
			t.Error("expected no secret scanning on private repo without paid opt-in")
		}
		if item.Name == "Security (Dependabot)" {
			foundDependabotOnly = true
		}
	}
	if !foundDependabotOnly {
		t.Error("expected unpaid private security item to be Dependabot-only")
	}
}

func TestBuildItems_PrivatePaidSecurity(t *testing.T) {
	c := github.NewClient("", false)
	items := buildItems(c, "owner", "repo", "main", "private", nil, false, false, false, github.BranchProtectionOptions{}, true)
	found := false
	for _, item := range items {
		if item.Name == "Security (secret scanning, Dependabot)" {
			found = true
		}
		if strings.Contains(item.Name, "CodeQL") {
			t.Error("paid private must still skip CodeQL default-setup name")
		}
	}
	if !found {
		t.Error("expected secret scanning in security item when paid features opted in")
	}
}

func TestBuildItems_TagRulesetExists(t *testing.T) {
	c := github.NewClient("", false)
	items := buildItems(c, "owner", "repo", "main", "private", nil, false, true, false, github.BranchProtectionOptions{}, false)

	for _, item := range items {
		if item.Name == "Tag ruleset (protect-version-tags)" {
			if item.Action != wizard.ActionSkip {
				t.Errorf("expected tag ruleset to be skipped, got %v", item.Action)
			}
		}
	}
}

func TestBuildItems_NoOverwrite(t *testing.T) {
	t.Cleanup(func() { globals.NoOverwrite = false })
	globals.NoOverwrite = true

	items := newBuildItemsTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return existing file with different content
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":"b2xkCg==","sha":"abc"}`))
	}), "private")

	// With --no-overwrite, files that exist should be skipped (not updated)
	for _, item := range items {
		if item.Action == wizard.ActionUpdate {
			t.Error("expected no update actions with --no-overwrite")
		}
	}
}

func TestBuildItems_AliasExists(t *testing.T) {
	t.Cleanup(func() { globals.NoOverwrite = false })

	// Mock: CODEOWNERS at root exists (alias of .github/CODEOWNERS)
	items := newBuildItemsTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/contents/CODEOWNERS") && !strings.Contains(r.URL.Path, ".github") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"d29ybGQ="}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}), "private")

	// CODEOWNERS should be skipped because the alias at root exists
	for _, item := range items {
		if item.Name == ".github/CODEOWNERS" {
			if item.Action != wizard.ActionSkip {
				t.Errorf("expected CODEOWNERS to be skipped (alias exists), got %v", item.Action)
			}
		}
	}
}

func TestBuildItems_AliasCanonicalUpdate(t *testing.T) {
	t.Cleanup(func() { globals.NoOverwrite = false })

	items := newBuildItemsTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Canonical .github/CODEOWNERS exists with different content.
		// Root CODEOWNERS (non-canonical alias) is missing.
		if strings.Contains(r.URL.Path, ".github") && strings.Contains(r.URL.Path, "CODEOWNERS") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"b2xkCg==","sha":"abc"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}), "private")

	for _, item := range items {
		if item.Name == ".github/CODEOWNERS" {
			if item.Action != wizard.ActionUpdate {
				t.Errorf("expected canonical CODEOWNERS to update when content differs, got %v", item.Action)
			}
		}
	}
}

func TestOtherAliases(t *testing.T) {
	got := otherAliases(".github/CODEOWNERS", []string{".github/CODEOWNERS", "CODEOWNERS"})
	if len(got) != 1 || got[0] != "CODEOWNERS" {
		t.Errorf("otherAliases = %v, want [CODEOWNERS]", got)
	}
	if got := otherAliases("only", []string{"only"}); len(got) != 0 {
		t.Errorf("expected empty when only canonical is listed, got %v", got)
	}
}

func TestBuildItems_FileStatusUpdate(t *testing.T) {
	t.Cleanup(func() { globals.NoOverwrite = false })

	// All files return "update" status (exist with different content)
	items := newBuildItemsTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":"b2xkCg==","sha":"abc"}`))
	}), "private")

	// Non-alias files should be ActionUpdate
	foundUpdate := false
	for _, item := range items {
		if item.Action == wizard.ActionUpdate {
			foundUpdate = true
			break
		}
	}
	if !foundUpdate {
		t.Error("expected at least one update action")
	}
}

func TestBuildItems_ClassicExists(t *testing.T) {
	items := newBuildItemsTestFull(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}), "private", false, false, true)

	// Branch protection should be ActionUpgrade
	foundUpgrade := false
	for _, item := range items {
		if strings.Contains(item.Name, "Branch protection") && item.Action == wizard.ActionUpgrade {
			foundUpgrade = true
			break
		}
	}
	if !foundUpgrade {
		t.Error("expected upgrade action for classic protection")
	}
}

// TestRun_ArgValidation verifies the command validates its arguments correctly.
func TestRun_ArgValidation(t *testing.T) {
	cmd := NewCmd()
	// Valid arg format passes validation
	err := cmd.Args(cmd, []string{"a/b"})
	if err != nil {
		t.Errorf("unexpected arg error: %v", err)
	}
	// No args fails
	err = cmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error for missing arg")
	}
}

// lineReader returns input one line at a time, mimicking terminal line-buffered
// input so sequential bufio.Scanner-based prompts each see their own line.
// (strings.Reader hands back the whole stream in one Read, so the first prompt
// buffers everything and later prompts hit EOF.)
type lineReader struct {
	lines []string
}

func newLineReader(input string) *lineReader {
	return &lineReader{lines: strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n")}
}

func (r *lineReader) Read(p []byte) (int, error) {
	if len(r.lines) == 0 {
		return 0, io.EOF
	}
	line := r.lines[0] + "\n"
	r.lines = r.lines[1:]
	n := copy(p, line)
	if n < len(line) {
		r.lines = append([]string{line[n:]}, r.lines...)
	}
	return n, nil
}

// newRunFlowServer returns a test server that mocks the full run() flow for a
// public repo with no existing protection or files: visibility check, ruleset
// checks, classic protection, file status/upsert, settings, and security.
func newRunFlowServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"visibility":"public"}`))
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
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/owner/repo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/rulesets"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestRunWithClient_FullFlow drives the complete apply flow against a mock
// server: visibility detection, pre-flight checks, solo prompt, defaults
// confirmation, and direct application of all items.
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"visibility":"public","default_branch":"main"}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rulesets"):
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/protection"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/branches/main"):
			_, _ = w.Write([]byte(`{"commit":{"sha":"abc123"}}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/refs"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/pulls"):
			createdPR = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":7}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/rulesets"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := newTestClient(srv)

	input := "solo\nn\n" + strings.Repeat("y\n", 40)
	err := runWithClient(c, "owner", "repo", newLineReader(input), &strings.Builder{})
	if err != nil {
		t.Fatalf("runWithClient() error: %v", err)
	}
	if !createdPR {
		t.Error("expected --pr to open a pull request after interactive selection")
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
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"visibility":"public","default_branch":"develop"}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rulesets"):
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/branches/develop"):
			sawDevelop = true
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/branches/main"):
			sawMain = true
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
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
func TestRun_Success(t *testing.T) {
	t.Cleanup(func() { newClient = github.NewClient })

	srv := newRunFlowServer()
	defer srv.Close()
	newClient = func(token string, verbose bool) *github.Client {
		return github.NewClient(token, verbose).WithBaseURL(srv.URL)
	}

	cmd := NewCmd()
	var out strings.Builder
	cmd.SetIn(newLineReader("solo\ny\n"))
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"owner/repo"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("run() error: %v", err)
	}
	if !strings.Contains(out.String(), "✓ Done — https://github.com/owner/repo") {
		t.Errorf("expected done message, got:\n%s", out.String())
	}
}

// TestRunWithClient_RenderError verifies template-render failures surface a
// wrapped error.
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
