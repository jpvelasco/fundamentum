package mcp

import (
	"encoding/json"
	"testing"

	"github.com/jpvelasco/fundamentum/cmd/globals"
)

func TestResult_NilIDReturnsNil(t *testing.T) {
	s := &server{}
	if got := s.result(nil, map[string]any{"ok": true}); got != nil {
		t.Errorf("result(nil id) = %+v, want nil", got)
	}
}

func TestFail_NilIDBecomesNull(t *testing.T) {
	s := &server{}
	resp := s.fail(nil, codeInternal, "boom")
	if resp == nil {
		t.Fatal("fail(nil id) = nil, want response")
	}
	if string(resp.ID) != "null" {
		t.Errorf("fail id = %s, want null", resp.ID)
	}
	if resp.Error == nil || resp.Error.Code != codeInternal {
		t.Errorf("fail error = %+v", resp.Error)
	}
}

func TestWriteMessage_MarshalError(t *testing.T) {
	// A value that cannot marshal (a channel) must surface the error.
	if err := writeMessage(&discardWriter{}, make(chan int)); err == nil {
		t.Fatal("writeMessage with unmarshalable value must error")
	}
}

func TestCall_NilArguments(t *testing.T) {
	// tools/call with a valid name but no arguments body: run() must be
	// invoked with empty args and surface its error as isError content.
	resetGlobals(t)
	globals.Token = ""
	t.Setenv("GITHUB_TOKEN", "")
	s := &server{tools: &toolSet{}}
	resp := s.call(&jsonRPCRequest{
		ID:     json.RawMessage("1"),
		Method: "tools/call",
		Params: json.RawMessage(`{"name":"audit"}`),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("call = %+v, want isError content", resp)
	}
	var result struct {
		IsError bool `json:"isError"`
	}
	b, _ := json.Marshal(resp.Result)
	_ = json.Unmarshal(b, &result)
	if !result.IsError {
		t.Errorf("call result = %+v, want isError (missing token)", result)
	}
}

type discardWriter struct{}

func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }
