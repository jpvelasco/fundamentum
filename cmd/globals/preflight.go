package globals

// LoadSharedFlags runs the flag preflight every subcommand needs before it
// reads flag state: load a portable baseline (--from) when one is named, then
// apply the named preset (--preset). Centralized so a new flag wires in one
// place instead of drifting between apply and audit.
func LoadSharedFlags() error {
	if FromFile != "" {
		if err := LoadBaselineFile(FromFile); err != nil {
			return err
		}
	}
	return ApplyPreset(Preset)
}
