package apply

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

func TestBranchProtectionItem_RulesetExists(t *testing.T) {
	item := branchProtectionItem(nil, "owner", "repo", "main", "public", github.RulesetPlan{Exists: true}, false, false, &github.BranchProtectionOptions{}, "", false, io.Discard)
	if item.Action != wizard.ActionSkip {
		t.Errorf("expected ActionSkip when matching ruleset exists, got %v", item.Action)
	}
}

func TestTagRulesetItem_CreateAndSkip(t *testing.T) {
	create := tagRulesetItem(nil, "owner", "repo", github.RulesetPlan{}, false)
	if create.Action != wizard.ActionCreate {
		t.Errorf("missing tag ruleset Action = %v, want create", create.Action)
	}
	skip := tagRulesetItem(nil, "owner", "repo", existsPlan(true), false)
	if skip.Action != wizard.ActionSkip {
		t.Errorf("matching tag ruleset Action = %v, want skip", skip.Action)
	}
}

func TestTagRulesetItem_Drift(t *testing.T) {
	item := tagRulesetItem(nil, "owner", "repo", github.RulesetPlan{Exists: true, Drift: []string{"enforcement"}}, false)
	if item.Action != wizard.ActionUpdate {
		t.Errorf("expected ActionUpdate on tag ruleset drift, got %v", item.Action)
	}
}

func TestBranchProtectionItem_RulesetDrift(t *testing.T) {
	item := branchProtectionItem(nil, "owner", "repo", "main", "public", github.RulesetPlan{Exists: true, Drift: []string{"enforcement"}}, false, false, &github.BranchProtectionOptions{}, "", false, io.Discard)
	if item.Action != wizard.ActionUpdate {
		t.Errorf("expected ActionUpdate on ruleset drift, got %v", item.Action)
	}
	if item.Apply == nil {
		t.Fatal("expected Apply on drifted ruleset")
	}
}

func TestBranchProtectionItem_Creation(t *testing.T) {
	// Test branch protection action determination: ActionUpgrade when classic exists,
	// ActionCreate for new repos. protect-main is required so a failed apply
	// cannot print overall success.
	tests := []struct {
		name          string
		visibility    string
		rulesetExists bool
		classicExists bool
		wantAction    wizard.Action
		wantOptional  bool
	}{
		{
			name:          "new public repo",
			visibility:    "public",
			rulesetExists: false,
			classicExists: false,
			wantAction:    wizard.ActionCreate,
			wantOptional:  false,
		},
		{
			name:          "new private repo",
			visibility:    "private",
			rulesetExists: false,
			classicExists: false,
			wantAction:    wizard.ActionCreate,
			wantOptional:  false,
		},
		{
			name:          "upgrade from classic",
			visibility:    "public",
			rulesetExists: false,
			classicExists: true,
			wantAction:    wizard.ActionUpgrade,
			wantOptional:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":1}`))
			}), func(c *github.Client) {
				item := branchProtectionItem(c, "owner", "repo", "main", tt.visibility, existsPlan(tt.rulesetExists), tt.classicExists, false, &github.BranchProtectionOptions{}, "", false, io.Discard)
				if item.Action != tt.wantAction {
					t.Errorf("expected action %v, got %v", tt.wantAction, item.Action)
				}
				if item.Optional != tt.wantOptional {
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
	items, err := buildItems(c, nil, "owner", "repo", "main", "private", nil, github.RulesetPlan{}, github.RulesetPlan{}, false, false, &github.BranchProtectionOptions{}, true, "", io.Discard)
	if err != nil {
		t.Fatalf("buildItems() error: %v", err)
	}
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

func TestBuildItems_SettingsAndSecurityActions(t *testing.T) {
	tests := []struct {
		name          string
		info          github.Repo
		defaultCodeQL string
		wantSettings  wizard.Action
		wantSecurity  wizard.Action
	}{
		{
			name: "already configured",
			info: github.Repo{
				Visibility:           "public",
				DeleteBranchOnMerge:  true,
				SecretScanning:       true,
				SecretPushProtection: true,
			},
			defaultCodeQL: "configured",
			wantSettings:  wizard.ActionSkip,
			wantSecurity:  wizard.ActionSkip,
		},
		{
			name: "requires reconciliation",
			info: github.Repo{
				Visibility:           "public",
				SecretScanning:       true,
				SecretPushProtection: true,
			},
			defaultCodeQL: "not-configured",
			wantSettings:  wizard.ActionUpdate,
			wantSecurity:  wizard.ActionUpdate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/owner/repo/vulnerability-alerts", "/repos/owner/repo/automated-security-fixes":
					w.WriteHeader(http.StatusNoContent)
				case "/repos/owner/repo/code-scanning/default-setup":
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]string{"state": tt.defaultCodeQL})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}), func(c *github.Client) {
				items, err := buildItems(c, &tt.info, "owner", "repo", "main", "public", nil, github.RulesetPlan{}, github.RulesetPlan{}, false, false, &github.BranchProtectionOptions{}, false, "", io.Discard)
				if err != nil {
					t.Fatalf("buildItems() error: %v", err)
				}
				foundSettings := false
				foundSecurity := false
				for _, item := range items {
					switch item.Name {
					case "General settings (auto-delete branches)":
						foundSettings = true
						if item.Action != tt.wantSettings {
							t.Errorf("settings action = %v, want %v", item.Action, tt.wantSettings)
						}
					case "Security (secret scanning, CodeQL, Dependabot)":
						foundSecurity = true
						if item.Action != tt.wantSecurity {
							t.Errorf("security action = %v, want %v", item.Action, tt.wantSecurity)
						}
					}
				}
				if !foundSettings || !foundSecurity {
					t.Errorf("expected settings and security items, got %v", items)
				}
			}, nil)
		})
	}
}

