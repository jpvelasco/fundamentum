// Package util provides shared utility functions for commands.
package util

import (
	"fmt"
	"os"
	"os/exec"
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

// GhAuthToken resolves a token from the GitHub CLI's authenticated session.
// It is the only exec.Command in this codebase — a deliberate, user-directed
// exception: fixed argv (no shell, no user-controlled arguments), and only
// consulted when neither --token nor GITHUB_TOKEN is available. Exported as a
// var so tests in other packages can stub the fallback deterministically.
var GhAuthToken = func() (string, error) {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// RequireToken returns the GitHub token from --token, GITHUB_TOKEN, or the
// GitHub CLI's authenticated session, in that order. An empty result is an
// error so commands fail locally instead of sending unauthenticated API calls.
func RequireToken(flagToken string) (string, error) {
	if flagToken != "" {
		return flagToken, nil
	}
	if env := os.Getenv("GITHUB_TOKEN"); env != "" {
		return env, nil
	}
	if tok, err := GhAuthToken(); err == nil && tok != "" {
		return tok, nil
	}
	return "", fmt.Errorf("GitHub token is required: set GITHUB_TOKEN, run `gh auth login`, or pass --token")
}
