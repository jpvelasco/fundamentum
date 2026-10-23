package root

import (
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
)

// resetRootGlobals restores package-level flag state after a test mutates it.
func resetRootGlobals(t *testing.T) {
	t.Cleanup(func() {
		globals.DryRun = false
		globals.Verbose = false
		globals.Token = ""
		globals.NoOverwrite = false
		globals.Strict = false
		globals.RequireChecks = nil
		globals.CIPack = ""
		globals.ViaPR = false
		globals.Preset = ""
		globals.AdvancedSecurity = false
		globals.FromFile = ""
		globals.ResetExplicit()
	})
	globals.ResetExplicit()
}

func TestRequireChecksFlag(t *testing.T) {
	resetRootGlobals(t)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--require-checks", "Lint,Test (ubuntu-latest)", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"Lint", "Test (ubuntu-latest)"}
	if len(globals.RequireChecks) != len(want) {
		t.Fatalf("RequireChecks = %#v, want %#v", globals.RequireChecks, want)
	}
	for i, name := range want {
		if globals.RequireChecks[i] != name {
			t.Errorf("RequireChecks[%d] = %q, want %q", i, globals.RequireChecks[i], name)
		}
	}
}

func TestPresetFlag(t *testing.T) {
	resetRootGlobals(t)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--preset", "oss", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if globals.Preset != "oss" {
		t.Errorf("Preset = %q, want oss", globals.Preset)
	}
}

func TestPRFlag(t *testing.T) {
	resetRootGlobals(t)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--pr", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !globals.ViaPR {
		t.Error("expected ViaPR=true after --pr flag")
	}
}

func TestCIPackFlag(t *testing.T) {
	resetRootGlobals(t)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--ci", "generic", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if globals.CIPack != "generic" {
		t.Errorf("CIPack = %q, want generic", globals.CIPack)
	}
}

func TestStrictFalseRecordsExplicit(t *testing.T) {
	resetRootGlobals(t)

	// Cobra returns flag.ErrHelp before PersistentPreRun, so --help never
	// notes the flag. export RunE returns before any network call.
	cmd := newRootCmd()
	cmd.SetOut(&strings.Builder{})
	cmd.SetArgs([]string{"--strict=false", "export"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if globals.Strict {
		t.Error("expected Strict=false after --strict=false")
	}
	if !globals.Explicit("strict") {
		t.Error("expected --strict=false to be recorded as explicit")
	}
	if globals.Explicit("advanced-security") || globals.Explicit("no-overwrite") {
		t.Error("flags the operator did not pass must not be explicit")
	}
}

func TestNoteExplicitBaselineFlags(t *testing.T) {
	resetRootGlobals(t)

	cmd := newRootCmd()
	export := cmd.Commands()[0]
	for _, child := range cmd.Commands() {
		if child.Name() == "export" {
			export = child
			break
		}
	}
	// ParseFlags merges persistent flags; calling PreRun before that sees
	// an empty local set and Changed is always false.
	if err := export.ParseFlags([]string{"--strict=false", "--advanced-security=false", "--no-overwrite=false"}); err != nil {
		t.Fatal(err)
	}
	if err := noteExplicitBaselineFlags(export, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"strict", "advanced-security", "no-overwrite"} {
		if !globals.Explicit(name) {
			t.Errorf("%s was passed and must be explicit", name)
		}
	}
}

func TestChildCommandsInheritBaselinePreRun(t *testing.T) {
	cmd := newRootCmd()
	for _, child := range cmd.Commands() {
		if child.PersistentPreRunE != nil || child.PersistentPreRun != nil || child.PreRunE != nil || child.PreRun != nil {
			t.Errorf("%s sets its own pre-run; it must inherit the root baseline note", child.Name())
		}
	}
}

func TestStrictFlag(t *testing.T) {
	resetRootGlobals(t)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--strict", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !globals.Strict {
		t.Error("expected Strict=true after --strict flag")
	}
}

func TestDryRunFlag(t *testing.T) {
	resetRootGlobals(t)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--dry-run", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !globals.DryRun {
		t.Error("expected DryRun=true after --dry-run flag")
	}
}

func TestVersionFlag(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error on --version, got: %v", err)
	}
}

func TestVersionDefault(t *testing.T) {
	if Version == "" {
		t.Error("expected non-empty default Version")
	}
}
