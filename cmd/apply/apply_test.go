package apply

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/templates"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

// prMockHandler returns an http.HandlerFunc that mocks the full PR workflow:
// CreatePRBranch (GET /branches/main, POST /git/refs),
// UpsertFileOnBranch (GET /contents/*, PUT /contents/*),
// CreatePullRequest (POST /pulls).
func prMockHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/branches/main"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"commit":{"sha":"abc123"}}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/refs"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/pulls"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":42}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// simpleFileHandler returns an http.HandlerFunc that returns 201 + {"content":{}} for all requests.
func simpleFileHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"content":{}}`))
	})
}

// newFileItems builds a wizard.Item slice with the given names, content, and Apply closures.
// names, content, and applies must be of equal length.
func newFileItems(names []string, content [][]byte, applies []func() error) []wizard.Item {
	items := make([]wizard.Item, len(names))
	for i, name := range names {
		items[i] = wizard.Item{
			Name:    name,
			Action:  wizard.ActionCreate,
			Content: content[i],
			Apply:   applies[i],
		}
	}
	return items
}

// runApplyItemsExpectNoError applies items against a client pointed at the
// given server handler, and fails the test if applyItems returns an error.
// The server is closed automatically.
func runApplyItemsExpectNoError(t *testing.T, handler http.HandlerFunc, items []wizard.Item, viaPR bool) {
	t.Helper()
	testWithServer(t, handler, func(c *github.Client) {
		if err := applyItems(c, "owner", "repo", "main", items, viaPR, nil, io.Discard); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	}, nil)
}

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

func TestBuildItems(t *testing.T) {
	// Mock server that returns file not found for all files
	items := newBuildItemsTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}), "private")

	// Should have file items + general settings + branch protection + tag ruleset + security
	if len(items) < 5 {
		t.Errorf("expected at least 5 items, got %d", len(items))
	}

	// Check that general settings item exists
	foundGeneral := false
	for _, item := range items {
		if item.Name == "General settings (auto-delete branches)" {
			foundGeneral = true
			break
		}
	}
	if !foundGeneral {
		t.Error("expected general settings item")
	}

	// Check that branch protection item exists
	foundBranch := false
	for _, item := range items {
		if item.Name == "Branch protection (protect-main)" {
			foundBranch = true
			break
		}
	}
	if !foundBranch {
		t.Error("expected branch protection item")
	}
}

func TestBuildItems_WithExistingRuleset(t *testing.T) {
	c := &github.Client{}
	items, err := buildItems(c, nil, "owner", "repo", "main", "public", nil, existsPlan(true), existsPlan(true), false, false, &github.BranchProtectionOptions{}, false, "")
	if err != nil {
		t.Fatalf("buildItems() error: %v", err)
	}

	// Branch protection should be skipped
	for _, item := range items {
		if item.Name == "Branch protection (protect-main ruleset)" {
			if item.Action != wizard.ActionSkip {
				t.Errorf("expected branch protection to be skipped, got %v", item.Action)
			}
		}
	}
}

// gitBatchHandler mocks the full git data API flow for a successful
// CommitFiles batch: GET ref → GET commit → POST blobs → POST tree →
// POST commit → PATCH ref (refStatus).
func gitBatchHandler(refStatus int) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/git/ref/heads/main"):
			_, _ = w.Write([]byte(`{"object":{"sha":"basesha"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/git/commits/basesha"):
			_, _ = w.Write([]byte(`{"tree":{"sha":"basesha-tree"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/blobs"):
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
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// perFileHandler answers the Contents API used by the per-file fallback.
// putStatus is the status returned for PUT /contents/*; contentsGetStatus
// for GET /contents/*.
func perFileHandler(contentsGetStatus, putStatus int) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(contentsGetStatus)
			_, _ = w.Write([]byte(`{"content":"","sha":"x"}`))
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(putStatus)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestApplyItems_WritesToInjectedWriter(t *testing.T) {
	var out strings.Builder
	items := newFileItems(
		[]string{".github/CODEOWNERS"},
		[][]byte{[]byte("me")},
		[]func() error{func() error { return nil }},
	)
	runApplyItemsExpectNoError(t, gitBatchHandler(http.StatusOK), items, false)
	testWithServerOutput(t, gitBatchHandler(http.StatusOK), items, false, &out, func(t *testing.T) {
		if !strings.Contains(out.String(), "committed as one commit") {
			t.Errorf("expected batch progress on injected writer, got %q", out.String())
		}
	})
}

// testWithServerOutput is testWithServer with an explicit output writer.
func testWithServerOutput(t *testing.T, handler http.HandlerFunc, items []wizard.Item, viaPR bool, out io.Writer, verify func(*testing.T)) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	if err := applyItems(c, "owner", "repo", "main", items, viaPR, nil, out); err != nil {
		t.Fatalf("applyItems() error: %v", err)
	}
	if verify != nil {
		verify(t)
	}
}

func TestApplyItems_No409(t *testing.T) {
	// All files land as ONE commit — no PR created.
	var prOpened bool
	handler := gitBatchHandler(http.StatusOK)
	items := newFileItems(
		[]string{".github/CODEOWNERS", ".github/SECURITY.md"},
		[][]byte{[]byte("me"), []byte("sec")},
		[]func() error{func() error { return nil }, func() error { return nil }},
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/pulls") {
			prOpened = true
		}
		handler(w, r)
	}))
	defer srv.Close()
	c := github.NewClient("t", false).WithBaseURL(srv.URL)
	if err := applyItems(c, "owner", "repo", "main", items, false, nil, io.Discard); err != nil {
		t.Fatalf("applyItems() error: %v", err)
	}
	if prOpened {
		t.Error("a successful single commit must not open a PR")
	}
}

func TestApplyItems_BatchConflictFallsBackToPR(t *testing.T) {
	// Branch protection rejects the direct commit (409) — all file changes
	// switch to PR mode.
	items := newFileItems(
		[]string{".github/CODEOWNERS", ".github/SECURITY.md"},
		[][]byte{[]byte("me"), []byte("sec")},
		[]func() error{func() error { return nil }, func() error { return nil }},
	)
	combined := combineHandlers(gitBatchHandler(http.StatusConflict), prMockHandler())
	runApplyItemsExpectNoError(t, combined, items, false)
}

func TestApplyItems_ViaPRFromStart(t *testing.T) {
	// viaPR=true — all files go directly to PR without trying direct apply.
	items := newFileItems(
		[]string{".github/CODEOWNERS", ".github/SECURITY.md"},
		[][]byte{[]byte("me"), []byte("sec")},
		[]func() error{func() error { return nil }, func() error { return nil }},
	)
	runApplyItemsExpectNoError(t, prMockHandler(), items, true)
}

// combineHandlers answers with the first handler that doesn't 404; lets a
// test layer a PR flow on top of a git-data flow.
func combineHandlers(handlers ...http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range handlers {
			rec := httptest.NewRecorder()
			h(rec, r)
			if rec.Code != http.StatusNotFound {
				for k, vs := range rec.Header() {
					for _, v := range vs {
						w.Header().Add(k, v)
					}
				}
				w.WriteHeader(rec.Code)
				_, _ = w.Write(rec.Body.Bytes())
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	})
}

func TestApplyItems_NonFileItemsApplyDirectly(t *testing.T) {
	// Non-file items (no Content) apply directly even when files fall back
	// to PR mode (batch 409).
	nonFileApplied := false
	items := []wizard.Item{
		{
			Name:    ".github/CODEOWNERS",
			Action:  wizard.ActionCreate,
			Content: []byte("me"),
		},
		{
			Name:   "General settings (auto-delete branches)",
			Action: wizard.ActionCreate,
			Apply: func() error {
				nonFileApplied = true
				return nil
			},
		},
	}
	runApplyItemsExpectNoError(t, combineHandlers(gitBatchHandler(http.StatusConflict), prMockHandler()), items, false)
	if !nonFileApplied {
		t.Error("expected non-file item to be applied directly after PR batch")
	}
}

func TestApplyItems_SkippedItemNotApplied(t *testing.T) {
	// Items with action Skip are not applied.
	items := []wizard.Item{
		{
			Name:    ".github/CODEOWNERS",
			Action:  wizard.ActionSkip,
			Content: []byte("me"),
		},
	}
	c := github.NewClient("", false)
	if err := applyItems(c, "owner", "repo", "main", items, false, nil, io.Discard); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestParseRequireChecks(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil uses defaults", nil, github.DefaultStatusChecks},
		{"empty requires none", []string{}, []string{}},
		{"explicit list", []string{"Lint", "Test (ubuntu-latest)"}, []string{"Lint", "Test (ubuntu-latest)"}},
		{"trims blanks", []string{" Lint ", "", "gosec"}, []string{"Lint", "gosec"}},
		{"dedups", []string{"Lint", "Lint"}, []string{"Lint"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := github.ResolveRequiredChecks(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("ResolveRequiredChecks(%#v) = %#v, want %#v", tt.in, got, tt.want)
			}
			for i, name := range tt.want {
				if got[i] != name {
					t.Errorf("ResolveRequiredChecks(...)[%d] = %q, want %q", i, got[i], name)
				}
			}
		})
	}
}

func TestItemFailedRequired(t *testing.T) {
	t.Cleanup(func() { globals.Strict = false })
	tests := []struct {
		name     string
		optional bool
		strict   bool
		want     bool
	}{
		{"required default", false, false, true},
		{"optional default", true, false, false},
		{"required strict", false, true, true},
		{"optional strict", true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			globals.Strict = tt.strict
			got := itemFailedRequired(wizard.Item{Optional: tt.optional})
			if got != tt.want {
				t.Errorf("itemFailedRequired(optional=%v, strict=%v) = %v, want %v", tt.optional, tt.strict, got, tt.want)
			}
		})
	}
}

func TestApplyItems_StrictOptionalFailureFailsRun(t *testing.T) {
	t.Cleanup(func() { globals.Strict = false })
	globals.Strict = true
	items := []wizard.Item{
		{
			Name:     "Tag ruleset (protect-version-tags)",
			Action:   wizard.ActionCreate,
			Optional: true,
			Apply:    func() error { return fmt.Errorf("API error") },
		},
	}
	c := github.NewClient("", false)
	err := applyItems(c, "owner", "repo", "main", items, false, nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "required items failed") {
		t.Errorf("--strict must fail the run when an optional core step fails, got: %v", err)
	}
}

func TestApplyItems_ErrorHandling_NonFatal(t *testing.T) {
	items := []wizard.Item{
		{
			Name:     "Security (secret scanning, CodeQL, Dependabot)",
			Action:   wizard.ActionCreate,
			Optional: true,
			Apply:    func() error { return fmt.Errorf("API error") },
		},
	}
	c := github.NewClient("", false)
	if err := applyItems(c, "owner", "repo", "main", items, false, nil, io.Discard); err != nil {
		t.Errorf("optional item failure must not fail the run, got: %v", err)
	}
}

func TestApplyItems_BranchProtectionFailureFailsRun(t *testing.T) {
	items := []wizard.Item{
		{
			Name:   "Branch protection (protect-main)",
			Action: wizard.ActionCreate,
			Apply:  func() error { return fmt.Errorf("ruleset create failed") },
		},
	}
	c := github.NewClient("", false)
	err := applyItems(c, "owner", "repo", "main", items, false, nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "required items failed") {
		t.Errorf("core branch-protection failure must fail the run, got: %v", err)
	}
}

func TestBranchProtectionItem_NilOpts(t *testing.T) {
	item := branchProtectionItem(nil, "owner", "repo", "main", "public", existsPlan(true), false, false, nil, "")
	if item.Action != wizard.ActionSkip {
		t.Errorf("Action = %v, want skip", item.Action)
	}
}

func TestDeferRequiredChecks_NilOpts(t *testing.T) {
	deferRequiredChecks(nil)
}

func TestApplyItems_ViaPR_DefersRequiredChecks(t *testing.T) {
	assertDeferredRequiredChecks(t, true, http.StatusOK, "PR mode must not require status checks the open harden PR cannot satisfy")
}

func TestApplyItems_409Fallback_DefersRequiredChecks(t *testing.T) {
	assertDeferredRequiredChecks(t, false, http.StatusConflict, "409 fallback must not require status checks the open harden PR cannot satisfy")
}

func assertDeferredRequiredChecks(t *testing.T, viaPR bool, refStatus int, failMsg string) {
	t.Helper()
	var rulesetBody map[string]any
	opts := &github.BranchProtectionOptions{}
	handler := combineHandlers(
		gitBatchHandler(refStatus),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/rulesets"):
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rulesets"):
				_ = json.NewDecoder(r.Body).Decode(&rulesetBody)
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":1}`))
			case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/branches/main"):
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"commit":{"sha":"abc123"}}`))
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/refs"):
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{}`))
			case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
				w.WriteHeader(http.StatusNotFound)
			case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/contents/"):
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"content":{"sha":"x"}}`))
			case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls"):
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"number":1}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}),
	)
	testWithServer(t, handler, func(c *github.Client) {
		items := []wizard.Item{
			{
				Name:    ".github/CODEOWNERS",
				Action:  wizard.ActionCreate,
				Content: []byte("me"),
			},
			branchProtectionItem(c, "owner", "repo", "main", "public", github.RulesetPlan{}, false, false, opts, ""),
		}
		if err := applyItems(c, "owner", "repo", "main", items, viaPR, opts, io.Discard); err != nil {
			t.Fatalf("applyItems() error: %v", err)
		}
	}, func(t *testing.T) {
		if !opts.SkipStatusChecks {
			t.Error("expected SkipStatusChecks after PR-mode apply")
		}
		if rulesetHasRequiredChecks(rulesetBody) {
			t.Error(failMsg)
		}
	})
}

func rulesetHasRequiredChecks(body map[string]any) bool {
	rules, ok := body["rules"].([]any)
	if !ok {
		return false
	}
	for _, rule := range rules {
		rm, ok := rule.(map[string]any)
		if ok && rm["type"] == "required_status_checks" {
			return true
		}
	}
	return false
}

func TestApplyItems_RequiredNonFileError(t *testing.T) {
	items := []wizard.Item{
		{
			Name:   "General settings (auto-delete branches)",
			Action: wizard.ActionCreate,
			Apply:  func() error { return fmt.Errorf("API error") },
		},
	}
	c := github.NewClient("", false)
	err := applyItems(c, "owner", "repo", "main", items, false, nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "required items failed") {
		t.Errorf("expected required-items error, got: %v", err)
	}
}

func TestBranchProtectionItem_FallbackOnlyOn403(t *testing.T) {
	tests := []struct {
		name            string
		visibility      string
		rulesetStatus   int
		rulesetBody     string
		classicStatus   int
		classicBody     string
		wantErr         bool
		wantErrContains string
		wantClassic     bool // true if classic API should be called
	}{
		{
			name:          "403 private upgrade message falls back to classic",
			visibility:    "private",
			rulesetStatus: http.StatusForbidden,
			rulesetBody:   `{"message":"Upgrade to GitHub Pro or make this repository public to enable this feature."}`,
			classicStatus: http.StatusOK,
			wantErr:       false,
			wantClassic:   true,
		},
		{
			// Free-tier private: rulesets 403 (upgrade) and the classic PUT
			// also 403s (upgrade) — both paths plan-gated, point at manual setup.
			name:            "403 private upgrade and classic also plan-gated",
			visibility:      "private",
			rulesetStatus:   http.StatusForbidden,
			rulesetBody:     `{"message":"Upgrade to GitHub Pro or make this repository public to enable this feature."}`,
			classicStatus:   http.StatusForbidden,
			classicBody:     `{"message":"Upgrade to GitHub Pro or make this repository public to enable this feature.","status":"403"}`,
			wantErr:         true,
			wantErrContains: "Settings → Branches",
			wantClassic:     true,
		},
		{
			name:            "403 private token-scope does not fall back",
			visibility:      "private",
			rulesetStatus:   http.StatusForbidden,
			rulesetBody:     `{"message":"Resource not accessible by integration"}`,
			wantErr:         true,
			wantErrContains: "403",
			wantClassic:     false,
		},
		{
			name:            "403 public returns error",
			visibility:      "public",
			rulesetStatus:   http.StatusForbidden,
			rulesetBody:     `{"message":"Forbidden"}`,
			wantErr:         true,
			wantErrContains: "403",
			wantClassic:     false,
		},
		{
			name:            "422 private returns error no fallback",
			visibility:      "private",
			rulesetStatus:   http.StatusUnprocessableEntity,
			rulesetBody:     `{"message":"validation failed"}`,
			wantErr:         true,
			wantErrContains: "422",
			wantClassic:     false,
		},
		{
			name:            "404 private returns error no fallback",
			visibility:      "private",
			rulesetStatus:   http.StatusNotFound,
			rulesetBody:     `{"message":"not found"}`,
			wantErr:         true,
			wantErrContains: "404",
			wantClassic:     false,
		},
		{
			name:            "500 private returns error no fallback",
			visibility:      "private",
			rulesetStatus:   http.StatusInternalServerError,
			rulesetBody:     `{"message":"internal error"}`,
			wantErr:         true,
			wantErrContains: "500",
			wantClassic:     false,
		},
		{
			name:          "201 private ruleset succeeds no fallback",
			visibility:    "private",
			rulesetStatus: http.StatusCreated,
			rulesetBody:   `{"id":1}`,
			wantErr:       false,
			wantClassic:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var classicCalled bool
			testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/rulesets"):
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`[]`))
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rulesets"):
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tt.rulesetStatus)
					var out any
					_ = json.Unmarshal([]byte(tt.rulesetBody), &out)
					_ = json.NewEncoder(w).Encode(out)
				case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/protection"):
					classicCalled = true
					if tt.classicBody != "" {
						w.Header().Set("Content-Type", "application/json")
					}
					w.WriteHeader(tt.classicStatus)
					if tt.classicBody != "" {
						var out any
						_ = json.Unmarshal([]byte(tt.classicBody), &out)
						_ = json.NewEncoder(w).Encode(out)
					}
				default:
					w.WriteHeader(http.StatusNoContent)
				}
			}), func(c *github.Client) {
				item := branchProtectionItem(c, "owner", "repo", "main", tt.visibility, github.RulesetPlan{}, false, false, &github.BranchProtectionOptions{}, "")
				err := item.Apply()
				if tt.wantErr && err == nil {
					t.Error("expected error, got nil")
				}
				if !tt.wantErr && err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
				if tt.wantErr && tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("expected error containing %q, got: %v", tt.wantErrContains, err)
				}
			}, func(t *testing.T) {
				if tt.wantClassic && !classicCalled {
					t.Error("expected classic API to be called, but it was not")
				}
				if !tt.wantClassic && classicCalled {
					t.Error("expected classic API not to be called, but it was")
				}
			})
		})
	}
}

func TestApplyItems_WorkflowLocked_Skipped(t *testing.T) {
	// When the token cannot batch workflow files (git data API refuses), the
	// per-file fallback runs; an Actions-locked workflow (PUT 404 on update)
	// is skipped and the other files still apply.
	items := newFileItems(
		[]string{".github/workflows/ci.yml", ".github/CODEOWNERS"},
		[][]byte{[]byte("workflow"), []byte("me")},
		[]func() error{func() error { return nil }, func() error { return nil }},
	)
	// GET contents: workflow exists (locked update), CODEOWNERS missing.
	// PUT workflow: 404 (Actions lock). PUT other: 201.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/.github/workflows/ci.yml"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"b2xk","sha":"wf"}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/contents/.github/workflows/ci.yml"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	// The batch commit fails (500 on ref update) so the per-file fallback runs.
	combined := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch || strings.Contains(r.URL.Path, "/git/") {
			gitBatchHandler(http.StatusInternalServerError)(w, r)
			return
		}
		handler(w, r)
	})
	runApplyItemsExpectNoError(t, combined, items, false)
}

func TestApplyItems_MixedFileAndNonFile(t *testing.T) {
	// Mix of file items and non-file items: files batch into one commit,
	// non-files apply directly after.
	nonFileApplied := false
	items := []wizard.Item{
		{
			Name:    ".github/CODEOWNERS",
			Action:  wizard.ActionCreate,
			Content: []byte("me"),
		},
		{
			Name:   "General settings (auto-delete branches)",
			Action: wizard.ActionCreate,
			Apply: func() error {
				nonFileApplied = true
				return nil
			},
		},
	}
	runApplyItemsExpectNoError(t, gitBatchHandler(http.StatusOK), items, false)
	if !nonFileApplied {
		t.Error("expected non-file item to be applied after the file batch")
	}
}

func TestBuildItems_AliasFormatVariants(t *testing.T) {
	// Test alias detection for format variants (.yml vs .md for issue templates).
	items := newBuildItemsTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// bug_report.md exists as alias (yml target)
		if strings.Contains(r.URL.Path, "/bug_report.md") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"b2xkCg=="}`))
			return
		}
		// feature_request.yml exists at target path with same content
		if strings.Contains(r.URL.Path, "/feature_request.yml") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"b2xkCg=="}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}), "private")

	// bug_report.yml should be skipped because .md alias exists
	for _, item := range items {
		if item.Name == ".github/ISSUE_TEMPLATE/bug_report.yml" {
			if item.Action != wizard.ActionSkip {
				t.Errorf("expected bug_report.yml to be skipped (md alias exists), got %v", item.Action)
			}
		}
	}
}

