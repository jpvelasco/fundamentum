package templatefs

import (
	"io"
	"io/fs"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestEmbeddedYAMLParses guards against shipping structurally invalid YAML
// templates (e.g. a lost indentation level), which GitHub Actions rejects as
// "Invalid workflow file" — silently disabling CI on every hardened repo.
// Markdown and other non-YAML templates are skipped; only *.yml/*.yaml parse.
func TestEmbeddedYAMLParses(t *testing.T) {
	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			return nil
		}
		f, err := FS.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		raw, err := io.ReadAll(f)
		if err != nil {
			return err
		}
		var doc any
		if yamlErr := yaml.Unmarshal(raw, &doc); yamlErr != nil {
			t.Errorf("template %s is not valid YAML: %v", path, yamlErr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk templates: %v", err)
	}
}
