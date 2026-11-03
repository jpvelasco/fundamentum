// Package mcp implements the "fundamentum mcp" command: a stdio MCP server
// exposing plan, audit, and apply tools for agent-driven repo hardening.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// Protocol revision this server speaks. MCP servers respond to initialize
// with the client's requested revision when they support it.
const protocolVersion = "2024-11-05"

// jsonRPCRequest is a JSON-RPC 2.0 request or notification.
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response. Result and Error are mutually
// exclusive; ID mirrors the request (null for parse errors).
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

// server dispatches MCP requests to the tool handlers.
type server struct {
	tools *toolSet
}

// serve reads newline-delimited JSON-RPC requests from r and writes responses
// to w until EOF or a read error. Notifications (no id) are answered with
// nothing, per the MCP spec.
func (s *server) serve(r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	// Tool results can carry a full audit report; allow generous lines.
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			if err := writeMessage(w, jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      json.RawMessage("null"),
				Error:   &jsonRPCError{Code: codeParseError, Message: "parse error"},
			}); err != nil {
				return err
			}
			continue
		}
		resp := s.dispatch(&req)
		if resp == nil { // notification
			continue
		}
		if err := writeMessage(w, resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

// dispatch routes one request; nil return means "notification, no response".
func (s *server) dispatch(req *jsonRPCRequest) *jsonRPCResponse {
	switch req.Method {
	case "notifications/initialized", "notifications/cancelled", "notifications/progress", "ping":
		if req.Method == "ping" {
			return s.result(req.ID, map[string]any{})
		}
		return nil
	case "initialize":
		return s.result(req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "fundamentum",
				"version": serverVersion,
			},
		})
	case "tools/list":
		return s.result(req.ID, map[string]any{"tools": s.tools.list()})
	case "tools/call":
		return s.call(req)
	default:
		// An unknown notification is dropped; an unknown request errors.
		if len(req.ID) == 0 {
			return nil
		}
		return s.fail(req.ID, codeMethodNotFound, "method not found: "+req.Method)
	}
}

// call runs a tools/call request and wraps the outcome as an MCP tool result.
func (s *server) call(req *jsonRPCRequest) *jsonRPCResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		return s.fail(req.ID, codeInvalidParams, "invalid tools/call params")
	}
	out, err := s.tools.run(params.Name, params.Arguments)
	if err != nil {
		// Tool failures are reported as isError content, not protocol errors,
		// so the agent sees the message and can act on it.
		return s.result(req.ID, map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
		})
	}
	return s.result(req.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": out}},
	})
}

func (s *server) result(id json.RawMessage, v any) *jsonRPCResponse {
	if len(id) == 0 {
		return nil
	}
	return &jsonRPCResponse{JSONRPC: "2.0", ID: id, Result: v}
}

func (s *server) fail(id json.RawMessage, code int, msg string) *jsonRPCResponse {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &jsonRPCResponse{JSONRPC: "2.0", ID: id, Error: &jsonRPCError{Code: code, Message: msg}}
}

// writeMessage writes one JSON-RPC message followed by a newline.
func writeMessage(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
