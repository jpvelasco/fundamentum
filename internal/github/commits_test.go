package github

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// commitFlowHandler mocks the full git data API flow for CommitFiles:
// GET ref → GET commit → POST blobs → POST tree → POST commit → PATCH ref.
func commitFlowHandler(t *testing.T, refStatus int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/git/ref/heads/main"):
			_, _ = w.Write([]byte(`{"object":{"sha":"basesha"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/git/commits/basesha"):
			_, _ = w.Write([]byte(`{"tree":{"sha":"basesha-tree"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/blobs"):
			var body struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode blob body: %v", err)
			}
			if _, err := base64.StdEncoding.DecodeString(body.Content); err != nil {
				t.Errorf("blob content must be base64, got %q", body.Content)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"blobsha"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/trees"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"newtree"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/commits"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"newcommit"}`))
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/git/refs/heads/main"):
			w.WriteHeader(refStatus)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestCommitFiles_SingleCommit(t *testing.T) {
	srv, c := newTestServer(commitFlowHandler(t, http.StatusOK))
	defer srv.Close()

	changes := []FileChange{
		{Path: ".github/CODEOWNERS", Content: []byte("me")},
		{Path: "SECURITY.md", Content: []byte("sec")},
	}
	if err := c.CommitFiles("owner", "repo", "main", changes); err != nil {
		t.Fatalf("CommitFiles() error: %v", err)
	}
}

func TestCommitFiles_RefConflictSurfaces(t *testing.T) {
	srv, c := newTestServer(commitFlowHandler(t, http.StatusConflict))
	defer srv.Close()

	err := c.CommitFiles("owner", "repo", "main", []FileChange{{Path: "a", Content: []byte("x")}})
	if err == nil {
		t.Fatal("409 ref update must surface for the PR fallback")
	}
	if !IsConflict409(err) {
		t.Errorf("ref-update conflict must classify as 409: %v", err)
	}
}

func TestCommitFiles_NoChanges(t *testing.T) {
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no changes must not call the API: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	if err := c.CommitFiles("owner", "repo", "main", nil); err != nil {
		t.Fatalf("empty changes = %v", err)
	}
}

func TestReadFileContents(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte("hello"))
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/contents/exists.txt":
			_, _ = w.Write([]byte(`{"content":"` + body + `","sha":"s"}`))
		case r.URL.Path == "/repos/owner/repo/contents/missing.txt":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()

	content, ok, err := c.ReadFileContents("owner", "repo", "exists.txt")
	if err != nil || !ok || string(content) != "hello" {
		t.Fatalf("read = (%q, %v, %v)", content, ok, err)
	}
	content, ok, err = c.ReadFileContents("owner", "repo", "missing.txt")
	if err != nil || ok || content != nil {
		t.Fatalf("missing = (%q, %v, %v), want (nil, false, nil)", content, ok, err)
	}
	_, _, err = c.ReadFileContents("owner", "repo", "forbidden.txt")
	if err == nil {
		t.Fatal("403 must surface as an error")
	}
}

func TestDiffStat(t *testing.T) {
	tests := []struct {
		name     string
		old, new string
		want     string
	}{
		{"identical", "a\nb\n", "a\nb\n", "+0/-0"},
		{"added line", "a\n", "a\nb\n", "+1/-0"},
		{"removed line", "a\nb\n", "a\n", "+0/-1"},
		{"changed line", "a\nb\n", "a\nc\n", "+1/-1"},
		{"empty old", "", "a\nb\n", "+2/-0"},
		{"empty new", "a\nb\n", "", "+0/-2"},
		{"no trailing newline", "a", "a\nb", "+1/-0"},
	}
	for _, tt := range tests {
		if got := DiffStat([]byte(tt.old), []byte(tt.new)); got != tt.want {
			t.Errorf("DiffStat(%q, %q) = %s, want %s", tt.old, tt.new, got, tt.want)
		}
	}
}

func TestFileStatusDetail(t *testing.T) {
	old := base64.StdEncoding.EncodeToString([]byte("line1\nline2\n"))
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/contents/same.txt":
			_, _ = w.Write([]byte(`{"content":"` + base64.StdEncoding.EncodeToString([]byte("same\n")) + `","sha":"s"}`))
		case r.URL.Path == "/repos/owner/repo/contents/drift.txt":
			_, _ = w.Write([]byte(`{"content":"` + old + `","sha":"s"}`))
		case r.URL.Path == "/repos/owner/repo/contents/gone.txt":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"` + old + `","sha":"s"}`))
		}
	}))
	defer srv.Close()

	if action, detail, err := c.FileStatusDetail("owner", "repo", "same.txt", []byte("same\n")); err != nil || action != "skip" || detail != "" {
		t.Fatalf("same = (%q, %q, %v)", action, detail, err)
	}
	if action, detail, err := c.FileStatusDetail("owner", "repo", "drift.txt", []byte("line1\nline2\nline3\n")); err != nil || action != "update" || detail != "+1/-0" {
		t.Fatalf("drift = (%q, %q, %v)", action, detail, err)
	}
	if action, _, err := c.FileStatusDetail("owner", "repo", "gone.txt", []byte("x\n")); err != nil || action != "create" {
		t.Fatalf("missing = (%q, %v)", action, err)
	}
}
