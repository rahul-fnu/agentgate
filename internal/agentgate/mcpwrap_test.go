package agentgate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIsAlreadyWrapped(t *testing.T) {
	tests := []struct {
		name   string
		entry  mcpServerEntry
		expect bool
	}{
		{
			name:   "unwrapped npx server",
			entry:  mcpServerEntry{Command: "npx", Args: []string{"@modelcontextprotocol/server-filesystem", "/tmp"}},
			expect: false,
		},
		{
			name:   "wrapped with absolute path",
			entry:  mcpServerEntry{Command: "/usr/local/bin/agentgate", Args: []string{"mcp-proxy", "--server-name", "fs", "--", "npx", "@modelcontextprotocol/server-filesystem"}},
			expect: true,
		},
		{
			name:   "wrapped with bare command",
			entry:  mcpServerEntry{Command: "agentgate", Args: []string{"mcp-proxy", "--server-name", "fs", "--", "npx"}},
			expect: true,
		},
		{
			name:   "agentgate but not mcp-proxy",
			entry:  mcpServerEntry{Command: "agentgate", Args: []string{"status"}},
			expect: false,
		},
		{
			name:   "empty args",
			entry:  mcpServerEntry{Command: "agentgate", Args: []string{}},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAlreadyWrapped(tt.entry)
			if got != tt.expect {
				t.Errorf("isAlreadyWrapped(%+v) = %v, want %v", tt.entry, got, tt.expect)
			}
		})
	}
}

func TestWrapServer(t *testing.T) {
	entry := mcpServerEntry{
		Command: "npx",
		Args:    []string{"@modelcontextprotocol/server-filesystem", "/tmp"},
	}
	result := wrapServer("filesystem", entry, "/usr/local/bin/agentgate")

	if result.Command != "/usr/local/bin/agentgate" {
		t.Errorf("expected command /usr/local/bin/agentgate, got %s", result.Command)
	}
	expectedArgs := []string{"mcp-proxy", "--server-name", "filesystem", "--", "npx", "@modelcontextprotocol/server-filesystem", "/tmp"}
	if len(result.Args) != len(expectedArgs) {
		t.Fatalf("expected %d args, got %d: %v", len(expectedArgs), len(result.Args), result.Args)
	}
	for i, a := range expectedArgs {
		if result.Args[i] != a {
			t.Errorf("arg[%d] = %q, want %q", i, result.Args[i], a)
		}
	}
}

func TestWrapUnwrapRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	settingsPath := filepath.Join(tmpDir, "settings.json")
	originalSettings := map[string]any{
		"mcpServers": map[string]any{
			"filesystem": map[string]any{
				"command": "npx",
				"args":    []any{"@modelcontextprotocol/server-filesystem", "/tmp"},
			},
			"postgres": map[string]any{
				"command": "npx",
				"args":    []any{"@modelcontextprotocol/server-postgres", "postgresql://localhost/db"},
			},
		},
		"otherSetting": "preserved",
	}
	writeJSON(t, settingsPath, originalSettings)

	paths := Paths{
		Root:             tmpDir,
		MCPOriginalsPath: filepath.Join(tmpDir, "mcp_originals.json"),
	}

	// Wrap
	exitCode := cmdMCPWrap(paths, []string{"--settings", settingsPath})
	if exitCode != 0 {
		t.Fatalf("mcp-wrap exited with %d", exitCode)
	}

	// Verify backup exists
	if _, err := os.Stat(paths.MCPOriginalsPath); err != nil {
		t.Fatalf("backup not created: %v", err)
	}

	// Verify settings were modified
	wrappedSettings := readJSON(t, settingsPath)
	mcpServers := wrappedSettings["mcpServers"].(map[string]any)
	fsServer := mcpServers["filesystem"].(map[string]any)
	if fsServer["command"] == "npx" {
		t.Error("expected filesystem server to be wrapped, still has npx command")
	}

	// Verify other settings preserved
	if wrappedSettings["otherSetting"] != "preserved" {
		t.Error("other settings not preserved")
	}

	// Unwrap
	exitCode = cmdMCPUnwrap(paths, []string{"--settings", settingsPath})
	if exitCode != 0 {
		t.Fatalf("mcp-unwrap exited with %d", exitCode)
	}

	// Verify settings restored
	restoredSettings := readJSON(t, settingsPath)
	mcpServers = restoredSettings["mcpServers"].(map[string]any)
	fsServer = mcpServers["filesystem"].(map[string]any)
	if fsServer["command"] != "npx" {
		t.Errorf("expected command to be restored to npx, got %v", fsServer["command"])
	}

	// Verify backup removed
	if _, err := os.Stat(paths.MCPOriginalsPath); !os.IsNotExist(err) {
		t.Error("backup should be removed after unwrap")
	}

	// Verify other settings still preserved
	if restoredSettings["otherSetting"] != "preserved" {
		t.Error("other settings not preserved after unwrap")
	}
}

