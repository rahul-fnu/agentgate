package agentgate

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestE2E_InitAndStatus tests the full init → status pipeline.
func TestE2E_InitAndStatus(t *testing.T) {
	paths := testPaths(t)

	// EnsureDirs + EnsureConfig
	if err := EnsureDirs(paths); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	cfg, err := EnsureConfig(paths)
	if err != nil {
		t.Fatalf("EnsureConfig: %v", err)
	}
	if cfg.Mode != ModeObserve {
		t.Errorf("initial mode = %q, want observe", cfg.Mode)
	}

	// Write starter policies
	if err := os.WriteFile(paths.PoliciesPath, []byte(starterPoliciesYAML), 0o644); err != nil {
		t.Fatalf("write policies: %v", err)
	}

	// Verify policies load
	policies, err := LoadPolicies(paths)
	if err != nil {
		t.Fatalf("LoadPolicies: %v", err)
	}
	if len(policies) < 20 {
		t.Errorf("expected 20+ policies, got %d", len(policies))
	}

	// Switch to enforce
	cfg.Mode = ModeEnforce
	if err := SaveConfig(paths, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	loaded, _ := LoadConfig(paths)
	if loaded.Mode != ModeEnforce {
		t.Errorf("mode after switch = %q, want enforce", loaded.Mode)
	}

	// Verify bin dir exists
	if _, err := os.Stat(paths.BinDir); err != nil {
		t.Errorf("bin dir should exist: %v", err)
	}
}

// TestE2E_ExplainPipeline tests the explain command logic end-to-end.
func TestE2E_ExplainPipeline(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)
	os.WriteFile(paths.PoliciesPath, []byte(starterPoliciesYAML), 0o644)

	// Configure enforce mode with default patterns
	SaveConfig(paths, Config{
		Mode:                      ModeEnforce,
		UnknownEnvDefaultDecision: DecisionWarn,
		EnvironmentPatterns:       DefaultConfig().EnvironmentPatterns,
	})

	tests := []struct {
		name         string
		tool         string
		args         []string
		wantDecision Decision
		wantAction   string
	}{
		{
			name:         "git force push to main",
			tool:         "git",
			args:         []string{"push", "--force", "origin", "main"},
			wantDecision: DecisionDeny,
			wantAction:   "push-force",
		},
		{
			name:         "docker system prune --all",
			tool:         "docker",
			args:         []string{"system", "prune", "--all"},
			wantDecision: DecisionDeny,
			wantAction:   "system-prune",
		},
		{
			name:         "git normal push",
			tool:         "git",
			args:         []string{"push", "origin", "feature"},
			wantDecision: DecisionWarn, // unknown env default
			wantAction:   "push",
		},
		{
			name:         "kubectl get pods (read)",
			tool:         "kubectl",
			args:         []string{"get", "pods"},
			wantDecision: DecisionWarn, // unknown env default
			wantAction:   "get",
		},
		{
			name:         "terraform plan",
			tool:         "terraform",
			args:         []string{"plan"},
			wantDecision: DecisionWarn, // unknown env default
			wantAction:   "plan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, result, _, _ := EvaluateCommand(paths, tt.tool, tt.args, "/tmp", false)
			if result.Decision != tt.wantDecision {
				t.Errorf("decision = %q, want %q (policy=%s)", result.Decision, tt.wantDecision, result.PolicyName)
			}
			if ctx.Action != tt.wantAction {
				t.Errorf("action = %q, want %q", ctx.Action, tt.wantAction)
			}
		})
	}
}

