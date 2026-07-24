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
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: visibility}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	return buildItems(c, "owner", "repo", "main", visibility, rendered, rulesetExists, tagExists, classicExists, github.BranchProtectionOptions{})
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
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":1}`))
			}))
			defer srv.Close()

			c := github.NewClient("t", false).WithBaseURL(srv.URL)
			item := branchProtectionItem(c, "owner", "repo", "main", tt.visibility, tt.rulesetExists, tt.classicExists, github.BranchProtectionOptions{})
			if item.Action != tt.wantAction {
				t.Errorf("expected action %v, got %v", tt.wantAction, item.Action)
			}
			if tt.checkOptional && item.Optional != tt.wantOptional {
				t.Errorf("expected Optional=%v, got %v", tt.wantOptional, item.Optional)
			}
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
