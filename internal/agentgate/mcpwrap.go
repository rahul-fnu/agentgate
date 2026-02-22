package agentgate

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type mcpServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type mcpOriginals struct {
	SettingsPath string                    `json:"settings_path"`
	Servers      map[string]mcpServerEntry `json:"servers"`
}

func findClaudeSettings() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(home, ".claude", "settings.json")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

func isAlreadyWrapped(entry mcpServerEntry) bool {
	cmd := filepath.Base(entry.Command)
	if !strings.HasPrefix(cmd, "agentgate") {
		return false
	}
	return len(entry.Args) > 0 && entry.Args[0] == "mcp-proxy"
}

func wrapServer(name string, entry mcpServerEntry, agentgatePath string) mcpServerEntry {
	args := []string{"mcp-proxy", "--server-name", name, "--"}
	args = append(args, entry.Command)
	args = append(args, entry.Args...)
	return mcpServerEntry{
		Command: agentgatePath,
		Args:    args,
	}
}

func cmdMCPWrap(paths Paths, args []string) int {
	fs := flag.NewFlagSet("mcp-wrap", flag.ContinueOnError)
	settingsFlag := fs.String("settings", "", "path to Claude Code settings.json")
	dryRun := fs.Bool("dry-run", false, "show what would change without writing")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	settingsPath := *settingsFlag
	if settingsPath == "" {
		settingsPath = findClaudeSettings()
	}
	if settingsPath == "" {
		home, _ := os.UserHomeDir()
		expected := filepath.Join(home, ".claude", "settings.json")
		fmt.Fprintf(os.Stderr, "agentgate mcp-wrap: settings file not found\n")
		fmt.Fprintf(os.Stderr, "  Expected: %s\n", expected)
		fmt.Fprintf(os.Stderr, "  Use --settings <path> to specify a custom location.\n")
		return 1
	}

	settingsData, err := os.ReadFile(settingsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-wrap: %v\n", err)
		return 1
	}

	var settings map[string]any
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-wrap: failed to parse settings: %v\n", err)
		return 1
	}

	mcpServersRaw, ok := settings["mcpServers"]
	if !ok {
		fmt.Println("No MCP servers found in settings.")
		return 0
	}
	mcpServersMap, ok := mcpServersRaw.(map[string]any)
	if !ok {
		fmt.Println("No MCP servers found in settings.")
		return 0
	}
	if len(mcpServersMap) == 0 {
		fmt.Println("No MCP servers found in settings.")
		return 0
	}

	agentgatePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-wrap: failed to resolve agentgate binary path: %v\n", err)
		return 1
	}

	originals := mcpOriginals{
		SettingsPath: settingsPath,
		Servers:      make(map[string]mcpServerEntry),
	}

	var wrapped []string
	var skipped []string

	for name, raw := range mcpServersMap {
		serverMap, ok := raw.(map[string]any)
		if !ok {
			skipped = append(skipped, name)
			continue
		}

		entry := extractServerEntry(serverMap)

		if isAlreadyWrapped(entry) {
			skipped = append(skipped, name)
			continue
		}

		originals.Servers[name] = entry
		newEntry := wrapServer(name, entry, agentgatePath)

		serverMap["command"] = newEntry.Command
		serverMap["args"] = newEntry.Args
		wrapped = append(wrapped, name)
	}

	if len(wrapped) == 0 {
		fmt.Println("All MCP servers are already wrapped.")
		return 0
	}

	fmt.Printf("Found Claude Code settings: %s\n", settingsPath)

	if *dryRun {
		fmt.Printf("Would wrap %d MCP server(s):\n", len(wrapped))
		for _, name := range wrapped {
			orig := originals.Servers[name]
			fmt.Printf("  %s: %s %s → agentgate mcp-proxy\n", name, orig.Command, strings.Join(orig.Args, " "))
		}
		if len(skipped) > 0 {
			fmt.Printf("Skipping %d already-wrapped server(s): %s\n", len(skipped), strings.Join(skipped, ", "))
		}
		fmt.Println("Dry run — no files modified.")
		return 0
	}

	// Merge with existing backup if present
	existingOriginals, err := loadMCPOriginals(paths)
	if err == nil {
		for name, entry := range existingOriginals.Servers {
			if _, exists := originals.Servers[name]; !exists {
				originals.Servers[name] = entry
			}
		}
	}

	backupData, err := json.MarshalIndent(originals, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-wrap: failed to marshal backup: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(paths.MCPOriginalsPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-wrap: %v\n", err)
		return 1
	}
	if err := os.WriteFile(paths.MCPOriginalsPath, backupData, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-wrap: failed to write backup: %v\n", err)
		return 1
	}

	settingsOut, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-wrap: failed to marshal settings: %v\n", err)
		return 1
	}
	settingsOut = append(settingsOut, '\n')
	if err := os.WriteFile(settingsPath, settingsOut, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-wrap: failed to write settings: %v\n", err)
		return 1
	}

	fmt.Printf("Wrapping %d MCP server(s):\n", len(wrapped))
	for _, name := range wrapped {
		orig := originals.Servers[name]
		fmt.Printf("  %s: %s %s → agentgate mcp-proxy\n", name, orig.Command, strings.Join(orig.Args, " "))
	}
	if len(skipped) > 0 {
		fmt.Printf("Skipping %d already-wrapped server(s): %s\n", len(skipped), strings.Join(skipped, ", "))
	}
	fmt.Printf("Backup saved to %s\n", paths.MCPOriginalsPath)
	fmt.Println("Done. Restart Claude Code to apply changes.")
	return 0
}