// TestE2E_EventLoggingPipeline tests the full event logging lifecycle.
func TestE2E_EventLoggingPipeline(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	commandID := NewCommandID()
	cmdHash := CommandHash("kubectl", "kubectl delete ns prod", "production")

	// Log start event
	startEv := StartEvent{
		TS:       time.Now().UTC(),
		ID:       commandID,
		Phase:    "start",
		Mode:     ModeEnforce,
		Tool:     "kubectl",
		Cmd:      "kubectl delete ns prod",
		CmdHash:  cmdHash,
		Env:      "production",
		Decision: DecisionDeny,
		Policy:   "no-prod-namespace-delete",
		Risk:     99,
		User:     "testuser",
		Parse:    ParseStatusParsed,
		Source:   "cli",
	}
	if err := AppendEvent(paths, startEv); err != nil {
		t.Fatalf("AppendEvent start: %v", err)
	}

	// Log end event
	endEv := EndEvent{
		TS:         time.Now().UTC(),
		ID:         commandID,
		Phase:      "end",
		ExitCode:   77,
		DurationMS: 15,
		Outcome:    "blocked-policy",
	}
	if err := AppendEvent(paths, endEv); err != nil {
		t.Fatalf("AppendEvent end: %v", err)
	}

	// Log history
	histRec := HistoryRecord{
		TS:         time.Now().UTC(),
		ID:         commandID,
		Tool:       "kubectl",
		Action:     "delete",
		ActionType: "destructive",
		Env:        "production",
		Decision:   DecisionDeny,
	}
	if err := AppendHistory(paths, histRec); err != nil {
		t.Fatalf("AppendHistory: %v", err)
	}

	// Verify event can be found
	found, err := FindStartEventByID(paths, commandID)
	if err != nil {
		t.Fatalf("FindStartEventByID: %v", err)
	}
	if found == nil {
		t.Fatal("should find start event")
	}
	if found.Decision != DecisionDeny {
		t.Errorf("found decision = %q, want deny", found.Decision)
	}
	if found.Source != "cli" {
		t.Errorf("found source = %q, want cli", found.Source)
	}

	// Verify ReadLastLines returns events
	lines, err := ReadLastLines(paths.EventsPath, 10)
	if err != nil {
		t.Fatalf("ReadLastLines: %v", err)
	}
	if len(lines) < 1 {
		t.Errorf("expected at least 1 event line, got %d", len(lines))
	}

	// Verify metrics collection
	snapshot, err := CollectMetrics(paths, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("CollectMetrics: %v", err)
	}
	if snapshot.CommandsTotal != 1 {
		t.Errorf("commands_total = %d, want 1", snapshot.CommandsTotal)
	}
	if snapshot.InFlightTotal != 0 {
		t.Errorf("inflight = %d, want 0", snapshot.InFlightTotal)
	}
}

// TestE2E_BypassPipeline tests the full bypass lifecycle.
func TestE2E_BypassPipeline(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)
	os.WriteFile(paths.PoliciesPath, []byte(starterPoliciesYAML), 0o644)

	// Step 1: A command gets denied
	cmdHash := CommandHash("kubectl", "kubectl delete ns prod", "production")
	commandID := "ag_bypass_test"

	// Log the denied event (normally done by intercept)
	startEv := StartEvent{
		TS:       time.Now().UTC(),
		ID:       commandID,
		Phase:    "start",
		Mode:     ModeEnforce,
		Tool:     "kubectl",
		Cmd:      "kubectl delete ns prod",
		CmdHash:  cmdHash,
		Env:      "production",
		Decision: DecisionDeny,
		Policy:   "no-prod-namespace-delete",
		Risk:     99,
	}
	AppendEvent(paths, startEv)

	// Step 2: User issues a bypass token
	token, err := IssueBypass(paths, commandID, cmdHash, 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueBypass: %v", err)
	}
	if token.TokenID == "" {
		t.Fatal("token ID should not be empty")
	}

	// Step 3: Next command consumes the bypass
	tokenID, ok, err := ConsumeValidBypass(paths, cmdHash, time.Now())
	if err != nil {
		t.Fatalf("ConsumeValidBypass: %v", err)
	}
	if !ok {
		t.Fatal("bypass should be consumable")
	}
	if tokenID != token.TokenID {
		t.Errorf("consumed token = %q, want %q", tokenID, token.TokenID)
	}

	// Step 4: Second attempt fails (already consumed)
	_, ok, _ = ConsumeValidBypass(paths, cmdHash, time.Now())
	if ok {
		t.Error("double consume should fail")
	}
}

