// Package mcp implements a stateless MCP server (JSON-RPC 2.0 over Streamable
// HTTP, JSON responses only) exposing Genkan's organizing tools.
package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/jae-jae/genkan/internal/plugin"
	"github.com/jae-jae/genkan/internal/store"
)

const defaultProtocol = "2025-06-18"

var knownProtocols = map[string]bool{
	"2024-11-05": true,
	"2025-03-26": true,
	"2025-06-18": true,
	"2025-11-25": true,
}

const instructions = `Genkan is a self-hosted homelab navigation page. Use these tools to organize it: ` +
	`workspaces contain categories, categories contain links. Call get_overview first to see the current structure and ids, ` +
	`prefer create_links (batch) over many single calls, and only delete things the user asked you to delete. ` +
	`All changes take effect immediately and the UI updates live.`

// Server handles MCP requests against a Store.
type Server struct {
	Store   *store.Store
	Version string
	// OnLinksCreated, if set, is called after create_links with the new links
	// (used to fetch icons in the background).
	OnLinksCreated func([]store.Link)
	// Plugins backs the plugin tools (nil: those tools report "plugins unavailable").
	Plugins *plugin.Manager

	tools []tool
}

// New builds a Server.
func New(st *store.Store, version string) *Server {
	s := &Server{Store: st, Version: version}
	s.tools = s.buildTools()
	return s
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

const (
	errParse          = -32700
	errInvalidRequest = -32600
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errInternal       = -32603
)

func errResp(id json.RawMessage, code int, msg string) rpcResponse {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{code, msg}}
}

// Handle processes one HTTP POST body. It returns the response body (nil for
// notification-only input) and the HTTP status code.
func (s *Server) Handle(body []byte) ([]byte, int) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return mustJSON(errResp(nil, errParse, "empty body")), 400
	}
	if body[0] == '[' {
		var raws []json.RawMessage
		if err := json.Unmarshal(body, &raws); err != nil {
			return mustJSON(errResp(nil, errParse, "parse error")), 400
		}
		if len(raws) == 0 {
			return mustJSON(errResp(nil, errInvalidRequest, "empty batch")), 400
		}
		out := make([]rpcResponse, 0, len(raws))
		for _, raw := range raws {
			if r, ok := s.handleOne(raw); ok {
				out = append(out, r)
			}
		}
		if len(out) == 0 {
			return nil, 202
		}
		return mustJSON(out), 200
	}
	r, ok := s.handleOne(body)
	if !ok {
		return nil, 202
	}
	status := 200
	if r.Error != nil && (r.Error.Code == errParse || r.Error.Code == errInvalidRequest) {
		status = 400
	}
	return mustJSON(r), status
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`)
	}
	return b
}

// handleOne returns (response, true) for requests and (_, false) for notifications.
func (s *Server) handleOne(raw []byte) (resp rpcResponse, ok bool) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return errResp(nil, errParse, "parse error"), true
	}
	if req.Method == "" {
		// a JSON-RPC response from the client, or garbage
		var probe map[string]json.RawMessage
		if json.Unmarshal(raw, &probe) == nil {
			if _, has := probe["result"]; has {
				return resp, false
			}
			if _, has := probe["error"]; has {
				return resp, false
			}
		}
		return errResp(req.ID, errInvalidRequest, "missing method"), true
	}
	if len(req.ID) == 0 { // notification
		return resp, false
	}
	defer func() {
		if r := recover(); r != nil {
			resp, ok = errResp(req.ID, errInternal, fmt.Sprintf("internal error: %v", r)), true
		}
	}()
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		pv := defaultProtocol
		if knownProtocols[p.ProtocolVersion] {
			pv = p.ProtocolVersion
		}
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": pv,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "genkan", "title": "Genkan", "version": s.Version},
			"instructions":    instructions,
		}}, true
	case "ping":
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}, true
	case "tools/list":
		list := make([]map[string]any, len(s.tools))
		for i, t := range s.tools {
			list[i] = map[string]any{
				"name":        t.name,
				"title":       t.title,
				"description": t.desc,
				"inputSchema": t.schema,
				"annotations": map[string]any{
					"title":           t.title,
					"readOnlyHint":    t.readOnly,
					"destructiveHint": t.destructive,
					"idempotentHint":  t.idempotent,
					"openWorldHint":   false,
				},
			}
		}
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": list}}, true
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
			return errResp(req.ID, errInvalidParams, "invalid params: name is required"), true
		}
		for _, t := range s.tools {
			if t.name == p.Name {
				args := p.Arguments
				if len(bytes.TrimSpace(args)) == 0 || string(bytes.TrimSpace(args)) == "null" {
					args = json.RawMessage("{}")
				}
				return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: s.call(t, args)}, true
			}
		}
		return errResp(req.ID, errInvalidParams, "unknown tool: "+p.Name), true
	}
	return errResp(req.ID, errMethodNotFound, "method not found: "+req.Method), true
}

func (s *Server) call(t tool, args json.RawMessage) (result map[string]any) {
	defer func() {
		if r := recover(); r != nil {
			result = toolError(fmt.Sprintf("internal error: %v", r))
		}
	}()
	out, err := t.fn(args)
	if err != nil {
		return toolError(err.Error())
	}
	if m, ok := out.(isErr); ok {
		res := toolResult(m.v)
		res["isError"] = true
		return res
	}
	return toolResult(out)
}

// isErr wraps a structured payload that should be reported with isError=true.
type isErr struct{ v any }

func toolResult(v any) map[string]any {
	b, _ := json.Marshal(v)
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(b)}},
		"structuredContent": v,
	}
}

func toolError(msg string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": msg}},
		"isError": true,
	}
}
