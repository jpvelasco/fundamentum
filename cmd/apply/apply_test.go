package apply

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/util"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/templates"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

func TestParseOwnerRepo(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		want    string
		want1   string
		wantErr bool
	}{
		{
			name:  "valid",
			arg:   "owner/repo",
			want:  "owner",
			want1: "repo",
		},
		{
			name:    "invalid",
			arg:     "repo",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := util.ParseOwnerRepo(tt.arg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseOwnerRepo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if owner != tt.want {
				t.Errorf("ParseOwnerRepo() owner = %v, want %v", owner, tt.want)
			}
			if repo != tt.want1 {
				t.Errorf("ParseOwnerRepo() repo = %v, want %v", repo, tt.want1)
			}
		})
	}
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
