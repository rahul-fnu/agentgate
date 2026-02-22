package agentgate

import (
	"encoding/json"
	"fmt"
	"os"
)

func cmdExplain(paths Paths, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: agentgate explain <tool> -- <args...>")
		return 1
	}
	tool := args[0]
	raw := args[1:]
	if len(raw) > 0 && raw[0] == "--" {
		raw = raw[1:]
	}

	cwd, _ := os.Getwd()
	ctx, result, mode, warnErr := EvaluateCommand(paths, tool, raw, cwd, IsInteractive())

	effective := result.Decision
	if mode == ModeObserve && (result.Decision == DecisionDeny || result.Decision == DecisionConfirm) {
		effective = DecisionAllow
	}

	payload := map[string]any{
		"tool":               tool,
		"raw_command":        ctx.RawCommand,
		"mode":               mode,
		"decision":           result.Decision,
		"effective_decision": effective,
		"policy":             result.PolicyName,
		"risk":               result.Risk,
		"suggestion":         result.Suggestion,
		"environment":        ctx.Environment,
		"environment_reason": ctx.EnvReason,
		"parse_status":       ctx.ParseStatus,
		"action":             ctx.Action,
		"action_type":        ctx.ActionType,
		"resource":           ctx.Resource,
		"resource_name":      ctx.ResourceName,
		"namespace":          ctx.Namespace,
		"flags":              ctx.Flags,
		"working_dir":        ctx.WorkingDir,
	}
	if warnErr != nil {
		payload["warning"] = warnErr.Error()
	}

	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate explain: %v\n", err)
		return 1
	}
	fmt.Println(string(b))
	return 0
}
