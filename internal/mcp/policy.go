package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"agentgate/internal/agentgate"
)

// EvaluateToolCall evaluates an MCP tool call against policies.
// It constructs a CommandContext from the MCP call and runs it through the policy engine.
func EvaluateToolCall(paths agentgate.Paths, serverName, toolName string, arguments map[string]any) (agentgate.CommandContext, agentgate.DecisionResult, agentgate.Mode, error) {
	argsJSON, _ := json.Marshal(arguments)
	rawCommand := fmt.Sprintf("mcp:%s/%s %s", serverName, toolName, string(argsJSON))

	ctx := agentgate.CommandContext{
		Tool:        serverName,
		Action:      toolName,
		ActionType:  classifyMCPAction(toolName),
		RawCommand:  rawCommand,
		RawArgs:     []string{toolName, string(argsJSON)},
		Flags:       map[string]string{},
		ParseStatus: agentgate.ParseStatusParsed,
	}

	// Populate flags from arguments for matching
	for k, v := range arguments {
		ctx.Flags[k] = fmt.Sprintf("%v", v)
	}

	// Build a synthetic raw_contains-friendly representation
	var rawParts []string
	rawParts = append(rawParts, toolName)
	for _, v := range arguments {
		rawParts = append(rawParts, fmt.Sprintf("%v", v))
	}
	ctx.RawCommand = strings.Join(rawParts, " ")

	cfg, cfgErr := agentgate.LoadConfig(paths)
	if cfgErr != nil {
		cfg = agentgate.DefaultConfig()
	}
	policies, policyErr := agentgate.LoadPolicies(paths)
	if policyErr != nil {
		policies = nil
	}

	result := agentgate.DecisionResult{
		Decision: agentgate.DecisionAllow,
		Risk:     10,
	}
	if cfgErr != nil || policyErr != nil {
		result.PolicyName = "fail-open"
		result.Suggestion = "AgentGate MCP proxy failed open due to config/policy issue."
		return ctx, result, cfg.Mode, fmt.Errorf("config or policy load warning")
	}

	result = agentgate.EvaluatePolicies(paths, cfg, policies, ctx)
	return ctx, result, cfg.Mode, nil
}

func classifyMCPAction(toolName string) string {
	t := strings.ToLower(toolName)
	switch {
	case strings.Contains(t, "delete") || strings.Contains(t, "drop") ||
		strings.Contains(t, "remove") || strings.Contains(t, "truncate") ||
		strings.Contains(t, "destroy") || strings.Contains(t, "purge"):
		return "destructive"
	case strings.Contains(t, "write") || strings.Contains(t, "create") ||
		strings.Contains(t, "update") || strings.Contains(t, "insert") ||
		strings.Contains(t, "put") || strings.Contains(t, "set") ||
		strings.Contains(t, "execute") || strings.Contains(t, "run"):
		return "write"
	case strings.Contains(t, "read") || strings.Contains(t, "get") ||
		strings.Contains(t, "list") || strings.Contains(t, "describe") ||
		strings.Contains(t, "search") || strings.Contains(t, "find"):
		return "read"
	default:
		return "other"
	}
}
