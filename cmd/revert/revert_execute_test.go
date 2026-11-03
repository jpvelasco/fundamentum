package revert

import (
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/internal/github"
)

func TestExecute_FilesOnlySkipsSettingsAndRulesets(t *testing.T) {
	snap := testSnapshot()
	snap.Rulesets.ProtectMain.Existed = true
	snap.Rulesets.ProtectMain.Body = map[string]any{"name": "protect-main"}
	srv := mockRevertServer(t, snap)
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeFiles, false, &out); err != nil {
		t.Fatalf("run error: %v", err)
	}
	if strings.Contains(out.String(), "settings") || strings.Contains(out.String(), "ruleset") {
		t.Errorf("files mode must not touch settings/rulesets:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "✓ Done — files reverted") {
		t.Errorf("output missing files done:\n%s", out.String())
	}
}

func TestExecute_SettingsModeSkipsRulesets(t *testing.T) {
	snap := testSnapshot()
	srv := mockRevertServer(t, snap)
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeSettings, false, &out); err != nil {
		t.Fatalf("run error: %v", err)
	}
	if strings.Contains(out.String(), "ruleset") {
		t.Errorf("settings mode must not touch rulesets:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "✓ Done — files + settings reverted") {
		t.Errorf("output missing settings done:\n%s", out.String())
	}
}

func TestExecute_ErrorOnFileRestore(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("corrupt file content must panic")
		}
	}()
	snap := testSnapshot()
	snap.Files = []FileState{{Path: ".github/workflows/ci.yml", Present: true, Content: "!!!", SHA: "s1"}}
	srv := mockRevertServer(t, snap)
	defer srv.Close()
	var out strings.Builder
	_ = run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeFull, false, &out)
}

func TestPlan_ModesReturnExpectedItems(t *testing.T) {
	snap := testSnapshot()
	srv := mockRevertServer(t, snap)
	defer srv.Close()
	for _, tc := range []struct {
		mode     Mode
		contains string
	}{
		{ModeFiles, "restore .github/workflows/ci.yml"},
		{ModeSettings, "restore security toggles"},
		{ModeFull, "restore protect-main ruleset"},
	} {
		items, err := plan(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", snap, tc.mode)
		if err != nil {
			t.Fatalf("plan(%s) error: %v", tc.mode, err)
		}
		found := false
		for _, item := range items {
			if strings.Contains(item, tc.contains) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("plan(%s) missing %q: %v", tc.mode, tc.contains, items)
		}
	}
}
