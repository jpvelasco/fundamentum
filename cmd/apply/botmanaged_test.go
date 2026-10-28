package apply

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/steps"
	"github.com/jpvelasco/fundamentum/internal/templates"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

func TestGeneralSettingsBotManaged(t *testing.T) {
	for _, tc := range []struct {
		name string
		bot  bool
		info github.Repo
		want wizard.Action
	}{
		{"squash disabled", true, github.Repo{DeleteBranchOnMerge: true}, wizard.ActionUpdate},
		{"merge enabled", true, github.Repo{DeleteBranchOnMerge: true, AllowSquashMerge: true, AllowMergeCommit: true}, wizard.ActionUpdate},
		{"rebase enabled", true, github.Repo{DeleteBranchOnMerge: true, AllowSquashMerge: true, AllowRebaseMerge: true}, wizard.ActionUpdate},
		{"compliant", true, github.Repo{DeleteBranchOnMerge: true, AllowSquashMerge: true}, wizard.ActionSkip},
		{"profile off", false, github.Repo{DeleteBranchOnMerge: true, AllowMergeCommit: true}, wizard.ActionSkip},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetBotManagedGlobals(t)
			globals.BotManaged = tc.bot
			item := generalSettingsItem(nil, &tc.info, "owner", "repo")
			if item.Action != tc.want {
				t.Fatalf("item = %+v, want %v", item, tc.want)
			}
		})
	}
}

func TestRunWithClientBotManagedPresets(t *testing.T) {
	for _, preset := range []string{"", "oss", "private", "strict"} {
		t.Run("preset="+preset, func(t *testing.T) {
			resetBotManagedGlobals(t)
			t.Cleanup(func() {
				globals.Preset = ""
				globals.Strict = false
				globals.AdvancedSecurity = false
			})
			globals.BotManaged = true
			globals.Preset = preset
			globals.KeepFiles = []string{".github/workflows/release.yml"}
			globals.PolicyChecks = []github.PolicyCheck{{Context: "external-review", IntegrationID: 4242}}
			base := runFlowHandler()
			var settings, permissions map[string]any
			var branch github.Ruleset
			filesWritten := 0
			srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/user":
					_, _ = io.WriteString(w, `{"login":"owner","type":"User"}`)
				case r.URL.Path == "/repos/owner/repo" && r.Method == http.MethodGet:
					_, _ = io.WriteString(w, `{"visibility":"public","delete_branch_on_merge":true,"allow_squash_merge":true,"allow_merge_commit":true,"allow_rebase_merge":true}`)
				case r.URL.Path == "/repos/owner/repo" && r.Method == http.MethodPatch:
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if _, ok := body["delete_branch_on_merge"]; ok {
						settings = body
					}
					_, _ = io.WriteString(w, `{}`)
				case strings.HasSuffix(r.URL.Path, "/actions/permissions/workflow"):
					switch {
					case r.Method == http.MethodPut:
						if err := json.NewDecoder(r.Body).Decode(&permissions); err != nil {
							t.Error(err)
						}
						w.WriteHeader(http.StatusNoContent)
					case permissions != nil:
						_ = json.NewEncoder(w).Encode(permissions)
					default:
						_, _ = io.WriteString(w, `{"default_workflow_permissions":"write","can_approve_pull_request_reviews":true}`)
					}
				case strings.HasSuffix(r.URL.Path, "/rulesets") && r.Method == http.MethodPost:
					var ruleset github.Ruleset
					if err := json.NewDecoder(r.Body).Decode(&ruleset); err != nil {
						t.Error(err)
					}
					if ruleset.Name == "protect-main" {
						branch = ruleset
					}
					w.WriteHeader(http.StatusCreated)
					_, _ = io.WriteString(w, `{"id":1}`)
				default:
					if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/contents/") {
						filesWritten++
					}
					base(w, r)
				}
			}))
			defer srv.Close()
			var out strings.Builder
			if err := runWithClient(client, "owner", "repo", strings.NewReader("solo\ny\n"), &out); err != nil {
				t.Fatalf("apply: %v\n%s", err, out.String())
			}
			if !reflect.DeepEqual(settings, map[string]any{"delete_branch_on_merge": true, "allow_squash_merge": true, "allow_merge_commit": false, "allow_rebase_merge": false}) {
				t.Errorf("settings = %v", settings)
			}
			if !reflect.DeepEqual(permissions, map[string]any{"default_workflow_permissions": "read", "can_approve_pull_request_reviews": false}) {
				t.Errorf("permissions = %v", permissions)
			}
			opts := github.BranchProtectionOptions{BotManaged: true, PolicyChecks: globals.PolicyChecks, StrictChecks: globals.BranchUpToDate, ShippedChecks: github.DefaultStatusChecks}
			if branch.Name != "protect-main" || len(branch.BypassActors) != 0 {
				t.Errorf("branch ruleset = %+v", branch)
			}
			if drift := github.BranchRulesetDrift(&branch, github.ResolveRequiredChecksForPack(nil, "go"), opts); len(drift) > 0 {
				t.Errorf("applied branch drift = %v", drift)
			}
			if filesWritten == 0 || !strings.Contains(out.String(), "Done") || !strings.Contains(out.String(), "after:") {
				t.Errorf("incomplete apply (%d files):\n%s", filesWritten, out.String())
			}
		})
	}
}

