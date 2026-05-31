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
	for _, f := range files {
		if f.Path == "CONTRIBUTING.md" {
			found = true
			if strings.Contains(f.Content, "{{") {
				t.Error("CONTRIBUTING.md still has unrendered template placeholders")
			}
		}
		if strings.HasPrefix(f.Path, "dotgithub/") {
			t.Errorf("path %q still has dotgithub prefix, expected .github/", f.Path)
		}
		if strings.HasPrefix(f.Path, "dotcodacy/") {
			t.Errorf("path %q still has dotcodacy prefix, expected .codacy/", f.Path)
		}
	}
	if !found {
		t.Error("CONTRIBUTING.md not found in rendered files")
	}
}
