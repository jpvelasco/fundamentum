package main

import (
	"os"
	"testing"
)

// resetArgs restores os.Args after a test mutates it, and restores the exit
// seam to its production implementation.
func resetArgs(t *testing.T) {
	t.Cleanup(func() {
		os.Args = []string{"fundamentum"}
		exit = os.Exit
	})
}

func TestMain_HappyPath(t *testing.T) {
	resetArgs(t)
	os.Args = []string{"fundamentum"}
	main()
}

func TestMain_ErrorPath(t *testing.T) {
	resetArgs(t)
	os.Args = []string{"fundamentum", "apply", "norepo"}
	code := -1
	exit = func(c int) { code = c }
	main()
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}
