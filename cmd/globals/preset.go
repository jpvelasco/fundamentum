package globals

import (
	"fmt"
	"strings"
)

// Named --preset values.
const (
	PresetOSS     = "oss"
	PresetPrivate = "private"
	PresetStrict  = "strict"
)

// Preset is the --preset value: oss, private, or strict. Empty means
// interactive defaults (prompt solo/team and GHAS when needed).
var Preset string

// ParsePreset normalizes --preset. Empty means no preset.
func ParsePreset(s string) (string, error) {
	switch p := strings.ToLower(strings.TrimSpace(s)); p {
	case "":
		return "", nil
	case PresetOSS, PresetPrivate, PresetStrict:
		return p, nil
	default:
		return "", fmt.Errorf("invalid --preset %q: use oss, private, or strict", s)
	}
}

// ApplyPreset sets flag defaults for a named preset. Explicit flags already
// set by the operator (Strict, AdvancedSecurity) are left alone.
func ApplyPreset(name string) error {
	p, err := ParsePreset(name)
	if err != nil {
		return err
	}
	switch p {
	case PresetStrict:
		Strict = true
		AdvancedSecurity = true
	case PresetOSS, PresetPrivate:
		// Public/private file sets still follow repo visibility. oss does
		// not force GHAS on a private repo; private does not enable it.
	}
	Preset = p
	return nil
}
