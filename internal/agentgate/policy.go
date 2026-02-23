package agentgate

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
)

func EvaluatePolicies(paths Paths, cfg Config, policies []Policy, ctx CommandContext) DecisionResult {
	matches := make([]Policy, 0, len(policies))
	for _, p := range policies {
		if !policyMatchesContext(p.Match, ctx) {
			continue
		}
		if !evaluateRule(paths, p, ctx) {
			continue
		}
		matches = append(matches, p)
	}

	result := DecisionResult{
		Decision: DecisionAllow,
		Risk:     baseRisk(ctx),
	}
	if len(matches) > 0 {
		best := matches[0]
		for _, p := range matches[1:] {
			if p.Priority > best.Priority {
				best = p
				continue
			}
			if p.Priority == best.Priority && restrictiveness(p.Decision) > restrictiveness(best.Decision) {
				best = p
			}
		}
		result.Decision = best.Decision
		result.PolicyName = best.Name
		result.Suggestion = best.Suggestion
		result.Risk = scoreRisk(ctx, result.Decision)
	}

	if ctx.Environment == "unknown" && cfg.UnknownEnvDefaultDecision != "" {
		if restrictiveness(cfg.UnknownEnvDefaultDecision) > restrictiveness(result.Decision) {
			result.Decision = cfg.UnknownEnvDefaultDecision
			result.PolicyName = "unknown-env-default"
			if result.Suggestion == "" {
				result.Suggestion = "Environment is unknown; action elevated by default policy."
			}
			result.Risk = scoreRisk(ctx, result.Decision)
		}
	}
	return result
}

func evaluateRule(paths Paths, policy Policy, ctx CommandContext) bool {
	if policy.RateLimit != nil {
		window := ParseWindow(policy.RateLimit.Window, 5*time.Minute)
		count := countHistorySince(paths, time.Now().Add(-window), func(r HistoryRecord) bool {
			if r.Env != ctx.Environment {
				return false
			}
			return r.ActionType == "write" || r.ActionType == "destructive"
		})
		return count >= policy.RateLimit.Limit
	}
	if policy.RequirePlan != nil {
		if ctx.Tool != "terraform" || ctx.Action != "apply" {
			return false
		}
		window := ParseWindow(policy.RequirePlan.Window, 2*time.Hour)
		return !hasRecentHistory(paths, time.Now().Add(-window), func(r HistoryRecord) bool {
			return r.Tool == "terraform" &&
				r.Action == "plan" &&
				r.WorkingDir == ctx.WorkingDir &&
				r.Env == ctx.Environment &&
				r.Decision != DecisionDeny
		})
	}
	return true
}

func policyMatchesContext(match PolicyMatch, ctx CommandContext) bool {
	if len(match.Tool) > 0 && !anyMatch(match.Tool, ctx.Tool) {
		return false
	}
	if len(match.Environment) > 0 && !anyMatch(match.Environment, ctx.Environment) {
		return false
	}
	if len(match.Action) > 0 && !anyMatch(match.Action, ctx.Action) {
		return false
	}
	if len(match.ActionType) > 0 && !anyMatch(match.ActionType, ctx.ActionType) {
		return false
	}
	if len(match.Resource) > 0 && !anyMatch(match.Resource, ctx.Resource) {
		return false
	}
	if len(match.ResourceName) > 0 && !anyMatch(match.ResourceName, ctx.ResourceName) {
		return false
	}
	if len(match.Namespace) > 0 && !anyMatch(match.Namespace, ctx.Namespace) {
		return false
	}
	if len(match.Flags) > 0 {
		ok := false
		for _, expected := range match.Flags {
			for k, v := range ctx.Flags {
				if MatchPattern(expected, k) || MatchPattern(expected, v) || MatchPattern(expected, "--"+k+"="+v) {
					ok = true
					break
				}
			}
			if ok {
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(match.RawContains) > 0 && !anyMatch(match.RawContains, ctx.RawCommand) {
		return false
	}
	return true
}

func anyMatch(patterns []string, value string) bool {
	for _, p := range patterns {
		if MatchPattern(p, value) {
			return true
		}
	}
	return false
}

func baseRisk(ctx CommandContext) int {
	risk := 10
	switch ctx.ActionType {
	case "read":
		risk += 5
	case "write":
		risk += 30
	case "destructive":
		risk += 55
	default:
		risk += 15
	}
	if ctx.Environment == "production" {
		risk += 20
	}
	if ctx.Environment == "unknown" {
		risk += 15
	}
	if risk > 99 {
		return 99
	}
	return risk
}

func scoreRisk(ctx CommandContext, d Decision) int {
	risk := baseRisk(ctx)
	switch d {
	case DecisionWarn:
		risk += 5
	case DecisionConfirm:
		risk += 15
	case DecisionDeny:
		risk += 25
	}
	if risk > 99 {
		return 99
	}
	return risk
}

func countHistorySince(paths Paths, since time.Time, match func(HistoryRecord) bool) int {
	count := 0
	_ = scanHistory(paths, since, func(r HistoryRecord) {
		if match(r) {
			count++
		}
	})
	return count
}

func hasRecentHistory(paths Paths, since time.Time, match func(HistoryRecord) bool) bool {
	found := false
	_ = scanHistory(paths, since, func(r HistoryRecord) {
		if found {
			return
		}
		if match(r) {
			found = true
		}
	})
	return found
}

func scanHistory(paths Paths, since time.Time, fn func(HistoryRecord)) error {
	f, err := os.Open(paths.EventsPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev StartEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Phase != "start" {
			continue
		}
		if ev.TS.Before(since) {
			continue
		}
		fn(HistoryRecord{
			TS:         ev.TS,
			ID:         ev.ID,
			Tool:       ev.Tool,
			Action:     ev.Action,
			ActionType: ev.ActionType,
			Env:        ev.Env,
			WorkingDir: ev.WorkingDir,
			Decision:   ev.Decision,
		})
	}
	return nil
}
