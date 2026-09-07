package templates

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Supported --ci values. "auto" picks go when go.mod exists, otherwise generic.
const (
	CIPackAuto    = "auto"
	CIPackGo      = "go"
	CIPackGeneric = "generic"
	CIPackNone    = "none"
)

// ParseCIPack normalizes --ci. Empty means auto.
func ParseCIPack(s string) (string, error) {
	switch pack := strings.ToLower(strings.TrimSpace(s)); pack {
	case "", CIPackAuto:
		return CIPackAuto, nil
	case CIPackGo, CIPackGeneric, CIPackNone:
		return pack, nil
	default:
		return "", fmt.Errorf("invalid --ci %q: use auto, go, generic, or none", s)
	}
}

// ResolveCIPack turns a parsed flag into a concrete pack. auto uses goModExists.
func ResolveCIPack(flag string, goModExists bool) (string, error) {
	pack, err := ParseCIPack(flag)
	if err != nil {
		return "", err
	}
	if pack != CIPackAuto {
		return pack, nil
	}
	if goModExists {
		return CIPackGo, nil
	}
	return CIPackGeneric, nil
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
	if strings.HasPrefix(base, "generic_") {
		return CIPackGeneric
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
