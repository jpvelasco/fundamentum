package templates

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	data := RepoData{Owner: "jpvelasco", RepoName: "fundamentum", DefaultBranch: "main"}
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
