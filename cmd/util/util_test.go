package util

import "testing"

func TestParseOwnerRepo(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		wantOwn string
		wantRepo string
		wantErr bool
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