func TestBuildItems_AliasWorkflowVariants(t *testing.T) {
	// Test alias detection for workflow name variants: an existing
	// octopus-review.yml counts as already having the octopus.yml workflow.
	items := newBuildItemsTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/octopus-review.yml") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"b2xkCg=="}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}), "public")

	// octopus.yml should be skipped because octopus-review.yml alias exists
	for _, item := range items {
		if item.Name == ".github/workflows/octopus.yml" {
			if item.Action != wizard.ActionSkip {
				t.Errorf("expected octopus.yml to be skipped (octopus-review.yml alias exists), got %v", item.Action)
			}
		}
	}
}

func TestBuildItems_AdvancedCodeQLSkipsDefaultSetup(t *testing.T) {
	// Public renders ship the advanced codeql.yml workflow, so the security
	// item must NOT enable default-setup CodeQL (GitHub rejects advanced
	// SARIF uploads while default setup is configured).
	var calledDefaultSetup bool
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "code-scanning/default-setup"):
			calledDefaultSetup = true
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
			return
		case r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":""}`))
	}), func(c *github.Client) {
		data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "public"}
		rendered, err := templates.Render(data)
		if err != nil {
			t.Fatalf("Render() error: %v", err)
		}
		items, err := buildItems(c, nil, "owner", "repo", "main", "public", rendered, github.RulesetPlan{}, github.RulesetPlan{}, false, false, &github.BranchProtectionOptions{}, false, "")
		if err != nil {
			t.Fatalf("buildItems() error: %v", err)
		}
		for _, item := range items {
			if item.Name == "Security (secret scanning, CodeQL, Dependabot)" {
				if err := item.Apply(); err != nil {
					t.Fatalf("security item Apply() error: %v", err)
				}
			}
		}
	}, func(t *testing.T) {
		if calledDefaultSetup {
			t.Error("expected no default-setup CodeQL PATCH when advanced codeql.yml workflow is rendered")
		}
	})
}

