package exportcmd

import (
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
	globals.Preset = ""
	if err := globals.LoadBaselineFile(path); err != nil {
		t.Fatal(err)
	}
	if globals.Preset != "strict" {
		t.Errorf("loaded preset = %q, want strict", globals.Preset)
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
