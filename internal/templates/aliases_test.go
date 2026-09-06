package templates

import "testing"

func TestFileCheckPaths(t *testing.T) {
	got := FileCheckPaths(".github/CODEOWNERS")
	if len(got) != 2 || got[1] != "CODEOWNERS" {
		t.Errorf("CODEOWNERS aliases = %#v", got)
	}
	if got := FileCheckPaths("README.md"); len(got) != 1 || got[0] != "README.md" {
		t.Errorf("unaliased = %#v", got)
	}
}
