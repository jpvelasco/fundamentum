package revert

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/internal/github"
)

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
