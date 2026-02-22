package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"agentgate/internal/agentgate"
)

// ProxyConfig holds configuration for the MCP proxy.
type ProxyConfig struct {
	Paths      agentgate.Paths
	ServerCmd  string
	ServerArgs []string
	ServerName string // derived from command for policy matching
}

// RunProxy starts the MCP proxy, spawning the real MCP server and
// intercepting JSON-RPC messages between the agent and the server.
func RunProxy(config ProxyConfig) int {
	sessionID := agentgate.NewCommandID()
	fmt.Fprintf(os.Stderr, "AGENTGATE_MCP_PROXY=true\n")
	fmt.Fprintf(os.Stderr, "AGENTGATE_MCP_SESSION=%s\n", sessionID)
	fmt.Fprintf(os.Stderr, "AGENTGATE_MCP_SERVER=%s\n", config.ServerName)

	child := exec.Command(config.ServerCmd, config.ServerArgs...)
	childIn, err := child.StdinPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-proxy: stdin pipe: %v\n", err)
		return 1
	}
	childOut, err := child.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-proxy: stdout pipe: %v\n", err)
		return 1
	}
	child.Stderr = os.Stderr

	if err := child.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "agentgate mcp-proxy: start server: %v\n", err)
		return 1
	}

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		_ = child.Process.Signal(syscall.SIGTERM)
	}()

	var wg sync.WaitGroup

	// pendingRequests tracks tools/call request IDs so we know which
	// responses need inspection
	pendingRequests := &sync.Map{}

	// Agent → Server (stdin): intercept tools/call requests
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer childIn.Close()
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var msg JSONRPCMessage
			if err := json.Unmarshal(line, &msg); err != nil {
				// Pass through non-JSON lines
				childIn.Write(append(line, '\n'))
				continue
			}

			if msg.Method == "tools/call" {
				blocked, response := handleToolsCall(config, &msg, sessionID)
				if blocked {
					// Send denial response directly to agent (stdout)
					respBytes, _ := json.Marshal(response)
					os.Stdout.Write(append(respBytes, '\n'))
					continue
				}
			}

			// Track request IDs for tools/call so we can log responses
			if msg.Method == "tools/call" && msg.ID != nil {
				pendingRequests.Store(string(msg.ID), true)
			}

			// Forward to server
			childIn.Write(append(line, '\n'))
		}
	}()

	// Server → Agent (stdout): pass through, log tools/call responses
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(childOut)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			// Check if this is a response to a tracked tools/call
			var msg JSONRPCMessage
			if err := json.Unmarshal(line, &msg); err == nil && msg.ID != nil {
				if _, ok := pendingRequests.LoadAndDelete(string(msg.ID)); ok {
					logMCPEvent(config.Paths, sessionID, config.ServerName, "tools/call-response", string(msg.ID), "allowed")
				}
			}

			os.Stdout.Write(append(line, '\n'))
		}
	}()

	wg.Wait()

	if err := child.Wait(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return 1
	}
	return 0
}

