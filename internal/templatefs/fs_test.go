package templatefs

import (
	"io/fs"
	"testing"
)

func TestFS_Init(t *testing.T) {
	if FS == nil {
		t.Fatal("FS should not be nil after init")
	}
}

func TestFS_ContainsTemplates(t *testing.T) {
	count := 0
	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir error: %v", err)
	}
	if count == 0 {
		t.Error("expected at least one template file")
	}
}

func TestFS_ReadDir(t *testing.T) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected entries in root")
	}
	// The root should contain "dotgithub" and "dotcodacy.yml"
	foundDotGitHub := false
	foundDotCodacy := false
	for _, e := range entries {
		if e.Name() == "dotgithub" {
			foundDotGitHub = true
		}
		if e.Name() == "dotcodacy.yml" {
			foundDotCodacy = true
		}
	}
	if !foundDotGitHub {
		t.Error("expected dotgithub directory")
	}
	if !foundDotCodacy {
		t.Error("expected dotcodacy.yml file")
	}
}