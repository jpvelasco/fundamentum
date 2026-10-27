package apply

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/templates"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

// resetBotManagedGlobals restores flag state mutated by these tests.
func resetBotManagedGlobals(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		globals.BotManaged = false
		globals.Octopus = false
		globals.PolicyChecks = nil
		globals.KeepFiles = nil
		globals.SkipSteps = nil
	})
}

func TestValidateBotManaged_OffIsNoop(t *testing.T) {
	resetBotManagedGlobals(t)
	globals.BotManaged = false
	// Even with --octopus set, an off profile never validates.
	globals.Octopus = true
	if err := validateBotManaged(nil); err != nil {
		t.Errorf("BotManaged off must be a no-op, got %v", err)
	}
}

func TestValidateBotManaged_RefusesOctopus(t *testing.T) {
	resetBotManagedGlobals(t)
	globals.BotManaged = true
	globals.Octopus = true
	err := validateBotManaged(nil)
	if err == nil || !strings.Contains(err.Error(), "--octopus") {
		t.Errorf("bot-managed + --octopus must be refused, got %v", err)
	}
}

func TestValidateBotManaged_RefusesUnpinnedPolicyCheck(t *testing.T) {
	resetBotManagedGlobals(t)
	globals.BotManaged = true
	globals.PolicyChecks = []github.PolicyCheck{{Context: "external-review"}} // no integration_id
	err := validateBotManaged(nil)
	if err == nil || !strings.Contains(err.Error(), "integration_id") {
		t.Errorf("unpinned policy check must be refused, got %v", err)
	}
}

func TestValidateBotManaged_RefusesBotToken(t *testing.T) {
	resetBotManagedGlobals(t)
	globals.BotManaged = true
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"login":"ci-bot[bot]","type":"Bot"}`))
	}))
	defer srv.Close()
	err := validateBotManaged(c)
	if err == nil || !strings.Contains(err.Error(), "bot or app token") {
		t.Errorf("bot token must be refused, got %v", err)
	}
}

func TestValidateBotManaged_RefusesInstallationToken(t *testing.T) {
	resetBotManagedGlobals(t)
	globals.BotManaged = true
	// Installation tokens (ghs_) are refused without any API call.
	c := github.NewClient("ghs_installationtoken", false)
	err := validateBotManaged(c)
	if err == nil || !strings.Contains(err.Error(), "bot or app token") {
		t.Errorf("installation token must be refused, got %v", err)
	}
}

func TestValidateBotManaged_AllowsHumanWithPinnedChecks(t *testing.T) {
	resetBotManagedGlobals(t)
	globals.BotManaged = true
	globals.PolicyChecks = []github.PolicyCheck{{Context: "external-review", IntegrationID: 4242}}
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"login":"octocat","type":"User"}`))
	}))
	defer srv.Close()
	if err := validateBotManaged(c); err != nil {
		t.Errorf("human token + pinned policy checks must pass, got %v", err)
	}
}

// TestKeepListProtectsHandTunedFile proves a file on the baseline keep list
// is excluded from apply (skip / opted out) even when its live content drifts
// from the template — apply never reverts a hand-tuned workflow.
func TestKeepListProtectsHandTunedFile(t *testing.T) {
	resetBotManagedGlobals(t)
	globals.KeepFiles = []string{".github/workflows/ci.yml"}

	// Mock: ci.yml EXISTS on the repo with content that differs from the
	// template (hand-tuned). Without the keep list this would be an "update";
	// with it, apply must skip.
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/contents/.github/workflows/ci.yml") {
			w.WriteHeader(http.StatusOK)
			// base64 of "hand tuned\n" — differs from the shipped template.
			_, _ = w.Write([]byte(`{"content":"aGFuZCB0dW5lZAo=","sha":"abc"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "public"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	items, err := buildItems(c, nil, "owner", "repo", "main", "public", rendered, github.RulesetPlan{}, github.RulesetPlan{}, false, false, &github.BranchProtectionOptions{}, false, "", io.Discard)
	if err != nil {
		t.Fatalf("buildItems() error: %v", err)
	}
	for _, item := range items {
		if item.Name == ".github/workflows/ci.yml" {
			if item.Action != wizard.ActionSkip {
				t.Errorf("kept file action = %v, want skip (never overwrite)", item.Action)
			}
			if item.OptOutReason == "" {
				t.Error("kept file should carry an opt-out reason")
			}
			return
		}
	}
	t.Error(".github/workflows/ci.yml not found in plan")
}

// TestKeepListDoesNotAffectOtherFiles proves the keep list only skips the
// listed file; another drifted file still updates.
func TestKeepListDoesNotAffectOtherFiles(t *testing.T) {
	resetBotManagedGlobals(t)
	globals.KeepFiles = []string{".github/workflows/ci.yml"}

	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Every shipped file exists with drifted content.
		if strings.Contains(r.URL.Path, "/contents/") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":"aGFuZCB0dW5lZAo=","sha":"abc"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	data := templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "public"}
	rendered, err := templates.Render(data)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	items, err := buildItems(c, nil, "owner", "repo", "main", "public", rendered, github.RulesetPlan{}, github.RulesetPlan{}, false, false, &github.BranchProtectionOptions{}, false, "", io.Discard)
	if err != nil {
		t.Fatalf("buildItems() error: %v", err)
	}
	for _, item := range items {
		// codecov.yml has no alias and is not on the keep list → update.
		if item.Name == "codecov.yml" {
			if item.Action != wizard.ActionUpdate {
				t.Errorf("non-kept file action = %v, want update", item.Action)
			}
			return
		}
	}
	t.Error("codecov.yml not found in plan")
}
