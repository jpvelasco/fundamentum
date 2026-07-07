package templates

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	data := RepoData{Owner: "jpvelasco", RepoName: "fundamentum", DefaultBranch: "main", Visibility: "private"}
	files, err := Render(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one rendered file")
	}
	found := false
	codacyFound := false
	for _, f := range files {
		if f.Path == ".github/CONTRIBUTING.md" {
			found = true
			if strings.Contains(f.Content, "{{") {
				t.Error(".github/CONTRIBUTING.md still has unrendered template placeholders")
			}
		}
		if f.Path == ".codacy.yml" {
			codacyFound = true
		}
		if strings.HasPrefix(f.Path, "dotgithub/") {
			t.Errorf("path %q still has dotgithub prefix, expected .github/", f.Path)
		}
		if strings.HasPrefix(f.Path, "dotcodacy") {
			t.Errorf("path %q still has dotcodacy prefix, expected .codacy.yml", f.Path)
		}
	}
	if !found {
		t.Error(".github/CONTRIBUTING.md not found in rendered files")
	}
	if !codacyFound {
		t.Error(".codacy.yml not found in rendered files")
	}
}

func TestRenderVisibilityFiltering(t *testing.T) {
	tests := []struct {
		name            string
		visibility      string
		wantPublicFiles []string
		wantPrivateFiles []string
	}{
		{
			name:       "public repo",
			visibility: "public",
			wantPublicFiles: []string{
				".github/workflows/codecov.yml",
				".github/workflows/octopus.yml",
				".github/workflows/codeql.yml",
				".github/codeql/codeql-config.yml",
			},
		},
		{
			name:       "private repo",
			visibility: "private",
			wantPrivateFiles: []string{
				".github/workflows/octocov.yml",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := RepoData{Owner: "o", RepoName: "r", DefaultBranch: "main", Visibility: tt.visibility}
			files, err := Render(data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			pathSet := make(map[string]bool)
			for _, f := range files {
				pathSet[f.Path] = true
			}
			for _, want := range tt.wantPublicFiles {
				if !pathSet[want] {
					t.Errorf("public: missing %q in rendered files", want)
				}
			}
			for _, want := range tt.wantPrivateFiles {
				if !pathSet[want] {
					t.Errorf("private: missing %q in rendered files", want)
				}
			}
			// Verify opposite visibility files are excluded.
			if tt.visibility == "public" {
				if pathSet[".github/workflows/octocov.yml"] {
					t.Error("public: octocov.yml should not be rendered for public repos")
				}
			} else {
				if pathSet[".github/workflows/codecov.yml"] {
					t.Error("private: codecov.yml should not be rendered for private repos")
				}
				if pathSet[".github/workflows/octopus.yml"] {
					t.Error("private: octopus.yml should not be rendered for private repos")
				}
				if pathSet[".github/workflows/codeql.yml"] {
					t.Error("private: codeql.yml should not be rendered for private repos")
				}
			}
		})
	}
}
