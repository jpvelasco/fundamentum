package templates

import "testing"

func TestParseCIPack(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", CIPackAuto, false},
		{" auto ", CIPackAuto, false},
		{"GO", CIPackGo, false},
		{"generic", CIPackGeneric, false},
		{"none", CIPackNone, false},
		{"rust", "", true},
	}
	for _, tt := range tests {
		got, err := ParseCIPack(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseCIPack(%q) error = nil, want error", tt.in)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("ParseCIPack(%q) = (%q, %v), want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestResolveCIPack(t *testing.T) {
	tests := []struct {
		flag    string
		goMod   bool
		want    string
		wantErr bool
	}{
		{"", true, CIPackGo, false},
		{"auto", false, CIPackGeneric, false},
		{"go", false, CIPackGo, false},
		{"generic", true, CIPackGeneric, false},
		{"none", true, CIPackNone, false},
		{"node", false, "", true},
	}
	for _, tt := range tests {
		got, err := ResolveCIPack(tt.flag, tt.goMod)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ResolveCIPack(%q, %v) error = nil, want error", tt.flag, tt.goMod)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("ResolveCIPack(%q, %v) = (%q, %v), want %q", tt.flag, tt.goMod, got, err, tt.want)
		}
	}
}

func TestPackMatches(t *testing.T) {
	tests := []struct {
		path string
		pack string
		want bool
	}{
		{"dotgithub/workflows/public_ci.yml", CIPackGo, true},
		{"dotgithub/workflows/public_ci.yml", CIPackGeneric, false},
		{"dotgithub/workflows/generic_ci.yml", CIPackGeneric, true},
		{"dotgithub/workflows/generic_ci.yml", CIPackGo, false},
		{"dotgithub/workflows/generic_ci.yml", CIPackNone, false},
		{"dotgithub/workflows/public_ci.yml", "", true}, // empty pack = go
		{"dotgithub/CODEOWNERS", CIPackNone, true},
		{"socket.yml", CIPackGeneric, true},
		{"public_codecov.yml", CIPackGo, true},
		{"public_codecov.yml", CIPackGeneric, false},
		{"dotgithub/workflows/codacy-coverage.yml", CIPackGo, true},
		{"dotgithub/workflows/codacy-coverage.yml", CIPackNone, false},
	}
	for _, tt := range tests {
		if got := packMatches(tt.path, tt.pack); got != tt.want {
			t.Errorf("packMatches(%q, %q) = %v, want %v", tt.path, tt.pack, got, tt.want)
		}
	}
}
