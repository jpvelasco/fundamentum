package globals

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// BaselineVersion is the portable baseline schema version.
const BaselineVersion = 1

// Baseline is a portable harden config that apply --from can reapply.
type Baseline struct {
	Version          int      `json:"version"`
	Preset           string   `json:"preset,omitempty"`
	CI               string   `json:"ci,omitempty"`
	RequireChecks    []string `json:"require_checks,omitempty"`
	AdvancedSecurity bool     `json:"advanced_security,omitempty"`
	Strict           bool     `json:"strict,omitempty"`
	NoOverwrite      bool     `json:"no_overwrite,omitempty"`
}

// CurrentBaseline snapshots the active flags into a portable baseline.
func CurrentBaseline() Baseline {
	b := Baseline{
		Version:          BaselineVersion,
		Preset:           Preset,
		CI:               CIPack,
		AdvancedSecurity: AdvancedSecurity,
		Strict:           Strict,
		NoOverwrite:      NoOverwrite,
	}
	if RequireChecks != nil {
		b.RequireChecks = append([]string{}, RequireChecks...)
	}
	return b
}

// WriteBaseline encodes b as indented JSON.
func WriteBaseline(w io.Writer, b Baseline) error {
	if b.Version == 0 {
		b.Version = BaselineVersion
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(b); err != nil {
		return fmt.Errorf("encode baseline: %w", err)
	}
	return nil
}

// LoadBaselineFile reads a JSON baseline and applies it to flag state.
// Explicit non-empty flags already set by the operator win over the file.
func LoadBaselineFile(path string) error {
	raw, err := readBaselineBytes(path)
	if err != nil {
		return err
	}
	var b Baseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return fmt.Errorf("parse baseline %s: %w", path, err)
	}
	return ApplyBaseline(b)
}

// ApplyBaseline copies file values into unset flag slots.
func ApplyBaseline(b Baseline) error {
	if b.Version != 0 && b.Version != BaselineVersion {
		return fmt.Errorf("unsupported baseline version %d (want %d)", b.Version, BaselineVersion)
	}
	if Preset == "" && b.Preset != "" {
		if _, err := ParsePreset(b.Preset); err != nil {
			return err
		}
		Preset = b.Preset
	}
	if CIPack == "" && b.CI != "" {
		CIPack = b.CI
	}
	if RequireChecks == nil && b.RequireChecks != nil {
		RequireChecks = append([]string{}, b.RequireChecks...)
	}
	if !AdvancedSecurity {
		AdvancedSecurity = b.AdvancedSecurity
	}
	if !Strict {
		Strict = b.Strict
	}
	if !NoOverwrite {
		NoOverwrite = b.NoOverwrite
	}
	return nil
}

// FromFile is the --from path for apply/audit.
var FromFile string

func readBaselineBytes(path string) ([]byte, error) {
	clean, err := regularFile(path)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(clean) // #nosec G304 -- path is a regular file the operator named
	if err != nil {
		return nil, fmt.Errorf("read baseline %s: %w", path, err)
	}
	return raw, nil
}

// CreateBaselineFile creates path with 0600 so the baseline is not world-readable.
func CreateBaselineFile(path string) (*os.File, error) {
	clean, err := cleanPath(path)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(clean, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- operator-named output
}

func regularFile(path string) (string, error) {
	clean, err := cleanPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(clean)
	if err != nil {
		return "", fmt.Errorf("read baseline %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("read baseline %s: not a regular file", path)
	}
	return clean, nil
}

func cleanPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("baseline path is empty")
	}
	return filepath.Clean(path), nil
}
