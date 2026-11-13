package audit

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/pii"
	"github.com/jpvelasco/fundamentum/internal/templates"
)

// serveReviewedPIITree models an already-landed policy and both gate assets.
func serveReviewedPIITree(t *testing.T, w http.ResponseWriter, r *http.Request, policy string) bool {
	t.Helper()
	if !strings.Contains(r.URL.Path, "/commits/") && !strings.Contains(r.URL.Path, "/git/") && !strings.HasSuffix(r.URL.Path, "/contents/"+pii.AllowlistPath) {
		return false
	}
	files, err := templates.Render(templates.RepoData{Owner: "owner", RepoName: "repo", DefaultBranch: "main", Visibility: "public", CIPack: "go"})
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]string{pii.AllowlistPath: policy}
	for _, file := range files {
		if file.Path == pii.WorkflowPath || file.Path == pii.HelperPath {
			body[file.Path] = file.Content
		}
	}
	var result any
	switch {
	case strings.Contains(r.URL.Path, "/commits/"):
		result = map[string]any{"sha": "head", "commit": map[string]any{"tree": map[string]any{"sha": "root"}}}
	case strings.Contains(r.URL.Path, "/trees/"):
		var entries []any
		for path := range body {
			entries = append(entries, map[string]any{"path": path, "sha": path, "type": "blob"})
		}
		result = map[string]any{"sha": "root", "tree": entries}
	case strings.Contains(r.URL.Path, "/blobs/"):
		path := strings.SplitN(r.URL.Path, "/blobs/", 2)[1]
		result = map[string]any{"sha": path, "size": len(body[path]), "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(body[path]))}
	default:
		result = map[string]any{}
	}
	_ = json.NewEncoder(w).Encode(result)
	return true
}

func TestPIIAudit(t *testing.T) {
	for _, tt := range []struct {
		name, policy, file, mode, want string
		reviewed, strict, fail         bool
		skip                           []string
	}{
		{name: "pending", want: "pending review"},
		{name: "strict pending", strict: true, fail: true, want: "pending review"},
		{name: "eligible missing context", reviewed: true, fail: true, want: "weaker than baseline"},
		{name: "reviewed required", reviewed: true, mode: "required", want: "pinned default tree scans clean"},
		{name: "invalid policy", reviewed: true, policy: "*", fail: true, want: ""},
		{name: "opt out", skip: []string{pii.WorkflowPath}, want: "SKIP"},
		{name: "workflow missing", file: pii.WorkflowPath, mode: "missing", fail: true, want: "missing"},
		{name: "helper drift", file: pii.HelperPath, mode: "drift", want: "content differs"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetAuditGlobals(t)
			globals.SkipSteps, globals.Strict = tt.skip, tt.strict
			report, err := runAuditWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
				if tt.reviewed && serveReviewedPIITree(t, w, r, tt.policy) {
					return
				}
				if tt.file != "" && strings.HasSuffix(r.URL.Path, "/contents/"+tt.file) {
					if tt.mode == "missing" {
						http.NotFound(w, r)
					} else {
						_, _ = w.Write([]byte(`{"content":"b2xk","sha":"x"}`))
					}
					return
				}
				if tt.mode == "required" && strings.HasSuffix(r.URL.Path, "/rulesets/1") {
					recorder := httptest.NewRecorder()
					writePassingAudit(t, recorder, r, true, true)
					var ruleset map[string]any
					if err := json.Unmarshal(recorder.Body.Bytes(), &ruleset); err != nil {
						t.Fatal(err)
					}
					for _, raw := range ruleset["rules"].([]any) {
						rule := raw.(map[string]any)
						if rule["type"] == "required_status_checks" {
							params := rule["parameters"].(map[string]any)
							params["required_status_checks"] = append(params["required_status_checks"].([]any), map[string]any{"context": pii.CheckName, "integration_id": 15368})
						}
					}
					_ = json.NewEncoder(w).Encode(ruleset)
					return
				}
				writePassingAudit(t, w, r, true, true)
			})
			if (err != nil) != tt.fail || !strings.Contains(report, tt.want) {
				t.Fatalf("unexpected audit outcome: error=%v, expected failure=%t", err, tt.fail)
			}
		})
	}
}

func TestPIIAuditExplicitEmptyOverride(t *testing.T) {
	resetAuditGlobals(t)
	t.Cleanup(func() { globals.RequireChecks = nil })
	globals.RequireChecks = []string{}
	globals.Strict = true
	globals.BotManaged = true
	report, err := runAuditWithHandler(t, botManagedAuditHandler(t, false))
	if err != nil {
		t.Fatal("explicit no-check override must not warn about missing enrollment", err)
	}
	if !strings.Contains(report, "new enrollment omitted by --require-checks") {
		t.Fatal("audit must explain the explicit override")
	}
}