// TestBuildItems_FileStatusSkip verifies buildItems maps FileStatus "skip"
// (existing file with identical content) to ActionSkip.
func TestBuildItems_FileStatusSkip(t *testing.T) {
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	// Pick a rendered file with no alias variants (socket.yml) so the exact
	// path check (FileStatus) runs instead of AnyFileExists.
	var target string
	for _, f := range rendered {
		if strings.HasSuffix(f.Path, "socket.yml") {
			target = f.Path
			break
		}
	}
	if target == "" {
		t.Fatal("expected a socket.yml rendered file")
	}
	var targetContent string
	for _, f := range rendered {
		if f.Path == target {
			targetContent = f.Content
			break
		}
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(targetContent))
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, target) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"content": encoded, "sha": "abc"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}), func(c *github.Client) {
		items, err := buildItems(c, nil, "owner", "repo", "main", "private", rendered, github.RulesetPlan{}, github.RulesetPlan{}, false, false, &github.BranchProtectionOptions{}, false, "")
		if err != nil {
			t.Fatalf("buildItems() error: %v", err)
		}
		for _, item := range items {
			if item.Name == target {
				if item.Action != wizard.ActionSkip {
					t.Errorf("expected %s to be skipped (content unchanged), got %v", target, item.Action)
				}
			}
		}
	}, nil)
}

