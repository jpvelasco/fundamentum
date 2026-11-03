package revert

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
)

// inject replaces the nth (1-based) request matching method + path substring
// with the given status/body; every other request passes through to upstream.
type inject struct {
	method, sub string
	nth         int
	status      int
}

// failingServer fronts upstream with a one-shot fault injector.
func failingServer(t *testing.T, upstream *httptest.Server, in inject) *httptest.Server {
	t.Helper()
	u, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	var mu sync.Mutex
	hits := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == in.method && strings.Contains(r.URL.Path, in.sub) {
			mu.Lock()
			hits++
			hit := hits == in.nth
			mu.Unlock()
			if hit {
				w.WriteHeader(in.status)
				return
			}
		}
		proxy.ServeHTTP(w, r)
	}))
}

func TestExecute_FailurePaths(t *testing.T) {
	bothExist := func(s *Snapshot) {
		s.Rulesets.ProtectMain = RulesetState{Existed: true, Body: map[string]any{"name": "protect-main"}}
		s.Rulesets.ProtectVersionTags = RulesetState{Existed: true, Body: map[string]any{"name": "protect-version-tags"}}
	}
	noneExist := func(s *Snapshot) {
		s.Rulesets.ProtectMain = RulesetState{}
		s.Rulesets.ProtectVersionTags = RulesetState{}
	}
	alertsOff := func(s *Snapshot) { s.Security.DependabotAlerts = false }
	cases := []struct {
		name   string
		mode   Mode
		mutate func(*Snapshot)
		in     inject
	}{
		{"file restore", ModeFiles, nil, inject{http.MethodPut, "/contents/.github/workflows/ci.yml", 1, 403}},
		{"file delete lookup", ModeFiles, nil, inject{http.MethodGet, "/contents/socket.yml", 1, 401}},
		{"file delete", ModeFiles, nil, inject{http.MethodDelete, "/contents/socket.yml", 1, 403}},
		{"settings", ModeSettings, nil, inject{http.MethodPatch, "/repos/owner/repo", 1, 403}},
		{"security", ModeSettings, alertsOff, inject{http.MethodDelete, "/vulnerability-alerts", 1, 403}},
		{"main ruleset lookup", ModeFull, bothExist, inject{http.MethodGet, "/rulesets", 1, 401}},
		{"main ruleset update", ModeFull, bothExist, inject{http.MethodPut, "/rulesets/", 1, 403}},
		{"main ruleset delete", ModeFull, noneExist, inject{http.MethodDelete, "/rulesets/", 1, 403}},
		{"tags ruleset lookup", ModeFull, bothExist, inject{http.MethodGet, "/rulesets", 2, 401}},
		{"tags ruleset update", ModeFull, bothExist, inject{http.MethodPut, "/rulesets/", 2, 403}},
		{"tags ruleset delete", ModeFull, noneExist, inject{http.MethodDelete, "/rulesets/", 2, 403}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := testSnapshot()
			if tc.mutate != nil {
				tc.mutate(snap)
			}
			up := mockRevertServer(t, snap)
			defer up.Close()
			// DeleteFile only issues DELETE when the file currently exists.
			if tc.in.method == http.MethodDelete && strings.Contains(tc.in.sub, "socket.yml") {
				up.Config.Handler = existingFileHandler(up.Config.Handler, "/contents/socket.yml")
			}
			srv := failingServer(t, up, tc.in)
			defer srv.Close()
			var out strings.Builder
			err := run(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", tc.mode, false, &out)
			if err == nil {
				t.Fatalf("expected error; output:\n%s", out.String())
			}
		})
	}
}

// existingFileHandler makes GET <suffix> report an existing file.
func existingFileHandler(next http.Handler, suffix string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, suffix) {
			writeContents(w, "x", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func TestCaptureSnapshot_FailurePaths(t *testing.T) {
	cases := []inject{
		{http.MethodGet, "/contents/.github/workflows/ci.yml", 1, 401},
		{http.MethodGet, "/repos/owner/repo", 1, 401},
		{http.MethodGet, "/vulnerability-alerts", 1, 401},
		{http.MethodGet, "/automated-security-fixes", 1, 401},
		{http.MethodGet, "/code-scanning/default-setup", 1, 401},
		{http.MethodGet, "/rulesets", 1, 401},
		{http.MethodGet, "/rulesets", 2, 401},
	}
	for _, in := range cases {
		t.Run(in.sub, func(t *testing.T) {
			up := mockRevertServer(t, testSnapshot())
			defer up.Close()
			srv := failingServer(t, up, in)
			defer srv.Close()
			_, err := CaptureSnapshot(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", "main", []string{".github/workflows/ci.yml"})
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestNewCmd_RunEErrors(t *testing.T) {
	t.Cleanup(func() { globals.Token = "" })
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	for name, args := range map[string][]string{
		"bad repo":      {"not-a-repo"},
		"missing token": {"owner/repo"},
	} {
		t.Run(name, func(t *testing.T) {
			globals.Token = ""
			cmd := NewCmd()
			cmd.SetArgs(args)
			cmd.SilenceUsage, cmd.SilenceErrors = true, true
			if err := cmd.Execute(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestNewCmd_ModeFlag(t *testing.T) {
	cmd := NewCmd()
	if err := cmd.ParseFlags([]string{"--mode", "files"}); err != nil {
		t.Fatal(err)
	}
	if got := cmd.Flag("mode").Value.String(); got != "files" {
		t.Errorf("mode = %q, want files", got)
	}
}
