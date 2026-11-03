package apply

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/templates"
)

func TestPlanNewRepo(t *testing.T) {
	t.Cleanup(func() { globals.ViaPR = false })
	var out strings.Builder
	if err := PlanNewRepo("owner", "new-repo", "public", &out); err != nil {
		t.Fatalf("PlanNewRepo() error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "would create") || !strings.Contains(got, "Dry run complete") {
		t.Errorf("expected dry-run plan, got:\n%s", got)
	}
	if !strings.Contains(got, "Branch protection") {
		t.Errorf("expected branch protection, got:\n%s", got)
	}
	if strings.Contains(got, "--pr: file changes go in a pull request") {
		t.Error("PlanNewRepo without --pr must not print the PR-mode notice")
	}
}

func TestPlanNewRepo_PRModeNotice(t *testing.T) {
	t.Cleanup(func() { globals.ViaPR = false })
	globals.ViaPR = true
	var out strings.Builder
	if err := PlanNewRepo("owner", "new-repo", "public", &out); err != nil {
		t.Fatalf("PlanNewRepo() error: %v", err)
	}
	if !strings.Contains(out.String(), "--pr: file changes go in a pull request") {
		t.Errorf("expected --pr notice, got:\n%s", out.String())
	}
}

func TestPlanNewRepo_AutoUsesGenericCI(t *testing.T) {
	t.Cleanup(func() { globals.CIPack = "" })
	var out strings.Builder
	if err := PlanNewRepo("owner", "new-repo", "public", &out); err != nil {
		t.Fatalf("PlanNewRepo() error: %v", err)
	}
	// A repo that does not exist yet has no go.mod, so auto must not
	// ship Go-only files (Codecov / advanced CodeQL) that assume go.mod.
	got := out.String()
	if strings.Contains(got, "codecov.yml") || strings.Contains(got, "codeql.yml") {
		t.Errorf("new-repo auto pack must not ship Go-only files, got:\n%s", got)
	}
	if !strings.Contains(got, ".github/workflows/ci.yml") {
		t.Errorf("expected generic ci.yml in auto plan, got:\n%s", got)
	}
}

func TestPlanNewRepo_CIGo(t *testing.T) {
	t.Cleanup(func() { globals.CIPack = "" })
	globals.CIPack = "go"
	var out strings.Builder
	if err := PlanNewRepo("owner", "new-repo", "public", &out); err != nil {
		t.Fatalf("PlanNewRepo() error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, ".github/workflows/ci.yml") || !strings.Contains(got, "codecov.yml") {
		t.Errorf("expected Go CI + codecov in --ci go plan, got:\n%s", got)
	}
}

func TestPlanNewRepo_PresetPrivateHidesGHAS(t *testing.T) {
	t.Cleanup(func() { globals.Preset = "" })
	globals.Preset = "private"
	var out strings.Builder
	if err := PlanNewRepo("owner", "new-repo", "private", &out); err != nil {
		t.Fatalf("PlanNewRepo() error: %v", err)
	}
	if strings.Contains(out.String(), "secret scanning") {
		t.Errorf("--preset private dry-run must not offer GHAS, got:\n%s", out.String())
	}
}

func TestPlanNewRepo_InvalidPreset(t *testing.T) {
	t.Cleanup(func() { globals.Preset = "" })
	globals.Preset = "enterprise"
	if err := PlanNewRepo("owner", "new-repo", "public", &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "invalid --preset") {
		t.Fatalf("expected invalid --preset error, got %v", err)
	}
}

func TestPlanNewRepo_InvalidCI(t *testing.T) {
	t.Cleanup(func() { globals.CIPack = "" })
	globals.CIPack = "java"
	if err := PlanNewRepo("owner", "new-repo", "public", &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "invalid --ci") {
		t.Fatalf("expected invalid --ci error, got %v", err)
	}
}

func TestResolveCIPack_DetectsGoMod(t *testing.T) {
	t.Cleanup(func() { globals.CIPack = "" })
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/contents/go.mod") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"sha":"x"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}), func(c *github.Client) {
		got, err := resolveCIPack(c, "owner", "repo")
		if err != nil || got != templates.CIPackGo {
			t.Fatalf("resolveCIPack() = (%q, %v), want go", got, err)
		}
	}, nil)
}

func TestResolveCIPack_DetectsPackageJSON(t *testing.T) {
	t.Cleanup(func() { globals.CIPack = "" })
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/contents/package.json") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"sha":"x"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}), func(c *github.Client) {
		got, err := resolveCIPack(c, "owner", "repo")
		if err != nil || got != templates.CIPackNode {
			t.Fatalf("resolveCIPack() = (%q, %v), want node", got, err)
		}
	}, nil)
}