// TestBranchProtectionItem_ClassicUpgradeApply verifies the upgrade item's
// Apply closure: create the ruleset first, then remove classic protection.
func TestBranchProtectionItem_ClassicUpgradeApply(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rulesets"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/rulesets"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/protection"):
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}), func(c *github.Client) {
		item := branchProtectionItem(c, "owner", "repo", "main", "public", github.RulesetPlan{}, true, false, &github.BranchProtectionOptions{}, "")
		if item.Action != wizard.ActionUpgrade {
			t.Fatalf("expected ActionUpgrade, got %v", item.Action)
		}
		if err := item.Apply(); err != nil {
			t.Fatalf("upgrade Apply() error: %v", err)
		}
	}, nil)
}

// TestBranchProtectionItem_ClassicUpgradeError verifies that a ruleset
// creation failure during the upgrade aborts before removing classic
// protection.
func TestBranchProtectionItem_ClassicUpgradeError(t *testing.T) {
	var classicRemoved bool
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rulesets"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not json`))
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/protection"):
			classicRemoved = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}), func(c *github.Client) {
		item := branchProtectionItem(c, "owner", "repo", "main", "public", github.RulesetPlan{}, true, false, &github.BranchProtectionOptions{}, "")
		if err := item.Apply(); err == nil {
			t.Fatal("expected error when ruleset creation fails")
		}
	}, func(t *testing.T) {
		if classicRemoved {
			t.Error("classic protection should not be removed when ruleset creation fails")
		}
	})
}

