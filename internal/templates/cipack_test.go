package templates

import (
	"fmt"
	"strings"
	"testing"
)

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
		{"node", CIPackNode, false},
		{"python", CIPackPython, false},
		{"rust", CIPackRust, false},
		{"java", "", true},
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
		{"node", false, CIPackNode, false},
		{"java", false, "", true},
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

func TestDetectPack(t *testing.T) {
	if got := DetectPack(Manifests{GoMod: true, PackageJSON: true}); got != CIPackGo {
		t.Errorf("go wins = %q", got)
	}
	if got := DetectPack(Manifests{PackageJSON: true}); got != CIPackNode {
		t.Errorf("node = %q", got)
	}
	if got := DetectPack(Manifests{PyProject: true}); got != CIPackPython {
		t.Errorf("python = %q", got)
	}
	if got := DetectPack(Manifests{CargoToml: true}); got != CIPackRust {
		t.Errorf("rust = %q", got)
	}
	if got := DetectPack(Manifests{}); got != CIPackGeneric {
		t.Errorf("empty = %q", got)
	}
}

func TestDetectManifests(t *testing.T) {
	m, err := DetectManifests(func(path string) (bool, error) {
		return path == "package.json", nil
	})
	if err != nil || !m.PackageJSON || m.GoMod {
		t.Fatalf("DetectManifests() = %+v, %v", m, err)
	}
	_, err = DetectManifests(func(path string) (bool, error) {
		if path == "Cargo.toml" {
			return false, fmt.Errorf("boom")
		}
		return false, nil
	})
	if err == nil || !strings.Contains(err.Error(), "detect Cargo.toml") {
		t.Fatalf("error = %v", err)
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
		{"dotgithub/workflows/node_ci.yml", CIPackNode, true},
		{"dotgithub/workflows/node_ci.yml", CIPackGo, false},
		{"dotgithub/workflows/python_ci.yml", CIPackPython, true},
		{"dotgithub/workflows/rust_ci.yml", CIPackRust, true},
	}
	for _, tt := range tests {
		if got := packMatches(tt.path, tt.pack); got != tt.want {
			t.Errorf("packMatches(%q, %q) = %v, want %v", tt.path, tt.pack, got, tt.want)
		}
	}
}
