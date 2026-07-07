package apply

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/templates"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

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

func TestBranchProtectionItem_ClassicExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	item := branchProtectionItem(c, "owner", "repo", "main", "public", false, true, github.BranchProtectionOptions{})
	if item.Action != wizard.ActionUpgrade {
		t.Errorf("expected ActionUpgrade when classic exists, got %v", item.Action)
	}
}

func TestBranchProtectionItem_PublicNew(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	item := branchProtectionItem(c, "owner", "repo", "main", "public", false, false, github.BranchProtectionOptions{})
	if item.Action != wizard.ActionCreate {
		t.Errorf("expected ActionCreate for new public, got %v", item.Action)
	}
	if !item.Optional {
		t.Error("expected Optional=true for new branch protection")
	}
}

func TestBranchProtectionItem_PrivateNew(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	item := branchProtectionItem(c, "owner", "repo", "main", "private", false, false, github.BranchProtectionOptions{})
	if item.Action != wizard.ActionCreate {
		t.Errorf("expected ActionCreate for new private, got %v", item.Action)
	}
}

func TestBuildItems_Public(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "public"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	items := buildItems(c, "owner", "repo", "main", "public", rendered, false, false, false, github.BranchProtectionOptions{})

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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	items := buildItems(c, "owner", "repo", "main", "private", rendered, false, false, false, github.BranchProtectionOptions{})

	// Check that CodeQL is NOT in the security item for private repos
	for _, item := range items {
		if strings.Contains(item.Name, "CodeQL") {
			t.Error("expected no CodeQL in security item for private repo")
		}
	}
}

func TestBuildItems_TagRulesetExists(t *testing.T) {
	c := github.NewClient("", false)
	items := buildItems(c, "owner", "repo", "main", "private", nil, false, true, false, github.BranchProtectionOptions{})

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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return existing file with different content
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":"b2xkCg==","sha":"abc"}`))
	}))
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	items := buildItems(c, "owner", "repo", "main", "private", rendered, false, false, false, github.BranchProtectionOptions{})

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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/contents/CODEOWNERS") && !strings.Contains(r.URL.Path, ".github") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"d29ybGQ="}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	items := buildItems(c, "owner", "repo", "main", "private", rendered, false, false, false, github.BranchProtectionOptions{})

	// CODEOWNERS should be skipped because the alias at root exists
	for _, item := range items {
		if item.Name == ".github/CODEOWNERS" {
			if item.Action != wizard.ActionSkip {
				t.Errorf("expected CODEOWNERS to be skipped (alias exists), got %v", item.Action)
			}
		}
	}
}

func TestBuildItems_FileStatusUpdate(t *testing.T) {
	t.Cleanup(func() { globals.NoOverwrite = false })

	// All files return "update" status (exist with different content)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":"b2xkCg==","sha":"abc"}`))
	}))
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	items := buildItems(c, "owner", "repo", "main", "private", rendered, false, false, false, github.BranchProtectionOptions{})

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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	items := buildItems(c, "owner", "repo", "main", "private", rendered, false, false, true, github.BranchProtectionOptions{})

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

func TestBranchProtectionItem_ClassicFallback(t *testing.T) {
	// Test the fallback path: ruleset fails → classic for private repos
	rulesetFailed := false
	classicCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "rulesets") {
			// RulesetExists returns empty list
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "rulesets") {
			rulesetFailed = true
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"forbidden"}`))
			return
		}
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "protection") {
			classicCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	item := branchProtectionItem(c, "owner", "repo", "main", "private", false, false, github.BranchProtectionOptions{})

	// Apply the item — it should fall back to classic after ruleset fails
	err := item.Apply()
	if err != nil {
		t.Fatalf("expected fallback to classic to succeed, got: %v", err)
	}
	if !rulesetFailed {
		t.Error("expected ruleset to be attempted first")
	}
	if !classicCalled {
		t.Error("expected classic protection fallback")
	}
}

func TestBranchProtectionItem_PublicNoFallback(t *testing.T) {
	// Public repos should NOT fall back to classic
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	item := branchProtectionItem(c, "owner", "repo", "main", "public", false, false, github.BranchProtectionOptions{})

	err := item.Apply()
	if err == nil {
		t.Error("expected error for public repo when ruleset fails (no fallback)")
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