// TestE2E_StarterPoliciesCoverage verifies key starter policies work correctly.
func TestE2E_StarterPoliciesCoverage(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)
	os.WriteFile(paths.PoliciesPath, []byte(starterPoliciesYAML), 0o644)
	SaveConfig(paths, Config{
		Mode:                      ModeEnforce,
		UnknownEnvDefaultDecision: "",
		EnvironmentPatterns:       DefaultConfig().EnvironmentPatterns,
	})

	tests := []struct {
		name         string
		tool         string
		args         []string
		env          string
		wantDecision Decision
		wantPolicy   string
	}{
		{
			name:         "deny git force-push to main",
			tool:         "git",
			args:         []string{"push", "--force", "origin", "main"},
			wantDecision: DecisionDeny,
			wantPolicy:   "deny-git-force-push-protected",
		},
		{
			name:         "deny docker system prune --all",
			tool:         "docker",
			args:         []string{"system", "prune", "--all"},
			wantDecision: DecisionDeny,
			wantPolicy:   "deny-docker-system-prune-destructive",
		},
		{
			name:         "confirm git force-push to feature branch",
			tool:         "git",
			args:         []string{"push", "--force", "origin", "feature-x"},
			wantDecision: DecisionConfirm,
			wantPolicy:   "confirm-git-force-push",
		},
		{
			name:         "confirm docker compose down --volumes",
			tool:         "docker",
			args:         []string{"compose", "down", "--volumes"},
			wantDecision: DecisionConfirm,
			wantPolicy:   "confirm-docker-compose-down-volumes",
		},
		{
			name:         "confirm docker rm -f",
			tool:         "docker",
			args:         []string{"rm", "-f", "container1"},
			wantDecision: DecisionConfirm,
			wantPolicy:   "confirm-unknown-destructive",
		},
		{
			name:         "confirm git clean -f",
			tool:         "git",
			args:         []string{"clean", "-f"},
			wantDecision: DecisionConfirm,
			wantPolicy:   "confirm-git-clean-force",
		},
		{
			name:         "warn git status (unknown env)",
			tool:         "git",
			args:         []string{"status"},
			wantDecision: DecisionWarn,
			wantPolicy:   "unknown-env-default",
		},
		{
			name:         "warn docker ps (unknown env)",
			tool:         "docker",
			args:         []string{"ps"},
			wantDecision: DecisionWarn,
			wantPolicy:   "unknown-env-default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommand(tt.tool, tt.args, "/tmp", false)
			// Set environment if specified, otherwise keep unknown
			if tt.env != "" {
				ctx.Environment = tt.env
			} else {
				ctx.Environment = "unknown"
			}

			cfg, _ := LoadConfig(paths)
			policies, _ := LoadPolicies(paths)
			result := EvaluatePolicies(paths, cfg, policies, ctx)

			if result.Decision != tt.wantDecision {
				t.Errorf("decision = %q, want %q (policy=%s)", result.Decision, tt.wantDecision, result.PolicyName)
			}
			if tt.wantPolicy != "" && result.PolicyName != tt.wantPolicy {
				t.Errorf("policy = %q, want %q", result.PolicyName, tt.wantPolicy)
			}
		})
	}
}

