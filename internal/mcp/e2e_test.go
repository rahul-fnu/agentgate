package mcp

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"agentgate/internal/agentgate"
)

// TestE2E_MCPPolicyEvaluation tests the full MCP policy evaluation pipeline.
func TestE2E_MCPPolicyEvaluation(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	agentgate.SaveConfig(paths, agentgate.Config{
		Mode:                      agentgate.ModeEnforce,
		UnknownEnvDefaultDecision: "",
		EnvironmentPatterns:       agentgate.DefaultConfig().EnvironmentPatterns,
	})

	policyYAML := `policies:
  - name: block-file-delete
    priority: 100
    decision: deny
    suggestion: "File deletion via MCP is blocked."
    match:
      mcp_server: ["filesystem"]
      mcp_tool: ["delete_file", "remove_file"]

  - name: block-destructive-sql
    priority: 100
    decision: deny
    suggestion: "Destructive SQL via MCP is blocked."
    match:
      mcp_server: ["postgres"]
      mcp_tool: ["execute_query"]
      mcp_args_contain: ["DROP", "TRUNCATE"]

  - name: warn-mcp-writes
    priority: 50
    decision: warn
    suggestion: "MCP write detected."
    match:
      action_type: [write, destructive]

  - name: allow-reads
    priority: 80
    decision: allow
    match:
      action_type: [read]
`
	os.WriteFile(paths.PoliciesPath, []byte(policyYAML), 0o644)

	tests := []struct {
		name         string
		server       string
		tool         string
		args         map[string]any
		wantDecision agentgate.Decision
		wantPolicy   string
	}{
		{
			name:         "filesystem delete_file → deny",
			server:       "filesystem",
			tool:         "delete_file",
			args:         map[string]any{"path": "/tmp/important.txt"},
			wantDecision: agentgate.DecisionDeny,
			wantPolicy:   "block-file-delete",
		},
		{
			name:         "filesystem remove_file → deny",
			server:       "filesystem",
			tool:         "remove_file",
			args:         map[string]any{"path": "/etc/hosts"},
			wantDecision: agentgate.DecisionDeny,
			wantPolicy:   "block-file-delete",
		},
		{
			name:         "filesystem read_file → allow",
			server:       "filesystem",
			tool:         "read_file",
			args:         map[string]any{"path": "/tmp/test.txt"},
			wantDecision: agentgate.DecisionAllow,
			wantPolicy:   "allow-reads",
		},
		{
			name:         "filesystem write_file → warn",
			server:       "filesystem",
			tool:         "write_file",
			args:         map[string]any{"path": "/tmp/out.txt", "content": "hello"},
			wantDecision: agentgate.DecisionWarn,
			wantPolicy:   "warn-mcp-writes",
		},
		{
			name:         "postgres DROP TABLE → deny",
			server:       "postgres",
			tool:         "execute_query",
			args:         map[string]any{"query": "DROP TABLE users"},
			wantDecision: agentgate.DecisionDeny,
			wantPolicy:   "block-destructive-sql",
		},
		{
			name:         "postgres TRUNCATE → deny",
			server:       "postgres",
			tool:         "execute_query",
			args:         map[string]any{"query": "TRUNCATE TABLE orders"},
			wantDecision: agentgate.DecisionDeny,
			wantPolicy:   "block-destructive-sql",
		},
		{
			name:         "postgres SELECT → warn (execute_query classified as write)",
			server:       "postgres",
			tool:         "execute_query",
			args:         map[string]any{"query": "SELECT * FROM users"},
			wantDecision: agentgate.DecisionWarn,
			wantPolicy:   "warn-mcp-writes",
		},
		{
			name:         "unknown server read_file → allow",
			server:       "unknown-server",
			tool:         "read_file",
			args:         map[string]any{"path": "/tmp/x"},
			wantDecision: agentgate.DecisionAllow,
			wantPolicy:   "allow-reads",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, result, _, _ := EvaluateToolCall(paths, tt.server, tt.tool, tt.args)
			if result.Decision != tt.wantDecision {
				t.Errorf("decision = %q, want %q (policy=%s)", result.Decision, tt.wantDecision, result.PolicyName)
			}
			if tt.wantPolicy != "" && result.PolicyName != tt.wantPolicy {
				t.Errorf("policy = %q, want %q", result.PolicyName, tt.wantPolicy)
			}
		})
	}
}

