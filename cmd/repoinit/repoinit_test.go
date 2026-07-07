package repoinit

import "testing"

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