// handleToolsCall intercepts a tools/call request, evaluates it against policies,
// and returns whether it was blocked and a response to send if so.
func handleToolsCall(config ProxyConfig, msg *JSONRPCMessage, sessionID string) (bool, *JSONRPCMessage) {
	var params ToolCallParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		// Can't parse → pass through (fail-open)
		return false, nil
	}

	ctx, result, mode, _ := EvaluateToolCall(config.Paths, config.ServerName, params.Name, params.Arguments)

	logMCPToolCall(config.Paths, sessionID, config.ServerName, params.Name, result, mode)

	effective := result.Decision
	if mode == agentgate.ModeObserve {
		if result.Decision == agentgate.DecisionDeny || result.Decision == agentgate.DecisionConfirm {
			effective = agentgate.DecisionAllow
			fmt.Fprintf(os.Stderr, "AGENTGATE_MCP_OBSERVE: tool=%s decision=%s (simulated)\n", params.Name, result.Decision)
		}
	}

	if effective == agentgate.DecisionDeny {
		blockMsg := fmt.Sprintf(
			"AGENTGATE BLOCKED: Policy '%s' denied this action. Risk: %d. %s",
			agentgate.Normalize(result.PolicyName),
			result.Risk,
			result.Suggestion,
		)

		response := &JSONRPCMessage{
			JSONRPC: "2.0",
			ID:      msg.ID,
		}
		toolResult := ToolResult{
			Content: []ToolContent{{Type: "text", Text: blockMsg}},
			IsError: true,
		}
		resultBytes, _ := json.Marshal(toolResult)
		response.Result = resultBytes

		fmt.Fprintf(os.Stderr, "AGENTGATE_MCP_BLOCKED: tool=%s policy=%s risk=%d\n",
			params.Name, result.PolicyName, result.Risk)

		logMCPEvent(config.Paths, sessionID, config.ServerName, params.Name, "", "blocked")
		_ = ctx // used in log

		return true, response
	}

	if effective == agentgate.DecisionConfirm {
		// In MCP context, confirm falls through to allow since we can't prompt interactively
		fmt.Fprintf(os.Stderr, "AGENTGATE_MCP_CONFIRM: tool=%s (auto-allowed, non-interactive)\n", params.Name)
	}

	if effective == agentgate.DecisionWarn {
		fmt.Fprintf(os.Stderr, "AGENTGATE_MCP_WARN: tool=%s policy=%s risk=%d\n",
			params.Name, result.PolicyName, result.Risk)
	}

	return false, nil
}

func logMCPToolCall(paths agentgate.Paths, sessionID, serverName, toolName string, result agentgate.DecisionResult, mode agentgate.Mode) {
	ev := agentgate.StartEvent{
		TS:       time.Now().UTC(),
		ID:       agentgate.NewCommandID(),
		Phase:    "start",
		Mode:     mode,
		Tool:     serverName,
		Cmd:      fmt.Sprintf("mcp:%s/%s", serverName, toolName),
		Env:      "unknown",
		Decision: result.Decision,
		Policy:   result.PolicyName,
		Risk:     result.Risk,
		Parse:    agentgate.ParseStatusParsed,
		Source:   "mcp",
	}
	_ = agentgate.AppendEvent(paths, ev)
}

func logMCPEvent(paths agentgate.Paths, sessionID, serverName, action, requestID, outcome string) {
	ev := agentgate.EndEvent{
		TS:       time.Now().UTC(),
		ID:       agentgate.NewCommandID(),
		Phase:    "end",
		ExitCode: 0,
		Outcome:  outcome,
	}
	_ = agentgate.AppendEvent(paths, ev)
}

// DeriveServerName extracts a human-readable server name from the command.
func DeriveServerName(cmd string, args []string) string {
	// Try to find a recognizable package name in args
	for _, arg := range args {
		if strings.Contains(arg, "@modelcontextprotocol/") {
			parts := strings.Split(arg, "/")
			return parts[len(parts)-1]
		}
		if strings.HasSuffix(arg, "-server") || strings.Contains(arg, "mcp-") {
			return arg
		}
	}
	// Fall back to command basename
	parts := strings.Split(cmd, "/")
	return parts[len(parts)-1]
}

// ParseProxyArgs parses the mcp-proxy command line arguments.
// Format: agentgate mcp-proxy [--server-name NAME] -- <server-cmd> <args...>
func ParseProxyArgs(args []string) (serverName string, serverCmd string, serverArgs []string, err error) {
	i := 0
	for i < len(args) {
		if args[i] == "--" {
			i++
			break
		}
		if args[i] == "--server-name" && i+1 < len(args) {
			serverName = args[i+1]
			i += 2
			continue
		}
		if strings.HasPrefix(args[i], "--server-name=") {
			serverName = strings.TrimPrefix(args[i], "--server-name=")
			i++
			continue
		}
		i++
	}

	if i >= len(args) {
		return "", "", nil, fmt.Errorf("usage: agentgate mcp-proxy [--server-name NAME] -- <server-command> [args...]")
	}

	serverCmd = args[i]
	if i+1 < len(args) {
		serverArgs = args[i+1:]
	}

	if serverName == "" {
		serverName = DeriveServerName(serverCmd, serverArgs)
	}

	return serverName, serverCmd, serverArgs, nil
}

// StreamCopy copies from src to dst, used for transparent proxying.
func StreamCopy(dst io.Writer, src io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			dst.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
}
