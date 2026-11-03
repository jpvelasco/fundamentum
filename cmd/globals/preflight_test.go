package globals

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSharedFlags(t *testing.T) {
	t.Cleanup(func() {
		FromFile = ""
		Preset = ""
		Strict = false
		AdvancedSecurity = false
		CIPack = ""
	})

	// No baseline, no preset: no-op.
	if err := LoadSharedFlags(); err != nil {
		t.Fatalf("LoadSharedFlags() error = %v", err)
	}

	// Baseline loads, then the named preset applies on top without clobbering
	// baseline values it does not own.
	path := filepath.Join(t.TempDir(), "b.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"ci":"generic"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	FromFile = path
	Preset = PresetStrict
	if err := LoadSharedFlags(); err != nil {
		t.Fatalf("LoadSharedFlags() error = %v", err)
	}
	if CIPack != "generic" {
		t.Fatalf("baseline CI = %q, want generic", CIPack)
	}
	if Preset != PresetStrict || !Strict || !AdvancedSecurity {
		t.Fatalf("preset after load: Preset=%q Strict=%v AdvancedSecurity=%v", Preset, Strict, AdvancedSecurity)
	}
}

func TestLoadSharedFlags_Errors(t *testing.T) {
	t.Cleanup(func() {
		FromFile = ""
		Preset = ""
	})

	FromFile = filepath.Join(t.TempDir(), "missing.json")
	if err := LoadSharedFlags(); err == nil {
		t.Fatal("LoadSharedFlags() error = nil, want error for missing baseline")
	}

	FromFile = ""
	Preset = "nope"
	if err := LoadSharedFlags(); err == nil {
		t.Fatal("LoadSharedFlags() error = nil, want error for bad preset")
	}
}