func TestWrapAlreadyWrapped(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, "settings.json")
	settings := map[string]any{
		"mcpServers": map[string]any{
			"filesystem": map[string]any{
				"command": "/usr/local/bin/agentgate",
				"args":    []any{"mcp-proxy", "--server-name", "filesystem", "--", "npx", "@modelcontextprotocol/server-filesystem"},
			},
		},
	}
	writeJSON(t, settingsPath, settings)

	paths := Paths{
		Root:             tmpDir,
		MCPOriginalsPath: filepath.Join(tmpDir, "mcp_originals.json"),
	}

	exitCode := cmdMCPWrap(paths, []string{"--settings", settingsPath})
	if exitCode != 0 {
		t.Fatalf("mcp-wrap exited with %d", exitCode)
	}

	// Backup should not be created since nothing was wrapped
	if _, err := os.Stat(paths.MCPOriginalsPath); !os.IsNotExist(err) {
		t.Error("backup should not be created when all servers already wrapped")
	}
}

func TestWrapDryRun(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, "settings.json")
	settings := map[string]any{
		"mcpServers": map[string]any{
			"filesystem": map[string]any{
				"command": "npx",
				"args":    []any{"@modelcontextprotocol/server-filesystem", "/tmp"},
			},
		},
	}
	writeJSON(t, settingsPath, settings)

	paths := Paths{
		Root:             tmpDir,
		MCPOriginalsPath: filepath.Join(tmpDir, "mcp_originals.json"),
	}

	exitCode := cmdMCPWrap(paths, []string{"--settings", settingsPath, "--dry-run"})
	if exitCode != 0 {
		t.Fatalf("mcp-wrap --dry-run exited with %d", exitCode)
	}

	// Settings should be unchanged
	result := readJSON(t, settingsPath)
	mcpServers := result["mcpServers"].(map[string]any)
	fsServer := mcpServers["filesystem"].(map[string]any)
	if fsServer["command"] != "npx" {
		t.Error("dry-run should not modify settings file")
	}

	// Backup should not exist
	if _, err := os.Stat(paths.MCPOriginalsPath); !os.IsNotExist(err) {
		t.Error("dry-run should not create backup")
	}
}

func TestWrapMissingSettings(t *testing.T) {
	tmpDir := t.TempDir()
	paths := Paths{
		Root:             tmpDir,
		MCPOriginalsPath: filepath.Join(tmpDir, "mcp_originals.json"),
	}

	exitCode := cmdMCPWrap(paths, []string{"--settings", filepath.Join(tmpDir, "nonexistent.json")})
	if exitCode != 1 {
		t.Errorf("expected exit code 1 for missing settings, got %d", exitCode)
	}
}

func TestUnwrapNoBackup(t *testing.T) {
	tmpDir := t.TempDir()
	paths := Paths{
		Root:             tmpDir,
		MCPOriginalsPath: filepath.Join(tmpDir, "mcp_originals.json"),
	}

	exitCode := cmdMCPUnwrap(paths, []string{})
	if exitCode != 1 {
		t.Errorf("expected exit code 1 when no backup exists, got %d", exitCode)
	}
}

func TestWrapNoMCPServers(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, "settings.json")
	writeJSON(t, settingsPath, map[string]any{"someSetting": true})

	paths := Paths{
		Root:             tmpDir,
		MCPOriginalsPath: filepath.Join(tmpDir, "mcp_originals.json"),
	}

	exitCode := cmdMCPWrap(paths, []string{"--settings", settingsPath})
	if exitCode != 0 {
		t.Errorf("expected exit code 0 for no MCP servers, got %d", exitCode)
	}
}

func TestWrapIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, "settings.json")
	settings := map[string]any{
		"mcpServers": map[string]any{
			"filesystem": map[string]any{
				"command": "npx",
				"args":    []any{"@modelcontextprotocol/server-filesystem", "/tmp"},
			},
		},
	}
	writeJSON(t, settingsPath, settings)

	paths := Paths{
		Root:             tmpDir,
		MCPOriginalsPath: filepath.Join(tmpDir, "mcp_originals.json"),
	}

	// First wrap
	if code := cmdMCPWrap(paths, []string{"--settings", settingsPath}); code != 0 {
		t.Fatalf("first wrap failed with %d", code)
	}

	// Second wrap should detect already wrapped
	if code := cmdMCPWrap(paths, []string{"--settings", settingsPath}); code != 0 {
		t.Fatalf("second wrap failed with %d", code)
	}

	// Unwrap should still restore correctly
	if code := cmdMCPUnwrap(paths, []string{"--settings", settingsPath}); code != 0 {
		t.Fatalf("unwrap failed with %d", code)
	}

	result := readJSON(t, settingsPath)
	mcpServers := result["mcpServers"].(map[string]any)
	fsServer := mcpServers["filesystem"].(map[string]any)
	if fsServer["command"] != "npx" {
		t.Errorf("expected command to be restored to npx, got %v", fsServer["command"])
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
