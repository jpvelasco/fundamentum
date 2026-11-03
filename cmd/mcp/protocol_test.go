package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// serveRoundTrip feeds newline-delimited requests through serve and returns
// the decoded responses (one per non-notification request).
func serveRoundTrip(t *testing.T, s *server, lines ...string) []jsonRPCResponse {
	t.Helper()
	var out bytes.Buffer
	if err := s.serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatalf("serve() error: %v", err)
	}
	var resps []jsonRPCResponse
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l == "" {
			continue
		}
		var r jsonRPCResponse
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("bad response line %q: %v", l, err)
		}
		resps = append(resps, r)
	}
	return resps
}

func TestServe_Initialize(t *testing.T) {
	s := &server{tools: &toolSet{}}
	resps := serveRoundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	)
	if len(resps) != 1 {
		t.Fatalf("responses = %d, want 1 (notification must not answer)", len(resps))
	}
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	b, _ := json.Marshal(resps[0].Result)
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("result: %v", err)
	}
	if result.ProtocolVersion != protocolVersion {
		t.Errorf("protocolVersion = %q, want %q", result.ProtocolVersion, protocolVersion)
	}
	if result.ServerInfo.Name != "fundamentum" {
		t.Errorf("serverInfo.name = %q", result.ServerInfo.Name)
	}
}

func TestServe_ToolsList(t *testing.T) {
	s := &server{tools: &toolSet{}}
	resps := serveRoundTrip(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if len(resps) != 1 {
		t.Fatalf("responses = %d, want 1", len(resps))
	}
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	b, _ := json.Marshal(resps[0].Result)
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("result: %v", err)
	}
	var names []string
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	want := []string{"plan", "audit", "apply"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("tools = %v, want %v", names, want)
	}
}

func TestServe_UnknownMethodAndParseError(t *testing.T) {
	s := &server{tools: &toolSet{}}
	resps := serveRoundTrip(t, s,
		`{"jsonrpc":"2.0","id":3,"method":"no/such"}`,
		`not json`,
	)
	if len(resps) != 2 {
		t.Fatalf("responses = %d, want 2", len(resps))
	}
	if resps[0].Error == nil || resps[0].Error.Code != codeMethodNotFound {
		t.Errorf("unknown method = %+v, want method-not-found error", resps[0].Error)
	}
	if resps[1].Error == nil || resps[1].Error.Code != codeParseError {
		t.Errorf("bad json = %+v, want parse error", resps[1].Error)
	}
}

func TestServe_Ping(t *testing.T) {
	s := &server{tools: &toolSet{}}
	resps := serveRoundTrip(t, s, `{"jsonrpc":"2.0","id":4,"method":"ping"}`)
	if len(resps) != 1 || resps[0].Error != nil {
		t.Fatalf("ping = %+v, want one ok response", resps)
	}
}