func TestApplyItems_RequiredFileError(t *testing.T) {
	// The batch fails for a non-409 reason and the per-file fallback hits an
	// API error on the first file — the run fails, but later files still run.
	secApplied := false
	items := []wizard.Item{
		{
			Name:    ".github/CODEOWNERS",
			Action:  wizard.ActionCreate,
			Content: []byte("me"),
		},
		{
			Name:    ".github/SECURITY.md",
			Action:  wizard.ActionCreate,
			Content: []byte("sec"),
		},
		{
			Name:   "General settings (auto-delete branches)",
			Action: wizard.ActionCreate,
			Apply: func() error {
				secApplied = true
				return nil
			},
		},
	}
	handler := combineHandlers(
		gitBatchHandler(http.StatusInternalServerError),
		perFileHandler(http.StatusNotFound, http.StatusForbidden),
	)
	testWithServer(t, handler, func(c *github.Client) {
		err := applyItems(c, "owner", "repo", "main", items, false, nil, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "required items failed") {
			t.Errorf("expected required-items error, got: %v", err)
		}
		if !secApplied {
			t.Error("non-file items should still run after a required file failure")
		}
	}, nil)
}

func TestApplyItems_PerFileFallbackSucceeds(t *testing.T) {
	// The batch fails (500 on ref update); the per-file fallback applies both
	// files through the Contents API and the run succeeds.
	items := newFileItems(
		[]string{".github/CODEOWNERS", ".github/SECURITY.md"},
		[][]byte{[]byte("me"), []byte("sec")},
		[]func() error{func() error { return nil }, func() error { return nil }},
	)
	handler := combineHandlers(
		gitBatchHandler(http.StatusInternalServerError),
		perFileHandler(http.StatusNotFound, http.StatusCreated),
	)
	runApplyItemsExpectNoError(t, handler, items, false)
}

