package revert

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
)

// mockRevertServer serves the API surface revert exercises: snapshot read,
// file restore/delete, settings, security, and the two managed rulesets.
func mockRevertServer(t *testing.T, snap *Snapshot) *httptest.Server {
	t.Helper()
	snapJSON, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/contents/"+BaselinePath):
			_, _ = w.Write([]byte(`{"content":"` + base64.StdEncoding.EncodeToString(snapJSON) + `","sha":"snapsha"}`))
		case strings.Contains(path, "/contents/") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case strings.Contains(path, "/contents/") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case strings.HasSuffix(path, "/vulnerability-alerts") || strings.HasSuffix(path, "/automated-security-fixes"):
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(path, "/code-scanning/default-setup"):
			_, _ = w.Write([]byte(`{"state":"configured"}`))
		case path == "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"name":"repo","default_branch":"main","owner":{"login":"owner","type":"User"},"delete_branch_on_merge":true,"allow_squash_merge":true,"allow_merge_commit":true,"allow_rebase_merge":true,"security_and_analysis":{"secret_scanning":{"status":"enabled"},"secret_scanning_push_protection":{"status":"enabled"}}}`))
		case strings.Contains(path, "/rulesets"):
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if r.Method == http.MethodPut {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
				return
			}
			_, _ = w.Write([]byte(`[{"id":1,"name":"protect-main","target":"branch","enforcement":"active","conditions":{},"rules":[]},{"id":2,"name":"protect-version-tags","target":"tag","enforcement":"active","conditions":{},"rules":[]}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func testSnapshot() *Snapshot {
	return &Snapshot{
		Version: 1,
		Owner:   "owner",
		Repo:    "repo",
		Branch:  "main",
		Files: []FileState{
			{Path: ".github/workflows/ci.yml", Present: true, Content: base64.StdEncoding.EncodeToString([]byte("on: push")), SHA: "s1"},
			{Path: "socket.yml", Present: false},
		},
		Settings: SettingsState{DeleteBranchOnMerge: true, AllowSquashMerge: true, AllowMergeCommit: true, AllowRebaseMerge: true},
		Security: SecurityState{DependabotAlerts: true, AutomatedFixes: true, SecretScanning: true, PushProtection: true},
		Rulesets: RulesetsState{
			ProtectMain:        RulesetState{Existed: true, Body: map[string]any{"name": "protect-main", "target": "branch"}},
			ProtectVersionTags: RulesetState{Existed: false},
		},
	}
}

func TestNewCmd_Structure(t *testing.T) {
	cmd := NewCmd()
	if cmd.Use != "revert OWNER/REPO" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("revert with 2 args must error (Args: ExactArgs(1))")
	}
}

func TestNewCmd_RunEReadsArgs(t *testing.T) {
	cmd := NewCmd()
	snap := testSnapshot()
	srv := mockRevertServer(t, snap)
	defer srv.Close()
	globals.Token = "t"
	globals.DryRun = true
	t.Cleanup(func() {
		globals.Token = ""
		globals.DryRun = false
		newClient = github.NewClient
	})
	newClient = func(token string, verbose bool) *github.Client {
		return github.NewClient(token, verbose).WithBaseURL(srv.URL)
	}
	cmd.SetArgs([]string{"owner/repo"})
	var out strings.Builder
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(out.String(), "fundamentum revert owner/repo") {
		t.Errorf("output missing header:\n%s", out.String())
	}
}

func TestRun_DryRunPrintsPlan(t *testing.T) {
	snap := testSnapshot()
	srv := mockRevertServer(t, snap)
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeFull, true, &out); err != nil {
		t.Fatalf("run error: %v", err)
	}
	for _, want := range []string{"fundamentum revert owner/repo (full)", "restore .github/workflows/ci.yml", "delete socket.yml", "Dry run complete"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("plan missing %q:\n%s", want, out.String())
		}
	}
}

func TestRun_Modes(t *testing.T) {
	snap := testSnapshot()
	srv := mockRevertServer(t, snap)
	defer srv.Close()
	for _, mode := range []Mode{ModeFiles, ModeSettings, ModeFull} {
		var out strings.Builder
		if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", mode, true, &out); err != nil {
			t.Fatalf("run(%s) error: %v", mode, err)
		}
		if !strings.Contains(out.String(), "Dry run complete") {
			t.Errorf("run(%s) missing dry-run note", mode)
		}
	}
}

func TestRun_MissingSnapshotErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeFull, true, &out); err == nil {
		t.Fatal("missing snapshot must error")
	}
}

func TestRun_SnapshotReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/contents/"+BaselinePath) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeFull, true, &out); err == nil {
		t.Fatal("snapshot read error must surface")
	}
}

func TestRun_SnapshotDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/contents/"+BaselinePath) {
			_, _ = w.Write([]byte(`{"content":"` + base64.StdEncoding.EncodeToString([]byte("not json")) + `","sha":"s1"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeFull, true, &out); err == nil {
		t.Fatal("corrupt snapshot must error")
	}
}

func TestRun_SnapshotInvalidBase64(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/contents/"+BaselinePath) {
			_, _ = w.Write([]byte(`{"content":"!!!","sha":"s1"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeFull, true, &out); err == nil {
		t.Fatal("invalid base64 must error")
	}
}

func TestRun_InvalidModeErrors(t *testing.T) {
	srv := mockRevertServer(t, testSnapshot())
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", Mode("bogus"), true, &out); err == nil {
		t.Fatal("invalid mode must error")
	}
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

func TestPlan_Modes(t *testing.T) {
	snap := testSnapshot()
	srv := mockRevertServer(t, snap)
	defer srv.Close()
	for _, mode := range []Mode{ModeFiles, ModeSettings, ModeFull} {
		items, err := plan(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", snap, mode)
		if err != nil {
			t.Fatalf("plan(%s) error: %v", mode, err)
		}
		if len(items) == 0 {
			t.Errorf("plan(%s) = empty", mode)
		}
	}
}

func TestCaptureSnapshot_PopulatesState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/contents/.github/workflows/ci.yml"):
			_, _ = w.Write([]byte(`{"content":"` + base64.StdEncoding.EncodeToString([]byte("on: push")) + `","sha":"s1"}`))
		case strings.Contains(path, "/contents/"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasSuffix(path, "/vulnerability-alerts") || strings.HasSuffix(path, "/automated-security-fixes"):
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(path, "/code-scanning/default-setup"):
			_, _ = w.Write([]byte(`{"state":"configured"}`))
		case path == "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"name":"repo","default_branch":"main","owner":{"login":"owner","type":"User"},"delete_branch_on_merge":true,"allow_squash_merge":true,"allow_merge_commit":true,"allow_rebase_merge":true,"security_and_analysis":{"secret_scanning":{"status":"enabled"},"secret_scanning_push_protection":{"status":"enabled"}}}`))
		case strings.Contains(path, "/rulesets/1"):
			_, _ = w.Write([]byte(`{"id":1,"name":"protect-main","target":"branch","enforcement":"active","conditions":{},"rules":[]}`))
		case strings.Contains(path, "/rulesets"):
			_, _ = w.Write([]byte(`[{"id":1,"name":"protect-main","target":"branch","enforcement":"active","conditions":{},"rules":[]}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	snap, err := CaptureSnapshot(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", "main", []string{".github/workflows/ci.yml", "socket.yml"})
	if err != nil {
		t.Fatalf("CaptureSnapshot error: %v", err)
	}
	if !snap.Settings.DeleteBranchOnMerge || !snap.Security.SecretScanning {
		t.Errorf("snapshot settings/security = %+v / %+v", snap.Settings, snap.Security)
	}
	if !snap.Rulesets.ProtectMain.Existed {
		t.Error("protect-main must exist")
	}
	if snap.Rulesets.ProtectVersionTags.Existed {
		t.Error("protect-version-tags must not exist")
	}
	if len(snap.Files) != 2 || !snap.Files[0].Present || snap.Files[1].Present {
		t.Errorf("snapshot files = %+v", snap.Files)
	}
}

func TestCaptureSnapshotForApply_Delegates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/contents/socket.yml"):
			w.WriteHeader(http.StatusNotFound)
		case strings.Contains(path, "/contents/"):
			_, _ = w.Write([]byte(`{"content":"` + base64.StdEncoding.EncodeToString([]byte("x")) + `","sha":"s1"}`))
		case strings.HasSuffix(path, "/vulnerability-alerts") || strings.HasSuffix(path, "/automated-security-fixes"):
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(path, "/code-scanning/default-setup"):
			_, _ = w.Write([]byte(`{"state":"configured"}`))
		case path == "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"name":"repo","default_branch":"main","owner":{"login":"owner","type":"User"},"delete_branch_on_merge":true,"allow_squash_merge":true,"allow_merge_commit":true,"allow_rebase_merge":true,"security_and_analysis":{"secret_scanning":{"status":"enabled"},"secret_scanning_push_protection":{"status":"enabled"}}}`))
		case strings.Contains(path, "/rulesets"):
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	snap, err := CaptureSnapshotForApply(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", "main", []string{"socket.yml"})
	if err != nil {
		t.Fatalf("CaptureSnapshotForApply error: %v", err)
	}
	if len(snap.Files) != 1 || snap.Files[0].Present {
		t.Errorf("snapshot files = %+v, want one absent file", snap.Files)
	}
}

func TestEncodeDecodeSnapshot_RoundTrip(t *testing.T) {
	snap := testSnapshot()
	b, err := snap.Encode()
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	got, err := DecodeSnapshot(b)
	if err != nil {
		t.Fatalf("DecodeSnapshot error: %v", err)
	}
	if got.Owner != snap.Owner || got.Repo != snap.Repo || got.Branch != snap.Branch {
		t.Errorf("round trip = %+v, want %+v", got, snap)
	}
}

func TestDecodeSnapshot_InvalidJSON(t *testing.T) {
	if _, err := DecodeSnapshot([]byte("{")); err == nil {
		t.Fatal("invalid JSON must error")
	}
}

func TestWriteSnapshot_Upserts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	snap := testSnapshot()
	snap.Files = []FileState{{Path: "x", Content: base64.StdEncoding.EncodeToString([]byte("x")), SHA: "s1"}}
	if err := WriteSnapshot(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", "main", snap); err != nil {
		t.Fatalf("WriteSnapshot error: %v", err)
	}
}

func TestWriteSnapshot_EncodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	snap := testSnapshot()
	snap.Files = []FileState{{Path: "x", Content: "!!!"}}
	if err := WriteSnapshot(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", "main", snap); err == nil {
		t.Fatal("WriteSnapshot with corrupt content must error")
	}
}

func TestWriteSnapshot_EncodeSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	snap := testSnapshot()
	snap.Files = []FileState{{Path: "x", Content: base64.StdEncoding.EncodeToString([]byte("x")), SHA: "s1"}}
	if err := WriteSnapshot(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", "main", snap); err != nil {
		t.Fatalf("WriteSnapshot error: %v", err)
	}
}