// TestE2E_ProductionPolicies verifies production-specific policies with simulated env.
func TestE2E_ProductionPolicies(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)
	os.WriteFile(paths.PoliciesPath, []byte(starterPoliciesYAML), 0o644)
	SaveConfig(paths, Config{
		Mode:                      ModeEnforce,
		UnknownEnvDefaultDecision: "",
		EnvironmentPatterns:       DefaultConfig().EnvironmentPatterns,
	})

	cfg, _ := LoadConfig(paths)
	policies, _ := LoadPolicies(paths)

	tests := []struct {
		name         string
		tool         string
		args         []string
		wantDecision Decision
		wantPolicy   string
	}{
		{
			name:         "deny kubectl delete namespace in prod",
			tool:         "kubectl",
			args:         []string{"delete", "namespace", "important"},
			wantDecision: DecisionDeny,
			wantPolicy:   "no-prod-namespace-delete",
		},
		{
			name:         "deny terraform destroy in prod",
			tool:         "terraform",
			args:         []string{"destroy"},
			wantDecision: DecisionDeny,
			wantPolicy:   "no-prod-terraform-destroy",
		},
		{
			name:         "deny terraform auto-approve in prod",
			tool:         "terraform",
			args:         []string{"apply", "--auto-approve"},
			wantDecision: DecisionDeny,
			wantPolicy:   "no-prod-terraform-auto-approve",
		},
		{
			name:         "deny kubectl force-delete in prod",
			tool:         "kubectl",
			args:         []string{"delete", "pod", "mypod", "--force", "--grace-period=0"},
			wantDecision: DecisionDeny,
			wantPolicy:   "no-prod-force-delete",
		},
		{
			name:         "deny helm uninstall in prod",
			tool:         "helm",
			args:         []string{"uninstall", "myrelease"},
			wantDecision: DecisionDeny,
			wantPolicy:   "no-prod-helm-uninstall",
		},
		{
			name:         "deny aws ec2 terminate in prod",
			tool:         "aws",
			args:         []string{"ec2", "terminate-instances", "--instance-ids", "i-123"},
			wantDecision: DecisionDeny,
			wantPolicy:   "no-prod-aws-ec2-terminate",
		},
		{
			name:         "deny gcloud project delete in prod",
			tool:         "gcloud",
			args:         []string{"projects", "delete", "my-project"},
			wantDecision: DecisionDeny,
			wantPolicy:   "no-prod-gcloud-project-delete",
		},
		{
			name:         "warn kubectl apply in prod",
			tool:         "kubectl",
			args:         []string{"apply", "-f", "deployment.yaml"},
			wantDecision: DecisionWarn,
			wantPolicy:   "warn-prod-writes",
		},
		{
			name:         "confirm kubectl drain in prod",
			tool:         "kubectl",
			args:         []string{"drain", "node1"},
			wantDecision: DecisionDeny,
			wantPolicy:   "no-prod-node-drain",
		},
		{
			name:         "confirm scale to zero in prod",
			tool:         "kubectl",
			args:         []string{"scale", "deployment", "web", "--replicas=0"},
			wantDecision: DecisionConfirm,
			wantPolicy:   "confirm-prod-destructive",
		},
		{
			name:         "allow kubectl get in prod",
			tool:         "kubectl",
			args:         []string{"get", "pods"},
			wantDecision: DecisionAllow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommand(tt.tool, tt.args, "/tmp", false)
			ctx.Environment = "production"

			result := EvaluatePolicies(paths, cfg, policies, ctx)
			if result.Decision != tt.wantDecision {
				t.Errorf("decision = %q, want %q (policy=%s)", result.Decision, tt.wantDecision, result.PolicyName)
			}
			if tt.wantPolicy != "" && result.PolicyName != tt.wantPolicy {
				t.Errorf("policy = %q, want %q", result.PolicyName, tt.wantPolicy)
			}
		})
	}
}