func TestApplyItems_ViaPRNoWrittenFiles(t *testing.T) {
	items := newFileItems(
		[]string{"README.md"},
		[][]byte{[]byte("hello")},
		[]func() error{func() error { return nil }},
	)
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/branches/"):
			_, _ = w.Write([]byte(`{"commit":{"sha":"abc"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
			_, _ = w.Write([]byte(`{"content":"aGVsbG8=","sha":"x"}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/refs"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/pulls"):
			t.Error("must not open a PR when every file is skipped")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}), func(c *github.Client) {
		if err := applyItems(c, "owner", "repo", "main", items, true, nil, io.Discard); err != nil {
			t.Errorf("expected no error when PR has no file changes, got: %v", err)
		}
	}, nil)
}

// TestApplyItems_ViaPRFailure verifies ApplyViaPR errors propagate from
// applyItems.
func TestApplyItems_ViaPRFailure(t *testing.T) {
	items := newFileItems(
		[]string{".github/CODEOWNERS"},
		[][]byte{[]byte("me")},
		[]func() error{func() error { return nil }},
	)
	// PR branch creation fails (404 on GET /branches/main) → ApplyViaPR errors.
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}), func(c *github.Client) {
		err := applyItems(c, "owner", "repo", "main", items, true, nil, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "apply via PR") {
			t.Errorf("expected apply-via-PR error, got: %v", err)
		}
	}, nil)
}

