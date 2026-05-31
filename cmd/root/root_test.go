package root

import (
	"testing"
)

func TestRootHelp(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error on --help, got: %v", err)
	}
}

func TestDryRunFlag(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--dry-run", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !DryRun {
		t.Error("expected DryRun=true after --dry-run flag")
	}
}