// TestE2E_MetricsPipeline tests the full metrics collection and output pipeline.
func TestE2E_MetricsPipeline(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	now := time.Now().UTC()

	// Simulate a realistic event stream
	events := []any{
		// Denied kubectl delete in production
		StartEvent{TS: now.Add(-2 * time.Hour), ID: "ag_001", Phase: "start", Mode: ModeEnforce, Tool: "kubectl", Env: "production", Decision: DecisionDeny, Policy: "no-prod-namespace-delete", Parse: ParseStatusParsed},
		EndEvent{TS: now.Add(-2*time.Hour + time.Second), ID: "ag_001", Phase: "end", ExitCode: 77, Outcome: "blocked-policy"},
		// Allowed terraform plan in staging
		StartEvent{TS: now.Add(-1 * time.Hour), ID: "ag_002", Phase: "start", Mode: ModeEnforce, Tool: "terraform", Env: "staging", Decision: DecisionAllow, Parse: ParseStatusParsed},
		EndEvent{TS: now.Add(-1*time.Hour + 3*time.Second), ID: "ag_002", Phase: "end", ExitCode: 0, Outcome: "executed"},
		// Warned kubectl apply in production
		StartEvent{TS: now.Add(-30 * time.Minute), ID: "ag_003", Phase: "start", Mode: ModeEnforce, Tool: "kubectl", Env: "production", Decision: DecisionWarn, Policy: "warn-prod-writes", Parse: ParseStatusParsed},
		EndEvent{TS: now.Add(-30*time.Minute + time.Second), ID: "ag_003", Phase: "end", ExitCode: 0, Outcome: "executed"},
		// MCP proxy blocked delete_file
		StartEvent{TS: now.Add(-15 * time.Minute), ID: "ag_004", Phase: "start", Mode: ModeEnforce, Tool: "filesystem", Cmd: "mcp:filesystem/delete_file", Decision: DecisionDeny, Policy: "block-mcp-file-delete", Parse: ParseStatusParsed, Source: "mcp"},
		EndEvent{TS: now.Add(-15*time.Minute + time.Second), ID: "ag_004", Phase: "end", ExitCode: 0, Outcome: "blocked"},
	}

	var sb strings.Builder
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		sb.Write(b)
		sb.WriteByte('\n')
	}
	os.WriteFile(paths.EventsPath, []byte(sb.String()), 0o644)

	// Collect metrics for last 24h
	snapshot, err := CollectMetrics(paths, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("CollectMetrics: %v", err)
	}

	if snapshot.CommandsTotal != 4 {
		t.Errorf("commands_total = %d, want 4", snapshot.CommandsTotal)
	}
	if snapshot.InFlightTotal != 0 {
		t.Errorf("inflight = %d, want 0", snapshot.InFlightTotal)
	}

	// Verify blocked counts
	blockedKubectl := blockedMetricKey{Tool: "kubectl", Environment: "production", Policy: "no-prod-namespace-delete"}
	if snapshot.BlockedCounts[blockedKubectl] != 1 {
		t.Errorf("blocked kubectl = %d, want 1", snapshot.BlockedCounts[blockedKubectl])
	}
	blockedMCP := blockedMetricKey{Tool: "filesystem", Environment: "unknown", Policy: "block-mcp-file-delete"}
	if snapshot.BlockedCounts[blockedMCP] != 1 {
		t.Errorf("blocked mcp = %d, want 1", snapshot.BlockedCounts[blockedMCP])
	}

	// Verify allowed counts
	allowedTf := allowedMetricKey{Tool: "terraform", Environment: "staging", Outcome: "executed"}
	if snapshot.AllowedCounts[allowedTf] != 1 {
		t.Errorf("allowed terraform = %d, want 1", snapshot.AllowedCounts[allowedTf])
	}

	// Verify Prometheus output contains expected metrics
	var buf strings.Builder
	printPrometheusMetrics(&buf, snapshot)
	promOutput := buf.String()

	expectedStrings := []string{
		"agentgate_commands_total",
		"agentgate_commands_blocked_total",
		"agentgate_commands_allowed_total",
		"agentgate_parse_status_total",
		"agentgate_inflight_commands",
		"agentgate_metrics_generated_unix",
	}
	for _, s := range expectedStrings {
		if !strings.Contains(promOutput, s) {
			t.Errorf("prometheus output missing %q", s)
		}
	}

	// Verify JSON output is valid
	var jsonBuf strings.Builder
	printJSONMetrics(&jsonBuf, snapshot, "24h")
	var jsonOutput map[string]any
	if err := json.Unmarshal([]byte(jsonBuf.String()), &jsonOutput); err != nil {
		t.Errorf("JSON output is invalid: %v", err)
	}
	if jsonOutput["commands_total"].(float64) != 4 {
		t.Errorf("JSON commands_total = %v, want 4", jsonOutput["commands_total"])
	}
}

// TestE2E_RiskScoring tests risk scores for realistic scenarios.
func TestE2E_RiskScoring(t *testing.T) {
	tests := []struct {
		name   string
		ctx    CommandContext
		dec    Decision
		minRsk int
		maxRsk int
	}{
		{
			name:   "read in dev = low risk",
			ctx:    CommandContext{ActionType: "read", Environment: "dev"},
			dec:    DecisionAllow,
			minRsk: 10, maxRsk: 20,
		},
		{
			name:   "write in prod with warn = medium risk",
			ctx:    CommandContext{ActionType: "write", Environment: "production"},
			dec:    DecisionWarn,
			minRsk: 60, maxRsk: 70,
		},
		{
			name:   "destructive in prod with deny = max risk",
			ctx:    CommandContext{ActionType: "destructive", Environment: "production"},
			dec:    DecisionDeny,
			minRsk: 99, maxRsk: 99,
		},
		{
			name:   "other in unknown = moderate risk",
			ctx:    CommandContext{ActionType: "other", Environment: "unknown"},
			dec:    DecisionAllow,
			minRsk: 30, maxRsk: 50,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			risk := scoreRisk(tt.ctx, tt.dec)
			if risk < tt.minRsk || risk > tt.maxRsk {
				t.Errorf("risk = %d, want [%d, %d]", risk, tt.minRsk, tt.maxRsk)
			}
		})
	}
}