// TestBuildItems_FileStatusErrorFailsPlan verifies that a failed canonical
// status check surfaces as an error instead of silently defaulting the plan
// to "create" (which misreports dry-runs and fails later with confusing PUTs).
func TestBuildItems_FileStatusErrorFailsPlan(t *testing.T) {
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	// Plain 403 is not retried, so this fails fast. socket.yml has no alias
	// variants, so the canonical FileStatus GET is what fails. Earlier files
	// in the render order must behave normally (404 = create).
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/contents/socket.yml") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}), func(c *github.Client) {
		items, err := buildItems(c, nil, "owner", "repo", "main", "private", rendered, github.RulesetPlan{}, github.RulesetPlan{}, false, false, &github.BranchProtectionOptions{}, false, "")
		if err == nil {
			t.Fatal("expected buildItems to fail when FileStatus errors, got nil")
		}
		if !strings.Contains(err.Error(), "check status of socket.yml") {
			t.Errorf("expected error to name the failing path, got: %v", err)
		}
		if items != nil {
			t.Errorf("expected nil items on error, got %d items", len(items))
		}
	}, nil)
}

// TestBuildItems_AliasCheckErrorFailsPlan verifies that a failed alias
// existence check (e.g. root CODEOWNERS variant) also fails the plan instead
// of being swallowed and risking a duplicate write.
func TestBuildItems_AliasCheckErrorFailsPlan(t *testing.T) {
	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "private"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/contents/CODEOWNERS"):
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}), func(c *github.Client) {
		items, err := buildItems(c, nil, "owner", "repo", "main", "private", rendered, github.RulesetPlan{}, github.RulesetPlan{}, false, false, &github.BranchProtectionOptions{}, false, "")
		if err == nil {
			t.Fatal("expected buildItems to fail when the alias check errors, got nil")
		}
		if !strings.Contains(err.Error(), "check aliases of .github/CODEOWNERS") {
			t.Errorf("expected error to name the alias check, got: %v", err)
		}
		if items != nil {
			t.Errorf("expected nil items on error, got %d items", len(items))
		}
	}, nil)
}

