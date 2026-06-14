// Package util provides shared utility functions for commands.
package util

import "fmt"

// ParseOwnerRepo splits "OWNER/REPO" into its two components.
func ParseOwnerRepo(arg string) (string, string, error) {
	for i, c := range arg {
		if c == '/' {
			return arg[:i], arg[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid OWNER/REPO %q: expected a slash separator", arg)
}