// TestE2E_ParseAllToolsComprehensive does comprehensive parser validation across all tools.
func TestE2E_ParseAllToolsComprehensive(t *testing.T) {
	// Ensure every supported tool can be parsed without panics
	for _, tool := range []string{"kubectl", "terraform", "helm", "aws", "gcloud", "git", "docker"} {
		t.Run(tool+"/empty", func(t *testing.T) {
			ctx := ParseCommand(tool, []string{}, "/tmp", false)
			if ctx.Tool != tool {
				t.Errorf("tool = %q, want %q", ctx.Tool, tool)
			}
		})
		t.Run(tool+"/single_arg", func(t *testing.T) {
			ctx := ParseCommand(tool, []string{"status"}, "/tmp", false)
			if ctx.Tool != tool {
				t.Errorf("tool = %q, want %q", ctx.Tool, tool)
			}
		})
		t.Run(tool+"/with_flags", func(t *testing.T) {
			ctx := ParseCommand(tool, []string{"--verbose", "--debug", "status"}, "/tmp", false)
			if ctx.Tool != tool {
				t.Errorf("tool = %q, want %q", ctx.Tool, tool)
			}
		})
	}

	// Ensure unknown tools don't panic
	ctx := ParseCommand("unknown-tool", []string{"foo", "bar", "--baz"}, "/tmp", true)
	if ctx.ParseStatus != ParseStatusUnknown {
		t.Errorf("unknown tool should get unknown status")
	}
	if ctx.ActionType != "other" {
		t.Errorf("unknown tool should get other action type")
	}
}

// TestE2E_PatternMatching tests pattern matching used throughout the policy engine.
func TestE2E_PatternMatching(t *testing.T) {
	tests := []struct {
		pattern string
		value   string
		want    bool
	}{
		// Exact match
		{"delete", "delete", true},
		{"production", "production", true},
		// Substring match
		{"delete", "force-delete", true},
		{"prod", "production", true},
		{"prod", "my-prod-cluster", true},
		// Wildcard match
		{"*prod*", "production", true},
		{"*prod*", "my-prod-cluster", true},
		{"*prod*", "staging", false},
		{"prod-*", "prod-east", true},
		{"prod-*", "staging-east", false},
		// Case insensitive
		{"DELETE", "delete", true},
		{"*PROD*", "production", true},
		// No match
		{"delete", "create", false},
		{"production", "staging", false},
		// Empty
		{"", "anything", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"_vs_"+tt.value, func(t *testing.T) {
			got := MatchPattern(tt.pattern, tt.value)
			if got != tt.want {
				t.Errorf("MatchPattern(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.want)
			}
		})
	}
}

// TestE2E_MultiPolicyInteraction tests how multiple policies interact.
func TestE2E_MultiPolicyInteraction(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	cfg := Config{
		Mode:                      ModeEnforce,
		UnknownEnvDefaultDecision: "",
		EnvironmentPatterns:       DefaultConfig().EnvironmentPatterns,
	}

	// Scenario: multiple overlapping policies
	policies := []Policy{
		{Name: "broad-warn", Priority: 50, Decision: DecisionWarn, Match: PolicyMatch{ActionType: []string{"destructive"}}},
		{Name: "prod-confirm", Priority: 80, Decision: DecisionConfirm, Match: PolicyMatch{ActionType: []string{"destructive"}, Environment: []string{"production"}}},
		{Name: "ns-deny", Priority: 100, Decision: DecisionDeny, Match: PolicyMatch{Tool: []string{"kubectl"}, Action: []string{"delete"}, Resource: []string{"namespace"}, Environment: []string{"production"}}},
	}

	// Test 1: kubectl delete namespace in prod → highest priority deny wins
	ctx := CommandContext{Tool: "kubectl", Action: "delete", ActionType: "destructive", Resource: "namespace", Environment: "production"}
	result := EvaluatePolicies(paths, cfg, policies, ctx)
	if result.Decision != DecisionDeny {
		t.Errorf("ns delete in prod: decision = %q, want deny", result.Decision)
	}
	if result.PolicyName != "ns-deny" {
		t.Errorf("ns delete in prod: policy = %q, want ns-deny", result.PolicyName)
	}

	// Test 2: kubectl delete pod in prod → confirm (ns-deny doesn't match pod)
	ctx2 := CommandContext{Tool: "kubectl", Action: "delete", ActionType: "destructive", Resource: "pod", Environment: "production"}
	result2 := EvaluatePolicies(paths, cfg, policies, ctx2)
	if result2.Decision != DecisionConfirm {
		t.Errorf("pod delete in prod: decision = %q, want confirm", result2.Decision)
	}
	if result2.PolicyName != "prod-confirm" {
		t.Errorf("pod delete in prod: policy = %q, want prod-confirm", result2.PolicyName)
	}

	// Test 3: kubectl delete pod in staging → broad warn (prod-confirm doesn't match)
	ctx3 := CommandContext{Tool: "kubectl", Action: "delete", ActionType: "destructive", Resource: "pod", Environment: "staging"}
	result3 := EvaluatePolicies(paths, cfg, policies, ctx3)
	if result3.Decision != DecisionWarn {
		t.Errorf("pod delete in staging: decision = %q, want warn", result3.Decision)
	}
	if result3.PolicyName != "broad-warn" {
		t.Errorf("pod delete in staging: policy = %q, want broad-warn", result3.PolicyName)
	}

	// Test 4: kubectl get pods in prod → no match, allow
	ctx4 := CommandContext{Tool: "kubectl", Action: "get", ActionType: "read", Resource: "pods", Environment: "production"}
	result4 := EvaluatePolicies(paths, cfg, policies, ctx4)
	if result4.Decision != DecisionAllow {
		t.Errorf("get pods in prod: decision = %q, want allow", result4.Decision)
	}
}

// TestE2E_TerraformStateHandling tests terraform state subcommand parsing.
func TestE2E_TerraformStateHandling(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)
	os.WriteFile(paths.PoliciesPath, []byte(starterPoliciesYAML), 0o644)

	cfg, _ := LoadConfig(paths)
	policies, _ := LoadPolicies(paths)

	// terraform state rm → should be destructive
	ctx := ParseCommand("terraform", []string{"state", "rm", "aws_instance.web"}, "/tmp", false)
	if ctx.Action != "state-rm" {
		t.Errorf("action = %q, want state-rm", ctx.Action)
	}
	if ctx.ActionType != "destructive" {
		t.Errorf("action_type = %q, want destructive", ctx.ActionType)
	}

	// In production → should trigger confirm-prod-terraform-state-mutation
	ctx.Environment = "production"
	result := EvaluatePolicies(paths, cfg, policies, ctx)
	if result.Decision != DecisionConfirm {
		t.Errorf("state-rm in prod: decision = %q, want confirm (policy=%s)", result.Decision, result.PolicyName)
	}

	// terraform state mv → also destructive
	ctx2 := ParseCommand("terraform", []string{"state", "mv", "aws_instance.a", "aws_instance.b"}, "/tmp", false)
	if ctx2.Action != "state-mv" {
		t.Errorf("action = %q, want state-mv", ctx2.Action)
	}
	if ctx2.ActionType != "destructive" {
		t.Errorf("action_type = %q, want destructive", ctx2.ActionType)
	}
}

