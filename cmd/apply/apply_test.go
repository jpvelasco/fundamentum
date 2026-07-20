package apply

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/templates"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

// newPRMockServer returns a test server that mocks the full PR workflow:
// CreatePRBranch (GET /branches/main, POST /git/refs),
// UpsertFileOnBranch (GET /contents/*, PUT /contents/*),
// CreatePullRequest (POST /pulls).
func newPRMockServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/branches/main"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"commit":{"sha":"abc123"}}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/refs"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/pulls"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":42}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestBuildItems(t *testing.T) {
	// Mock server that returns file not found for all files
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)

	// Render templates to get files
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	items := buildItems(c, "owner", "repo", "main", "private", rendered, false, false, false, github.BranchProtectionOptions{})

	// Should have file items + general settings + branch protection + tag ruleset + security
	if len(items) < 5 {
		t.Errorf("expected at least 5 items, got %d", len(items))
	}

	// Check that general settings item exists
	foundGeneral := false
	for _, item := range items {
		if item.Name == "General settings (auto-delete branches)" {
			foundGeneral = true
			break
		}
	}
	if !foundGeneral {
		t.Error("expected general settings item")
	}

	// Check that branch protection item exists
	foundBranch := false
	for _, item := range items {
		if item.Name == "Branch protection (protect-main)" {
			foundBranch = true
			break
		}
	}
	if !foundBranch {
		t.Error("expected branch protection item")
	}
}

func TestBuildItems_WithExistingRuleset(t *testing.T) {
	c := &github.Client{}
	items := buildItems(c, "owner", "repo", "main", "public", nil, true, true, false, github.BranchProtectionOptions{})

	// Branch protection should be skipped
	for _, item := range items {
		if item.Name == "Branch protection (protect-main ruleset)" {
			if item.Action != wizard.ActionSkip {
				t.Errorf("expected branch protection to be skipped, got %v", item.Action)
			}
		}
	}
}

func TestActionFromExists(t *testing.T) {
	if actionFromExists(true) != wizard.ActionSkip {
		t.Error("expected ActionSkip for existing item")
	}
	if actionFromExists(false) != wizard.ActionCreate {
		t.Error("expected ActionCreate for new item")
	}
}

