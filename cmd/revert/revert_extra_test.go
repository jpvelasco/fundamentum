package revert

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/internal/github"
)

func TestNewCmd_Structure(t *testing.T) {
	cmd := NewCmd()
	if cmd.Use != "revert OWNER/REPO" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("revert with no args must error")
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
		case strings.Contains(path, "/rulesets"):
			if strings.Contains(path, "/rulesets/1") {
				_, _ = w.Write([]byte(`{"id":1,"name":"protect-main","target":"branch","enforcement":"active","conditions":{},"rules":[]}`))
				return
			}
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

var _ = json.Marshal // keep json import for future snapshot shape tests
