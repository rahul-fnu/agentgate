package agentgate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	dir := t.TempDir()
	return Paths{
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

func TestPolicyMatchesContext_ToolMatch(t *testing.T) {
	match := PolicyMatch{Tool: []string{"kubectl"}}
	ctx := CommandContext{Tool: "kubectl", ActionType: "read"}
	if !policyMatchesContext(match, ctx) {
		t.Error("should match tool kubectl")
	}
	ctx.Tool = "terraform"
	if policyMatchesContext(match, ctx) {
		t.Error("should not match tool terraform")
	}
}

func TestPolicyMatchesContext_Environment(t *testing.T) {
	match := PolicyMatch{Environment: []string{"production"}}
	ctx := CommandContext{Environment: "production", ActionType: "read"}
	if !policyMatchesContext(match, ctx) {
		t.Error("should match production")
	}
	ctx.Environment = "staging"
	if policyMatchesContext(match, ctx) {
		t.Error("should not match staging")
	}
}

func TestPolicyMatchesContext_Action(t *testing.T) {
	match := PolicyMatch{Action: []string{"delete", "destroy"}}
	ctx := CommandContext{Action: "delete", ActionType: "destructive"}
	if !policyMatchesContext(match, ctx) {
		t.Error("should match delete")
	}
	ctx.Action = "get"
	if policyMatchesContext(match, ctx) {
		t.Error("should not match get")
	}
}

func TestPolicyMatchesContext_ActionType(t *testing.T) {
	match := PolicyMatch{ActionType: []string{"destructive"}}
	ctx := CommandContext{ActionType: "destructive"}
	if !policyMatchesContext(match, ctx) {
		t.Error("should match destructive")
	}
	ctx.ActionType = "read"
	if policyMatchesContext(match, ctx) {
		t.Error("should not match read")
	}
}

func TestPolicyMatchesContext_Resource(t *testing.T) {
	match := PolicyMatch{Resource: []string{"namespace", "namespaces"}}
	ctx := CommandContext{Resource: "namespace", ActionType: "read"}
	if !policyMatchesContext(match, ctx) {
		t.Error("should match namespace")
	}
}

func TestPolicyMatchesContext_ResourceName(t *testing.T) {
	match := PolicyMatch{ResourceName: []string{"main", "master"}}
	ctx := CommandContext{ResourceName: "main", ActionType: "read"}
	if !policyMatchesContext(match, ctx) {
		t.Error("should match main")
	}
	ctx.ResourceName = "feature-branch"
	if policyMatchesContext(match, ctx) {
		t.Error("should not match feature-branch")
	}
}

func TestPolicyMatchesContext_Namespace(t *testing.T) {
	match := PolicyMatch{Namespace: []string{"default"}}
	ctx := CommandContext{Namespace: "default", ActionType: "read"}
	if !policyMatchesContext(match, ctx) {
		t.Error("should match default")
	}
}

func TestPolicyMatchesContext_Flags(t *testing.T) {
	match := PolicyMatch{Flags: []string{"force"}}
	ctx := CommandContext{Flags: map[string]string{"force": "true"}, ActionType: "read"}
	if !policyMatchesContext(match, ctx) {
		t.Error("should match force flag")
	}
	ctx.Flags = map[string]string{"verbose": "true"}
	if policyMatchesContext(match, ctx) {
		t.Error("should not match verbose flag")
	}
}

func TestPolicyMatchesContext_RawContains(t *testing.T) {
	match := PolicyMatch{RawContains: []string{" main"}}
	ctx := CommandContext{RawCommand: "git reset --hard main", ActionType: "read"}
	if !policyMatchesContext(match, ctx) {
		t.Error("should match raw_contains ' main'")
	}
	ctx.RawCommand = "git reset --hard feat"
	if policyMatchesContext(match, ctx) {
		t.Error("should not match raw_contains when absent")
	}
}

func TestPolicyMatchesContext_WildcardPattern(t *testing.T) {
	match := PolicyMatch{Environment: []string{"*prod*"}}
	ctx := CommandContext{Environment: "production", ActionType: "read"}
	if !policyMatchesContext(match, ctx) {
		t.Error("should match *prod* against production")
	}
	ctx.Environment = "staging"
	if policyMatchesContext(match, ctx) {
		t.Error("should not match *prod* against staging")
	}
}

func TestPolicyMatchesContext_EmptyMatch(t *testing.T) {
	match := PolicyMatch{}
	ctx := CommandContext{Tool: "kubectl", Action: "get", ActionType: "read"}
	if !policyMatchesContext(match, ctx) {
		t.Error("empty match should match everything")
	}
}

func TestEvaluatePolicies_HighestPriorityWins(t *testing.T) {
	paths := testPaths(t)
	cfg := DefaultConfig()
	policies := []Policy{
		{Name: "low", Priority: 50, Decision: DecisionWarn, Match: PolicyMatch{}},
		{Name: "high", Priority: 100, Decision: DecisionDeny, Match: PolicyMatch{}},
	}
	ctx := CommandContext{Tool: "kubectl", Action: "delete", ActionType: "destructive", Environment: "production"}
	result := EvaluatePolicies(paths, cfg, policies, ctx)
	if result.PolicyName != "high" {
		t.Errorf("expected 'high', got %q", result.PolicyName)
	}
	if result.Decision != DecisionDeny {
		t.Errorf("expected deny, got %q", result.Decision)
	}
}

func TestEvaluatePolicies_TieBreakByRestrictiveness(t *testing.T) {
	paths := testPaths(t)
	cfg := DefaultConfig()
	policies := []Policy{
		{Name: "warn", Priority: 100, Decision: DecisionWarn, Match: PolicyMatch{}},
		{Name: "deny", Priority: 100, Decision: DecisionDeny, Match: PolicyMatch{}},
	}
	ctx := CommandContext{Tool: "kubectl", Action: "delete", ActionType: "destructive", Environment: "production"}
	result := EvaluatePolicies(paths, cfg, policies, ctx)
	if result.PolicyName != "deny" {
		t.Errorf("should tiebreak to deny, got %q", result.PolicyName)
	}
}

func TestEvaluatePolicies_NoMatch(t *testing.T) {
	paths := testPaths(t)
	cfg := DefaultConfig()
	policies := []Policy{
		{Name: "kubectl-only", Priority: 100, Decision: DecisionDeny, Match: PolicyMatch{Tool: []string{"kubectl"}}},
	}
	ctx := CommandContext{Tool: "terraform", Action: "apply", ActionType: "write"}
	result := EvaluatePolicies(paths, cfg, policies, ctx)
	if result.Decision != DecisionAllow {
		t.Errorf("no match should allow, got %q", result.Decision)
	}
}

func TestEvaluatePolicies_UnknownEnvDefault(t *testing.T) {
	paths := testPaths(t)
	cfg := DefaultConfig()
	cfg.UnknownEnvDefaultDecision = DecisionConfirm
	policies := []Policy{}
	ctx := CommandContext{Tool: "kubectl", Action: "delete", ActionType: "destructive", Environment: "unknown"}
	result := EvaluatePolicies(paths, cfg, policies, ctx)
	if result.Decision != DecisionConfirm {
		t.Errorf("expected confirm for unknown env, got %q", result.Decision)
	}
	if result.PolicyName != "unknown-env-default" {
		t.Errorf("expected unknown-env-default, got %q", result.PolicyName)
	}
}

func TestBaseRisk(t *testing.T) {
	tests := []struct {
		name     string
		ctx      CommandContext
		wantRisk int
	}{
		{"read prod", CommandContext{ActionType: "read", Environment: "production"}, 35},
		{"write prod", CommandContext{ActionType: "write", Environment: "production"}, 60},
		{"destructive prod", CommandContext{ActionType: "destructive", Environment: "production"}, 85},
		{"read dev", CommandContext{ActionType: "read", Environment: "dev"}, 15},
		{"other unknown", CommandContext{ActionType: "other", Environment: "unknown"}, 40},
		{"destructive unknown", CommandContext{ActionType: "destructive", Environment: "unknown"}, 80},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := baseRisk(tt.ctx)
			if got != tt.wantRisk {
				t.Errorf("baseRisk = %d, want %d", got, tt.wantRisk)
			}
		})
	}
}

