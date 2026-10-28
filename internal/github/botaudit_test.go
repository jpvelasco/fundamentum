package github

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestAnalyzeWorkflow_PrivilegedTriggers(t *testing.T) {
	for _, trigger := range []string{"workflow_run:", "pull_request_target:"} {
		findings := AnalyzeWorkflow(".github/workflows/x.yml", "on:\n  "+trigger+"\n", nil)
		if len(findings) == 0 {
			t.Errorf("trigger %s must be flagged", trigger)
		}
	}
	// A plain pull_request / push workflow is clean.
	if got := AnalyzeWorkflow(".github/workflows/x.yml", "on:\n  push:\n  pull_request:\n", nil); len(got) != 0 {
		t.Errorf("clean workflow flagged: %v", got)
	}
}

func TestAnalyzeWorkflow_LocalAction(t *testing.T) {
	if got := AnalyzeWorkflow(".github/workflows/x.yml", "jobs:\n  test:\n    steps:\n      - uses: ./local-action\n", nil); len(got) == 0 {
		t.Error("local ./ action must be flagged")
	}
	// A commented-out local action is not a finding.
	if got := AnalyzeWorkflow(".github/workflows/x.yml", "jobs:\n  test:\n    steps:\n      # - uses: ./local-action\n", nil); len(got) != 0 {
		t.Errorf("commented local action must not be flagged: %v", got)
	}
	// A pinned third-party action is clean.
	if got := AnalyzeWorkflow(".github/workflows/x.yml", "jobs:\n  test:\n    steps:\n      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1\n", nil); len(got) != 0 {
		t.Errorf("pinned action must not be flagged: %v", got)
	}
}

func TestAnalyzeWorkflow_YAMLSemantics(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "trailing comment",
			body: "jobs:\n  upload:\n    permissions:\n      id-token: write # upload\n",
			want: []string{`job "upload": grants a write scope (id-token: write)`},
		},
		{
			name: "quoted flow permissions",
			body: `jobs: {review: {permissions: {"pull-requests": "write"}}}`,
			want: []string{`job "review": grants a write scope (pull-requests: write)`},
		},
		{
			name: "job write-all",
			body: `jobs: {review: {permissions: write-all}}`,
			want: []string{`job "review": grants a write scope (write-all)`},
		},
		{
			name: "workflow write-all inherited",
			body: "permissions: write-all\njobs: {test: {}, upload: {environment: codacy}}",
			want: []string{`job "test": grants a write scope (write-all)`},
		},
		{
			name: "workflow map inherited",
			body: "permissions: {contents: write}\njobs: {test: {}, upload: {environment: codacy}}",
			want: []string{`job "test": grants a write scope (contents: write)`},
		},
		{
			name: "read override replaces workflow grants",
			body: "permissions: write-all\njobs: {test: {permissions: {contents: read}}, second: {permissions: read-all}, third: {permissions: {}}}",
		},
		{
			name: "job write overrides workflow read",
			body: "permissions: read-all\njobs: {test: {permissions: {contents: write}}}",
			want: []string{`job "test": grants a write scope (contents: write)`},
		},
		{
			name: "restricted sibling does not shield write job",
			body: "jobs:\n  safe:\n    environment: codacy\n  unsafe:\n    permissions: {contents: write}\n",
			want: []string{`job "unsafe": grants a write scope (contents: write)`},
		},
		{
			name: "unrestricted read sibling does not taint safe write job",
			body: "jobs:\n  safe:\n    environment: {name: codacy, url: 'https://example.com'}\n    permissions: write-all\n  test:\n    permissions: read-all\n",
		},
		{
			name: "unknown and dynamic environments",
			body: "permissions: write-all\njobs:\n  first:\n    environment: {name: open}\n  second:\n    environment: '${{ matrix.environment }}'\n",
			want: []string{`job "first": grants a write scope`, `job "second": grants a write scope`},
		},
		{
			name: "security events exception does not hide other grants",
			body: `jobs: {analyze: {permissions: {security-events: write, contents: write, issues: write}}}`,
			want: []string{`job "analyze": grants a write scope (contents: write, issues: write)`},
		},
		{
			name: "scalar trigger",
			body: `on: pull_request_target`,
			want: []string{"uses privileged pull_request_target trigger"},
		},
		{
			name: "sequence triggers",
			body: `on: [push, workflow_run, pull_request_target]`,
			want: []string{"uses privileged workflow_run trigger", "uses privileged pull_request_target trigger"},
		},
		{
			name: "quoted mapping trigger",
			body: `"on": {"workflow_run": {workflows: [CI], types: [completed]}}`,
			want: []string{"uses privileged workflow_run trigger"},
		},
		{
			name: "comments and script content are not workflow declarations",
			body: "on: push\n# workflow_run:\n# pull_request_target:\njobs:\n  test:\n    steps:\n      - run: |\n          echo 'workflow_run: and pull_request_target:'\n          echo 'uses: ./script'\n          echo 'contents: write'\n",
		},
		{
			name: "quoted local action",
			body: `jobs: {test: {steps: [{uses: "./action"}]}}`,
			want: []string{`job "test": uses a local (./) action`},
		},
		{
			name: "local reusable workflow",
			body: `jobs: {test: {uses: "./.github/workflows/reusable.yml"}}`,
			want: []string{`job "test": uses a local (./) action`},
		},
		{
			name: "aliases and merge keys",
			body: "permissions: &grants {contents: write}\njobs:\n  first: &job\n    permissions: *grants\n  second:\n    <<: *job\n    environment: codacy\n",
			want: []string{`job "first": grants a write scope (contents: write)`},
		},
		{
			name: "invalid YAML is reported",
			body: "jobs: [",
			want: []string{"could not parse workflow YAML"},
		},
		{
			name: "duplicate YAML keys are reported",
			body: "permissions: read-all\npermissions: write-all\n",
			want: []string{"could not parse workflow YAML"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const path = ".github/workflows/x.yml"
			got := AnalyzeWorkflow(path, tt.body, map[string]bool{"codacy": true})
			if len(got) != len(tt.want) {
				t.Fatalf("findings = %v, want %v", got, tt.want)
			}
			for i, want := range tt.want {
				if got[i].Path != path || !strings.Contains(got[i].Detail, want) {
					t.Errorf("finding = %+v, want path %s and detail containing %q", got[i], path, want)
				}
			}
		})
	}
}

