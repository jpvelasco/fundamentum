package apply

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/templates"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

// prMockHandler returns an http.HandlerFunc that mocks the full PR workflow:
// CreatePRBranch (GET /branches/main, POST /git/refs),
// UpsertFileOnBranch (GET /contents/*, PUT /contents/*),
// CreatePullRequest (POST /pulls).
func prMockHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})
}

// simpleFileHandler returns an http.HandlerFunc that returns 201 + {"content":{}} for all requests.
func simpleFileHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"content":{}}`))
	})
}

// newFileItems builds a wizard.Item slice with the given names, content, and Apply closures.
// names, content, and applies must be of equal length.
func newFileItems(names []string, content [][]byte, applies []func() error) []wizard.Item {
	items := make([]wizard.Item, len(names))
	for i, name := range names {
		items[i] = wizard.Item{
			Name:    name,
			Action:  wizard.ActionCreate,
			Content: content[i],
			Apply:   applies[i],
		}
	}
	return items
}

// runApplyItemsExpectNoError applies items against a client pointed at the
// given server handler, and fails the test if applyItems returns an error.
// The server is closed automatically.
func runApplyItemsExpectNoError(t *testing.T, handler http.HandlerFunc, items []wizard.Item, viaPR bool) {
	t.Helper()
	testWithServer(t, handler, func(c *github.Client) {
		if err := applyItems(c, "owner", "repo", "main", items, viaPR); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	}, nil)
}

func TestBuildItems(t *testing.T) {
	// Mock server that returns file not found for all files
	items := newBuildItemsTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}), "private")

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
	items := buildItems(c, "owner", "repo", "main", "public", nil, true, true, false, github.BranchProtectionOptions{}, false)

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
	items := newFileItems(
		[]string{".github/CODEOWNERS", ".github/SECURITY.md"},
		[][]byte{[]byte("me"), []byte("sec")},
		[]func() error{func() error { return nil }, func() error { return nil }},
	)
	runApplyItemsExpectNoError(t, simpleFileHandler(), items, false)
}

// err409 is a stand-in Apply for an item that triggers PR-mode fallback.
func err409() error {
	return fmt.Errorf("409 Conflict: Repository rule violations found — GH013")
}

func TestApplyItems_FirstFile409_FallbackToPR(t *testing.T) {
	// First file returns 409 — triggers fallback. Remaining files collected for PR.
	// The 409 comes from the item.Apply() closure, not the server.
	// Server only handles PR flow: CreatePRBranch → UpsertFileOnBranch × N → CreatePullRequest.
	// First item.Apply() returns 409, second item.Apply() is never called (fallback collects it).
	items := newFileItems(
		[]string{".github/CODEOWNERS", ".github/SECURITY.md"},
		[][]byte{[]byte("me"), []byte("sec")},
		[]func() error{err409, func() error { return nil }},
	)
	runApplyItemsExpectNoError(t, prMockHandler(), items, false)
}

func TestApplyItems_All409_AllToPR(t *testing.T) {
	// All files return 409 — first triggers fallback, rest collected directly via fallback flag.
	// Both item.Apply() closures return 409 — only first is actually called (second collected via fallback).
	items := newFileItems(
		[]string{".github/CODEOWNERS", ".github/SECURITY.md"},
		[][]byte{[]byte("me"), []byte("sec")},
		[]func() error{err409, err409},
	)
	runApplyItemsExpectNoError(t, prMockHandler(), items, false)
}

func TestApplyItems_ViaPRFromStart(t *testing.T) {
	// viaPR=true — all files go directly to PR without trying direct apply.
	items := newFileItems(
		[]string{".github/CODEOWNERS", ".github/SECURITY.md"},
		[][]byte{[]byte("me"), []byte("sec")},
		[]func() error{func() error { return nil }, func() error { return nil }},
	)
	runApplyItemsExpectNoError(t, prMockHandler(), items, true)
}

func TestApplyItems_NonFileItemsApplyDirectly(t *testing.T) {
	// Non-file items (no Content) apply directly even when fallback is triggered.
	// First file 409 → fallback → non-file items still apply directly after PR batch.
	nonFileApplied := false
	items := []wizard.Item{
		{
			Name:    ".github/CODEOWNERS",
			Action:  wizard.ActionCreate,
			Content: []byte("me"),
			Apply:   err409,
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
	runApplyItemsExpectNoError(t, prMockHandler(), items, false)
	if !nonFileApplied {
		t.Error("expected non-file item to be applied directly after PR batch")
	}
}

func TestApplyItems_SkippedItemNotApplied(t *testing.T) {
	// Items with action Skip are not applied.
	applyCalled := false
	items := []wizard.Item{
		{
			Name:    ".github/CODEOWNERS",
			Action:  wizard.ActionSkip,
			Content: []byte("me"),
			Apply: func() error {
				applyCalled = true
				return nil
			},
		},
	}
	c := github.NewClient("", false)
	if err := applyItems(c, "owner", "repo", "main", items, false); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if applyCalled {
		t.Error("expected Apply not to be called")
	}
}

func TestApplyItems_ErrorHandling_NonFatal(t *testing.T) {
	items := []wizard.Item{
		{
			Name:     "Security (secret scanning, CodeQL, Dependabot)",
			Action:   wizard.ActionCreate,
			Optional: true,
			Apply:    func() error { return fmt.Errorf("API error") },
		},
	}
	c := github.NewClient("", false)
	if err := applyItems(c, "owner", "repo", "main", items, false); err != nil {
		t.Errorf("optional item failure must not fail the run, got: %v", err)
	}
}

func TestApplyItems_RequiredNonFileError(t *testing.T) {
	items := []wizard.Item{
		{
			Name:   "General settings (auto-delete branches)",
			Action: wizard.ActionCreate,
			Apply:  func() error { return fmt.Errorf("API error") },
		},
	}
	c := github.NewClient("", false)
	err := applyItems(c, "owner", "repo", "main", items, false)
	if err == nil || !strings.Contains(err.Error(), "required items failed") {
		t.Errorf("expected required-items error, got: %v", err)
	}
}

func TestBranchProtectionItem_FallbackOnlyOn403(t *testing.T) {
	tests := []struct {
		name            string
		visibility      string
		rulesetStatus   int
		rulesetBody     string
		classicStatus   int
		wantErr         bool
		wantErrContains string
		wantClassic     bool // true if classic API should be called
	}{
		{
			name:          "403 private falls back to classic",
			visibility:    "private",
			rulesetStatus: http.StatusForbidden,
			rulesetBody:   `{"message":"Forbidden"}`,
			classicStatus: http.StatusOK,
			wantErr:       false,
			wantClassic:   true,
		},
		{
			name:            "403 public returns error",
			visibility:      "public",
			rulesetStatus:   http.StatusForbidden,
			rulesetBody:     `{"message":"Forbidden"}`,
			wantErr:         true,
			wantErrContains: "403",
			wantClassic:     false,
		},
		{
			name:            "422 private returns error no fallback",
			visibility:      "private",
			rulesetStatus:   http.StatusUnprocessableEntity,
			rulesetBody:     `{"message":"validation failed"}`,
			wantErr:         true,
			wantErrContains: "422",
			wantClassic:     false,
		},
		{
			name:            "404 private returns error no fallback",
			visibility:      "private",
			rulesetStatus:   http.StatusNotFound,
			rulesetBody:     `{"message":"not found"}`,
			wantErr:         true,
			wantErrContains: "404",
			wantClassic:     false,
		},
		{
			name:            "500 private returns error no fallback",
			visibility:      "private",
			rulesetStatus:   http.StatusInternalServerError,
			rulesetBody:     `{"message":"internal error"}`,
			wantErr:         true,
			wantErrContains: "500",
			wantClassic:     false,
		},
		{
			name:          "201 private ruleset succeeds no fallback",
			visibility:    "private",
			rulesetStatus: http.StatusCreated,
			rulesetBody:   `{"id":1}`,
			wantErr:       false,
			wantClassic:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var classicCalled bool
			testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/rulesets"):
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`[]`))
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rulesets"):
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tt.rulesetStatus)
					var out any
					_ = json.Unmarshal([]byte(tt.rulesetBody), &out)
					_ = json.NewEncoder(w).Encode(out)
				case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/protection"):
					classicCalled = true
					w.WriteHeader(tt.classicStatus)
				default:
					w.WriteHeader(http.StatusNoContent)
				}
			}), func(c *github.Client) {
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
			}, func(t *testing.T) {
				if tt.wantClassic && !classicCalled {
					t.Error("expected classic API to be called, but it was not")
				}
				if !tt.wantClassic && classicCalled {
					t.Error("expected classic API not to be called, but it was")
				}
			})
		})
	}
}

func TestApplyItems_WorkflowLocked_Skipped(t *testing.T) {
	// Workflow lock error should be treated as skip and processing continues.
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
			Apply:   func() error { return nil },
		},
	}
	runApplyItemsExpectNoError(t, simpleFileHandler(), items, false)
}

func TestApplyItems_MixedFileAndNonFile(t *testing.T) {
	// Mix of file items and non-file items: files apply directly, non-files defer.
	fileApplied := false
	nonFileApplied := false
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
	runApplyItemsExpectNoError(t, simpleFileHandler(), items, false)
	if !fileApplied {
		t.Error("expected file item to be applied directly")
	}
	if !nonFileApplied {
		t.Error("expected non-file item to be applied after files")
	}
}

func TestBuildItems_AliasFormatVariants(t *testing.T) {
	// Test alias detection for format variants (.yml vs .md for issue templates).
	items := newBuildItemsTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}), "private")

	// bug_report.yml should be skipped because .md alias exists
	for _, item := range items {
		if item.Name == ".github/ISSUE_TEMPLATE/bug_report.yml" {
			if item.Action != wizard.ActionSkip {
				t.Errorf("expected bug_report.yml to be skipped (md alias exists), got %v", item.Action)
			}
		}
	}
}

func TestBuildItems_AliasWorkflowVariants(t *testing.T) {
	// Test alias detection for workflow name variants: an existing
	// octopus-review.yml counts as already having the octopus.yml workflow.
	items := newBuildItemsTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/octopus-review.yml") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"b2xkCg=="}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}), "public")

	// octopus.yml should be skipped because octopus-review.yml alias exists
	for _, item := range items {
		if item.Name == ".github/workflows/octopus.yml" {
			if item.Action != wizard.ActionSkip {
				t.Errorf("expected octopus.yml to be skipped (octopus-review.yml alias exists), got %v", item.Action)
			}
		}
	}
}

func TestBuildItems_AdvancedCodeQLSkipsDefaultSetup(t *testing.T) {
	// Public renders ship the advanced codeql.yml workflow, so the security
	// item must NOT enable default-setup CodeQL (GitHub rejects advanced
	// SARIF uploads while default setup is configured).
	var calledDefaultSetup bool
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "code-scanning/default-setup"):
			calledDefaultSetup = true
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
			return
		case r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":""}`))
	}), func(c *github.Client) {
		data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "public"}
		rendered, err := templates.Render(data)
		if err != nil {
			t.Fatalf("Render() error: %v", err)
		}
		items := buildItems(c, "owner", "repo", "main", "public", rendered, false, false, false, github.BranchProtectionOptions{}, false)
		for _, item := range items {
			if item.Name == "Security (secret scanning, CodeQL, Dependabot)" {
				if err := item.Apply(); err != nil {
					t.Fatalf("security item Apply() error: %v", err)
				}
			}
		}
	}, func(t *testing.T) {
		if calledDefaultSetup {
			t.Error("expected no default-setup CodeQL PATCH when advanced codeql.yml workflow is rendered")
		}
	})
}

