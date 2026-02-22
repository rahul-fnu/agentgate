package agentgate

import "fmt"

func EvaluateCommand(paths Paths, tool string, args []string, cwd string, interactive bool) (CommandContext, DecisionResult, Mode, error) {
	cfg, cfgErr := LoadConfig(paths)
	if cfgErr != nil {
		cfg = DefaultConfig()
	}
	policies, policyErr := LoadPolicies(paths)
	if policyErr != nil {
		policies = nil
	}

	ctx := ParseCommand(tool, args, cwd, interactive)
	DetectEnvironment(paths, cfg, &ctx)

	result := DecisionResult{
		Decision:   DecisionAllow,
		PolicyName: "",
		Suggestion: "",
		Risk:       baseRisk(ctx),
	}
	if cfgErr != nil || policyErr != nil {
		result.PolicyName = "fail-open"
		result.Suggestion = "AgentGate failed open due to configuration/policy parse issue."
		result.Decision = DecisionAllow
		result.Risk = scoreRisk(ctx, DecisionWarn)
		return ctx, result, cfg.Mode, fmt.Errorf("configuration or policy load warning")
	}

	result = EvaluatePolicies(paths, cfg, policies, ctx)
	return ctx, result, cfg.Mode, nil
}
