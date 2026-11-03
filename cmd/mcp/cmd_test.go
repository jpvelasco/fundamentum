package mcp

import (
	"strings"
	"testing"
)

func TestNewCmd_Structure(t *testing.T) {
	SetVersion("v9.9.9")
	if serverVersion != "v9.9.9" {
		t.Errorf("serverVersion = %q, want v9.9.9", serverVersion)
	}
	cmd := NewCmd()
	if cmd.Use != "mcp" {
		t.Errorf("Use = %q, want mcp", cmd.Use)
	}
	if cmd.RunE == nil {
		t.Error("RunE must be set")
	}
	if err := cmd.Args(cmd, []string{"extra"}); err == nil {
		t.Error("mcp with args must error (Args: NoArgs)")
	}
}

func TestNewCmd_RunEServesAndStopsAtEOF(t *testing.T) {
	// A single initialize request then EOF: serve must return nil and write
	// exactly one response.
	cmd := NewCmd()
	cmd.SetIn(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n"))
	var out strings.Builder
	cmd.SetOut(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE error: %v", err)
	}
	if !strings.Contains(out.String(), `"protocolVersion"`) {
		t.Errorf("initialize response missing protocolVersion: %s", out.String())
	}
}