// TestBuildItems_FileStatusSkip verifies buildItems maps FileStatus "skip"
// (existing file with identical content) to ActionSkip.
func TestBuildItems_FileStatusSkip(t *testing.T) {
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	// Pick a rendered file with no alias variants (socket.yml) so the exact
	// path check (FileStatus) runs instead of AnyFileExists.
	var target string
	for _, f := range rendered {
		if strings.HasSuffix(f.Path, "socket.yml") {
			target = f.Path
			break
		}
	}
	if target == "" {
		t.Fatal("expected a socket.yml rendered file")
	}
	var targetContent string
	for _, f := range rendered {
		if f.Path == target {
			targetContent = f.Content
			break
		}
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(targetContent))
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, target) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"content": encoded, "sha": "abc"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}), func(c *github.Client) {
		items := buildItems(c, "owner", "repo", "main", "private", rendered, false, false, false, github.BranchProtectionOptions{}, false)
		for _, item := range items {
			if item.Name == target {
				if item.Action != wizard.ActionSkip {
					t.Errorf("expected %s to be skipped (content unchanged), got %v", target, item.Action)
				}
			}
		}
	}, nil)
}

// TestBranchProtectionItem_ClassicUpgradeApply verifies the upgrade item's
// Apply closure: create the ruleset first, then remove classic protection.
func TestBranchProtectionItem_ClassicUpgradeApply(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rulesets"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/rulesets"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/protection"):
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}), func(c *github.Client) {
		item := branchProtectionItem(c, "owner", "repo", "main", "public", false, true, github.BranchProtectionOptions{})
		if item.Action != wizard.ActionUpgrade {
			t.Fatalf("expected ActionUpgrade, got %v", item.Action)
		}
		if err := item.Apply(); err != nil {
			t.Fatalf("upgrade Apply() error: %v", err)
		}
	}, nil)
}