func TestBaselinePolicyChecksOff(t *testing.T) {
	for _, explicitOff := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit-off=%t", explicitOff), func(t *testing.T) {
			resetBotManagedGlobals(t)
			globals.ResetExplicit()
			t.Cleanup(globals.ResetExplicit)
			globals.BotManaged = false
			globals.PolicyChecks = nil
			if explicitOff {
				globals.NoteExplicit("bot-managed")
			}
			if err := globals.ApplyBaseline(globals.Baseline{
				BotManaged:      explicitOff,
				BotPolicyChecks: []globals.PolicyCheck{{Context: "unvalidated-policy"}},
			}); err != nil {
				t.Fatal(err)
			}
			if err := ValidateBotManagedFlags(); err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				w.WriteHeader(http.StatusCreated)
			}))
			defer srv.Close()
			if err := client.CreateBranchRuleset("owner", "repo", []string{"Lint"}, github.BranchProtectionOptions{
				Solo: true, BotManaged: globals.BotManaged, PolicyChecks: globals.PolicyChecks,
			}); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "unvalidated-policy") {
				t.Fatalf("disabled profile leaked policy into request: %s", encoded)
			}
		})
	}
}

func TestApplyCustomCheckNamesStayUnpinned(t *testing.T) {
	for _, pack := range []string{"none", "go"} {
		for _, bot := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/bot=%t", pack, bot), func(t *testing.T) {
				resetBotManagedGlobals(t)
				t.Cleanup(func() {
					globals.Preset, globals.CIPack = "", ""
					globals.OnlySteps, globals.RequireChecks = nil, nil
				})
				globals.Preset, globals.CIPack = "oss", pack
				globals.BotManaged = bot
				globals.RequireChecks = []string{"CI", "Test", "Lint"}
				globals.OnlySteps = []string{"protect-main"}
				base := runFlowHandler()
				var body map[string]any
				srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.URL.Path == "/user":
						_ = json.NewEncoder(w).Encode(map[string]string{"login": "owner", "type": "User"})
					case strings.HasSuffix(r.URL.Path, "/rulesets") && r.Method == http.MethodPost:
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						w.WriteHeader(http.StatusCreated)
						_ = json.NewEncoder(w).Encode(map[string]int{"id": 1})
					default:
						base(w, r)
					}
				}))
				defer srv.Close()
				if err := runWithClient(client, "owner", "repo", strings.NewReader(""), io.Discard); err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				if body["name"] != "protect-main" || strings.Contains(string(encoded), "integration_id") {
					t.Fatalf("custom check names acquired app pins: %s", encoded)
				}
				for _, context := range globals.RequireChecks {
					if !strings.Contains(string(encoded), `"context":"`+context+`"`) {
						t.Errorf("required context %s missing: %s", context, encoded)
					}
				}
			})
		}
	}
}

func TestPlanNewRepoPortableKeep(t *testing.T) {
	resetBotManagedGlobals(t)
	globals.KeepFiles = []string{"codecov.yml", ".github/workflows/release.yml"}
	if err := PlanNewRepo("owner", "repo", "private", io.Discard); err != nil {
		t.Fatalf("keep entries absent from private templates must be harmless: %v", err)
	}
}

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