func TestApplyItems_No409(t *testing.T) {
	// All files apply directly without 409 — no PR created.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"content":{}}`))
	}))
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	items := []wizard.Item{
		{Name: ".github/CODEOWNERS", Action: wizard.ActionCreate, Content: []byte("me"), Apply: func() error { return nil }},
		{Name: ".github/SECURITY.md", Action: wizard.ActionCreate, Content: []byte("sec"), Apply: func() error { return nil }},
	}
	err := applyItems(c, "owner", "repo", "main", items, false, false)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestApplyItems_FirstFile409_FallbackToPR(t *testing.T) {
	// First file returns 409 — triggers fallback. Remaining files collected for PR.
	// The 409 comes from the item.Apply() closure, not the server.
	// Server only handles PR flow: CreatePRBranch → UpsertFileOnBranch × N → CreatePullRequest.
	srv := newPRMockServer()
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	// First item.Apply() returns 409, second item.Apply() is never called (fallback collects it)
	items := []wizard.Item{
		{
			Name:    ".github/CODEOWNERS",
			Action:  wizard.ActionCreate,
			Content: []byte("me"),
			Apply: func() error {
				return fmt.Errorf("409 Conflict: Repository rule violations found — GH013")
			},
		},
		{
			Name:    ".github/SECURITY.md",
			Action:  wizard.ActionCreate,
			Content: []byte("sec"),
			Apply:   func() error { return nil },
		},
	}
	err := applyItems(c, "owner", "repo", "main", items, false, false)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestApplyItems_All409_AllToPR(t *testing.T) {
	// All files return 409 — first triggers fallback, rest collected directly via fallback flag.
	srv := newPRMockServer()
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	// Both item.Apply() closures return 409 — only first is actually called (second collected via fallback)
	items := []wizard.Item{
		{
			Name:    ".github/CODEOWNERS",
			Action:  wizard.ActionCreate,
			Content: []byte("me"),
			Apply: func() error {
				return fmt.Errorf("409 Conflict: Repository rule violations found — GH013")
			},
		},
		{
			Name:    ".github/SECURITY.md",
			Action:  wizard.ActionCreate,
			Content: []byte("sec"),
			Apply: func() error {
				return fmt.Errorf("409 Conflict: Repository rule violations found — GH013")
			},
		},
	}
	err := applyItems(c, "owner", "repo", "main", items, false, false)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestApplyItems_ViaPRFromStart(t *testing.T) {
	// viaPR=true — all files go directly to PR without trying direct apply.
	srv := newPRMockServer()
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	items := []wizard.Item{
		{Name: ".github/CODEOWNERS", Action: wizard.ActionCreate, Content: []byte("me"), Apply: func() error { return nil }},
		{Name: ".github/SECURITY.md", Action: wizard.ActionCreate, Content: []byte("sec"), Apply: func() error { return nil }},
	}
	err := applyItems(c, "owner", "repo", "main", items, false, true)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestApplyItems_NonFileItemsApplyDirectly(t *testing.T) {
	// Non-file items (no Content) apply directly even when fallback is triggered.
	// First file 409 → fallback → non-file items still apply directly after PR batch.
	nonFileApplied := false
	srv := newPRMockServer()
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	items := []wizard.Item{
		{
			Name:    ".github/CODEOWNERS",
			Action:  wizard.ActionCreate,
			Content: []byte("me"),
			Apply: func() error {
				return fmt.Errorf("409 Conflict: Repository rule violations found — GH013")
			},
		},
		{
			Name:   "General settings (auto-delete branches)",
			Action: wizard.ActionCreate,
			Apply: func() error {
				nonFileApplied = true
				return nil
			},
		},
	}
	err := applyItems(c, "owner", "repo", "main", items, false, false)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if !nonFileApplied {
		t.Error("expected non-file item to be applied directly after PR batch")
	}
}

func TestApplyItems_DryRun_SkipsApply(t *testing.T) {
	// Dry run should not call any Apply functions.
	applyCalled := false
	items := []wizard.Item{
		{Name: ".github/CODEOWNERS", Action: wizard.ActionCreate, Content: []byte("me"), Apply: func() error {
			applyCalled = true
			return nil
		}},
	}
	c := github.NewClient("", false)
	err := applyItems(c, "owner", "repo", "main", items, true, false)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if applyCalled {
		t.Error("expected Apply not to be called in dry run")
	}
}

func TestApplyItems_SkippedItems_NotApplied(t *testing.T) {
	// Skipped items should not be applied.
	applyCalled := false
	items := []wizard.Item{
		{Name: ".github/CODEOWNERS", Action: wizard.ActionSkip, Content: []byte("me"), Apply: func() error {
			applyCalled = true
			return nil
		}},
	}
	c := github.NewClient("", false)
	err := applyItems(c, "owner", "repo", "main", items, false, false)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if applyCalled {
		t.Error("expected Apply not to be called for skipped item")
	}
}

func TestApplyItems_OptionalError_NonFatal(t *testing.T) {
	// Optional items that fail should not cause a fatal error.
	items := []wizard.Item{
		{
			Name:     "Security (secret scanning, CodeQL, Dependabot)",
			Action:   wizard.ActionCreate,
			Optional: true,
			Apply:    func() error { return fmt.Errorf("some error") },
		},
	}
	c := github.NewClient("", false)
	err := applyItems(c, "owner", "repo", "main", items, false, false)
	if err != nil {
		t.Errorf("expected no error for optional failure, got: %v", err)
	}
}

func TestBranchProtectionItem_FallbackOnlyOn403(t *testing.T) {
	tests := []struct {
		name           string
		visibility     string
		rulesetStatus  int
		rulesetBody    string
		classicStatus  int
		wantErr        bool
		wantErrContains string
		wantClassic    bool // true if classic API should be called
	}{
		{
			name: "403 private falls back to classic",
			visibility: "private",
			rulesetStatus: http.StatusForbidden,
			rulesetBody: `{"message":"Forbidden"}`,
			classicStatus: http.StatusOK,
			wantErr: false,
			wantClassic: true,
		},
		{
			name: "403 public returns error",
			visibility: "public",
			rulesetStatus: http.StatusForbidden,
			rulesetBody: `{"message":"Forbidden"}`,
			wantErr: true,
			wantErrContains: "403",
			wantClassic: false,
		},
		{
			name: "422 private returns error no fallback",
			visibility: "private",
			rulesetStatus: http.StatusUnprocessableEntity,
			rulesetBody: `{"message":"validation failed"}`,
			wantErr: true,
			wantErrContains: "422",
			wantClassic: false,
		},
		{
			name: "404 private returns error no fallback",
			visibility: "private",
			rulesetStatus: http.StatusNotFound,
			rulesetBody: `{"message":"not found"}`,
			wantErr: true,
			wantErrContains: "404",
			wantClassic: false,
		},
		{
			name: "500 private returns error no fallback",
			visibility: "private",
			rulesetStatus: http.StatusInternalServerError,
			rulesetBody: `{"message":"internal error"}`,
			wantErr: true,
			wantErrContains: "500",
			wantClassic: false,
		},
		{
			name: "201 private ruleset succeeds no fallback",
			visibility: "private",
			rulesetStatus: http.StatusCreated,
			rulesetBody: `{"id":1}`,
			wantErr: false,
			wantClassic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classicCalled := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/rulesets"):
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`[]`))
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rulesets"):
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tt.rulesetStatus)
					_, _ = io.WriteString(w, tt.rulesetBody)
				case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/protection"):
					classicCalled = true
					w.WriteHeader(tt.classicStatus)
				default:
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer srv.Close()

			c := github.NewClient("t", false).WithBaseURL(srv.URL)
			item := branchProtectionItem(c, "owner", "repo", "main", tt.visibility, false, false, github.BranchProtectionOptions{})

			err := item.Apply()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
			if tt.wantErr && tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErrContains, err)
			}
			if tt.wantClassic && !classicCalled {
				t.Error("expected classic API to be called, but it was not")
			}
			if !tt.wantClassic && classicCalled {
				t.Error("expected classic API not to be called, but it was")
			}
		})
	}
}

