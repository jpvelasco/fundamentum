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
		switch r.URL.Path {
		case "/repos/owner/repo/contents/exists.txt":
			_, _ = w.Write([]byte(`{"content":"` + body + `","sha":"s"}`))
		case "/repos/owner/repo/contents/missing.txt":
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
		switch r.URL.Path {
		case "/repos/owner/repo/contents/same.txt":
			_, _ = w.Write([]byte(`{"content":"` + base64.StdEncoding.EncodeToString([]byte("same\n")) + `","sha":"s"}`))
		case "/repos/owner/repo/contents/drift.txt":
			_, _ = w.Write([]byte(`{"content":"` + old + `","sha":"s"}`))
		case "/repos/owner/repo/contents/gone.txt":
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

func TestCommitFiles_APIErrorsSurface(t *testing.T) {
	tests := []struct {
		name   string
		fails  string
		status int
	}{
		{"ref head missing", "ref", http.StatusNotFound},
		{"ref head forbidden", "ref", http.StatusForbidden},
		{"commit lookup fails", "commit", http.StatusForbidden},
		{"blob create fails", "blob", http.StatusInternalServerError},
		{"tree create fails", "tree", http.StatusForbidden},
		{"commit create fails", "commit-create", http.StatusInternalServerError},
		{"ref update forbidden", "patch", http.StatusForbidden},
		{"ref update unprocessable", "patch", http.StatusUnprocessableEntity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Wrap the happy flow: force the failing stage to return tt.status.
			srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hit := false
				switch {
				case tt.fails == "ref" && strings.HasSuffix(r.URL.Path, "/git/ref/heads/main"):
					hit = true
				case tt.fails == "commit" && strings.HasSuffix(r.URL.Path, "/git/commits/basesha"):
					hit = true
				case tt.fails == "blob" && strings.HasSuffix(r.URL.Path, "/git/blobs"):
					hit = true
				case tt.fails == "tree" && strings.HasSuffix(r.URL.Path, "/git/trees"):
					hit = true
				case tt.fails == "commit-create" && strings.HasSuffix(r.URL.Path, "/git/commits") && r.Method == http.MethodPost:
					hit = true
				case tt.fails == "patch" && r.Method == http.MethodPatch:
					hit = true
				}
				if hit {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(`{}`))
					return
				}
				commitFlowHandler(t, http.StatusOK)(w, r)
			}))
			defer srv.Close()
			err := c.CommitFiles("owner", "repo", "main", []FileChange{{Path: "a", Content: []byte("x")}})
			if err == nil {
				t.Fatalf("%s must fail", tt.name)
			}
		})
	}
}

func TestCommitFiles_MalformedJSONFails(t *testing.T) {
	// A broken ref body must error, not silently continue.
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/git/ref/heads/main") {
			_, _ = w.Write([]byte(`{not json`))
			return
		}
		commitFlowHandler(t, http.StatusOK)(w, r)
	}))
	defer srv.Close()
	if err := c.CommitFiles("owner", "repo", "main", []FileChange{{Path: "a", Content: []byte("x")}}); err == nil {
		t.Fatal("malformed ref JSON must fail")
	}
}

func TestReadFileContents_EdgeCases(t *testing.T) {
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/contents/badbase64.txt":
			_, _ = w.Write([]byte(`{"content":"!!!!","sha":"s"}`))
		case "/repos/owner/repo/contents/wrapped.txt":
			// GitHub wraps base64 content with newlines.
			_, _ = w.Write([]byte(`{"content":"aGVs\nbG8=","sha":"s"}`))
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()

	if _, _, err := c.ReadFileContents("owner", "repo", "badbase64.txt"); err == nil {
		t.Error("undecodable content must fail")
	}
	content, ok, err := c.ReadFileContents("owner", "repo", "wrapped.txt")
	if err != nil || !ok || string(content) != "hello" {
		t.Fatalf("newline-wrapped base64 = (%q, %v, %v)", content, ok, err)
	}
	if _, _, err := c.ReadFileContents("owner", "repo", "forbidden.txt"); err == nil {
		t.Error("403 must fail")
	}
}