// TestE2E_InfoCommandsBypass verifies info commands bypass policy.
func TestE2E_InfoCommandsBypass(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"--help"}, true},
		{[]string{"-h"}, true},
		{[]string{"--version"}, true},
		{[]string{"-v"}, true},
		{[]string{"help"}, true},
		{[]string{"version"}, true},
		{[]string{}, true},
		{[]string{"delete", "namespace", "prod"}, false},
		{[]string{"apply", "-f", "deploy.yaml"}, false},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, "_"), func(t *testing.T) {
			got := isInfoOnlyCommand(tt.args)
			if got != tt.want {
				t.Errorf("isInfoOnlyCommand(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

// TestE2E_BuildRawCommandAndHash tests command reconstruction and hashing.
func TestE2E_BuildRawCommandAndHash(t *testing.T) {
	raw := BuildRawCommand("kubectl", []string{"delete", "namespace", "prod"})
	if raw != "kubectl delete namespace prod" {
		t.Errorf("raw = %q", raw)
	}

	// Same raw command + env = same hash
	h1 := CommandHash("kubectl", raw, "production")
	h2 := CommandHash("kubectl", raw, "production")
	if h1 != h2 {
		t.Error("identical inputs should produce identical hashes")
	}

	// Different env = different hash
	h3 := CommandHash("kubectl", raw, "staging")
	if h1 == h3 {
		t.Error("different environments should produce different hashes")
	}

	// Args with spaces are quoted
	raw2 := BuildRawCommand("kubectl", []string{"apply", "-f", "my file.yaml"})
	if !strings.Contains(raw2, `"my file.yaml"`) {
		t.Errorf("should quote args with spaces: %q", raw2)
	}
}
