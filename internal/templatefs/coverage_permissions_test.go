package templatefs

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCoverageUploadTrustBoundaries(t *testing.T) {
	live, err := os.ReadFile(liveCIWorkflow)
	if err != nil {
		t.Fatal(err)
	}
	template, err := fs.ReadFile(FS, "dotgithub/workflows/public_ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{"live": live, "template": template} {
		t.Run(name, func(t *testing.T) {
			var workflow struct {
				Permissions map[string]string
				Jobs        map[string]struct {
					If          string
					Environment string
					Permissions map[string]string
					Steps       []struct {
						Uses string
						If   string
						With map[string]any
						Env  map[string]string
					}
				}
			}
			if err := yaml.Unmarshal(content, &workflow); err != nil {
				t.Fatal(err)
			}
			for scope, grant := range workflow.Permissions {
				if grant == "write" {
					t.Errorf("workflow grants %s: write to untrusted jobs", scope)
				}
			}
			testJob := workflow.Jobs["test"]
			if testJob.Environment != "" {
				t.Error("test job must not access environment secrets")
			}
			for scope, grant := range testJob.Permissions {
				if grant == "write" {
					t.Errorf("test job grants %s: write", scope)
				}
			}
			uploads := 0
			for _, step := range testJob.Steps {
				if !strings.HasPrefix(step.Uses, "codecov/codecov-action@") {
					continue
				}
				uploads++
				if step.If != "github.event_name == 'pull_request' && github.event.repository.private == false" {
					t.Errorf("tokenless upload must be restricted to public PRs: %s", step.If)
				}
				if len(step.Env) != 0 || step.With["token"] != nil || step.With["use_oidc"] != false {
					t.Error("PR upload must use neither secrets nor OIDC")
				}
				for key, want := range map[string]string{
					"files":           "./coverage.out",
					"override_commit": "${{ github.event.pull_request.head.sha }}",
					"override_pr":     "${{ github.event.number }}",
					"override_branch": "pr${{ github.event.number }}:${{ github.head_ref }}",
					"slug":            "${{ github.repository }}",
				} {
					if step.With[key] != want {
						t.Errorf("PR upload %s = %v, want %s", key, step.With[key], want)
					}
				}
			}
			if uploads != 1 {
				t.Errorf("expected one tokenless PR coverage upload, got %d", uploads)
			}
			for _, job := range []string{"coverage-upload", "codacy-upload"} {
				if workflow.Jobs[job].If != "github.event_name == 'push'" {
					t.Errorf("privileged %s job must only run on a default-branch push", job)
				}
			}
			if workflow.Jobs["coverage-upload"].Permissions["id-token"] != "write" {
				t.Error("trusted Codecov upload must use OIDC")
			}
			if workflow.Jobs["codacy-upload"].Environment != "codacy" {
				t.Error("Codacy secret must be scoped to its environment")
			}
		})
	}
}
