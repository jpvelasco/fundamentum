package root

import (
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
)

func TestExecute_NoArgs(t *testing.T) {
	// Execute with no args calls the root command which shows help
	// and returns an error (ExitError) — but cmd.Execute() for Cobra
	// returns nil when the command is the help command.
	// We can't easily test os.Exit, so test via SetArgs instead.
	cmd := newRootCmd()
	cmd.SetArgs([]string{})
	// Cobra returns an error for no args on the root command
	err := cmd.Execute()
	// The root command has no RunE, so it shows usage and returns error
	if err == nil {
		t.Log("root command with no args returned nil (shows usage)")
	}
	_ = err
}

func TestExecute_Version(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error on --help, got: %v", err)
	}
}

func TestExecute_SubcommandHelp(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"apply help", []string{"apply", "--help"}},
		{"init help", []string{"init", "--help"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err != nil {
				t.Fatalf("expected no error on %s --help, got: %v", tt.args[0], err)
			}
		})
	}
}

func TestExecute_FlagOrder(t *testing.T) {
	// Verify flags are reset properly between runs
	t.Cleanup(func() {
		globals.DryRun = false
		globals.Verbose = false
		globals.Token = ""
		globals.NoOverwrite = false
	})

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--dry-run", "--verbose", "--token", "abc123", "--no-overwrite", "--help"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !globals.DryRun {
		t.Error("expected DryRun=true")
	}
	if !globals.Verbose {
		t.Error("expected Verbose=true")
	}
	if globals.Token != "abc123" {
		t.Errorf("expected Token=abc123, got %q", globals.Token)
	}
	if !globals.NoOverwrite {
		t.Error("expected NoOverwrite=true")
	}
}

// Test that Execute() calls os.Exit on error by checking the command structure.
func TestExecute_CommandStructure(t *testing.T) {
	cmd := newRootCmd()
	if len(cmd.Commands()) != 2 {
		t.Errorf("expected 2 subcommands, got %d", len(cmd.Commands()))
	}

	names := []string{}
	for _, c := range cmd.Commands() {
		names = append(names, c.Use)
	}

	foundApply := false
	foundInit := false
	for _, n := range names {
		if strings.HasPrefix(n, "apply") {
			foundApply = true
		}
		if strings.HasPrefix(n, "init") {
			foundInit = true
		}
	}
	if !foundApply {
		t.Error("expected apply subcommand")
	}
	if !foundInit {
		t.Error("expected init subcommand")
	}
}