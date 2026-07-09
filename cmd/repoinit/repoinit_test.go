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

func TestRun_DryRunPath(t *testing.T) {
	t.Cleanup(func() {
		globals.DryRun = false
		globals.Token = ""
		globals.Verbose = false
	})

	globals.DryRun = true

	err := run("owner/repo", false)
	// The apply command's run() makes API calls even in dry-run mode,
	// so we expect an error from the apply subcommand (network error).
	// The key assertion is that run() does not return a "create repo" error
	// — the dry-run path skips CreateRepo entirely.
	if err != nil {
		if strings.Contains(err.Error(), "create repo") {
			t.Error("dry-run path should skip CreateRepo, got create repo error")
		}
	}
}

func TestRun_NonDryRun_ReturnsError(t *testing.T) {
	t.Cleanup(func() {
		globals.DryRun = false
		globals.Token = ""
		globals.Verbose = false
	})

	// With no token and no mock server, CreateRepo will fail with a
	// network error. The error should contain "create repo".
	err := run("owner/repo", false)
	if err == nil {
		t.Error("expected error when CreateRepo fails")
	}
	if !strings.Contains(err.Error(), "create repo") {
		t.Errorf("expected 'create repo' in error, got: %v", err)
	}
}

func TestRun_GlobalStateReset(t *testing.T) {
	// Verify globals are not left in a dirty state after run().
	origDryRun := globals.DryRun
	origToken := globals.Token
	origVerbose := globals.Verbose
	origNoOverwrite := globals.NoOverwrite
	origViaPR := globals.ViaPR

	t.Cleanup(func() {
		globals.DryRun = origDryRun
		globals.Token = origToken
		globals.Verbose = origVerbose
		globals.NoOverwrite = origNoOverwrite
		globals.ViaPR = origViaPR
	})

	globals.DryRun = false
	globals.Token = ""
	globals.Verbose = false

	_ = run("owner/repo", false)

	if globals.DryRun != origDryRun {
		t.Errorf("globals.DryRun changed: got %v, want %v", globals.DryRun, origDryRun)
	}
	if globals.Token != origToken {
		t.Errorf("globals.Token changed: got %q, want %q", globals.Token, origToken)
	}
	if globals.Verbose != origVerbose {
		t.Errorf("globals.Verbose changed: got %v, want %v", globals.Verbose, origVerbose)
	}
}

func TestRun_PrivateFlag(t *testing.T) {
	t.Cleanup(func() {
		globals.DryRun = false
		globals.Token = ""
		globals.Verbose = false
	})

	// private=true should still fail with no token, but error should
	// contain "create repo" (not a different error path).
	err := run("owner/repo", true)
	if err == nil {
		t.Error("expected error when CreateRepo fails with private=true")
	}
	if !strings.Contains(err.Error(), "create repo") {
		t.Errorf("expected 'create repo' in error for private=true, got: %v", err)
	}
}