func TestBuildItems_SkipStepsMarksOptOut(t *testing.T) {
	t.Cleanup(func() { globals.SkipSteps = nil; globals.OnlySteps = nil })
	reset := func() {
		globals.SkipSteps = nil
		globals.OnlySteps = nil
	}
	t.Cleanup(reset)

	globals.SkipSteps = []string{"protect-version-tags", "socket.yml"}
	items, err := buildItems(nil, nil, "owner", "repo", "main", "public", nil, github.RulesetPlan{}, github.RulesetPlan{}, false, false, &github.BranchProtectionOptions{}, false, "")
	if err != nil {
		t.Fatalf("buildItems() error: %v", err)
	}
	for _, item := range items {
		if item.Name == "Tag ruleset (protect-version-tags)" {
			if item.Action != wizard.ActionSkip || item.OptOutReason != "--skip" {
				t.Errorf("skipped tag ruleset = %+v, want skip via --skip", item)
			}
		}
	}

	globals.SkipSteps = nil
	globals.OnlySteps = []string{"files"}
	rendered, err := templates.Render(templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "public", CIPack: "go"})
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	items, err = buildItems(nil, nil, "owner", "repo", "main", "public", rendered, github.RulesetPlan{}, github.RulesetPlan{}, false, false, &github.BranchProtectionOptions{}, false, "")
	if err != nil {
		t.Fatalf("buildItems() error: %v", err)
	}
	var fileSeen, stepSeen bool
	for _, item := range items {
		if item.Content != nil {
			fileSeen = true
			if item.Action == wizard.ActionSkip {
				t.Errorf("file %q must stay included under --only files: %+v", item.Name, item)
			}
		} else {
			stepSeen = true
			if item.Action != wizard.ActionSkip || item.OptOutReason != "--only" {
				t.Errorf("non-file item must be opted out via --only: %+v", item)
			}
		}
	}
	if !fileSeen || !stepSeen {
		t.Fatalf("expected both file and step items, fileSeen=%v stepSeen=%v", fileSeen, stepSeen)
	}
}

func TestChangesAfter(t *testing.T) {
	changes := []github.FileChange{
		{Path: "a"},
		{Path: "b"},
		{Path: "c"},
	}
	if got := changesAfter(changes, "b"); len(got) != 1 || got[0].Path != "c" {
		t.Errorf("changesAfter(b) = %v, want [c]", got)
	}
	if got := changesAfter(changes, "missing"); got != nil {
		t.Errorf("changesAfter(missing) = %v, want nil", got)
	}
}

func TestApplyItems_PerFile409SwitchesToPR(t *testing.T) {
	// Batch fails (500), per-file fallback: first file PUTs fine, second
	// file's PUT returns 409 → remaining files go to a PR.
	items := newFileItems(
		[]string{"a.txt", "b.txt", "c.txt"},
		[][]byte{[]byte("1"), []byte("2"), []byte("3")},
		[]func() error{func() error { return nil }, func() error { return nil }, func() error { return nil }},
	)
	bPuts := 0
	perFile := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/contents/b.txt"):
			// The default-branch PUT 409s (protection); the PR-branch retry succeeds.
			bPuts++
			if bPuts == 1 {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"message":"Repository rule violations found","documentation_url":"x"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	pr := prMockHandler()
	// The PR flow needs GET /branches/main + POST /git/refs + POST /pulls —
	// prMockHandler handles those; route unmatched calls to it.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		perFileRec := httptest.NewRecorder()
		perFile(perFileRec, r)
		if perFileRec.Code != http.StatusNotFound {
			w.WriteHeader(perFileRec.Code)
			_, _ = w.Write(perFileRec.Body.Bytes())
			return
		}
		gitRec := httptest.NewRecorder()
		gitBatchHandler(http.StatusInternalServerError)(gitRec, r)
		if gitRec.Code != http.StatusNotFound {
			w.WriteHeader(gitRec.Code)
			_, _ = w.Write(gitRec.Body.Bytes())
			return
		}
		pr(w, r)
	})
	opts := &github.BranchProtectionOptions{}
	var dbg strings.Builder
	testWithServer(t, handler, func(c *github.Client) {
		if err := applyItems(c, "owner", "repo", "main", items, false, opts, &dbg); err != nil {
			t.Fatalf("applyItems() error: %v\n%s", err, dbg.String())
		}
		if !opts.SkipStatusChecks {
			t.Errorf("per-file 409 fallback must defer required checks for the open PR\n%s", dbg.String())
		}
	}, nil)
}
