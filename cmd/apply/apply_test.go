package apply

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/templates"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

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
					w.WriteHeader(tt.rulesetStatus)
					_, _ = w.Write([]byte(tt.rulesetBody))
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
