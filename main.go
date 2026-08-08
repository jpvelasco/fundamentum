package main

import (
	"fmt"
	"os"

	"github.com/jpvelasco/fundamentum/cmd/root"
)

// exit is a seam so tests can observe the exit path without terminating the
// test process. Production behavior is os.Exit.
var exit = os.Exit

func main() {
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		exit(1)
	}
}
