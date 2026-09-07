package exportcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
)

func TestNewCmd(t *testing.T) {
	cmd := NewCmd()
	if cmd.Use != "export" {
		t.Errorf("Use = %q, want export", cmd.Use)
	}
	if cmd.Flags().Lookup("output") == nil {
		t.Error("expected --output flag")
	}
}

func TestRun_Stdout(t *testing.T) {
	t.Cleanup(func() {
		globals.Preset = ""
		globals.Strict = false
		globals.AdvancedSecurity = false
	})
	globals.Preset = "oss"
	var out strings.Builder
	if err := run("", &out); err != nil {
		t.Fatalf("run() error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `"version": 1`) || !strings.Contains(got, `"preset": "oss"`) {
		t.Errorf("expected oss baseline JSON, got:\n%s", got)
	}
}

func TestRun_File(t *testing.T) {
	t.Cleanup(func() { globals.Preset = "" })
	globals.Preset = "strict"
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")
	var out strings.Builder
	if err := run(path, &out); err != nil {
		t.Fatalf("run() error: %v", err)
	}
	if !strings.Contains(out.String(), "wrote baseline") {
		t.Errorf("expected write confirmation, got:\n%s", out.String())
	}
	if !strings.HasPrefix(path, dir+string(os.PathSeparator)) && path != filepath.Join(dir, "baseline.json") {
		t.Fatalf("path %q escaped temp dir %q", path, dir)
	}
	raw, err := os.ReadFile(path) // path is t.TempDir()/baseline.json
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"preset": "strict"`) {
		t.Errorf("file = %s", raw)
	}
}

func TestRun_InvalidPreset(t *testing.T) {
	t.Cleanup(func() { globals.Preset = "" })
	globals.Preset = "enterprise"
	if err := run("", &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "invalid --preset") {
		t.Fatalf("error = %v, want invalid --preset", err)
	}
}

func TestRun_CreateError(t *testing.T) {
	err := run(filepath.Join(t.TempDir(), "missing", "baseline.json"), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "create") {
		t.Fatalf("error = %v, want create", err)
	}
}
