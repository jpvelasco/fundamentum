package apply

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/templates"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

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
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	items := buildItemsForTest(t, c, "public", nil, existsPlan(true), existsPlan(true), false)

	// Branch protection should be skipped
	for _, item := range items {
		if item.Name == "Branch protection (protect-main ruleset)" {
			if item.Action != wizard.ActionSkip {
				t.Errorf("expected branch protection to be skipped, got %v", item.Action)
			}
		}
	}
}

// gitBatchHandler mocks the full git data API flow for a successful
// CommitFiles batch: GET ref → GET commit → POST blobs → POST tree →
// POST commit → PATCH ref (refStatus).

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
		items := buildItemsForTest(t, c, "public", rendered, github.RulesetPlan{}, github.RulesetPlan{}, false)
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
		items := buildItemsForTest(t, c, "private", rendered, github.RulesetPlan{}, github.RulesetPlan{}, false)
		for _, item := range items {
			if item.Name == target {
				if item.Action != wizard.ActionSkip {
					t.Errorf("expected %s to be skipped (content unchanged), got %v", target, item.Action)
				}
			}
		}
	}, nil)
}

// TestBranchProtectionItem_ClassicUpgradeApply covers successful upgrades and
// confirms a failed ruleset creation leaves classic protection in place.

func TestBranchProtectionItem_ClassicUpgradeApply(t *testing.T) {
	for _, tc := range []struct {
		name                string
		rulesetCreateStatus int
		wantApplyError      bool
		wantClassicRemoval  bool
	}{
		{"create ruleset before removing classic protection", http.StatusCreated, false, true},
		{"keep classic protection if ruleset creation fails", http.StatusUnprocessableEntity, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var classicRemoved bool
			testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rulesets"):
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode([]struct{}{})
				case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/rulesets"):
					w.WriteHeader(tc.rulesetCreateStatus)
				case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/protection"):
					classicRemoved = true
					w.WriteHeader(http.StatusNoContent)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}), func(c *github.Client) {
				item := branchProtectionItem(c, "owner", "repo", "main", "public", github.RulesetPlan{}, true, false, &github.BranchProtectionOptions{}, "", false, io.Discard)
				if item.Action != wizard.ActionUpgrade {
					t.Fatalf("expected ActionUpgrade, got %v", item.Action)
				}
				err := item.Apply()
				if (err != nil) != tc.wantApplyError {
					t.Errorf("upgrade Apply() error = %v, want error=%t", err, tc.wantApplyError)
				}
			}, func(t *testing.T) {
				if classicRemoved != tc.wantClassicRemoval {
					t.Errorf("classic protection removed = %t, want %t", classicRemoved, tc.wantClassicRemoval)
				}
			})
		})
	}
}

func TestBuildItems_FileStatusErrorFailsPlan(t *testing.T) {
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	// Plain 403 is not retried, so this fails fast. socket.yml has no alias
	// variants, so the canonical FileStatus GET is what fails. Earlier files
	// in the render order must behave normally (404 = create).
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/contents/socket.yml") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}), func(c *github.Client) {
		items, err := buildItems(c, nil, "owner", "repo", "main", "private", rendered, github.RulesetPlan{}, github.RulesetPlan{}, false, false, &github.BranchProtectionOptions{}, false, "", io.Discard)
		if err == nil {
			t.Fatal("expected buildItems to fail when FileStatus errors, got nil")
		}
		if !strings.Contains(err.Error(), "check status of socket.yml") {
			t.Errorf("expected error to name the failing path, got: %v", err)
		}
		if items != nil {
			t.Errorf("expected nil items on error, got %d items", len(items))
		}
	}, nil)
}

// TestBuildItems_AliasCheckErrorFailsPlan verifies that a failed alias
// existence check (e.g. root CODEOWNERS variant) also fails the plan instead
// of being swallowed and risking a duplicate write.

func TestBuildItems_AliasCheckErrorFailsPlan(t *testing.T) {
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/contents/CODEOWNERS"):
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}), func(c *github.Client) {
		items, err := buildItems(c, nil, "owner", "repo", "main", "private", rendered, github.RulesetPlan{}, github.RulesetPlan{}, false, false, &github.BranchProtectionOptions{}, false, "", io.Discard)
		if err == nil {
			t.Fatal("expected buildItems to fail when the alias check errors, got nil")
		}
		if !strings.Contains(err.Error(), "check aliases of .github/CODEOWNERS") {
			t.Errorf("expected error to name the alias check, got: %v", err)
		}
		if items != nil {
			t.Errorf("expected nil items on error, got %d items", len(items))
		}
	}, nil)
}

func TestBuildItems_SkipStepsMarksOptOut(t *testing.T) {
	t.Cleanup(func() { globals.SkipSteps = nil; globals.OnlySteps = nil })
	reset := func() {
		globals.SkipSteps = nil
		globals.OnlySteps = nil
	}
	t.Cleanup(reset)

	globals.SkipSteps = []string{"protect-version-tags", "socket.yml"}
	items := buildItemsForTest(t, nil, "public", nil, github.RulesetPlan{}, github.RulesetPlan{}, false)
	for _, item := range items {
		if item.Name == "Tag ruleset (protect-version-tags)" {
			if item.Action != wizard.ActionSkip || item.OptOutReason != "--skip" {
				t.Errorf("skipped tag ruleset = %+v, want skip via --skip", item)
			}
		}
	}

	globals.SkipSteps = nil
	globals.OnlySteps = []string{"files"}
	rendered, err := templates.Render(templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "public", CIPack: "go"})
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	items = buildItemsForTest(t, nil, "public", rendered, github.RulesetPlan{}, github.RulesetPlan{}, false)
	var fileSeen, stepSeen bool
	for _, item := range items {
		if item.Content != nil {
			fileSeen = true
			if item.Action == wizard.ActionSkip {
				t.Errorf("file %q must stay included under --only files: %+v", item.Name, item)
			}
		} else {
			stepSeen = true
			if item.Action != wizard.ActionSkip || item.OptOutReason != "--only" {
				t.Errorf("non-file item must be opted out via --only: %+v", item)
			}
		}
	}
	if !fileSeen || !stepSeen {
		t.Fatalf("expected both file and step items, fileSeen=%v stepSeen=%v", fileSeen, stepSeen)
	}
}

func TestBuildItems_PlanGatedWithGuardSkipsViaGuard(t *testing.T) {
	// A free-tier private repo with the guard rendered: the branch-protection
	// item points at the guard instead of planning an impossible ruleset.
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatal(err)
	}
	items := buildFreeTierPrivateItemsForTest(t, c, rendered)
	for _, item := range items {
		if strings.Contains(item.Name, "free-tier Actions guard") {
			if item.Action != wizard.ActionSkip {
				t.Errorf("guard item action = %v, want skip", item.Action)
			}
			if !strings.Contains(item.Detail, github.FreeTierGuardPath) {
				t.Errorf("guard item detail must name the workflow: %q", item.Detail)
			}
			return
		}
	}
	t.Fatal("expected the free-tier guard item in the plan")
}

func TestBuildItems_PlanGatedWithoutGuardKeepsManualPath(t *testing.T) {
	// Plan-gated + the guard file skipped: keep the honest manual-message
	// path (an operator who opts out of the guard gets the Settings pointer).
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	items := buildFreeTierPrivateItemsForTest(t, c, nil)
	for _, item := range items {
		if item.Name == "Branch protection (protect-main)" {
			if item.Action != wizard.ActionCreate {
				t.Errorf("without the guard, plan-gated protection stays create: %v", item.Action)
			}
			return
		}
	}
	t.Fatal("expected the branch protection item")
}
