package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"agentgate/internal/agentgate"
)

func testPaths(t *testing.T) agentgate.Paths {
	t.Helper()
	dir := t.TempDir()
	return agentgate.Paths{
		Root:         dir,
		BinDir:       filepath.Join(dir, "bin"),
		ConfigPath:   filepath.Join(dir, "config.yaml"),
		PoliciesPath: filepath.Join(dir, "policies.yaml"),
		EventsPath:   filepath.Join(dir, "events.jsonl"),
		BypassesPath: filepath.Join(dir, "bypasses.jsonl"),
		CachePath:    filepath.Join(dir, "cache.json"),
		EventsLock:   filepath.Join(dir, ".events.lock"),
	}
}

func TestParseProxyArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantServer string
		wantCmd    string
		wantArgs   []string
		wantErr    bool
	}{
		{
			name:       "basic",
			args:       []string{"--", "npx", "@modelcontextprotocol/server-filesystem", "/tmp"},
			wantServer: "server-filesystem",
			wantCmd:    "npx",
			wantArgs:   []string{"@modelcontextprotocol/server-filesystem", "/tmp"},
		},
		{
			name:       "with server name",
			args:       []string{"--server-name", "fs", "--", "npx", "some-server"},
			wantServer: "fs",
			wantCmd:    "npx",
			wantArgs:   []string{"some-server"},
		},
		{
			name:       "server name equals",
			args:       []string{"--server-name=postgres", "--", "node", "pg-server.js"},
			wantServer: "postgres",
			wantCmd:    "node",
			wantArgs:   []string{"pg-server.js"},
		},
		{
			name:    "missing separator",
			args:    []string{"--server-name", "test"},
			wantErr: true,
		},
		{
			name:    "empty",
			args:    []string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverName, cmd, args, err := ParseProxyArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if serverName != tt.wantServer {
				t.Errorf("server_name = %q, want %q", serverName, tt.wantServer)
			}
			if cmd != tt.wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, tt.wantCmd)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("args len = %d, want %d", len(args), len(tt.wantArgs))
			}
			for i, a := range args {
				if a != tt.wantArgs[i] {
					t.Errorf("args[%d] = %q, want %q", i, a, tt.wantArgs[i])
				}
			}
		})
	}
}

func TestDeriveServerName(t *testing.T) {
	tests := []struct {
		cmd  string
		args []string
		want string
	}{
		{"npx", []string{"@modelcontextprotocol/server-filesystem", "/tmp"}, "server-filesystem"},
		{"node", []string{"mcp-postgres.js"}, "mcp-postgres.js"},
		{"python", []string{"server.py"}, "python"},
	}
	for _, tt := range tests {
		got := DeriveServerName(tt.cmd, tt.args)
		if got != tt.want {
			t.Errorf("DeriveServerName(%q, %v) = %q, want %q", tt.cmd, tt.args, got, tt.want)
		}
	}
}

func TestEvaluateToolCall_Allow(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	ctx, result, _, err := EvaluateToolCall(paths, "filesystem", "read_file", map[string]any{"path": "/tmp/test.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctx.Tool != "filesystem" {
		t.Errorf("tool = %q, want filesystem", ctx.Tool)
	}
	if ctx.Action != "read_file" {
		t.Errorf("action = %q, want read_file", ctx.Action)
	}
	// MCP calls don't have environment detection, so no unknown-env-default applies
	if result.Decision != agentgate.DecisionAllow {
		t.Errorf("decision = %q, want allow", result.Decision)
	}
}

func TestEvaluateToolCall_DenyWithPolicy(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	policyYAML := `policies:
  - name: block-destructive-file-ops
    priority: 100
    decision: deny
    suggestion: "File deletion blocked"
    match:
      tool: ["filesystem"]
      action: ["delete_file"]
`
	os.WriteFile(paths.PoliciesPath, []byte(policyYAML), 0o644)

	_, result, _, _ := EvaluateToolCall(paths, "filesystem", "delete_file", map[string]any{"path": "/etc/important"})
	if result.Decision != agentgate.DecisionDeny {
		t.Errorf("decision = %q, want deny", result.Decision)
	}
	if result.PolicyName != "block-destructive-file-ops" {
		t.Errorf("policy = %q, want block-destructive-file-ops", result.PolicyName)
	}
}

func TestEvaluateToolCall_MCPArgsContain(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	policyYAML := `policies:
  - name: block-prod-queries
    priority: 100
    decision: deny
    suggestion: "Destructive SQL blocked"
    match:
      tool: ["postgres"]
      action: ["execute_query"]
      raw_contains: ["DROP", "TRUNCATE"]
`
	os.WriteFile(paths.PoliciesPath, []byte(policyYAML), 0o644)

	_, result, _, _ := EvaluateToolCall(paths, "postgres", "execute_query", map[string]any{"query": "DROP TABLE users"})
	if result.Decision != agentgate.DecisionDeny {
		t.Errorf("decision = %q, want deny", result.Decision)
	}
}

func TestClassifyMCPAction(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"delete_file", "destructive"},
		{"drop_table", "destructive"},
		{"read_file", "read"},
		{"list_files", "read"},
		{"write_file", "write"},
		{"create_directory", "write"},
		{"execute_query", "write"},
		{"some_tool", "other"},
	}
	for _, tt := range tests {
		got := classifyMCPAction(tt.name)
		if got != tt.want {
			t.Errorf("classifyMCPAction(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestHandleToolsCall_Block(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	// Set enforce mode
	agentgate.SaveConfig(paths, agentgate.Config{
		Mode:                      agentgate.ModeEnforce,
		UnknownEnvDefaultDecision: agentgate.DecisionWarn,
		EnvironmentPatterns:       agentgate.DefaultConfig().EnvironmentPatterns,
	})

	policyYAML := `policies:
  - name: block-rm
    priority: 100
    decision: deny
    suggestion: "Blocked"
    match:
      tool: ["filesystem"]
      action: ["delete_file"]
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

	blocked, resp := handleToolsCall(config, msg, "session-test")
	if !blocked {
		t.Error("should block delete_file")
	}
	if resp == nil {
		t.Fatal("response should not be nil")
	}

	var toolResult ToolResult
	json.Unmarshal(resp.Result, &toolResult)
	if !toolResult.IsError {
		t.Error("blocked response should have isError=true")
	}
	if len(toolResult.Content) == 0 || toolResult.Content[0].Type != "text" {
		t.Error("should have text content")
	}
}

func TestHandleToolsCall_Allow(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	config := ProxyConfig{
		Paths:      paths,
		ServerName: "filesystem",
	}

	params := ToolCallParams{Name: "read_file", Arguments: map[string]any{"path": "/tmp/x"}}
	paramsBytes, _ := json.Marshal(params)
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "tools/call",
		Params:  paramsBytes,
	}

	blocked, _ := handleToolsCall(config, msg, "session-test")
	if blocked {
		t.Error("read_file should not be blocked by default")
	}
}

func TestHandleToolsCall_ObserveMode(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	// Observe mode (default)
	policyYAML := `policies:
  - name: block-rm
    priority: 100
    decision: deny
    suggestion: "Blocked"
    match:
      tool: ["filesystem"]
      action: ["delete_file"]
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
		ID:      json.RawMessage(`3`),
		Method:  "tools/call",
		Params:  paramsBytes,
	}

	blocked, _ := handleToolsCall(config, msg, "session-test")
	if blocked {
		t.Error("observe mode should not block")
	}
}
