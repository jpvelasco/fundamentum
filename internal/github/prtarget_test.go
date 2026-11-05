package github

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAnalyzePullRequestTarget(t *testing.T) {
	const path = ".github/workflows/x.yml"
	for _, tc := range []struct {
		name     string
		content  string
		wantFail bool
		wantNone bool
		wantSubs []string
	}{
		{
			name: "benign labeler: no checkout of the PR head, no write scope, first-party action",
			content: "on: pull_request_target\njobs:\n  label:\n    steps:\n" +
				"      - uses: actions/labeler@v5\n",
			wantNone: true,
		},
		{
			name: "octopus shape: write scope plus third-party action is one WARN",
			content: "on: pull_request_target\njobs:\n  review:\n    permissions:\n      pull-requests: write\n" +
				"    steps:\n      - uses: octopusdeploy/review@v1\n",
			wantSubs: []string{"write scope (pull-requests: write)", "third-party action octopusdeploy/review@v1"},
		},
		{
			name: "pwn request: checkout of the PR head sha with a secret in env FAILs",
			content: "on: pull_request_target\njobs:\n  build:\n    env:\n      NPM_TOKEN: ${{ secrets.NPM_TOKEN }}\n" +
				"    steps:\n      - uses: actions/checkout@v4\n        with:\n" +
				"          ref: ${{ github.event.pull_request.head.sha }}\n",
			wantFail: true,
			wantSubs: []string{"checks out PR head", "secret secrets.NPM_TOKEN"},
		},
		{
			name: "checkout of the PR head via repository and head_ref FAILs",
			content: "on: pull_request_target\njobs:\n  build:\n    steps:\n" +
				"      - uses: actions/checkout@v4\n        with:\n" +
				"          repository: ${{ github.event.pull_request.head.repo.full_name }}\n" +
				"          ref: ${{ github.head_ref }}\n",
			wantFail: true,
			wantSubs: []string{"checks out PR head"},
		},
		{
			name: "checkout of the PR head plus third-party action keeps the FAIL and names both",
			content: "on: pull_request_target\njobs:\n  build:\n    steps:\n" +
				"      - uses: actions/checkout@v4\n        with:\n" +
				"          ref: ${{ github.event.pull_request.head.sha }}\n" +
				"      - uses: someone/tool@v1\n",
			wantFail: true,
			wantSubs: []string{"checks out PR head", "third-party action someone/tool@v1"},
		},
		{
			name: "workflow-level write-all is a write scope",
			content: "on: pull_request_target\npermissions: write-all\njobs:\n  x:\n    steps:\n" +
				"      - uses: actions/checkout@v4\n",
			wantSubs: []string{"write scope (write-all)"},
		},
		{
			name: "GITHUB_TOKEN is not a secret concern and a base-branch checkout is benign",
			content: "on: pull_request_target\njobs:\n  x:\n    steps:\n" +
				"      - uses: actions/checkout@v4\n        env:\n          T: ${{ secrets.GITHUB_TOKEN }}\n",
			wantNone: true,
		},
		{
			name: "pull_request trigger is out of scope even with a head checkout",
			content: "on: pull_request\njobs:\n  x:\n    steps:\n" +
				"      - uses: actions/checkout@v4\n        with:\n" +
				"          ref: ${{ github.event.pull_request.head.sha }}\n",
			wantNone: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := AnalyzePullRequestTarget(path, tc.content)
			if tc.wantNone {
				if len(got) != 0 {
					t.Fatalf("findings = %+v, want none", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("findings = %+v, want exactly one combined finding", got)
			}
			if got[0].Path != path || got[0].Fail != tc.wantFail {
				t.Errorf("finding = %+v, want path %s fail=%t", got[0], path, tc.wantFail)
			}
			for _, sub := range tc.wantSubs {
				if !strings.Contains(got[0].Detail, sub) {
					t.Errorf("detail %q missing %q", got[0].Detail, sub)
				}
			}
		})
	}
}

func TestScanPullRequestTargetWorkflows(t *testing.T) {
	const pwn = "on: pull_request_target\njobs:\n  build:\n    steps:\n" +
		"      - uses: actions/checkout@v4\n        with:\n" +
		"          ref: ${{ github.event.pull_request.head.sha }}\n"
	for _, tc := range []struct {
		name       string
		listStatus int
		badList    bool
		wantError  bool
		wantCount  int
		wantFail   bool
	}{
		{"one pwn workflow among benign ones", http.StatusOK, false, false, 1, true},
		{"listing forbidden", http.StatusForbidden, false, true, 0, false},
		{"invalid listing JSON", http.StatusOK, true, true, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/contents/.github/workflows" {
					w.WriteHeader(tc.listStatus)
					if tc.badList {
						_, _ = io.WriteString(w, "{")
						return
					}
					_ = json.NewEncoder(w).Encode([]map[string]string{
						{"type": "file", "path": ".github/workflows/pwn.yml"},
						{"type": "file", "path": ".github/workflows/label.yml"},
					})
					return
				}
				content := "on: pull_request\n"
				switch r.URL.Path {
				case "/repos/owner/repo/contents/.github/workflows/pwn.yml":
					content = pwn
				case "/repos/owner/repo/contents/.github/workflows/label.yml":
					content = "on: pull_request_target\njobs:\n  l:\n    steps:\n      - uses: actions/labeler@v5\n"
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"content": base64.StdEncoding.EncodeToString([]byte(content))})
			}))
			defer srv.Close()
			findings, err := client.ScanPullRequestTargetWorkflows("owner", "repo")
			if (err != nil) != tc.wantError {
				t.Fatalf("scan error = %v, wantError=%t", err, tc.wantError)
			}
			if len(findings) != tc.wantCount {
				t.Fatalf("findings = %+v, want %d", findings, tc.wantCount)
			}
			if tc.wantCount > 0 && findings[0].Fail != tc.wantFail {
				t.Errorf("finding = %+v, want fail=%t", findings[0], tc.wantFail)
			}
		})
	}
}
