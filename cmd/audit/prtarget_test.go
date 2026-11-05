package audit

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/internal/github"
)

const prtWorkflowPath = ".github/workflows/w.yml"

// pullRequestTargetChecksFor runs the default-path check against one workflow
// body served at prtWorkflowPath. A non-200 listStatus fails the listing.
func pullRequestTargetChecksFor(t *testing.T, listStatus int, body string) []check {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/contents/.github/workflows":
			if listStatus != http.StatusOK {
				w.WriteHeader(listStatus)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]string{{"type": "file", "path": prtWorkflowPath}})
		case strings.HasSuffix(r.URL.Path, "/contents/"+prtWorkflowPath):
			_ = json.NewEncoder(w).Encode(map[string]string{"content": base64.StdEncoding.EncodeToString([]byte(body))})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	client := github.NewClient("t", false).WithBaseURL(srv.URL)
	return pullRequestTargetChecks(client, "owner", "repo")
}

func TestPullRequestTargetChecks(t *testing.T) {
	const pwn = "on: pull_request_target\njobs:\n  build:\n    env:\n      NPM_TOKEN: ${{ secrets.NPM_TOKEN }}\n" +
		"    steps:\n      - uses: actions/checkout@v4\n        with:\n" +
		"          ref: ${{ github.event.pull_request.head.sha }}\n"
	const octopus = "on: pull_request_target\njobs:\n  review:\n    permissions:\n      pull-requests: write\n" +
		"    steps:\n      - uses: octopusdeploy/review@v1\n"
	const labeler = "on: pull_request_target\njobs:\n  label:\n    steps:\n      - uses: actions/labeler@v5\n"

	for _, tc := range []struct {
		name       string
		listStatus int
		body       string
		wantStatus status
		wantDetail string
		wantFail   bool
	}{
		{"benign labeler passes", http.StatusOK, labeler, statusPass, "", false},
		{"octopus shape warns once, naming both concerns", http.StatusOK, octopus, statusWarn, "pull-requests: write", false},
		{"pwn request fails", http.StatusOK, pwn, statusFail, "checks out PR head", true},
		{"unreadable listing warns, never fails the run", http.StatusForbidden, "", statusWarn, "could not scan", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks := pullRequestTargetChecksFor(t, tc.listStatus, tc.body)
			if len(checks) != 1 {
				t.Fatalf("checks = %+v, want one row", checks)
			}
			got := checks[0]
			if got.Status != tc.wantStatus {
				t.Errorf("status = %v, want %v (%+v)", got.Status, tc.wantStatus, got)
			}
			if tc.wantDetail != "" && !strings.Contains(got.Detail, tc.wantDetail) {
				t.Errorf("detail %q missing %q", got.Detail, tc.wantDetail)
			}
			if failedRequired(checks) != tc.wantFail {
				t.Errorf("failedRequired = %t, want %t", failedRequired(checks), tc.wantFail)
			}
		})
	}
}

func TestPullRequestTargetChecks_OctopusNamesThirdParty(t *testing.T) {
	const octopus = "on: pull_request_target\njobs:\n  review:\n    permissions:\n      pull-requests: write\n" +
		"    steps:\n      - uses: octopusdeploy/review@v1\n"
	checks := pullRequestTargetChecksFor(t, http.StatusOK, octopus)
	if !strings.Contains(checks[0].Detail, "third-party action octopusdeploy/review@v1") {
		t.Errorf("detail %q must name the third-party action", checks[0].Detail)
	}
}