func cmdMCPUnwrap(paths Paths, args []string) int {
	fs := flag.NewFlagSet("mcp-unwrap", flag.ContinueOnError)
	settingsFlag := fs.String("settings", "", "path to Claude Code settings.json")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	originals, err := loadMCPOriginals(paths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-unwrap: no backup found at %s\n", paths.MCPOriginalsPath)
		fmt.Fprintln(os.Stderr, "  Nothing to unwrap. Run 'agentgate mcp-wrap' first.")
		return 1
	}

	settingsPath := *settingsFlag
	if settingsPath == "" {
		settingsPath = originals.SettingsPath
	}
	if settingsPath == "" {
		settingsPath = findClaudeSettings()
	}
	if settingsPath == "" {
		fmt.Fprintf(os.Stderr, "agentgate mcp-unwrap: settings file not found\n")
		return 1
	}

	settingsData, err := os.ReadFile(settingsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-unwrap: %v\n", err)
		return 1
	}

	var settings map[string]any
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-unwrap: failed to parse settings: %v\n", err)
		return 1
	}

	mcpServersRaw, ok := settings["mcpServers"]
	if !ok {
		fmt.Println("No MCP servers found in settings.")
		return 0
	}
	mcpServersMap, ok := mcpServersRaw.(map[string]any)
	if !ok {
		fmt.Println("No MCP servers found in settings.")
		return 0
	}

	var restored []string
	for name, orig := range originals.Servers {
		serverRaw, ok := mcpServersMap[name]
		if !ok {
			continue
		}
		serverMap, ok := serverRaw.(map[string]any)
		if !ok {
			continue
		}
		serverMap["command"] = orig.Command
		serverMap["args"] = orig.Args
		restored = append(restored, name)
	}

	if len(restored) == 0 {
		fmt.Println("No servers to restore.")
		_ = os.Remove(paths.MCPOriginalsPath)
		return 0
	}

	settingsOut, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-unwrap: failed to marshal settings: %v\n", err)
		return 1
	}
	settingsOut = append(settingsOut, '\n')
	if err := os.WriteFile(settingsPath, settingsOut, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-unwrap: failed to write settings: %v\n", err)
		return 1
	}

	_ = os.Remove(paths.MCPOriginalsPath)

	fmt.Printf("Restoring %d MCP server(s) from backup:\n", len(restored))
	for _, name := range restored {
		fmt.Printf("  %s: restored\n", name)
	}
	fmt.Println("Backup removed.")
	fmt.Println("Done. Restart Claude Code to apply changes.")
	return 0
}

func loadMCPOriginals(paths Paths) (mcpOriginals, error) {
	data, err := os.ReadFile(paths.MCPOriginalsPath)
	if err != nil {
		return mcpOriginals{}, err
	}
	var orig mcpOriginals
	if err := json.Unmarshal(data, &orig); err != nil {
		return mcpOriginals{}, err
	}
	return orig, nil
}

func extractServerEntry(serverMap map[string]any) mcpServerEntry {
	entry := mcpServerEntry{}
	if cmd, ok := serverMap["command"].(string); ok {
		entry.Command = cmd
	}
	if argsRaw, ok := serverMap["args"].([]any); ok {
		for _, a := range argsRaw {
			if s, ok := a.(string); ok {
				entry.Args = append(entry.Args, s)
			}
		}
	}
	return entry
}