func TestResolveCIPack_MissingGoModIsGeneric(t *testing.T) {
	t.Cleanup(func() { globals.CIPack = "" })
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}), func(c *github.Client) {
		got, err := resolveCIPack(c, "owner", "repo")
		if err != nil || got != templates.CIPackGeneric {
			t.Fatalf("resolveCIPack() = (%q, %v), want generic", got, err)
		}
	}, nil)
}

func TestResolveCIPack_DetectError(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}), func(c *github.Client) {
		_, err := resolveCIPack(c, "owner", "repo")
		if err == nil || !strings.Contains(err.Error(), "detect go.mod") {
			t.Fatalf("error = %v, want detect go.mod", err)
		}
	}, nil)
}

func TestResolveCIPack_InvalidFlagWithClient(t *testing.T) {
	t.Cleanup(func() { globals.CIPack = "" })
	globals.CIPack = "java"
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}), func(c *github.Client) {
		_, err := resolveCIPack(c, "owner", "repo")
		if err == nil || !strings.Contains(err.Error(), "invalid --ci") {
			t.Fatalf("error = %v, want invalid --ci", err)
		}
	}, nil)
}

func TestRenderData_DetectionErrorsPropagate(t *testing.T) {
	// Any detection error aborts the plan instead of falling back silently.
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/contents/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	info := github.Repo{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "public"}
	_, err := RenderData(c, info, "go", false)
	if err == nil {
		t.Fatal("a 403 on the README detection must propagate")
	}
	if !strings.Contains(err.Error(), "check file README.md") {
		t.Errorf("error must name the failing probe: %v", err)
	}
}

func TestRenderData_NilClientTakesDefaults(t *testing.T) {
	info := github.Repo{Owner: "o", RepoName: "r", DefaultBranch: "main", Visibility: "public"}
	data, err := RenderData(nil, info, "node", false)
	if err != nil {
		t.Fatalf("nil client must not probe: %v", err)
	}
	// Detection fields stay empty; RenderData.sanitize substitutes the
	// defaults (README.md, the AGENTS.md standard line, and the honest
	// failing node commands) at render time.
	rendered, err := renderTemplates(data)
	if err != nil {
		t.Fatal(err)
	}
	tpl := ""
	for _, f := range rendered {
		if f.Path == ".github/PULL_REQUEST_TEMPLATE.md" {
			tpl = f.Content
		}
	}
	if !strings.Contains(tpl, "Updated README.md") || !strings.Contains(tpl, "Updated AGENTS.md") {
		t.Errorf("defaults must land in the PR template:\n%s", tpl)
	}
	ci := ""
	for _, f := range rendered {
		if f.Path == ".github/workflows/ci.yml" {
			ci = f.Content
		}
	}
	if !strings.Contains(ci, "package.json has no lint script") {
		t.Errorf("node defaults must fail honestly:\n%s", ci)
	}
}

func TestRunWithClient_UnknownSkipKeyErrors(t *testing.T) {
	t.Cleanup(func() {
		globals.SkipSteps = nil
		globals.OnlySteps = nil
	})
	globals.SkipSteps = []string{"protect-mian"} // typo: silent no-op would be dangerous
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"name":"repo","visibility":"public","default_branch":"main","owner":{"type":"User"}}`))
		case strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/rulesets"):
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	err := runWithClient(c, "owner", "repo", newLineReader("\n"), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "unknown --skip/--only key(s): protect-mian") {
		t.Fatalf("typo in --skip must error with the token named, got: %v", err)
	}

	// A valid path token passes validation.
	globals.SkipSteps = []string{"socket.yml"}
	if err := runWithClient(c, "owner", "repo", newLineReader("\n"), &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "Dry run complete") {
		// runWithClient without --dry-run does a live run; with the mock server
		// the flow ends in apply errors. Validation happens BEFORE any of that,
		// so only the unknown-key case must error; the valid case must NOT error
		// with the unknown-key message.
		if err != nil && strings.Contains(err.Error(), "unknown --skip/--only") {
			t.Fatalf("valid token must not trigger the unknown-key error: %v", err)
		}
	}
}

func TestGuardShipped(t *testing.T) {
	rendered := []templates.RenderedFile{{Path: github.FreeTierGuardPath}}
	if !guardShipped(nil, "owner", "repo", rendered) {
		t.Error("guard in the rendered set must force PR mode")
	}
	if guardShipped(nil, "owner", "repo", nil) {
		t.Error("nil client with no rendered guard must not force PR mode")
	}

	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contents/.github/workflows/protect-main-guard.yml"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":""}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	if !guardShipped(c, "owner", "repo", nil) {
		t.Error("existing guard on the repo must force PR mode")
	}
}
