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
		ResetExplicit()
	})
	ResetExplicit()
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

func TestApplyBaseline_ExplicitFalseBeatsFileTrue(t *testing.T) {
	resetBaseline(t)
	NoteExplicit("strict")
	NoteExplicit("advanced-security")
	NoteExplicit("no-overwrite")
	Strict = false
	AdvancedSecurity = false
	NoOverwrite = false
	if err := ApplyBaseline(Baseline{
		AdvancedSecurity: true,
		Strict:           true,
		NoOverwrite:      true,
	}); err != nil {
		t.Fatal(err)
	}
	if Strict || AdvancedSecurity || NoOverwrite {
		t.Fatalf("explicit false must stay false: Strict=%v GHAS=%v NoOverwrite=%v", Strict, AdvancedSecurity, NoOverwrite)
	}
}

func TestApplyBaseline_UnnotedBoolInherits(t *testing.T) {
	resetBaseline(t)
	NoteExplicit("strict")
	Strict = false
	if err := ApplyBaseline(Baseline{Strict: true, AdvancedSecurity: true, NoOverwrite: true}); err != nil {
		t.Fatal(err)
	}
	if Strict {
		t.Fatal("explicit --strict=false must stay false")
	}
	if !AdvancedSecurity || !NoOverwrite {
		t.Fatalf("unnoted bools must inherit: GHAS=%v NoOverwrite=%v", AdvancedSecurity, NoOverwrite)
	}
}

func TestApplyBaseline_ExplicitTrueBeatsFileFalse(t *testing.T) {
	resetBaseline(t)
	NoteExplicit("strict")
	NoteExplicit("advanced-security")
	NoteExplicit("no-overwrite")
	Strict = true
	AdvancedSecurity = true
	NoOverwrite = true
	if err := ApplyBaseline(Baseline{}); err != nil {
		t.Fatal(err)
	}
	if !Strict || !AdvancedSecurity || !NoOverwrite {
		t.Fatalf("explicit true must stay true: Strict=%v GHAS=%v NoOverwrite=%v", Strict, AdvancedSecurity, NoOverwrite)
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

func TestCurrentBaseline_NewFields(t *testing.T) {
	resetBaseline(t)
	NoteExplicit("branch-up-to-date")
	BranchUpToDate = false
	BranchUpToDateSet = true
	SkipSteps = []string{"protect-main", "socket.yml"}
	OnlySteps = []string{"security"}
	Octopus = true
	CodacyInstructions = true
	t.Cleanup(func() {
		BranchUpToDate = true
		BranchUpToDateSet = false
		SkipSteps = nil
		OnlySteps = nil
		Octopus = false
		CodacyInstructions = false
	})

	var out strings.Builder
	if err := WriteBaseline(&out, CurrentBaseline()); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		`"skip"`, `"protect-main"`, `"socket.yml"`,
		`"only"`, `"security"`,
		`"octopus": true`,
		`"codacy_instructions": true`,
		`"require_branch_up_to_date": false`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestApplyBaseline_NewFieldsFillUnset(t *testing.T) {
	resetBaseline(t)
	falseUpToDate := false
	if err := ApplyBaseline(Baseline{
		Skip:                  []string{"security"},
		Only:                  []string{"files"},
		Octopus:               true,
		CodacyInstructions:    true,
		RequireBranchUpToDate: &falseUpToDate,
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		BranchUpToDate = true
		BranchUpToDateSet = false
	}()
	if len(SkipSteps) != 1 || SkipSteps[0] != "security" {
		t.Errorf("SkipSteps = %v", SkipSteps)
	}
	if len(OnlySteps) != 1 || OnlySteps[0] != "files" {
		t.Errorf("OnlySteps = %v", OnlySteps)
	}
	if !Octopus || !CodacyInstructions {
		t.Errorf("opt-in bools not applied: octopus=%v codacy=%v", Octopus, CodacyInstructions)
	}
	if BranchUpToDate || !BranchUpToDateSet {
		t.Errorf("tri-state not applied: up-to-date=%v set=%v", BranchUpToDate, BranchUpToDateSet)
	}
}

func TestApplyBaseline_BranchUpToDateExplicitWins(t *testing.T) {
	resetBaseline(t)
	NoteExplicit("branch-up-to-date")
	t.Cleanup(func() {
		BranchUpToDate = true
		BranchUpToDateSet = false
	})
	// Explicit flag true must beat the file's false.
	if err := ApplyBaseline(Baseline{RequireBranchUpToDate: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	if !BranchUpToDate {
		t.Error("explicit --branch-up-to-date must win over the baseline file")
	}
}

func TestApplyBaseline_SetStateBlocksReread(t *testing.T) {
	resetBaseline(t)
	t.Cleanup(func() {
		BranchUpToDate = true
		BranchUpToDateSet = false
	})
	// First load applies the file value and marks it set.
	if err := ApplyBaseline(Baseline{RequireBranchUpToDate: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	if !BranchUpToDateSet {
		t.Fatal("baseline tri-state must mark the value as set")
	}
	// A second load (different value) must not overwrite the already-set one.
	if err := ApplyBaseline(Baseline{RequireBranchUpToDate: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	if !BranchUpToDate {
		t.Error("an already-set tri-state must not be overwritten by a later baseline")
	}
}

func boolPtr(b bool) *bool { return &b }
