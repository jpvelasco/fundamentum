package revert

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestRun_FullRevertApplies(t *testing.T) {
	snap := testSnapshot()
	srv := mockRevertServer(t, snap)
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeFull, false, &out); err != nil {
		t.Fatalf("run error: %v", err)
	}
	if !strings.Contains(out.String(), "✓ Done") {
		t.Errorf("apply output missing Done:\n%s", out.String())
	}
}

func TestRun_FilesModeOnly(t *testing.T) {
	snap := testSnapshot()
	srv := mockRevertServer(t, snap)
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeFiles, false, &out); err != nil {
		t.Fatalf("run error: %v", err)
	}
	if strings.Contains(out.String(), "restored repo settings") {
		t.Errorf("files mode must not touch settings:\n%s", out.String())
	}
}

func TestRun_InvalidModeErrors(t *testing.T) {
	snap := testSnapshot()
	srv := mockRevertServer(t, snap)
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", Mode("bogus"), true, &out); err == nil {
		t.Fatal("invalid mode must error")
	}
}

var _ = fmt.Sprintf // keep fmt for future plan formatting
