package mcp

import "encoding/json"

// JSONRPCMessage represents a generic JSON-RPC 2.0 message.
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ToolCallParams represents the parameters of a tools/call request.
type ToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// ToolResult represents the result returned for a tools/call response.
type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent represents a content block in a tool result.
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ToolDefinition represents a tool definition from tools/list response.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ToolsListResult represents the result of a tools/list response.
type ToolsListResult struct {
	Tools []ToolDefinition `json:"tools"`
}
