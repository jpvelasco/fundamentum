package github

import (
	"encoding/json"
	"net/http"
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
	if got := AnalyzeWorkflow(".github/workflows/x.yml", "steps:\n  - uses: ./local-action\n", nil); len(got) == 0 {
		t.Error("local ./ action must be flagged")
	}
	// A commented-out local action is not a finding.
	if got := AnalyzeWorkflow(".github/workflows/x.yml", "steps:\n  # - uses: ./local-action\n", nil); len(got) != 0 {
		t.Errorf("commented local action must not be flagged: %v", got)
	}
	// A pinned third-party action is clean.
	if got := AnalyzeWorkflow(".github/workflows/x.yml", "steps:\n  - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1\n", nil); len(got) != 0 {
		t.Errorf("pinned action must not be flagged: %v", got)
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
