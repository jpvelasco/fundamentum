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
	})
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
