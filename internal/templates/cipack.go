package templates

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Supported --ci values. "auto" picks from repo manifests, else generic.
const (
	CIPackAuto    = "auto"
	CIPackGo      = "go"
	CIPackNode    = "node"
	CIPackPython  = "python"
	CIPackRust    = "rust"
	CIPackGeneric = "generic"
	CIPackNone    = "none"
)

// Manifests is which language files exist in the target repo.
type Manifests struct {
	GoMod        bool
	PackageJSON  bool
	PyProject    bool
	Requirements bool
	CargoToml    bool
}

// ParseCIPack normalizes --ci. Empty means auto.
func ParseCIPack(s string) (string, error) {
	switch pack := strings.ToLower(strings.TrimSpace(s)); pack {
	case "", CIPackAuto:
		return CIPackAuto, nil
	case CIPackGo, CIPackNode, CIPackPython, CIPackRust, CIPackGeneric, CIPackNone:
		return pack, nil
	default:
		return "", fmt.Errorf("invalid --ci %q: use auto, go, node, python, rust, generic, or none", s)
	}
}

// ResolveCIPack turns a parsed flag into a concrete pack. auto uses manifests.
func ResolveCIPack(flag string, goModExists bool) (string, error) {
	return ResolveCIPackFrom(flag, Manifests{GoMod: goModExists})
}

// ResolveCIPackFrom is ResolveCIPack with the full manifest set.
func ResolveCIPackFrom(flag string, m Manifests) (string, error) {
	pack, err := ParseCIPack(flag)
	if err != nil {
		return "", err
	}
	if pack != CIPackAuto {
		return pack, nil
	}
	return DetectPack(m), nil
}

// DetectManifests checks the language manifests via exists.
func DetectManifests(exists func(path string) (bool, error)) (Manifests, error) {
	var m Manifests
	for _, ck := range []struct {
		path string
		set  *bool
	}{
		{"go.mod", &m.GoMod},
		{"package.json", &m.PackageJSON},
		{"pyproject.toml", &m.PyProject},
		{"requirements.txt", &m.Requirements},
		{"Cargo.toml", &m.CargoToml},
	} {
		ok, err := exists(ck.path)
		if err != nil {
			return Manifests{}, fmt.Errorf("detect %s: %w", ck.path, err)
		}
		*ck.set = ok
	}
	return m, nil
}

// DetectPack picks a pack from manifests. Go wins, then Node, Python, Rust.
func DetectPack(m Manifests) string {
	switch {
	case m.GoMod:
		return CIPackGo
	case m.PackageJSON:
		return CIPackNode
	case m.PyProject || m.Requirements:
		return CIPackPython
	case m.CargoToml:
		return CIPackRust
	default:
		return CIPackGeneric
	}
}

// normalizeCIPack maps empty (legacy Render callers) to the Go pack so existing
// tests and direct Render() use keep shipping the historical Go CI templates.
func normalizeCIPack(pack string) string {
	if pack == "" {
		return CIPackGo
	}
	return pack
}

func templatePack(path string) string {
	base := filepath.Base(path)
	for _, p := range []struct{ prefix, pack string }{
		{"generic_", CIPackGeneric},
		{"node_", CIPackNode},
		{"python_", CIPackPython},
		{"rust_", CIPackRust},
	} {
		if strings.HasPrefix(base, p.prefix) {
			return p.pack
		}
	}
	switch base {
	case "public_ci.yml", "private_ci.yml", "public_codecov.yml", "private_octocov.yml",
		"public_codeql.yml", "public_codeql-config.yml", "codacy-coverage.yml":
		return CIPackGo
	default:
		return ""
	}
}

func packMatches(path, pack string) bool {
	need := templatePack(path)
	if need == "" {
		return true
	}
	return need == normalizeCIPack(pack)
}
