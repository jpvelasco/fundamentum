package util

import (
	"os"
	"strings"
	"testing"
)

func TestParseOwnerRepo(t *testing.T) {
	tests := []struct {
		name     string
		arg      string
		wantOwn  string
		wantRepo string
		wantErr  bool
	}{
		{"valid", "owner/repo", "owner", "repo", false},
		{"with hyphens", "my-org/my-repo", "my-org", "my-repo", false},
		{"with dots", "owner.dev/repo.test", "owner.dev", "repo.test", false},
		{"with underscores", "owner_dev/repo_x", "owner_dev", "repo_x", false},
		{"numeric", "123/456", "123", "456", false},
		{"no slash", "norepo", "", "", true},
		{"empty", "", "", "", true},
		{"slash only", "/", "", "", true},
		{"empty owner", "/repo", "", "", true},
		{"trailing slash", "owner/", "", "", true},
		{"multiple slashes", "owner/repo/extra", "", "", true},
		{"invalid owner chars", "own er/repo", "", "", true},
		{"invalid repo chars", "owner/rep o", "", "", true},
		{"invalid repo slash char", "owner/rep/o", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			own, repo, err := ParseOwnerRepo(tt.arg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseOwnerRepo(%q) error = %v, wantErr %v", tt.arg, err, tt.wantErr)
				return
			}
			if own != tt.wantOwn {
				t.Errorf("owner = %q, want %q", own, tt.wantOwn)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestRequireToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	// Stub the gh fallback so the test is machine-independent (a developer
	// machine with an authenticated gh CLI would otherwise provide a token).
	restore := GhAuthToken
	GhAuthToken = func() (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { GhAuthToken = restore })

	if _, err := RequireToken(""); err == nil || !strings.Contains(err.Error(), "GitHub token is required") {
		t.Fatalf("empty token error = %v", err)
	}
	got, err := RequireToken("flag-token")
	if err != nil || got != "flag-token" {
		t.Fatalf("flag token = (%q, %v)", got, err)
	}
	t.Setenv("GITHUB_TOKEN", "env-token")
	got, err = RequireToken("")
	if err != nil || got != "env-token" {
		t.Fatalf("env token = (%q, %v)", got, err)
	}
	_ = os.Unsetenv("GITHUB_TOKEN")
}

func TestRequireToken_GhAuthFallback(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	restore := GhAuthToken
	GhAuthToken = func() (string, error) { return "gh-token", nil }
	t.Cleanup(func() { GhAuthToken = restore })

	got, err := RequireToken("")
	if err != nil || got != "gh-token" {
		t.Fatalf("gh auth fallback = (%q, %v), want gh-token", got, err)
	}

	// env var still wins over gh
	t.Setenv("GITHUB_TOKEN", "env-token")
	got, err = RequireToken("")
	if err != nil || got != "env-token" {
		t.Fatalf("env must beat gh: (%q, %v)", got, err)
	}
	_ = os.Unsetenv("GITHUB_TOKEN")
}

func TestRequireToken_GhAuthFails(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	restore := GhAuthToken
	GhAuthToken = func() (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { GhAuthToken = restore })

	if _, err := RequireToken(""); err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("expected token-required error mentioning gh, got %v", err)
	}
}

func TestRequireToken_GhAuthEmpty(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	restore := GhAuthToken
	GhAuthToken = func() (string, error) { return "", nil }
	t.Cleanup(func() { GhAuthToken = restore })

	if _, err := RequireToken(""); err == nil {
		t.Fatal("empty gh token must not count as authenticated")
	}
}
func TestGhAuthTokenExecutes(t *testing.T) {
	// Cover the production fallback body by invoking it directly. The
	// outcome is machine-dependent (gh installed + authenticated or not):
	// a valid token with nil error, or any error — both acceptable. The
	// contract is only that it returns without panicking.
	t.Setenv("GITHUB_TOKEN", "")
	token, err := GhAuthToken()
	if err == nil && token == "" {
		t.Fatal("gh success with an empty token is a contradiction")
	}
}