// TestBranchProtectionItem_ClassicUpgradeError verifies that a ruleset
// creation failure during the upgrade aborts before removing classic
// protection.
func TestBranchProtectionItem_ClassicUpgradeError(t *testing.T) {
	var classicRemoved bool
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rulesets"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not json`))
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/protection"):
			classicRemoved = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}), func(c *github.Client) {
		item := branchProtectionItem(c, "owner", "repo", "main", "public", false, true, github.BranchProtectionOptions{})
		if err := item.Apply(); err == nil {
			t.Fatal("expected error when ruleset creation fails")
		}
	}, func(t *testing.T) {
		if classicRemoved {
			t.Error("classic protection should not be removed when ruleset creation fails")
		}
	})
}

func TestApplyItems_RequiredFileError(t *testing.T) {
	secApplied := false
	items := []wizard.Item{
		{
			Name:    ".github/CODEOWNERS",
			Action:  wizard.ActionCreate,
			Content: []byte("me"),
			Apply:   func() error { return fmt.Errorf("API error") },
		},
		{
			Name:    ".github/SECURITY.md",
			Action:  wizard.ActionCreate,
			Content: []byte("sec"),
			Apply: func() error {
				secApplied = true
				return nil
			},
		},
	}
	c := github.NewClient("", false)
	err := applyItems(c, "owner", "repo", "main", items, false)
	if err == nil || !strings.Contains(err.Error(), "required items failed") {
		t.Errorf("expected required-items error, got: %v", err)
	}
	if !secApplied {
		t.Error("later items should still run after a required file failure")
	}
}

