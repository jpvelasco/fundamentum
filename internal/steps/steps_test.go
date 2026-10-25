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