func TestMustDecode_PanicsOnCorrupt(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("mustDecode on corrupt data must panic")
		}
	}()
	_ = mustDecode("not-base64!!!")
}

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

func TestExecute_RulesetDeleteBoth(t *testing.T) {
	snap := testSnapshot()
	snap.Rulesets.ProtectMain.Existed = false
	snap.Rulesets.ProtectVersionTags.Existed = false
	srv := mockRevertServer(t, snap)
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeFull, false, &out); err != nil {
		t.Fatalf("run error: %v", err)
	}
	if !strings.Contains(out.String(), "deleted protect-main ruleset") || !strings.Contains(out.String(), "deleted protect-version-tags ruleset") {
		t.Errorf("output missing ruleset deletes:\n%s", out.String())
	}
}

func TestExecute_RulesetRestoreBranch(t *testing.T) {
	snap := testSnapshot()
	snap.Rulesets.ProtectMain.Existed = true
	snap.Rulesets.ProtectMain.Body = map[string]any{"name": "protect-main", "target": "branch", "rules": []any{}}
	snap.Rulesets.ProtectVersionTags.Existed = true
	snap.Rulesets.ProtectVersionTags.Body = map[string]any{"name": "protect-version-tags", "target": "tag", "rules": []any{}}
	srv := mockRevertServer(t, snap)
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeFull, false, &out); err != nil {
		t.Fatalf("run error: %v", err)
	}
	if !strings.Contains(out.String(), "restored protect-main ruleset") {
		t.Errorf("output missing ruleset restore:\n%s", out.String())
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
