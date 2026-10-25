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
		{name: "octopus", current: &Octopus, file: file.Octopus},
		{name: "codacy-instructions", current: &CodacyInstructions, file: file.CodacyInstructions},
	}
}

// branchUpToDateFlag is the one bool flag with a true default, so it cannot
// use the false-means-unset baseline-bool logic: its explicit state is
// tracked separately, and the baseline carries it as a tri-state pointer.
const branchUpToDateFlag = "branch-up-to-date"

// trackedBoolFlags is every bool flag whose Changed state must be recorded
// after Cobra parses the command line.
func trackedBoolFlags() []string {
	flags := make([]string, 0, len(baselineBools(Baseline{}))+1)
	for _, slot := range baselineBools(Baseline{}) {
		flags = append(flags, slot.name)
	}
	return append(flags, branchUpToDateFlag)
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
	for _, name := range trackedBoolFlags() {
		if changed(name) {
			NoteExplicit(name)
		}
	}
	if changed(branchUpToDateFlag) || Explicit(branchUpToDateFlag) {
		BranchUpToDateSet = true
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
