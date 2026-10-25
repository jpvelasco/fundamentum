package steps

import "testing"

func TestSelector_NoConstraintsIncludesEverything(t *testing.T) {
	s := New(nil, nil)
	if s.Len() != 0 {
		t.Fatalf("Len = %d, want 0", s.Len())
	}
	for _, key := range []string{Settings, ProtectMain, ProtectVersionTags, Security, Files} {
		if !s.IncludeStep(key) {
			t.Errorf("IncludeStep(%q) = false, want true", key)
		}
	}
	if !s.IncludeFile(".github/dependabot.yml") {
		t.Error("IncludeFile = false, want true")
	}
}

func TestSelector_SkipStep(t *testing.T) {
	s := New([]string{"protect-main", "security"}, nil)
	if s.IncludeStep(ProtectMain) || s.IncludeStep(Security) {
		t.Error("skipped steps must be excluded")
	}
	if !s.IncludeStep(Settings) || !s.IncludeStep(ProtectVersionTags) {
		t.Error("unskipped steps must stay included")
	}
	if !s.IncludeFile("SECURITY.md") {
		t.Error("file items are independent of step skips")
	}
}

func TestSelector_SkipFiles(t *testing.T) {
	s := New([]string{"files"}, nil)
	if s.IncludeFile(".github/CODEOWNERS") {
		t.Error("skip files must exclude every file item")
	}
	if !s.IncludeStep(ProtectMain) {
		t.Error("skip files must not affect steps")
	}
}

func TestSelector_SkipSinglePath(t *testing.T) {
	s := New([]string{"socket.yml"}, nil)
	if s.IncludeFile("socket.yml") {
		t.Error("skipped path must be excluded")
	}
	if !s.IncludeFile(".github/CODEOWNERS") {
		t.Error("other files must stay included")
	}
}

func TestSelector_OnlySteps(t *testing.T) {
	s := New(nil, []string{"protect-main", "files"})
	if !s.IncludeStep(ProtectMain) {
		t.Error("only-listed step must be included")
	}
	if s.IncludeStep(Settings) || s.IncludeStep(Security) {
		t.Error("unlisted steps must be excluded by --only")
	}
	if !s.IncludeFile(".github/SECURITY.md") {
		t.Error("only files must include file items")
	}
}

func TestSelector_OnlySinglePath(t *testing.T) {
	s := New(nil, []string{"socket.yml"})
	if !s.IncludeFile("socket.yml") {
		t.Error("only-listed path must be included")
	}
	if s.IncludeFile(".github/CODEOWNERS") {
		t.Error("unlisted paths must be excluded by --only")
	}
	if s.IncludeStep(ProtectMain) {
		t.Error("only path must exclude steps")
	}
}

func TestSelector_SkipBeatsOnly(t *testing.T) {
	s := New([]string{"socket.yml"}, []string{"files"})
	if s.IncludeFile("socket.yml") {
		t.Error("skip must win over only for the same path")
	}
	if !s.IncludeFile(".github/CODEOWNERS") {
		t.Error("only files must still include the rest")
	}
}

func TestSelector_TokensAreNormalized(t *testing.T) {
	s := New([]string{" Protect-Main "}, []string{"FILES"})
	if s.IncludeStep(ProtectMain) {
		t.Error("normalized skip token must match")
	}
	if !s.IncludeFile("README.md") {
		t.Error("normalized only token must match")
	}
}

func TestSelector_EmptyTokensIgnored(t *testing.T) {
	s := New([]string{"", "  "}, []string{""})
	if s.Len() != 0 {
		t.Errorf("empty tokens must be ignored, Len = %d", s.Len())
	}
	if !s.IncludeStep(Settings) || !s.IncludeFile("x.md") {
		t.Error("empty tokens must not filter anything")
	}
}

func TestSelector_ExcludeReason(t *testing.T) {
	skipped := New([]string{"protect-main"}, nil)
	if got := skipped.ExcludeReason(ProtectMain); got != "--skip" {
		t.Errorf("skip reason = %q, want --skip", got)
	}
	if got := skipped.ExcludeReason("settings"); got != "" {
		t.Errorf("included reason = %q, want empty", got)
	}

	only := New(nil, []string{"files"})
	if got := only.ExcludeReason("settings"); got != "--only" {
		t.Errorf("only-restricted reason = %q, want --only", got)
	}
	if got := only.ExcludeReason(Files); got != "" {
		t.Errorf("only-listed reason = %q, want empty", got)
	}

	// A path listed in both: skip wins.
	both := New([]string{"socket.yml"}, []string{"socket.yml"})
	if got := both.ExcludeReason("socket.yml"); got != "--skip" {
		t.Errorf("skip must beat only: %q", got)
	}

	// File path in skip set, step key untouched.
	pathSkip := New([]string{".github/workflows/ci.yml"}, nil)
	if pathSkip.IncludeFile(".github/workflows/ci.yml") {
		t.Error("path skip must exclude the file")
	}
	if !pathSkip.IncludeFile(".github/CODEOWNERS") {
		t.Error("other files stay included")
	}
}