func TestScoreRisk(t *testing.T) {
	ctx := CommandContext{ActionType: "destructive", Environment: "production"} // base = 85
	if scoreRisk(ctx, DecisionDeny) != 99 {
		t.Errorf("destructive+prod+deny should cap at 99, got %d", scoreRisk(ctx, DecisionDeny))
	}
	if scoreRisk(ctx, DecisionWarn) != 90 {
		t.Errorf("destructive+prod+warn should be 90, got %d", scoreRisk(ctx, DecisionWarn))
	}

	ctx2 := CommandContext{ActionType: "read", Environment: "dev"} // base = 15
	if scoreRisk(ctx2, DecisionAllow) != 15 {
		t.Errorf("read+dev+allow should be 15, got %d", scoreRisk(ctx2, DecisionAllow))
	}
}

func TestEvaluatePolicies_RateLimit(t *testing.T) {
	paths := testPaths(t)
	cfg := DefaultConfig()

	policy := Policy{
		Name:     "rate-limit",
		Priority: 100,
		Decision: DecisionDeny,
		Match:    PolicyMatch{Environment: []string{"production"}},
		RateLimit: &RateLimitRule{
			Limit:  3,
			Window: "5m",
		},
	}

	// Write 3 start events
	for i := 0; i < 3; i++ {
		ev := StartEvent{
			TS:         time.Now().Add(-time.Duration(i) * time.Minute),
			Phase:      "start",
			Tool:       "kubectl",
			Action:     "apply",
			ActionType: "write",
			Env:        "production",
		}
		b, _ := json.Marshal(ev)
		f, _ := os.OpenFile(paths.EventsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		f.Write(append(b, '\n'))
		f.Close()
	}

	ctx := CommandContext{Tool: "kubectl", Action: "apply", ActionType: "write", Environment: "production"}
	result := EvaluatePolicies(paths, cfg, []Policy{policy}, ctx)
	if result.Decision != DecisionDeny {
		t.Errorf("rate limit should trigger deny, got %q", result.Decision)
	}
}

func TestEvaluatePolicies_RequirePlan(t *testing.T) {
	paths := testPaths(t)
	cfg := DefaultConfig()

	policy := Policy{
		Name:     "require-plan",
		Priority: 100,
		Decision: DecisionDeny,
		Match:    PolicyMatch{Tool: []string{"terraform"}, Action: []string{"apply"}},
		RequirePlan: &RequirePlanRule{
			Window: "2h",
		},
	}

	// No plan in history -> should deny
	ctx := CommandContext{Tool: "terraform", Action: "apply", ActionType: "write", Environment: "production", WorkingDir: "/opt/tf"}
	result := EvaluatePolicies(paths, cfg, []Policy{policy}, ctx)
	if result.Decision != DecisionDeny {
		t.Errorf("require-plan without recent plan should deny, got %q", result.Decision)
	}

	// Add a recent plan event
	planEv := StartEvent{
		TS:         time.Now().Add(-30 * time.Minute),
		Phase:      "start",
		Tool:       "terraform",
		Action:     "plan",
		ActionType: "other",
		Env:        "production",
		WorkingDir: "/opt/tf",
		Decision:   DecisionAllow,
	}
	b, _ := json.Marshal(planEv)
	os.WriteFile(paths.EventsPath, append(b, '\n'), 0o644)

	result = EvaluatePolicies(paths, cfg, []Policy{policy}, ctx)
	if result.Decision != DecisionAllow {
		t.Errorf("require-plan with recent plan should allow, got %q", result.Decision)
	}
}

func TestRestrictiveness(t *testing.T) {
	if restrictiveness(DecisionDeny) <= restrictiveness(DecisionConfirm) {
		t.Error("deny should be more restrictive than confirm")
	}
	if restrictiveness(DecisionConfirm) <= restrictiveness(DecisionWarn) {
		t.Error("confirm should be more restrictive than warn")
	}
	if restrictiveness(DecisionWarn) <= restrictiveness(DecisionAllow) {
		t.Error("warn should be more restrictive than allow")
	}
}
