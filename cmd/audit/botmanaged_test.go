package audit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
)

// botManagedAuditHandler extends the passing baseline with the real API
// settings and required app pins that the profile adds.
func botManagedAuditHandler(t *testing.T, mergeCommit bool) http.HandlerFunc {
	t.Helper()
	probes := botManagedHandler(compliantBotManagedProbe)
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"visibility": "public", "default_branch": "main", "delete_branch_on_merge": true,
				"allow_squash_merge": true, "allow_merge_commit": mergeCommit, "allow_rebase_merge": false,
				"owner": map[string]string{"type": "User"},
				"security_and_analysis": map[string]any{
					"secret_scanning":                 map[string]string{"status": "enabled"},
					"secret_scanning_push_protection": map[string]string{"status": "enabled"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/rulesets/1"):
			var checks []map[string]any
			for _, name := range github.ResolveRequiredChecksForPack(nil, "go") {
				checks = append(checks, map[string]any{"context": name, "integration_id": 15368})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 1, "name": "protect-main", "target": "branch", "enforcement": "active",
				"bypass_actors": []any{},
				"conditions":    map[string]any{"ref_name": map[string]any{"include": []string{"~DEFAULT_BRANCH"}}},
				"rules": []map[string]any{
					{"type": "deletion"}, {"type": "non_fast_forward"},
					{"type": "pull_request", "parameters": map[string]any{
						"required_review_thread_resolution": true, "allowed_merge_methods": []string{"squash"},
					}},
					{"type": "required_status_checks", "parameters": map[string]any{
						"strict_required_status_checks_policy": true, "required_status_checks": checks,
					}},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/rulesets/2"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 2, "name": "protect-version-tags", "target": "tag", "enforcement": "active",
				"conditions": map[string]any{"ref_name": map[string]any{"include": []string{"refs/tags/v*"}}},
				"rules":      []map[string]string{{"type": "creation"}, {"type": "deletion"}, {"type": "non_fast_forward"}},
			})
		case strings.Contains(r.URL.Path, "/actions/") || strings.Contains(r.URL.Path, "/environments") || r.URL.Path == "/repos/owner/repo/contents/.github/workflows":
			probes(w, r)
		default:
			writePassingAudit(t, w, r, true, true)
		}
	}
}

func TestAuditBotManagedPresets(t *testing.T) {
	for _, preset := range []string{"", "oss", "private", "strict"} {
		t.Run("preset="+preset, func(t *testing.T) {
			resetAuditGlobals(t)
			globals.BotManaged = true
			globals.Preset = preset
			globals.KeepFiles = []string{".github/workflows/release.yml"}
			report, err := runAuditWithHandler(t, botManagedAuditHandler(t, false))
			if err != nil {
				t.Fatalf("audit: %v\n%s", err, report)
			}
			for _, name := range []string{"squash-only merges", "actions default token", "environment codacy", "workflow scan"} {
				if !strings.Contains(report, name) {
					t.Errorf("missing %q:\n%s", name, report)
				}
			}
			if strings.Contains(report, "WARN") || strings.Contains(report, "FAIL") {
				t.Fatalf("hardened repo has drift:\n%s", report)
			}
		})
	}
}

func TestAuditBotManagedMergeSettings(t *testing.T) {
	for _, skip := range []bool{false, true} {
		t.Run(map[bool]string{false: "required", true: "opted out"}[skip], func(t *testing.T) {
			resetAuditGlobals(t)
			globals.BotManaged = true
			if skip {
				globals.SkipSteps = []string{"settings"}
			}
			report, err := runAuditWithHandler(t, botManagedAuditHandler(t, true))
			if (err == nil) != skip {
				t.Fatalf("audit error = %v, skip=%t\n%s", err, skip, report)
			}
			if !skip && !strings.Contains(report, "allow_merge_commit=true") {
				t.Errorf("missing merge drift:\n%s", report)
			}
		})
	}
}

func TestAuditBotManagedRefusals(t *testing.T) {
	for _, tc := range []struct {
		name    string
		octopus bool
		policy  []github.PolicyCheck
		want    string
	}{
		{"octopus", true, nil, "--octopus"},
		{"unpinned policy", false, []github.PolicyCheck{{Context: "policy"}}, "integration_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetAuditGlobals(t)
			globals.BotManaged = true
			globals.Octopus = tc.octopus
			globals.PolicyChecks = tc.policy
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/repos/owner/repo" {
					t.Errorf("unexpected request before refusal: %s %s", r.Method, r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"visibility": "public"})
			}))
			defer srv.Close()
			var out strings.Builder
			err := runWithClient(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", &out)
			if err == nil || !strings.Contains(err.Error(), tc.want) || out.Len() != 0 {
				t.Fatalf("expected refusal %q before report, got %v\n%s", tc.want, err, out.String())
			}
		})
	}
}

func TestAuditPolicyChecksIgnoredWithProfileOff(t *testing.T) {
	resetAuditGlobals(t)
	srv := httptest.NewServer(passingAuditHandler(t))
	defer srv.Close()
	client := github.NewClient("t", false).WithBaseURL(srv.URL)
	var baseline, withPolicy strings.Builder
	if err := runWithClient(client, "owner", "repo", &baseline); err != nil {
		t.Fatal(err)
	}
	globals.PolicyChecks = []github.PolicyCheck{{Context: "unvalidated-policy"}}
	if err := runWithClient(client, "owner", "repo", &withPolicy); err != nil {
		t.Fatal(err)
	}
	if withPolicy.String() != baseline.String() {
		t.Fatalf("profile-off audit output changed:\n%s\nwant:\n%s", withPolicy.String(), baseline.String())
	}
}

func TestBotManagedProbesUnreadable(t *testing.T) {
	resetAuditGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	results := botManagedChecks(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", "main")
	if len(results) != 4 {
		t.Fatalf("results = %+v", results)
	}
	for _, result := range results {
		if result.Status != statusWarn || !strings.Contains(result.Detail, "403") {
			t.Errorf("unreadable probe must warn: %+v", result)
		}
	}
	if failedRequired(results) {
		t.Error("report-only probe errors must not fail an ordinary audit")
	}
	globals.Strict = true
	if !failedRequired(results) {
		t.Error("strict audit must fail when it cannot verify posture")
	}
}

func TestEnvironmentChecksUnsafe(t *testing.T) {
	for _, tc := range []struct {
		name, detail, policies, want string
	}{
		{"unrestricted", `{"deployment_branch_policy":null}`, "", "unrestricted"},
		{"tag access", compliantBotManagedProbe.envDetail, `{"branch_policies":[{"name":"main","type":"branch"},{"name":"v*","type":"tag"}]}`, "tags=[v*]"},
		{"protected branches", `{"deployment_branch_policy":{"protected_branches":true,"custom_branch_policies":false}}`, "", "protected-branches=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := compliantBotManagedProbe
			probe.envDetail, probe.envPolicies = tc.detail, tc.policies
			srv := httptest.NewServer(botManagedHandler(probe))
			defer srv.Close()
			checks, allowed := environmentChecks(github.NewClient("t", false).WithBaseURL(srv.URL), "owner", "repo", "main")
			if len(allowed) != 0 || len(checks) != 1 || checks[0].Status != statusWarn || !strings.Contains(checks[0].Detail, tc.want) {
				t.Fatalf("unsafe environment accepted or misreported: checks=%+v allowed=%v", checks, allowed)
			}
		})
	}
}