func TestAnalyzeWorkflow_ShippedWrites(t *testing.T) {
	for _, tt := range []struct{ file, job, scope string }{
		{"public_ci.yml", "coverage-upload", "id-token: write"},
		{"public_opt_octopus.yml", "review", "pull-requests: write"},
	} {
		t.Run(tt.file, func(t *testing.T) {
			content, err := os.ReadFile("../templatefs/templates/dotgithub/workflows/" + tt.file)
			if err != nil {
				t.Fatal(err)
			}
			got := AnalyzeWorkflow(tt.file, string(content), map[string]bool{"codacy": true})
			if len(got) != 1 || !strings.Contains(got[0].Detail, `job "`+tt.job+`"`) || !strings.Contains(got[0].Detail, tt.scope) {
				t.Fatalf("findings = %v, want write scope %s on job %s", got, tt.scope, tt.job)
			}
		})
	}
}

func TestAnalyzeWorkflow_WriteScopes(t *testing.T) {
	wf := "jobs:\n  upload:\n    permissions:\n      contents: read\n      id-token: write\n"
	// No environment → write scope is reachable by any branch → flagged.
	if got := AnalyzeWorkflow(".github/workflows/x.yml", wf, nil); len(got) == 0 {
		t.Error("write scope outside a default-branch-only environment must be flagged")
	}
	// security-events: write (CodeQL) is the accepted write scope → not flagged.
	codeql := "jobs:\n  analyze:\n    permissions:\n      contents: read\n      security-events: write\n"
	if got := AnalyzeWorkflow(".github/workflows/codeql.yml", codeql, nil); len(got) != 0 {
		t.Errorf("security-events: write (CodeQL) must not be flagged: %v", got)
	}
	// A write scope inside a default-branch-only environment is accepted.
	wfEnv := "jobs:\n  upload:\n    environment: codacy\n    permissions:\n      id-token: write\n"
	if got := AnalyzeWorkflow(".github/workflows/x.yml", wfEnv, map[string]bool{"codacy": true}); len(got) != 0 {
		t.Errorf("write scope in a default-branch-only env must be accepted: %v", got)
	}
	// Same workflow but the env is NOT default-branch-only → flagged.
	if got := AnalyzeWorkflow(".github/workflows/x.yml", wfEnv, map[string]bool{"codacy": false}); len(got) == 0 {
		t.Error("write scope in a non-default-branch env must be flagged")
	}
}

