package agentgate

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCollectMetrics_Empty(t *testing.T) {
	paths := testPaths(t)
	snapshot, err := CollectMetrics(paths, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot.CommandsTotal != 0 {
		t.Errorf("commands_total = %d, want 0", snapshot.CommandsTotal)
	}
	if snapshot.InFlightTotal != 0 {
		t.Errorf("inflight = %d, want 0", snapshot.InFlightTotal)
	}
}

func TestCollectMetrics_WithEvents(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	now := time.Now().UTC()
	events := []any{
		StartEvent{
			TS:       now.Add(-1 * time.Hour),
			ID:       "ag_001",
			Phase:    "start",
			Mode:     ModeEnforce,
			Tool:     "kubectl",
			Env:      "production",
			Decision: DecisionDeny,
			Policy:   "no-prod-delete",
			Parse:    ParseStatusParsed,
		},
		EndEvent{
			TS:       now.Add(-1*time.Hour + time.Second),
			ID:       "ag_001",
			Phase:    "end",
			ExitCode: policyExitCode,
			Outcome:  "blocked-policy",
		},
		StartEvent{
			TS:       now.Add(-30 * time.Minute),
			ID:       "ag_002",
			Phase:    "start",
			Mode:     ModeObserve,
			Tool:     "terraform",
			Env:      "staging",
			Decision: DecisionAllow,
			Parse:    ParseStatusParsed,
		},
		EndEvent{
			TS:       now.Add(-30*time.Minute + 5*time.Second),
			ID:       "ag_002",
			Phase:    "end",
			ExitCode: 0,
			Outcome:  "executed",
		},
	}

	var sb strings.Builder
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		sb.Write(b)
		sb.WriteByte('\n')
	}
	os.WriteFile(paths.EventsPath, []byte(sb.String()), 0o644)

	snapshot, err := CollectMetrics(paths, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if snapshot.CommandsTotal != 2 {
		t.Errorf("commands_total = %d, want 2", snapshot.CommandsTotal)
	}
	if snapshot.InFlightTotal != 0 {
		t.Errorf("inflight = %d, want 0", snapshot.InFlightTotal)
	}

	// Check blocked count
	blockedKey := blockedMetricKey{Tool: "kubectl", Environment: "production", Policy: "no-prod-delete"}
	if snapshot.BlockedCounts[blockedKey] != 1 {
		t.Errorf("blocked count = %d, want 1", snapshot.BlockedCounts[blockedKey])
	}

	// Check allowed count
	allowedKey := allowedMetricKey{Tool: "terraform", Environment: "staging", Outcome: "executed"}
	if snapshot.AllowedCounts[allowedKey] != 1 {
		t.Errorf("allowed count = %d, want 1", snapshot.AllowedCounts[allowedKey])
	}
}

func TestCollectMetrics_InFlight(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	now := time.Now().UTC()
	// Start event with no matching end
	ev := StartEvent{
		TS:       now.Add(-5 * time.Minute),
		ID:       "ag_inflight",
		Phase:    "start",
		Mode:     ModeEnforce,
		Tool:     "kubectl",
		Env:      "production",
		Decision: DecisionAllow,
		Parse:    ParseStatusParsed,
	}
	b, _ := json.Marshal(ev)
	os.WriteFile(paths.EventsPath, append(b, '\n'), 0o644)

	snapshot, _ := CollectMetrics(paths, now.Add(-1*time.Hour))
	if snapshot.InFlightTotal != 1 {
		t.Errorf("inflight = %d, want 1", snapshot.InFlightTotal)
	}
}

func TestCollectMetrics_FilterBySince(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	now := time.Now().UTC()
	events := []any{
		StartEvent{TS: now.Add(-48 * time.Hour), ID: "ag_old", Phase: "start", Tool: "kubectl", Env: "production", Decision: DecisionAllow, Parse: ParseStatusParsed},
		StartEvent{TS: now.Add(-1 * time.Hour), ID: "ag_recent", Phase: "start", Tool: "terraform", Env: "staging", Decision: DecisionAllow, Parse: ParseStatusParsed},
	}
	var sb strings.Builder
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		sb.Write(b)
		sb.WriteByte('\n')
	}
	os.WriteFile(paths.EventsPath, []byte(sb.String()), 0o644)

	snapshot, _ := CollectMetrics(paths, now.Add(-24*time.Hour))
	if snapshot.CommandsTotal != 1 {
		t.Errorf("commands_total = %d, want 1 (filtered by since)", snapshot.CommandsTotal)
	}
}

func TestPrintPrometheusMetrics(t *testing.T) {
	snapshot := MetricsSnapshot{
		GeneratedAt:    time.Now().UTC(),
		CommandsTotal:  5,
		InFlightTotal:  1,
		DecisionCounts: map[decisionMetricKey]int{{Tool: "kubectl", Environment: "production", Decision: "deny", Mode: "enforce"}: 3},
		BlockedCounts:  map[blockedMetricKey]int{{Tool: "kubectl", Environment: "production", Policy: "no-prod-delete"}: 2},
		AllowedCounts:  map[allowedMetricKey]int{{Tool: "terraform", Environment: "staging", Outcome: "executed"}: 1},
		ParseCounts:    map[parseMetricKey]int{{Tool: "kubectl", ParseStatus: "parsed"}: 4},
	}

	var buf bytes.Buffer
	printPrometheusMetrics(&buf, snapshot)
	output := buf.String()

	if !strings.Contains(output, "agentgate_commands_total") {
		t.Error("should contain agentgate_commands_total")
	}
	if !strings.Contains(output, "agentgate_commands_blocked_total") {
		t.Error("should contain agentgate_commands_blocked_total")
	}
	if !strings.Contains(output, "agentgate_inflight_commands") {
		t.Error("should contain agentgate_inflight_commands")
	}
	if !strings.Contains(output, `tool="kubectl"`) {
		t.Error("should contain tool label")
	}
}

func TestPrintJSONMetrics(t *testing.T) {
	snapshot := MetricsSnapshot{
		GeneratedAt:    time.Now().UTC(),
		CommandsTotal:  2,
		InFlightTotal:  0,
		DecisionCounts: map[decisionMetricKey]int{},
		BlockedCounts:  map[blockedMetricKey]int{},
		AllowedCounts:  map[allowedMetricKey]int{},
		ParseCounts:    map[parseMetricKey]int{},
	}

	var buf bytes.Buffer
	printJSONMetrics(&buf, snapshot, "24h")
	output := buf.String()

	var parsed map[string]any
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("should produce valid JSON: %v", err)
	}
	if parsed["commands_total"].(float64) != 2 {
		t.Errorf("commands_total = %v, want 2", parsed["commands_total"])
	}
	if parsed["window"] != "24h" {
		t.Errorf("window = %v, want 24h", parsed["window"])
	}
}
