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

func TestNewCmd_RunEReadsArgs(t *testing.T) {
	cmd := NewCmd()
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("revert with 2 args must error (Args: ExactArgs(1))")
	}
	// Dry-run through RunE against a mock server.
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

func TestExecute_RulesetDeleteBranch(t *testing.T) {
	snap := testSnapshot()
	snap.Rulesets.ProtectMain.Existed = false
	snap.Rulesets.ProtectVersionTags.Existed = false
	srv := mockRevertServer(t, snap)
	defer srv.Close()
	var out strings.Builder
	if err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", ModeFull, false, &out); err != nil {
		t.Fatalf("run error: %v", err)
	}
	if !strings.Contains(out.String(), "deleted protect-main ruleset") {
		t.Errorf("output missing ruleset delete:\n%s", out.String())
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

var _ = json.Marshal // keep json import for future snapshot shape tests
