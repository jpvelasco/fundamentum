package globals

import "testing"

func TestParsePreset(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{" OSS ", PresetOSS, false},
		{"private", PresetPrivate, false},
		{"strict", PresetStrict, false},
		{"enterprise", "", true},
	}
	for _, tt := range tests {
		got, err := ParsePreset(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParsePreset(%q) error = nil, want error", tt.in)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("ParsePreset(%q) = (%q, %v), want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestApplyPreset(t *testing.T) {
	t.Cleanup(func() {
		Preset = ""
		Strict = false
		AdvancedSecurity = false
	})

	if err := ApplyPreset("nope"); err == nil {
		t.Fatal("ApplyPreset(nope) error = nil, want error")
	}

	Strict = false
	AdvancedSecurity = false
	if err := ApplyPreset("oss"); err != nil || Preset != PresetOSS {
		t.Fatalf("oss: Preset=%q err=%v", Preset, err)
	}
	if Strict || AdvancedSecurity {
		t.Fatal("oss must not force Strict or AdvancedSecurity")
	}

	if err := ApplyPreset("strict"); err != nil {
		t.Fatal(err)
	}
	if !Strict || !AdvancedSecurity || Preset != PresetStrict {
		t.Fatalf("strict: Strict=%v AdvancedSecurity=%v Preset=%q", Strict, AdvancedSecurity, Preset)
	}
}