// TestE2E_MCPBlockedResponse verifies the blocked response format matches MCP spec.
func TestE2E_MCPBlockedResponse(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	agentgate.SaveConfig(paths, agentgate.Config{
		Mode:                      agentgate.ModeEnforce,
		UnknownEnvDefaultDecision: "",
		EnvironmentPatterns:       agentgate.DefaultConfig().EnvironmentPatterns,
	})

	policyYAML := `policies:
  - name: block-delete
    priority: 100
    decision: deny
    suggestion: "Deletion blocked by policy."
    match:
      mcp_server: ["filesystem"]
      mcp_tool: ["delete_file"]
`
	os.WriteFile(paths.PoliciesPath, []byte(policyYAML), 0o644)

	config := ProxyConfig{
		Paths:      paths,
		ServerName: "filesystem",
	}

	params := ToolCallParams{Name: "delete_file", Arguments: map[string]any{"path": "/etc/important"}}
	paramsBytes, _ := json.Marshal(params)
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`42`),
		Method:  "tools/call",
		Params:  paramsBytes,
	}

	blocked, resp := handleToolsCall(config, msg, "session-e2e")
	if !blocked {
		t.Fatal("should block delete_file")
	}

	// Validate response structure
	if resp.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", resp.JSONRPC)
	}
	if string(resp.ID) != "42" {
		t.Errorf("id = %s, want 42", string(resp.ID))
	}

	// Validate result is a proper tool result
	var toolResult ToolResult
	if err := json.Unmarshal(resp.Result, &toolResult); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if !toolResult.IsError {
		t.Error("isError should be true")
	}
	if len(toolResult.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(toolResult.Content))
	}
	if toolResult.Content[0].Type != "text" {
		t.Errorf("content type = %q, want text", toolResult.Content[0].Type)
	}
	if !strings.Contains(toolResult.Content[0].Text, "AGENTGATE BLOCKED") {
		t.Errorf("content should contain AGENTGATE BLOCKED: %q", toolResult.Content[0].Text)
	}
	if !strings.Contains(toolResult.Content[0].Text, "block-delete") {
		t.Errorf("content should contain policy name: %q", toolResult.Content[0].Text)
	}

	// Validate the full response is valid JSON
	respBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("response should be marshalable: %v", err)
	}
	var roundtrip map[string]any
	if err := json.Unmarshal(respBytes, &roundtrip); err != nil {
		t.Fatalf("response should be valid JSON: %v", err)
	}
}

// TestE2E_MCPObserveMode verifies observe mode doesn't block MCP calls.
func TestE2E_MCPObserveMode(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	// Default mode is observe
	policyYAML := `policies:
  - name: block-delete
    priority: 100
    decision: deny
    suggestion: "Blocked"
    match:
      mcp_server: ["filesystem"]
      mcp_tool: ["delete_file"]
`
	os.WriteFile(paths.PoliciesPath, []byte(policyYAML), 0o644)

	config := ProxyConfig{
		Paths:      paths,
		ServerName: "filesystem",
	}

	params := ToolCallParams{Name: "delete_file", Arguments: map[string]any{"path": "/tmp/x"}}
	paramsBytes, _ := json.Marshal(params)
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  paramsBytes,
	}

	blocked, _ := handleToolsCall(config, msg, "session-observe")
	if blocked {
		t.Error("observe mode should not block")
	}
}

// TestE2E_MCPEnforceMode verifies enforce mode blocks MCP calls.
func TestE2E_MCPEnforceMode(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	agentgate.SaveConfig(paths, agentgate.Config{
		Mode:                      agentgate.ModeEnforce,
		UnknownEnvDefaultDecision: "",
		EnvironmentPatterns:       agentgate.DefaultConfig().EnvironmentPatterns,
	})

	policyYAML := `policies:
  - name: block-delete
    priority: 100
    decision: deny
    suggestion: "Blocked"
    match:
      mcp_server: ["filesystem"]
      mcp_tool: ["delete_file"]
`
	os.WriteFile(paths.PoliciesPath, []byte(policyYAML), 0o644)

	config := ProxyConfig{
		Paths:      paths,
		ServerName: "filesystem",
	}

	// Blocked call
	params := ToolCallParams{Name: "delete_file", Arguments: map[string]any{"path": "/tmp/x"}}
	paramsBytes, _ := json.Marshal(params)
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  paramsBytes,
	}
	blocked, _ := handleToolsCall(config, msg, "session-enforce")
	if !blocked {
		t.Error("enforce mode should block denied calls")
	}

	// Allowed call
	params2 := ToolCallParams{Name: "read_file", Arguments: map[string]any{"path": "/tmp/x"}}
	paramsBytes2, _ := json.Marshal(params2)
	msg2 := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "tools/call",
		Params:  paramsBytes2,
	}
	blocked2, _ := handleToolsCall(config, msg2, "session-enforce")
	if blocked2 {
		t.Error("enforce mode should allow non-matching calls")
	}
}

