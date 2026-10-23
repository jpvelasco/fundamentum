package globals

// boolFlag is one baseline bool. False is also the zero value, so the
// operator's Changed() state is stored beside the value.
type boolFlag struct {
	name    string
	current *bool
	file    bool
}

func baselineBools(file Baseline) []boolFlag {
	return []boolFlag{
		{name: "advanced-security", current: &AdvancedSecurity, file: file.AdvancedSecurity},
		{name: "strict", current: &Strict, file: file.Strict},
		{name: "no-overwrite", current: &NoOverwrite, file: file.NoOverwrite},
	}
}

var explicitFlags = map[string]bool{}

// NoteExplicit records that the operator passed name on the command line.
func NoteExplicit(name string) {
	explicitFlags[name] = true
}

// Explicit reports whether NoteExplicit was called for name.
func Explicit(name string) bool {
	return explicitFlags[name]
}

// ResetExplicit clears the explicit-flag set. Tests call it so a prior
// case cannot leak into another package's baseline load.
func ResetExplicit() {
	explicitFlags = map[string]bool{}
}

// NoteExplicitBools records each baseline bool flag the operator passed.
// changed is cmd.Flags().Changed after Cobra has parsed the command.
func NoteExplicitBools(changed func(string) bool) {
	for _, slot := range baselineBools(Baseline{}) {
		if changed(slot.name) {
			NoteExplicit(slot.name)
		}
	}
}

// applyBaselineBools copies file values into slots the operator left unset.
func applyBaselineBools(file Baseline) {
	for _, slot := range baselineBools(file) {
		if Explicit(slot.name) || *slot.current {
			continue
		}
		*slot.current = slot.file
	}
}
