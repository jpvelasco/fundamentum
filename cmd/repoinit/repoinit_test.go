package repoinit

import (
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
)

func TestNewCmd(t *testing.T) {
	cmd := NewCmd()
	if cmd.Use != "init OWNER/REPO" {
		t.Errorf("expected use 'init OWNER/REPO', got %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("expected non-empty Short")
	}
}

func TestNewCmd_PrivateFlag(t *testing.T) {
	cmd := NewCmd()
	if cmd.Flags().Lookup("private") == nil {
		t.Error("expected --private flag")
	}
}

func TestRun_InvalidArg(t *testing.T) {
	err := run("norepo", false)
	if err == nil {
		t.Error("expected error for invalid arg")
	}
}

// resetGlobals restores package-level flag state after a test mutates it.
func resetGlobals(t *testing.T) {
	t.Cleanup(func() {
		globals.DryRun = false
		globals.Token = ""
		globals.Verbose = false
	})
}

func TestRun_DryRunPath(t *testing.T) {
	resetGlobals(t)

	globals.DryRun = true
	err := run("owner/repo", false)
	// In dry-run mode, run() should skip CreateRepo entirely.
	// Any error should NOT contain "create repo" — that would mean
	// the dry-run branch was not taken.
	if err != nil && strings.Contains(err.Error(), "create repo") {
		t.Errorf("dry-run path should skip CreateRepo, got: %v", err)
	}
}

func TestRun_CreateRepo_Fails(t *testing.T) {
	tests := []struct {
		name    string
		private bool
	}{
		{
			name:    "private=false",
			private: false,
		},
		{
			name:    "private=true",
			private: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetGlobals(t)

			// With no token and no mock server, CreateRepo will fail with a
			// network error. The error should contain "create repo".
			err := run("owner/repo", tt.private)
			if err == nil {
				t.Error("expected error when CreateRepo fails")
			}
			if !strings.Contains(err.Error(), "create repo") {
				t.Errorf("expected 'create repo' in error, got: %v", err)
			}
		})
	}
}
