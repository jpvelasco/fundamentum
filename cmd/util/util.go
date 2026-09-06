// Package util provides shared utility functions for commands.
package util

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	// segmentRe mirrors the identifier charset used by internal/templates
	// sanitization: letters, digits, dots, hyphens, and underscores.
	segmentRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// ParseOwnerRepo splits "OWNER/REPO" into its two components, requiring
// exactly one slash and non-empty, GitHub-plausible segments. The error names
// the problem and shows the expected shape so callers can fix their input.
func ParseOwnerRepo(arg string) (string, string, error) {
	invalid := func(reason string) (string, string, error) {
		return "", "", fmt.Errorf("invalid OWNER/REPO %q (%s): expected OWNER/REPO, e.g. octocat/hello-world", arg, reason)
	}
	owner, repo, found := strings.Cut(arg, "/")
	switch {
	case !found:
		return invalid("missing slash")
	case strings.Contains(repo, "/"):
		return invalid("must contain exactly one slash")
	case owner == "" || repo == "":
		return invalid("owner and repo must not be empty")
	case !segmentRe.MatchString(owner) || !segmentRe.MatchString(repo):
		return invalid("segments may only contain letters, digits, dots, hyphens, and underscores")
	}
	return owner, repo, nil
}

// RequireToken returns the GitHub token from --token or GITHUB_TOKEN.
// An empty result is an error so commands fail locally instead of sending
// unauthenticated API calls.
func RequireToken(flagToken string) (string, error) {
	if flagToken != "" {
		return flagToken, nil
	}
	if env := os.Getenv("GITHUB_TOKEN"); env != "" {
		return env, nil
	}
	return "", fmt.Errorf("GitHub token is required: set GITHUB_TOKEN or pass --token")
}
