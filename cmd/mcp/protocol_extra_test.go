package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
)

// Unknown notifications (no id) must be dropped with no response; unknown
// requests must error.
func TestServe_DropsUnknownNotification(t *testing.T) {
	s := &server{tools: &toolSet{}}
	resps := serveRoundTrip(t, s, `{"jsonrpc":"2.0","method":"no/such"}`)
	if len(resps) != 0 {
		t.Fatalf("unknown notification answered: %v", resps)
	}
}

// call with malformed params returns an invalid-params error.
func TestServe_ToolsCallBadParams(t *testing.T) {
	s := &server{tools: &toolSet{}}
	resps := serveRoundTrip(t, s,
		`{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":123}}`,
	)
	if len(resps) != 1 || resps[0].Error == nil || resps[0].Error.Code != codeInvalidParams {
		t.Fatalf("bad params = %+v, want invalid-params", resps)
	}
}

// A tool whose run() fails surfaces isError content (not a protocol error).
func TestServe_ToolsCallToolError(t *testing.T) {
	resetGlobals(t)
	globals.Token = ""
	t.Setenv("GITHUB_TOKEN", "")
	s := &server{tools: &toolSet{}}
	resps := serveRoundTrip(t, s,
		`{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"audit","arguments":{"repo":"owner/repo"}}}`,
	)
	if len(resps) != 1 || resps[0].Error != nil {
		t.Fatalf("tool error must be isError content: %+v", resps)
	}
	var result struct {
		IsError bool `json:"isError"`
	}
	b, _ := json.Marshal(resps[0].Result)
	_ = json.Unmarshal(b, &result)
	if !result.IsError {
		t.Errorf("missing token tool result = %+v, want isError", result)
	}
}

// serve must surface a writer error.
func TestServe_WriteError(t *testing.T) {
	s := &server{tools: &toolSet{}}
	err := s.serve(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`+"\n"), &failWriter{})
	if err == nil {
		t.Fatal("serve with a failing writer must error")
	}
}

// serve must surface a scanner error on an over-long line.
func TestServe_ScannerError(t *testing.T) {
	s := &server{tools: &toolSet{}}
	long := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"pad":"` + strings.Repeat("x", 2<<20) + `"}}` + "\n"
	// The buffer cap is 1<<20, so a 2<<20 padding forces a scanner error.
	if err := s.serve(strings.NewReader(long), &strings.Builder{}); err == nil {
		t.Fatal("serve with an over-long line must error")
	}
}

type failWriter struct{}

func (w *failWriter) Write(p []byte) (int, error) { return 0, errWrite }

var errWrite = &writeError{}

type writeError struct{}

func (e *writeError) Error() string { return "write failed" }
