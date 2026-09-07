package globals

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func resetBaseline(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		Preset = ""
		CIPack = ""
		RequireChecks = nil
		AdvancedSecurity = false
		Strict = false
		NoOverwrite = false
		FromFile = ""
	})
}

func TestCurrentAndWriteBaseline(t *testing.T) {
	resetBaseline(t)
	Preset = "oss"
	CIPack = "generic"
	RequireChecks = []string{"CI", "Trivy"}
	var out strings.Builder
	if err := WriteBaseline(&out, CurrentBaseline()); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{`"version": 1`, `"preset": "oss"`, `"ci": "generic"`, `"CI"`, `"Trivy"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestApplyBaseline_FillsUnset(t *testing.T) {
	resetBaseline(t)
	if err := ApplyBaseline(Baseline{
		Version:          BaselineVersion,
		Preset:           "strict",
		CI:               "none",
		RequireChecks:    []string{"Lint"},
		AdvancedSecurity: true,
		Strict:           true,
		NoOverwrite:      true,
	}); err != nil {
		t.Fatal(err)
	}
	if Preset != "strict" || CIPack != "none" || !AdvancedSecurity || !Strict || !NoOverwrite {
		t.Fatalf("got Preset=%q CI=%q GHAS=%v Strict=%v NoOverwrite=%v", Preset, CIPack, AdvancedSecurity, Strict, NoOverwrite)
	}
	if len(RequireChecks) != 1 || RequireChecks[0] != "Lint" {
		t.Fatalf("RequireChecks = %#v", RequireChecks)
	}
}

func TestApplyBaseline_ExplicitFlagsWin(t *testing.T) {
	resetBaseline(t)
	Preset = "oss"
	CIPack = "go"
	RequireChecks = []string{"Lint"}
	if err := ApplyBaseline(Baseline{Preset: "strict", CI: "none", RequireChecks: []string{"CI"}}); err != nil {
		t.Fatal(err)
	}
	gotPreset, gotCI := Preset, CIPack
	if gotPreset != "oss" || gotCI != "go" || RequireChecks[0] != "Lint" {
		t.Fatalf("explicit flags must win: Preset=%q CI=%q checks=%v", gotPreset, gotCI, RequireChecks)
	}
}

func TestApplyBaseline_BadVersion(t *testing.T) {
	if err := ApplyBaseline(Baseline{Version: 99}); err == nil || !strings.Contains(err.Error(), "unsupported baseline version") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyBaseline_BadPreset(t *testing.T) {
	resetBaseline(t)
	if err := ApplyBaseline(Baseline{Preset: "enterprise"}); err == nil || !strings.Contains(err.Error(), "invalid --preset") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadBaselineFile(t *testing.T) {
	resetBaseline(t)
	path := filepath.Join(t.TempDir(), "b.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"preset":"oss","ci":"generic"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadBaselineFile(path); err != nil {
		t.Fatal(err)
	}
	if Preset != "oss" || CIPack != "generic" {
		t.Fatalf("loaded Preset=%q CI=%q", Preset, CIPack)
	}
}

func TestLoadBaselineFile_Errors(t *testing.T) {
	if err := LoadBaselineFile(filepath.Join(t.TempDir(), "missing.json")); err == nil || !strings.Contains(err.Error(), "read baseline") {
		t.Fatalf("missing = %v", err)
	}
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadBaselineFile(path); err == nil || !strings.Contains(err.Error(), "parse baseline") {
		t.Fatalf("parse = %v", err)
	}
	if err := LoadBaselineFile(""); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty = %v", err)
	}
	if err := LoadBaselineFile(t.TempDir()); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("dir = %v", err)
	}
}

func TestCreateBaselineFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	f, err := CreateBaselineFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %o, want 0600", info.Mode().Perm())
	}
	if _, err := CreateBaselineFile(""); err == nil {
		t.Fatal("empty path must fail")
	}
}