// TestActionsPermissionsItemApply exercises the actions step's Apply: it
// prints before/after and PUTs the read-only lockdown.
func TestActionsPermissionsItemApply(t *testing.T) {
	resetBotManagedGlobals(t)
	putBody := ""
	locked := false
	srv, c := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/actions/permissions/workflow"):
			w.WriteHeader(http.StatusOK)
			if locked {
				_, _ = w.Write([]byte(`{"default_workflow_permissions":"read","can_approve_pull_request_reviews":false}`))
			} else {
				_, _ = w.Write([]byte(`{"default_workflow_permissions":"write","can_approve_pull_request_reviews":true}`))
			}
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/actions/permissions/workflow"):
			body, _ := io.ReadAll(r.Body)
			putBody = string(body)
			locked = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	var out strings.Builder
	item := actionsPermissionsItem(c, "owner", "repo", steps.New(nil, nil), &out)
	if item.Apply == nil {
		t.Fatal("live actions item must have an Apply closure")
	}
	if err := item.Apply(); err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "before: default_workflow_permissions=write can_approve_pull_request_reviews=true") {
		t.Errorf("missing before state:\n%s", got)
	}
	if !strings.Contains(got, "after:  default_workflow_permissions=read can_approve_pull_request_reviews=false") {
		t.Errorf("missing after state:\n%s", got)
	}
	if !strings.Contains(putBody, `"default_workflow_permissions":"read"`) || !strings.Contains(putBody, `"can_approve_pull_request_reviews":false`) {
		t.Errorf("PUT body = %s", putBody)
	}

	// Opted out via --skip actions → skip row, no Apply.
	item = actionsPermissionsItem(c, "owner", "repo", steps.New([]string{"actions"}, nil), &out)
	if item.Action != wizard.ActionSkip || item.Apply != nil {
		t.Errorf("opted-out actions item = %+v", item)
	}
}

// TestPrintRemovedBypassActors covers the removed-bypass-actors notice.
func TestPrintRemovedBypassActors(t *testing.T) {
	// Nothing to print when there are no removed actors.
	var out strings.Builder
	printRemovedBypassActors(&out, nil)
	if out.String() != "" {
		t.Errorf("no removed actors must print nothing, got %q", out.String())
	}

	// Removed actors are listed.
	teamID := int64(7)
	out.Reset()
	printRemovedBypassActors(&out, []github.BypassActor{
		{ActorID: &teamID, ActorType: "Team", BypassMode: "always"},
		{ActorType: "OrganizationAdmin", BypassMode: "pull_request"},
	})
	got := out.String()
	if !strings.Contains(got, "removing 2 ruleset bypass actor(s)") {
		t.Errorf("missing removal count:\n%s", got)
	}
	if !strings.Contains(got, "Team#7 (always)") || !strings.Contains(got, "OrganizationAdmin (pull_request)") {
		t.Errorf("missing actor descriptions:\n%s", got)
	}
}

// TestPlanNewRepoBotManaged shows the actions step in an init --dry-run plan
// and enforces the flag guardrails without a client.
func TestPlanNewRepoBotManaged(t *testing.T) {
	resetBotManagedGlobals(t)
	globals.BotManaged = true
	globals.DryRun = true
	t.Cleanup(func() { globals.DryRun = false })

	var out strings.Builder
	if err := PlanNewRepo("owner", "repo", "private", &out); err != nil {
		t.Fatalf("PlanNewRepo() error: %v", err)
	}
	if !strings.Contains(out.String(), "Actions default token") {
		t.Errorf("bot-managed init --dry-run must plan the actions step:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "squash-only merges") {
		t.Errorf("bot-managed settings must be named in the plan:\n%s", out.String())
	}

	// --octopus is refused even without a client.
	globals.Octopus = true
	if err := PlanNewRepo("owner", "repo", "private", &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "--octopus") {
		t.Errorf("init --dry-run must refuse --octopus + --bot-managed, got %v", err)
	}
	globals.Octopus = false

	// An unpinned policy check is refused even without a client.
	globals.PolicyChecks = []github.PolicyCheck{{Context: "external-review"}}
	if err := PlanNewRepo("owner", "repo", "private", &strings.Builder{}); err == nil {
		t.Error("init --dry-run must refuse an unpinned policy check")
	}
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
