package piibootstrap

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/cmd/util"
	"github.com/jpvelasco/fundamentum/internal/github"
)

func TestBootstrap(t *testing.T) {
	value := "42" + "4242424242"
	for _, tt := range []struct {
		name                string
		dry, exists, broken bool
	}{
		{name: "write"}, {name: "dry", dry: true}, {name: "no overwrite", exists: true}, {name: "read failure", broken: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("unexpected mutation")
				}
				var result any
				switch {
				case r.URL.Path == "/repos/o/r":
					result = map[string]any{"default_branch": "main"}
				case strings.Contains(r.URL.Path, "/commits/"):
					result = map[string]any{"sha": "head", "commit": map[string]any{"tree": map[string]any{"sha": "root"}}}
				case strings.Contains(r.URL.Path, "/trees/"):
					result = map[string]any{"sha": "root", "tree": []any{
						map[string]any{"path": "nested/file", "type": "blob", "sha": "b"},
						map[string]any{"path": ".github/pii-allowlist.txt", "type": "blob", "sha": "b"},
					}}
				case strings.Contains(r.URL.Path, "/blobs/"):
					if tt.broken {
						w.WriteHeader(http.StatusForbidden)
						_, _ = w.Write([]byte(value))
						return
					}
					body := value + "\n123456789012\n" + value
					result = map[string]any{"sha": "b", "size": len(body), "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(body))}
				}
				_ = json.NewEncoder(w).Encode(result)
			}))
			defer srv.Close()
			path := filepath.Join(t.TempDir(), "nested", "policy")
			if tt.exists {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			restore := newClient
			newClient = func(token string, verbose bool) *github.Client {
				return github.NewClient(token, verbose).WithBaseURL(srv.URL)
			}
			t.Cleanup(func() { newClient = restore; globals.DryRun = false; globals.Token = "" })
			globals.Token, globals.DryRun = "token", tt.dry
			cmd := NewCmd()
			var out strings.Builder
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"o/r", "--output", path})
			err := cmd.Execute()
			if (err != nil) != (tt.exists || tt.broken) || strings.Contains(out.String(), value) {
				t.Fatal("unsafe output or unexpected command outcome")
			}
			body, readErr := os.ReadFile(path)
			switch {
			case tt.exists:
				if string(body) != "original" {
					t.Fatal("overwrote existing output")
				}
			case tt.dry || tt.broken:
				if !os.IsNotExist(readErr) {
					t.Fatal("must not write before complete scan or on dry run")
				}
				if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
					t.Fatal("created directory during dry run or failed read")
				}
			default:
				if readErr != nil || !strings.Contains(string(body), "UNREVIEWED") || strings.Count(string(body), value) != 1 || strings.Contains(string(body), "123456789012") {
					t.Fatal("expected deduplicated unreviewed candidates")
				}
			}
		})
	}
}

func TestBootstrapPreflightErrors(t *testing.T) {
	restore := util.GhAuthToken
	util.GhAuthToken = func() (string, error) { return "", errors.New("unavailable") }
	t.Cleanup(func() { util.GhAuthToken = restore; globals.Token = "" })
	t.Setenv("GITHUB_TOKEN", "")
	for _, arg := range []string{"invalid", "o/r"} {
		cmd := NewCmd()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{arg})
		if err := cmd.Execute(); err == nil {
			t.Fatal("invalid input or missing auth must fail")
		}
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if err := run(github.NewClient("token", false).WithBaseURL(srv.URL), "o", "r", "unused", false, io.Discard); err == nil {
		t.Fatal("repository read must fail closed")
	}
}