func TestApplyItems_WorkflowLocked_Skipped(t *testing.T) {
	// Workflow lock error should be treated as skip and processing continues.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"content":{}}`))
	}))
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	items := []wizard.Item{
		{
			Name:    ".github/workflows/ci.yml",
			Action:  wizard.ActionUpdate,
			Content: []byte("workflow"),
			Apply: func() error {
				return fmt.Errorf("upsert file .github/workflows/ci.yml: %w", github.ErrWorkflowLocked)
			},
		},
		{
			Name:    ".github/CODEOWNERS",
			Action:  wizard.ActionCreate,
			Content: []byte("me"),
			Apply: func() error { return nil },
		},
	}
	err := applyItems(c, "owner", "repo", "main", items, false, false)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestApplyItems_NonFileFatalError(t *testing.T) {
	// Non-optional non-file item that fails should print error but not return fatal.
	// (applyItems does not return errors for individual item failures — it continues.)
	items := []wizard.Item{
		{
			Name:     "General settings (auto-delete branches)",
			Action:   wizard.ActionCreate,
			Optional: false,
			Apply:    func() error { return fmt.Errorf("API error") },
		},
	}
	c := github.NewClient("", false)
	err := applyItems(c, "owner", "repo", "main", items, false, false)
	if err != nil {
		t.Errorf("expected no error return (non-fatal item failure), got: %v", err)
	}
}

func TestApplyItems_MixedFileAndNonFile(t *testing.T) {
	// Mix of file items and non-file items: files apply directly, non-files defer.
	fileApplied := false
	nonFileApplied := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	items := []wizard.Item{
		{
			Name:    ".github/CODEOWNERS",
			Action:  wizard.ActionCreate,
			Content: []byte("me"),
			Apply: func() error {
				fileApplied = true
				return nil
			},
		},
		{
			Name:   "General settings (auto-delete branches)",
			Action: wizard.ActionCreate,
			Apply: func() error {
				nonFileApplied = true
				return nil
			},
		},
	}
	err := applyItems(c, "owner", "repo", "main", items, false, false)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if !fileApplied {
		t.Error("expected file item to be applied directly")
	}
	if !nonFileApplied {
		t.Error("expected non-file item to be applied after files")
	}
}

func TestBuildItems_AliasFormatVariants(t *testing.T) {
	// Test alias detection for format variants (.yml vs .md for issue templates).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// bug_report.md exists as alias (yml target)
		if strings.Contains(r.URL.Path, "/bug_report.md") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"b2xkCg=="}`))
			return
		}
		// feature_request.yml exists at target path with same content
		if strings.Contains(r.URL.Path, "/feature_request.yml") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"b2xkCg=="}`))
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

	// bug_report.yml should be skipped because .md alias exists
	for _, item := range items {
		if item.Name == ".github/ISSUE_TEMPLATE/bug_report.yml" {
			if item.Action != wizard.ActionSkip {
				t.Errorf("expected bug_report.yml to be skipped (md alias exists), got %v", item.Action)
			}
		}
	}
}
