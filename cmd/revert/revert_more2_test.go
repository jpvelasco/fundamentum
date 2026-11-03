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

var _ = json.Marshal // keep json import
