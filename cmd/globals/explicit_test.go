package globals

import "testing"

func TestNoteExplicitBools(t *testing.T) {
	resetBaseline(t)
	passed := map[string]bool{"strict": true}
	NoteExplicitBools(func(name string) bool { return passed[name] })
	if !Explicit("strict") {
		t.Fatal("strict must be explicit")
	}
	if Explicit("advanced-security") || Explicit("no-overwrite") {
		t.Fatal("unpassed bools must not be explicit")
	}
	NoteExplicit("advanced-security")
	if !Explicit("advanced-security") {
		t.Fatal("NoteExplicit must record the name")
	}
}

func TestResetExplicit(t *testing.T) {
	NoteExplicit("strict")
	ResetExplicit()
	if Explicit("strict") {
		t.Fatal("ResetExplicit must clear the set")
	}
}
