package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
)

func TestRun_ApplyToolMutates(t *testing.T) {
	resetGlobals(t)
	var wrote bool
	srv := mockGitHub(t)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut || r.Method == http.MethodPost || r.Method == http.MethodPatch {
			wrote = true
		}
		mockGitHub(t).Config.Handler.ServeHTTP(w, r)
	})
	defer srv.Close()
	newClient = func(token string, verbose bool) *github.Client {
		return github.NewClient(token, verbose).WithBaseURL(srv.URL)
	}
	// The mock repo has no protect-main, so apply fails after attempting
	// writes — the point is the mutating path runs (not DryRun-only).
	_, err := (&toolSet{}).run("apply", json.RawMessage(`{"repo":"owner/repo"}`))
	if err == nil {
		t.Log("apply succeeded against mock (unexpected but fine)")
	}
	if !wrote {
		t.Errorf("apply never issued a write; output path not exercised")
	}
}

func TestRun_ApplyMissingToken(t *testing.T) {
	resetGlobals(t)
	globals.Token = ""
	t.Setenv("GITHUB_TOKEN", "")
	if _, err := (&toolSet{}).run("apply", json.RawMessage(`{"repo":"owner/repo"}`)); err == nil {
		t.Fatal("apply without a token must error")
	}
}

func TestServe_ToolsCallDispatch(t *testing.T) {
	resetGlobals(t)
	srv := mockGitHub(t)
	defer srv.Close()
	newClient = func(token string, verbose bool) *github.Client {
		return github.NewClient(token, verbose).WithBaseURL(srv.URL)
	}
	s := &server{tools: &toolSet{}}
	resps := serveRoundTrip(t, s,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"audit","arguments":{"repo":"owner/repo"}}}`,
	)
	if len(resps) != 1 {
		t.Fatalf("responses = %d, want 1", len(resps))
	}
	// The mock repo has no protect-main, so audit FAILs — the tool must
	// surface that as isError content, never as a JSON-RPC protocol error.
	if resps[0].Error != nil {
		t.Fatalf("tools/call returned protocol error: %+v", resps[0].Error)
	}
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	b, _ := json.Marshal(resps[0].Result)
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("result: %v", err)
	}
	if !result.IsError {
		t.Errorf("audit against unprotected repo must be isError; got: %v", result.Content)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "audit failed") {
		t.Errorf("tools/call audit content: %+v", result.Content)
	}
}

func TestServe_ToolsCallUnknownToolIsErrorContent(t *testing.T) {
	resetGlobals(t)
	s := &server{tools: &toolSet{}}
	resps := serveRoundTrip(t, s,
		`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"nope","arguments":{"repo":"owner/repo"}}}`,
	)
	if len(resps) != 1 || resps[0].Error != nil {
		t.Fatalf("unknown tool must be isError content, not protocol error: %+v", resps)
	}
	var result struct {
		IsError bool `json:"isError"`
	}
	b, _ := json.Marshal(resps[0].Result)
	_ = json.Unmarshal(b, &result)
	if !result.IsError {
		t.Errorf("unknown tool result = %+v, want isError", result)
	}
}