func TestRepoSecretCount(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 3, "secrets": []any{}})
	}), nil, func(c *Client) {
		n, err := c.RepoSecretCount("owner", "repo")
		if err != nil || n != 3 {
			t.Fatalf("RepoSecretCount() = (%d, %v), want (3, nil)", n, err)
		}
	}, nil)
}

func TestEnvironmentBranchPolicies(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/environments":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"environments":[{"name":"codacy"},{"name":"open"}]}`))
		case "/repos/owner/repo/environments/codacy":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"protection_rules":[{"type":"branch_policy","protected_branches":false,"custom_branch_policies":true,"branch_name_pattern_policy":[{"name":"main"}]}]}`))
		case "/repos/owner/repo/environments/open":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"protection_rules":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}), nil, func(c *Client) {
		pols, err := c.EnvironmentBranchPolicies("owner", "repo", "main")
		if err != nil {
			t.Fatalf("EnvironmentBranchPolicies() error: %v", err)
		}
		if len(pols) != 2 {
			t.Fatalf("got %d policies, want 2", len(pols))
		}
		byName := map[string]EnvironmentBranchPolicy{}
		for _, p := range pols {
			byName[p.Name] = p
		}
		if !byName["codacy"].DefaultBranchOnly {
			t.Errorf("codacy must be default-branch-only: %+v", byName["codacy"])
		}
		if byName["open"].Restricted {
			t.Errorf("open env must be unrestricted: %+v", byName["open"])
		}
	}, nil)
}

func TestGetActionsWorkflowPermissions(t *testing.T) {
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"default_workflow_permissions":"read","can_approve_pull_request_reviews":false}`))
	}), nil, func(c *Client) {
		perms, err := c.GetActionsWorkflowPermissions("owner", "repo")
		if err != nil {
			t.Fatalf("GetActionsWorkflowPermissions() error: %v", err)
		}
		if perms.DefaultWorkflowPermissions != "read" || perms.CanApprovePullRequestReviews {
			t.Errorf("perms = %+v", perms)
		}
	}, nil)
}

func TestSetActionsWorkflowPermissions(t *testing.T) {
	var body map[string]any
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}), nil, func(c *Client) {
		if err := c.SetActionsWorkflowPermissions("owner", "repo"); err != nil {
			t.Fatalf("SetActionsWorkflowPermissions() error: %v", err)
		}
	}, func(t *testing.T) {
		if body["default_workflow_permissions"] != "read" || body["can_approve_pull_request_reviews"] != false {
			t.Errorf("PUT body = %v", body)
		}
	})
}

func TestIsBotOrAppToken(t *testing.T) {
	// Installation token (ghs_) short-circuits without an API call.
	c := NewClient("ghs_abc123", false)
	isBot, why, err := c.IsBotOrAppToken()
	if err != nil || !isBot {
		t.Fatalf("installation token = (%v, %v), want (true, nil)", isBot, err)
	}
	if why == "" {
		t.Error("expected a reason for the installation token")
	}

	// A bot account (GET /user type Bot).
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"login":"my-bot[bot]","type":"Bot"}`))
	}), nil, func(c *Client) {
		isBot, _, err := c.IsBotOrAppToken()
		if err != nil || !isBot {
			t.Fatalf("bot account = (%v, %v), want (true, nil)", isBot, err)
		}
	}, nil)

	// A human user.
	testWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"login":"octocat","type":"User"}`))
	}), nil, func(c *Client) {
		isBot, _, err := c.IsBotOrAppToken()
		if err != nil || isBot {
			t.Fatalf("human user = (%v, %v), want (false, nil)", isBot, err)
		}
	}, nil)
}
