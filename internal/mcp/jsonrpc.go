package mcp

import (
	"encoding/json"
	"io"
	"net/http"
)

const (
	jsonRPCVersion = "2.0"
	serverName     = "harness-memory"
	serverVersion  = "0.1.0"
	defaultProto   = "2025-03-26"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type callResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeRPC(w, rpcResponse{
			JSONRPC: jsonRPCVersion,
			Error:   &rpcError{Code: -32700, Message: "parse error"},
		})
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil || req.JSONRPC != jsonRPCVersion || req.Method == "" {
		writeRPC(w, rpcResponse{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &rpcError{Code: -32600, Message: "invalid request"},
		})
		return
	}
	if isNotification(req) {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	result, rpcErr := h.dispatch(r, req)
	resp := rpcResponse{JSONRPC: jsonRPCVersion, ID: req.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	writeRPC(w, resp)
}

func (h *handler) dispatch(r *http.Request, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return initializeResult(req.Params), nil
	case "notifications/initialized":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefs()}, nil
	case "tools/call":
		return h.toolsCall(r, req.Params)
	case "ping":
		return map[string]any{}, nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found"}
	}
}

func initializeResult(params json.RawMessage) map[string]any {
	version := defaultProto
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 && json.Unmarshal(params, &p) == nil && p.ProtocolVersion != "" {
		version = p.ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    serverName,
			"version": serverVersion,
		},
	}
}

func (h *handler) toolsCall(r *http.Request, params json.RawMessage) (any, *rpcError) {
	var p callParams
	if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
		return nil, &rpcError{Code: -32602, Message: "invalid params"}
	}
	args := p.Arguments
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	harness, _ := harnessFrom(r.Context())
	text, err := h.callTool(r.Context(), p.Name, harness, args)
	if err != nil {
		return callResult{
			Content: []toolContent{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, nil
	}
	return callResult{
		Content: []toolContent{{Type: "text", Text: text}},
	}, nil
}

func isNotification(req rpcRequest) bool {
	return len(req.ID) == 0 || string(req.ID) == "null"
}

func writeRPC(w http.ResponseWriter, resp rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func toolJSON(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
