package root

import (
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
	})
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