func TestSelector_OnlyFileKeySelectsAllFiles(t *testing.T) {
	s := New(nil, []string{Files})
	if !s.IncludeFile("any/path.yml") {
		t.Error("only files must include every file")
	}
	if s.IncludeStep(ProtectMain) {
		t.Error("only files must exclude steps")
	}
}

func TestSelector_SkipFilesKeyExcludesAll(t *testing.T) {
	s := New([]string{Files, "security"}, nil)
	if s.IncludeFile("x.md") {
		t.Error("skip files must exclude every file")
	}
	if s.IncludeStep(Security) {
		t.Error("skip security must exclude security")
	}
	if !s.IncludeStep(Settings) {
		t.Error("unskipped steps stay included")
	}
}

func TestSelector_UppercasePathsMatch(t *testing.T) {
	// keySet lowercases flag tokens; file lookups must match the same way,
	// or --skip .github/CODEOWNERS silently does nothing.
	s := New([]string{".github/CODEOWNERS"}, nil)
	if s.IncludeFile(".github/CODEOWNERS") {
		t.Error("uppercase path in --skip must be excluded")
	}
	if !s.IncludeFile(".github/SECURITY.md") {
		t.Error("other files stay included")
	}
	if got := s.ExcludeFileReason(".github/CODEOWNERS"); got != "--skip" {
		t.Errorf("ExcludeFileReason = %q, want --skip", got)
	}

	only := New(nil, []string{".github/SECURITY.md"})
	if !only.IncludeFile(".github/SECURITY.md") {
		t.Error("uppercase path in --only must be included")
	}
	if only.IncludeFile(".github/CODEOWNERS") {
		t.Error("unlisted uppercase path must be excluded by --only")
	}
}

func TestSelector_ExcludeReasonFilesKey(t *testing.T) {
	// Files excluded via the "files" key must still name their mechanism —
	// otherwise the plan reports them as "already exists".
	s := New([]string{Files}, nil)
	if got := s.ExcludeFileReason(".github/CODEOWNERS"); got != "--skip" {
		t.Errorf("ExcludeFileReason under --skip files = %q, want --skip", got)
	}
	if got := s.ExcludeReason(ProtectMain); got != "" {
		t.Errorf("steps stay included: %q", got)
	}

	// Only-files: every file included, steps excluded.
	only := New(nil, []string{Files})
	if got := only.ExcludeFileReason(".github/CODEOWNERS"); got != "" {
		t.Errorf("file under --only files = %q, want included", got)
	}
	if got := only.ExcludeReason(Security); got != "--only" {
		t.Errorf("step under --only files = %q, want --only", got)
	}

	// Exact path in --only: that file included, others excluded by --only.
	pathOnly := New(nil, []string{"socket.yml"})
	if got := pathOnly.ExcludeFileReason("socket.yml"); got != "" {
		t.Errorf("only-listed path = %q, want included", got)
	}
	if got := pathOnly.ExcludeFileReason(".github/CODEOWNERS"); got != "--only" {
		t.Errorf("unlisted path = %q, want --only", got)
	}
}

func TestSelector_Unknown(t *testing.T) {
	paths := []string{".github/CODEOWNERS", "socket.yml", ".github/workflows/octopus.yml"}
	tests := []struct {
		name string
		skip []string
		only []string
		want []string
	}{
		{"known steps pass", []string{"protect-main", "security"}, nil, nil},
		{"known paths pass", []string{"socket.yml", ".github/CODEOWNERS"}, nil, nil},
		{"files key passes", []string{"files"}, nil, nil},
		{"typo flagged", []string{"protect-mian"}, nil, []string{"protect-mian"}},
		{"bare filename flagged", []string{"octopus.yml"}, nil, []string{"octopus.yml"}},
		{"wrong separator flagged", []string{"protect_main"}, nil, []string{"protect_main"}},
		{"only typo flagged", nil, []string{"settngs"}, []string{"settngs"}},
		{"case-insensitive path passes", []string{".github/codeowners"}, nil, nil},
	}
	for _, tt := range tests {
		got := New(tt.skip, tt.only).Unknown(paths)
		if len(got) != len(tt.want) {
			t.Errorf("%s: Unknown = %v, want %v", tt.name, got, tt.want)
			continue
		}
		for i := range tt.want {
			if got[i] != tt.want[i] {
				t.Errorf("%s: Unknown = %v, want %v", tt.name, got, tt.want)
			}
		}
	}
}
