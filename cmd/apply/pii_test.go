package apply

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/pii"
	"github.com/jpvelasco/fundamentum/internal/templates"
)

func TestPIIRollout(t *testing.T) {
	for _, tt := range []struct {
		name, policy, asset, bad string
		missing, ready, fail     bool
		checks, skip, only, keep []string
	}{
		{name: "pending", missing: true},
		{name: "reviewed empty", ready: true},
		{name: "old helper", asset: pii.HelperPath},
		{name: "old workflow", asset: pii.WorkflowPath},
		{name: "dirty default tree", bad: "42" + "4242424242"},
		{name: "exact reviewed", policy: "42" + "4242424242", bad: "42" + "4242424242", ready: true},
		{name: "invalid policy", policy: "*", fail: true},
		{name: "workflow skip", skip: []string{pii.WorkflowPath}},
		{name: "helper skip", skip: []string{pii.HelperPath}},
		{name: "files skip", skip: []string{"files"}},
		{name: "keep", keep: []string{pii.HelperPath}},
		{name: "protection excluded", only: []string{pii.WorkflowPath}},
		{name: "protection reapply", only: []string{"protect-main"}, ready: true},
		{name: "custom", checks: []string{"vendor"}, ready: true},
		{name: "empty override", checks: []string{}, ready: true},
		{name: "explicit ready", checks: []string{pii.CheckName}, ready: true},
		{name: "explicit pending", checks: []string{pii.CheckName}, missing: true, fail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			globals.RequireChecks, globals.SkipSteps, globals.OnlySteps, globals.KeepFiles = tt.checks, tt.skip, tt.only, tt.keep
			t.Cleanup(func() {
				globals.RequireChecks, globals.SkipSteps, globals.OnlySteps, globals.KeepFiles = nil, nil, nil, nil
			})
			files, err := templates.Render(templates.RepoData{Owner: "o", RepoName: "r", DefaultBranch: "main", Visibility: "public", CIPack: "none"})
			if err != nil {
				t.Fatal(err)
			}
			contents := map[string]string{pii.AllowlistPath: tt.policy, "data": tt.bad}
			for _, file := range files {
				if file.Path == pii.HelperPath || file.Path == pii.WorkflowPath {
					contents[file.Path] = file.Content
				}
			}
			if tt.asset != "" {
				contents[tt.asset] = "old"
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Fatal("readiness is read-only")
				}
				var result any
				switch {
				case strings.Contains(r.URL.Path, "/contents/"):
					if tt.missing {
						http.NotFound(w, r)
						return
					}
					result = map[string]any{}
				case strings.Contains(r.URL.Path, "/commits/"):
					result = map[string]any{"sha": "head", "commit": map[string]any{"tree": map[string]any{"sha": "root"}}}
				case strings.Contains(r.URL.Path, "/trees/"):
					entries := []any{}
					for path := range contents {
						entries = append(entries, map[string]any{"path": path, "sha": path, "type": "blob"})
					}
					result = map[string]any{"sha": "root", "tree": entries}
				case strings.Contains(r.URL.Path, "/blobs/"):
					path := strings.SplitN(r.URL.Path, "/blobs/", 2)[1]
					body := contents[path]
					result = map[string]any{"sha": path, "size": len(body), "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(body))}
				}
				_ = json.NewEncoder(w).Encode(result)
			}))
			defer srv.Close()
			checks, shipped, rollout, err := ResolvePIIChecks(github.NewClient("token", false).WithBaseURL(srv.URL), github.Repo{Owner: "o", RepoName: "r", DefaultBranch: "main"}, "none", files)
			if (err != nil) != tt.fail {
				t.Fatal("unexpected failure", err)
			}
			if tt.fail {
				return
			}
			enrolled := slices.Contains(checks, pii.CheckName)
			wantEnrollment := tt.ready && (tt.checks == nil || slices.Contains(tt.checks, pii.CheckName))
			if enrolled != wantEnrollment || slices.Contains(shipped, pii.CheckName) != enrolled {
				t.Fatal("unsafe enrollment or incorrect check ownership")
			}
			if (rollout.Ready && !rollout.OptedOut) != wantEnrollment {
				t.Fatal("wrong readiness")
			}
			if tt.checks != nil && !slices.Equal(checks, tt.checks) {
				t.Fatal("custom override expanded")
			}
		})
	}
}

func TestPIILiveRule(t *testing.T) {
	for _, pin := range []any{nil, float64(github.GitHubActionsAppID), float64(12)} {
		plan := github.RulesetPlan{Live: &github.Ruleset{Rules: []map[string]any{{
			"type": "required_status_checks", "parameters": map[string]any{
				"required_status_checks": []any{map[string]any{"context": pii.CheckName, "integration_id": pin}},
			},
		}}}}
		required, detail, err := InspectPIIRule(plan, PIIRollout{Detail: "pending"})
		if !required || (err != nil) != (pin == float64(12)) || err == nil && !strings.Contains(detail, "needs repair") {
			t.Fatal("live identity or repair guidance")
		}
	}
}

func TestPIIResolvedIntentApplied(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		t.Run(map[bool]string{false: "enroll", true: "defer"}[deferred], func(t *testing.T) {
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPut:
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					_, _ = w.Write([]byte(`{}`))
				case strings.HasSuffix(r.URL.Path, "/rulesets"):
					_, _ = w.Write([]byte(`[{"id":7,"name":"protect-main"}]`))
				default:
					_, _ = w.Write([]byte(`{"id":7,"name":"protect-main","enforcement":"active","rules":[{"type":"required_status_checks","parameters":{"required_status_checks":[{"context":"vendor","integration_id":77}]}}]}`))
				}
			}))
			defer srv.Close()
			opts := &github.BranchProtectionOptions{ResolvedChecks: []string{pii.CheckName}, ShippedChecks: []string{pii.CheckName}}
			item := branchProtectionItem(github.NewClient("token", false).WithBaseURL(srv.URL), "o", "r", "main", "public",
				github.RulesetPlan{Exists: true, Drift: []string{"checks"}}, false, false, opts, "go", false, io.Discard)
			if deferred {
				deferRequiredChecks(opts)
			}
			// Closures must use preflight intent even if global flags subsequently change.
			globals.RequireChecks = []string{"unrelated"}
			t.Cleanup(func() { globals.RequireChecks = nil })
			if err := item.Apply(); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), `"context":"pii-gate"`) == deferred ||
				!strings.Contains(string(encoded), `"context":"vendor","integration_id":77`) ||
				strings.Contains(string(encoded), "unrelated") {
				t.Fatal("request lost preflight intent, custom context, or pin")
			}
			if !deferred && !strings.Contains(string(encoded), `"integration_id":15368`) {
				t.Fatal("eligible gate must be pinned to Actions")
			}
		})
	}
}