func TestSecurityMissing(t *testing.T) {
	tests := []struct {
		name    string
		info    github.Repo
		opts    github.SecurityOptions
		alerts  int
		fixes   int
		want    []string
		wantErr bool
	}{
		{
			name: "alerts disabled",
			info: github.Repo{SecretScanning: true, SecretPushProtection: true},
			opts: github.SecurityOptions{Visibility: "public", AdvancedCodeQL: true},
			// default-setup is skipped (AdvancedCodeQL), so only alerts drift
			alerts: http.StatusNotFound,
			fixes:  http.StatusNoContent,
			want:   []string{"dependabot alerts"},
		},
		{
			name:   "security fixes disabled",
			info:   github.Repo{SecretScanning: true, SecretPushProtection: true},
			opts:   github.SecurityOptions{Visibility: "public", AdvancedCodeQL: true},
			alerts: http.StatusNoContent,
			fixes:  http.StatusNotFound,
			want:   []string{"automated security fixes"},
		},
		{
			name:   "secret scanning disabled",
			opts:   github.SecurityOptions{Visibility: "public", AdvancedCodeQL: true},
			alerts: http.StatusNoContent,
			fixes:  http.StatusNoContent,
			want:   []string{"secret scanning", "push protection"},
		},
		{
			name:   "codeql counts via workflow when default setup is off",
			opts:   github.SecurityOptions{Visibility: "public"},
			alerts: http.StatusNoContent,
			fixes:  http.StatusNoContent,
			// info has secret scanning off; default setup 404s; codeql.yml
			// workflow exists on the mock server.
			want: []string{"secret scanning", "push protection"},
		},
		{
			name:   "private baseline configured",
			opts:   github.SecurityOptions{Visibility: "private"},
			alerts: http.StatusNoContent,
			fixes:  http.StatusNoContent,
			want:   nil,
		},
		{
			name: "advanced CodeQL workflow configured",
			info: github.Repo{
				SecretScanning:       true,
				SecretPushProtection: true,
			},
			opts:   github.SecurityOptions{Visibility: "public", AdvancedCodeQL: true},
			alerts: http.StatusNoContent,
			fixes:  http.StatusNoContent,
			want:   nil,
		},
		{
			name:    "alerts status error",
			opts:    github.SecurityOptions{Visibility: "public"},
			alerts:  http.StatusForbidden,
			fixes:   http.StatusNoContent,
			wantErr: true,
		},
		{
			name:    "security fixes status error",
			opts:    github.SecurityOptions{Visibility: "public"},
			alerts:  http.StatusNoContent,
			fixes:   http.StatusForbidden,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/owner/repo/vulnerability-alerts":
					w.WriteHeader(tt.alerts)
				case "/repos/owner/repo/automated-security-fixes":
					w.WriteHeader(tt.fixes)
				case "/repos/owner/repo/code-scanning/default-setup":
					w.WriteHeader(http.StatusNotFound)
				case "/repos/owner/repo/contents/.github/workflows/codeql.yml":
					// A codeql workflow exists: CodeQL is on without default setup.
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"content":""}`))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}), func(c *github.Client) {
				got, err := securityMissing(c, tt.info, "owner", "repo", tt.opts)
				if (err != nil) != tt.wantErr {
					t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
				}
				if !sameStrSlices(got, tt.want) {
					t.Errorf("missing = %v, want %v", got, tt.want)
				}
			}, nil)
		})
	}
}

// TestSecurityMissing_AdvancedWorkflowCountsAsCodeQL pins the fix for the
// dry-run false positive: a repo with CodeQL via .github/workflows/codeql.yml
// (not default setup) must NOT be flagged as missing CodeQL.

func TestSecurityMissing_AdvancedWorkflowCountsAsCodeQL(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/vulnerability-alerts":
			w.WriteHeader(http.StatusNoContent)
		case "/repos/owner/repo/automated-security-fixes":
			w.WriteHeader(http.StatusNoContent)
		case "/repos/owner/repo/code-scanning/default-setup":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/owner/repo/contents/.github/workflows/codeql.yml":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":""}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}), func(c *github.Client) {
		info := github.Repo{Visibility: "public", SecretScanning: true, SecretPushProtection: true}
		missing, err := securityMissing(c, info, "owner", "repo", github.SecurityOptions{Visibility: "public"})
		if err != nil {
			t.Fatalf("securityMissing() error: %v", err)
		}
		if len(missing) != 0 {
			t.Errorf("codeql workflow must satisfy the CodeQL check, missing = %v", missing)
		}
	}, nil)
}

func TestSecurityItem_StatusErrorFailsClosed(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}), func(c *github.Client) {
		item, err := securityItem(c, &github.Repo{}, "owner", "repo", "public", nil, false)
		if err == nil {
			t.Fatal("a status-check error must fail the plan, not plan an update")
		}
		if item.Action != wizard.ActionCreate {
			t.Errorf("on error the item is not used, action = %v", item.Action)
		}
	}, nil)
}

func TestSecurityItem_DetailListsMissingFeatures(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/vulnerability-alerts":
			w.WriteHeader(http.StatusNoContent)
		case "/repos/owner/repo/automated-security-fixes":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/owner/repo/code-scanning/default-setup":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}), func(c *github.Client) {
		info := github.Repo{Visibility: "public", SecretScanning: true, SecretPushProtection: true}
		item, err := securityItem(c, &info, "owner", "repo", "public", nil, false)
		if err != nil {
			t.Fatalf("securityItem() error: %v", err)
		}
		if item.Action != wizard.ActionUpdate {
			t.Errorf("action = %v, want update", item.Action)
		}
		if !strings.Contains(item.Detail, "automated security fixes") {
			t.Errorf("detail must name the missing feature: %q", item.Detail)
		}
	}, nil)
}

func sameStrSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestBuildItems_TagRulesetExists(t *testing.T) {
	c := github.NewClient("", false)
	items := buildItemsForTest(t, c, "private", nil, github.RulesetPlan{}, existsPlan(true), false)

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
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
			// No files exist yet.
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		}
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