func TestApplyItems_OptionalFileError(t *testing.T) {
	items := []wizard.Item{
		{
			Name:     "optional.md",
			Action:   wizard.ActionCreate,
			Optional: true,
			Content:  []byte("x"),
			Apply:    func() error { return fmt.Errorf("API error") },
		},
	}
	c := github.NewClient("", false)
	if err := applyItems(c, "owner", "repo", "main", items, false); err != nil {
		t.Errorf("optional file failure must not fail the run, got: %v", err)
	}
}

func TestApplyItems_ViaPRNoWrittenFiles(t *testing.T) {
	items := newFileItems(
		[]string{"README.md"},
		[][]byte{[]byte("hello")},
		[]func() error{func() error { return nil }},
	)
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/branches/"):
			_, _ = w.Write([]byte(`{"commit":{"sha":"abc"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
			_, _ = w.Write([]byte(`{"content":"aGVsbG8=","sha":"x"}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/refs"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/pulls"):
			t.Error("must not open a PR when every file is skipped")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}), func(c *github.Client) {
		if err := applyItems(c, "owner", "repo", "main", items, true); err != nil {
			t.Errorf("expected no error when PR has no file changes, got: %v", err)
		}
	}, nil)
}

// TestApplyItems_ViaPRFailure verifies ApplyViaPR errors propagate from
// applyItems.
func TestApplyItems_ViaPRFailure(t *testing.T) {
	items := newFileItems(
		[]string{".github/CODEOWNERS"},
		[][]byte{[]byte("me")},
		[]func() error{func() error { return nil }},
	)
	// PR branch creation fails (404 on GET /branches/main) → ApplyViaPR errors.
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}), func(c *github.Client) {
		err := applyItems(c, "owner", "repo", "main", items, true)
		if err == nil || !strings.Contains(err.Error(), "apply via PR") {
			t.Errorf("expected apply-via-PR error, got: %v", err)
		}
	}, nil)
}
