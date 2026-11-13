package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
)

func TestMCPAppAuditCredentialsStayServerSide(t *testing.T) {
	for _, tool := range []string{"plan", "apply", "audit"} {
		t.Run(tool, func(t *testing.T) {
			resetGlobals(t)
			t.Cleanup(func() { globals.AppAuditPolicy = nil; globals.AppTokenEnvs = nil })
			globals.AppAuditPolicy = &globals.AppAuditConfig{ExpectedApps: []globals.ExpectedApp{{AppID: 123, Permissions: map[string]string{}}}}
			globals.AppTokenEnvs = []string{"123=PRIVATE_APP_TOKEN"}
			t.Setenv("PRIVATE_APP_TOKEN", "app-token-secret-sentinel")
			srv := mockGitHub(t)
			defer srv.Close()
			base := srv.Config.Handler
			appCalls := 0
			srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/installation/repositories" {
					appCalls++
					if r.Header.Get("Authorization") != "Bearer app-token-secret-sentinel" {
						t.Error("app credential not used for app probe")
					}
					w.WriteHeader(http.StatusForbidden)
					_, _ = fmt.Fprint(w, "app-token-secret-sentinel")
					return
				}
				base.ServeHTTP(w, r)
			})
			newClient = func(token string, verbose bool) *github.Client {
				return github.NewClient(token, verbose).WithBaseURL(srv.URL)
			}
			s := &server{tools: &toolSet{}}
			request := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":{"repo":"owner/repo"}}}`, tool)
			responses := serveRoundTrip(t, s, request)
			raw, err := json.Marshal(responses)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"app-token-secret-sentinel", "PRIVATE_APP_TOKEN"} {
				if strings.Contains(string(raw), secret) {
					t.Fatal("MCP output exposed app credentials")
				}
			}
			if tool == "audit" {
				if appCalls != 1 || !strings.Contains(string(raw), "app installations") || !strings.Contains(string(raw), `"isError":true`) {
					t.Fatalf("audit report lost: %s", raw)
				}
			} else if appCalls != 0 || strings.Contains(string(raw), "app installations") {
				t.Fatalf("%s consumed app audit credentials", tool)
			}
		})
	}
}
