package agentgate

import (
	"os"
	"testing"
)

func TestEvaluateCommand_BasicAllow(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	// No policies file → unknown env gets default decision (warn)
	ctx, result, mode, err := EvaluateCommand(paths, "kubectl", []string{"get", "pods"}, "/tmp", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctx.Action != "get" {
		t.Errorf("action = %q, want get", ctx.Action)
	}
	// Default config has UnknownEnvDefaultDecision=warn, and no env detection → unknown env → warn
	if result.Decision != DecisionWarn {
		t.Errorf("decision = %q, want warn (unknown env default)", result.Decision)
	}
	if mode != ModeObserve {
		t.Errorf("mode = %q, want observe", mode)
	}
}

func TestEvaluateCommand_WithPolicies(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	// Write a simple policy
	policyYAML := `policies:
  - name: deny-all-delete
    priority: 100
    decision: deny
    suggestion: "No deletes"
    match:
      action: [delete]
`
	os.WriteFile(paths.PoliciesPath, []byte(policyYAML), 0o644)

	_, result, _, err := EvaluateCommand(paths, "kubectl", []string{"delete", "pod", "x"}, "/tmp", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != DecisionDeny {
		t.Errorf("decision = %q, want deny", result.Decision)
	}
	if result.PolicyName != "deny-all-delete" {
		t.Errorf("policy = %q, want deny-all-delete", result.PolicyName)
	}
}

func TestEvaluateCommand_FailOpen(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	// Write malformed policies
	os.WriteFile(paths.PoliciesPath, []byte("{{invalid"), 0o644)

	_, result, _, err := EvaluateCommand(paths, "kubectl", []string{"delete", "pod", "x"}, "/tmp", false)
	if err == nil {
		t.Error("should return error for malformed config")
	}
	if result.Decision != DecisionAllow {
		t.Errorf("should fail open, got %q", result.Decision)
	}
	if result.PolicyName != "fail-open" {
		t.Errorf("policy = %q, want fail-open", result.PolicyName)
	}
}

func TestEvaluateCommand_StarterPolicies(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)
	os.WriteFile(paths.PoliciesPath, []byte(starterPoliciesYAML), 0o644)

	// Write config with enforce mode
	SaveConfig(paths, Config{
		Mode:                      ModeEnforce,
		UnknownEnvDefaultDecision: DecisionWarn,
		EnvironmentPatterns:       DefaultConfig().EnvironmentPatterns,
	})

	// Test: git force push to main → should deny
	_, result, _, _ := EvaluateCommand(paths, "git", []string{"push", "--force", "origin", "main"}, "/tmp", false)
	if result.Decision != DecisionDeny {
		t.Errorf("git force push to main: decision = %q, want deny", result.Decision)
	}

	// Test: docker system prune --all → should deny
	_, result, _, _ = EvaluateCommand(paths, "docker", []string{"system", "prune", "--all"}, "/tmp", false)
	if result.Decision != DecisionDeny {
		t.Errorf("docker system prune --all: decision = %q, want deny", result.Decision)
	}
}
