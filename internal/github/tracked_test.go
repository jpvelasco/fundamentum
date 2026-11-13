package github

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrackedTree(t *testing.T) {
	for _, truncated := range []bool{false, true} {
		t.Run(map[bool]string{false: "recursive", true: "fallback"}[truncated], func(t *testing.T) {
			blobReads := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Fatal("bootstrap transport must be read-only")
				}
				var body any
				switch r.URL.Path {
				case "/repos/o/r/commits/main":
					body = map[string]any{"sha": "head", "commit": map[string]any{"tree": map[string]any{"sha": "root"}}}
				case "/repos/o/r/git/trees/root":
					if r.URL.RawQuery != "" {
						body = map[string]any{"sha": "root", "truncated": truncated, "tree": []any{
							map[string]any{"path": "dir/a", "type": "blob", "sha": "blob"},
							map[string]any{"path": "link", "type": "blob", "sha": "blob"},
							map[string]any{"path": "module", "type": "commit", "sha": "other"},
						}}
					} else {
						body = map[string]any{"sha": "root", "tree": []any{
							map[string]any{"path": "dir", "type": "tree", "sha": "sub"},
							map[string]any{"path": "link", "type": "blob", "sha": "blob"},
							map[string]any{"path": "module", "type": "commit", "sha": "other"},
						}}
					}
				case "/repos/o/r/git/trees/sub":
					body = map[string]any{"sha": "sub", "tree": []any{map[string]any{"path": "a", "type": "blob", "sha": "blob"}}}
				case "/repos/o/r/git/blobs/blob":
					blobReads++
					body = map[string]any{"sha": "blob", "size": 4, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("data"))}
				case "/repos/o/r/contents/.github/pii-allowlist.txt":
					body = map[string]any{}
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer srv.Close()
			client := NewClient("token", true).WithBaseURL(srv.URL)
			present, err := client.HasPIIPolicy("o", "r")
			if err != nil || !present {
				t.Fatal("presence probe", err)
			}
			head, blobs, err := client.TrackedTree("o", "r", "main")
			if err != nil || head != "head" || len(blobs) != 2 || blobs[0].Path != "dir/a" || string(blobs[1].Data) != "data" || blobReads != 1 {
				t.Fatal("pinned complete tree and deduplicated blobs", err)
			}
		})
	}
}

func TestTrackedFailuresSafe(t *testing.T) {
	private := "42" + "4242424242"
	for _, tt := range []struct {
		name, commit, tree, subtree, blob string
		status                            int
	}{
		{name: "auth", status: 403, commit: private},
		{name: "json", commit: `{"` + private},
		{name: "empty identity", commit: `{}`},
		{name: "wrong tree", tree: `{"sha":"wrong"}`},
		{name: "missing listing", tree: `{"sha":"root"}`},
		{name: "bad entry", tree: `{"sha":"root","tree":[{"type":"blob","path":"a"}]}`},
		{name: "duplicate entry", tree: `{"sha":"root","tree":[{"type":"blob","path":"a","sha":"b"},{"type":"blob","path":"a","sha":"b"}]}`},
		{name: "unknown type", tree: `{"sha":"root","tree":[{"type":"bad"}]}`},
		{name: "truncated subtree", tree: `{"sha":"root","truncated":true}`, subtree: `{"sha":"root","truncated":true}`},
		{name: "cycle", tree: `{"sha":"root","truncated":true}`, subtree: `{"sha":"root","tree":[{"path":"dir","type":"tree","sha":"root"}]}`},
		{name: "subtree read", tree: `{"sha":"root","truncated":true}`, subtree: `not json`},
		{name: "invalid subtree entry", tree: `{"sha":"root","truncated":true}`, subtree: `{"sha":"root","tree":[{"path":"","type":"blob","sha":"b"}]}`},
		{name: "unknown subtree type", tree: `{"sha":"root","truncated":true}`, subtree: `{"sha":"root","tree":[{"path":"a","type":"bad","sha":"b"}]}`},
		{name: "duplicate subtree entry", tree: `{"sha":"root","truncated":true}`, subtree: `{"sha":"root","tree":[{"path":"a","type":"blob","sha":"b"},{"path":"a","type":"blob","sha":"b"}]}`},
		{name: "invalid blob", blob: `{"sha":"b","encoding":"base64","content":"!","size":1}`},
		{name: "wrong blob", blob: `{"sha":"wrong","encoding":"base64"}`},
		{name: "short blob", blob: `{"sha":"b","encoding":"base64","content":"YQ==","size":2}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body string
				switch {
				case strings.Contains(r.URL.Path, "/commits/"):
					body = tt.commit
					if body == "" {
						body = `{"sha":"head","commit":{"tree":{"sha":"root"}}}`
					}
					if tt.status != 0 {
						w.WriteHeader(tt.status)
					}
				case strings.Contains(r.URL.Path, "/trees/"):
					body = tt.tree
					if r.URL.RawQuery == "" && tt.subtree != "" {
						body = tt.subtree
					}
					if body == "" {
						body = `{"sha":"root","tree":[{"path":"a","type":"blob","sha":"b"}]}`
					}
				case strings.Contains(r.URL.Path, "/blobs/"):
					body = tt.blob
				}
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			_, _, err := NewClient("token", false).WithBaseURL(srv.URL).TrackedTree("o", "r", "main")
			if err == nil || strings.Contains(err.Error(), private) {
				t.Fatal("must fail with safe diagnostic")
			}
		})
	}
}
