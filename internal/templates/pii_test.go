package templates

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/internal/pii"
	"gopkg.in/yaml.v3"
)

func TestPIITemplates(t *testing.T) {
	for _, visibility := range []string{"public", "private"} {
		for _, pack := range []string{"go", "node", "python", "rust", "generic", "none"} {
			t.Run(visibility+"/"+pack, func(t *testing.T) {
				files, err := Render(RepoData{Owner: "o", RepoName: "r", DefaultBranch: "release/test+branch", Visibility: visibility, CIPack: pack})
				if err != nil {
					t.Fatal(err)
				}
				helper := contentFor(files, pii.HelperPath)
				if helper != pii.Standalone() {
					t.Fatal("generated source must preserve exact canonical bytes")
				}
				if _, err := parser.ParseFile(token.NewFileSet(), "helper.go", helper, parser.AllErrors); err != nil {
					t.Fatal("invalid standalone source", err)
				}
				workflow := contentFor(files, pii.WorkflowPath)
				var parsed map[string]any
				if err := yaml.Unmarshal([]byte(workflow), &parsed); err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{"release/test+branch", "name: pii-gate", "contents: read", "github.event.pull_request.head.sha", "git ls-tree -r -z", "git cat-file --batch", "GOTOOLCHAIN: local"} {
					if !strings.Contains(workflow, want) {
						t.Errorf("workflow missing %q", want)
					}
				}
				for _, forbidden := range []string{"uses:", "secrets.", "pull_request_target", "workflow_run", "paths:", "write", "id-token", "export-ignore", "git archive"} {
					if strings.Contains(workflow, forbidden) {
						t.Errorf("workflow contains %q", forbidden)
					}
				}
				if containsPath(files, pii.AllowlistPath) {
					t.Fatal("operator-owned allowlist must never be a managed template")
				}
			})
		}
	}
}
