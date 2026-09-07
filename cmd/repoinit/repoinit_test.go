package repoinit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/apply"
	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
)

func TestNewCmd(t *testing.T) {
	cmd := NewCmd()
	if cmd.Use != "init OWNER/REPO" {
		t.Errorf("expected use 'init OWNER/REPO', got %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("expected non-empty Short")
	}
}

func TestNewCmd_PrivateFlag(t *testing.T) {
	cmd := NewCmd()
	if cmd.Flags().Lookup("private") == nil {
		t.Error("expected --private flag")
	}
}

func TestRun_InvalidArg(t *testing.T) {
	err := run("norepo", false, nil)
	if err == nil {
		t.Error("expected error for invalid arg")
	}
}

// resetGlobals restores package-level flag state after a test mutates it.
func resetGlobals(t *testing.T) {
	t.Cleanup(func() {
		globals.DryRun = false
		globals.Token = ""
		globals.Verbose = false
		globals.Strict = false
		globals.RequireChecks = nil
		globals.CIPack = ""
	})
}

func TestRun_DryRunPath(t *testing.T) {
	resetGlobals(t)
	globals.DryRun = true

	var out strings.Builder
	err := run("owner/new-repo", false, &out)
	if err != nil {
		t.Fatalf("init --dry-run must succeed without inspecting the missing repo, got: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "would create repo owner/new-repo (public)") {
		t.Errorf("expected create-repo line, got:\n%s", got)
	}
	if !strings.Contains(got, "would create") {
		t.Errorf("expected harden plan, got:\n%s", got)
	}
	if !strings.Contains(got, "Branch protection") {
		t.Errorf("expected branch protection in plan, got:\n%s", got)
	}
	if !strings.Contains(got, "Dry run complete") {
		t.Errorf("expected dry-run complete, got:\n%s", got)
	}
}

func TestRun_DryRunPrivate(t *testing.T) {
	resetGlobals(t)
	globals.DryRun = true
	var out strings.Builder
	if err := run("owner/new-repo", true, &out); err != nil {
		t.Fatalf("init --dry-run private: %v", err)
	}
	if !strings.Contains(out.String(), "would create repo owner/new-repo (private)") {
		t.Errorf("expected private visibility, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "secret scanning") {
		t.Errorf("private init --dry-run must show offered GHAS plan lines, got:\n%s", out.String())
	}
}

func TestRun_CreateRepo_Fails(t *testing.T) {
	tests := []struct {
		name    string
		private bool
	}{
		{
			name:    "private=false",
			private: false,
		},
		{
			name:    "private=true",
			private: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetGlobals(t)
			globals.Token = "t"

			// With a dummy token and no mock server, CreateRepo will fail with a
			// network error. The error should contain "create repo".
			err := run("owner/repo", tt.private, nil)
			if err == nil {
				t.Error("expected error when CreateRepo fails")
			}
			if !strings.Contains(err.Error(), "create repo") {
				t.Errorf("expected 'create repo' in error, got: %v", err)
			}
		})
	}
}

func TestRun_MissingToken(t *testing.T) {
	resetGlobals(t)
	t.Setenv("GITHUB_TOKEN", "")
	err := run("owner/repo", true, nil)
	if err == nil || !strings.Contains(err.Error(), "GitHub token is required") {
		t.Fatalf("expected missing-token error, got %v", err)
	}
}

func TestRun_CreateRepoSuccess(t *testing.T) {
	resetGlobals(t)
	globals.Token = "t"
	t.Cleanup(func() { newClient = github.NewClient })
	t.Cleanup(func() {
		runApply = func(ownerRepo string) error {
			applyCmd := apply.NewCmd()
			applyCmd.SetArgs([]string{ownerRepo})
			return applyCmd.Execute()
		}
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/user" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"login":"owner"}`))
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/user/repos") {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	newClient = func(token string, verbose bool) *github.Client {
		return github.NewClient(token, verbose).WithBaseURL(srv.URL)
	}
	runApply = func(string) error { return nil }

	if err := run("owner/repo", false, nil); err != nil {
		t.Fatalf("run() error: %v", err)
	}
}

func TestExecute_RunE(t *testing.T) {
	resetGlobals(t)
	globals.Token = "t"
	t.Cleanup(func() { newClient = github.NewClient })
	t.Cleanup(func() {
		runApply = func(ownerRepo string) error {
			applyCmd := apply.NewCmd()
			applyCmd.SetArgs([]string{ownerRepo})
			return applyCmd.Execute()
		}
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/user" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"login":"owner"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	newClient = func(token string, verbose bool) *github.Client {
		return github.NewClient(token, verbose).WithBaseURL(srv.URL)
	}
	runApply = func(string) error { return nil }

	// The RunE closure must propagate the private flag into run().
	cmd := NewCmd()
	cmd.SetArgs([]string{"owner/repo", "--private=false"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
}