// TestE2E_MCPEventLogging verifies MCP events are logged correctly.
func TestE2E_MCPEventLogging(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	agentgate.SaveConfig(paths, agentgate.Config{
		Mode:                      agentgate.ModeEnforce,
		UnknownEnvDefaultDecision: "",
		EnvironmentPatterns:       agentgate.DefaultConfig().EnvironmentPatterns,
	})

	policyYAML := `policies:
  - name: block-delete
    priority: 100
    decision: deny
    suggestion: "Blocked"
    match:
      mcp_server: ["filesystem"]
      mcp_tool: ["delete_file"]
`
	os.WriteFile(paths.PoliciesPath, []byte(policyYAML), 0o644)

	config := ProxyConfig{
		Paths:      paths,
		ServerName: "filesystem",
	}

	params := ToolCallParams{Name: "delete_file", Arguments: map[string]any{"path": "/tmp/x"}}
	paramsBytes, _ := json.Marshal(params)
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  paramsBytes,
	}

	handleToolsCall(config, msg, "session-log-test")

	// Check events were logged
	b, err := os.ReadFile(paths.EventsPath)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	events := string(b)

	if !strings.Contains(events, `"source":"mcp"`) {
		t.Error("events should contain source:mcp")
	}
	if !strings.Contains(events, `"tool":"filesystem"`) {
		t.Error("events should contain tool:filesystem")
	}
	if !strings.Contains(events, `"decision":"deny"`) {
		t.Error("events should contain decision:deny")
	}
	if !strings.Contains(events, `"policy":"block-delete"`) {
		t.Error("events should contain policy:block-delete")
	}
}

// TestE2E_MCPClassifyAction tests comprehensive MCP action classification.
func TestE2E_MCPClassifyAction(t *testing.T) {
	tests := []struct {
		toolName string
		wantType string
	}{
		// Destructive
		{"delete_file", "destructive"},
		{"drop_table", "destructive"},
		{"remove_directory", "destructive"},
		{"truncate_table", "destructive"},
		{"destroy_resource", "destructive"},
		{"purge_cache", "destructive"},
		// Write
		{"write_file", "write"},
		{"create_directory", "write"},
		{"update_record", "write"},
		{"insert_row", "write"},
		{"put_object", "write"},
		{"set_value", "write"},
		{"execute_query", "write"},
		{"run_command", "write"},
		// Read
		{"read_file", "read"},
		{"get_content", "read"},
		{"list_files", "read"},
		{"describe_table", "read"},
		{"search_documents", "read"},
		{"find_records", "read"},
		// Other
		{"ping", "other"},
		{"status", "other"},
		{"version", "other"},
		{"connect", "other"},
	}
	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			got := classifyMCPAction(tt.toolName)
			if got != tt.wantType {
				t.Errorf("classifyMCPAction(%q) = %q, want %q", tt.toolName, got, tt.wantType)
			}
		})
	}
}

// TestE2E_MCPServerNameDerivation tests server name derivation for various MCP configs.
func TestE2E_MCPServerNameDerivation(t *testing.T) {
	tests := []struct {
		cmd  string
		args []string
		want string
	}{
		{"npx", []string{"@modelcontextprotocol/server-filesystem", "/tmp"}, "server-filesystem"},
		{"npx", []string{"@modelcontextprotocol/server-postgres", "postgresql://localhost/db"}, "server-postgres"},
		{"node", []string{"./mcp-custom-server.js"}, "./mcp-custom-server.js"},
		{"python", []string{"-m", "some_server"}, "python"},
		{"uvx", []string{"mcp-server-git"}, "mcp-server-git"},
	}
	for _, tt := range tests {
		t.Run(tt.cmd+"_"+strings.Join(tt.args, "_"), func(t *testing.T) {
			got := DeriveServerName(tt.cmd, tt.args)
			if got != tt.want {
				t.Errorf("DeriveServerName(%q, %v) = %q, want %q", tt.cmd, tt.args, got, tt.want)
			}
		})
	}
}
