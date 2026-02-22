package agentgate

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAppendEvent(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	ev := StartEvent{
		TS:       time.Now().UTC(),
		ID:       "ag_test123",
		Phase:    "start",
		Mode:     ModeEnforce,
		Tool:     "kubectl",
		Cmd:      "kubectl delete ns prod",
		Decision: DecisionDeny,
	}
	if err := AppendEvent(paths, ev); err != nil {
		t.Fatalf("append: %v", err)
	}

	b, err := os.ReadFile(paths.EventsPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(b), "ag_test123") {
		t.Error("event should be written to file")
	}
}

func TestAppendEvent_Multiple(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	for i := 0; i < 5; i++ {
		ev := StartEvent{
			TS:    time.Now().UTC(),
			ID:    NewCommandID(),
			Phase: "start",
			Tool:  "kubectl",
		}
		if err := AppendEvent(paths, ev); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	b, _ := os.ReadFile(paths.EventsPath)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}
}

func TestReadLastLines(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	var lines []string
	for i := 0; i < 10; i++ {
		ev := StartEvent{TS: time.Now().UTC(), ID: NewCommandID(), Phase: "start", Tool: "kubectl"}
		b, _ := json.Marshal(ev)
		lines = append(lines, string(b))
	}
	os.WriteFile(paths.EventsPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	result, err := ReadLastLines(paths.EventsPath, 10)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected some lines, got 0")
	}
	if len(result) > 10 {
		t.Errorf("expected at most 10 lines, got %d", len(result))
	}
}

func TestReadLastLines_Empty(t *testing.T) {
	paths := testPaths(t)
	_, err := ReadLastLines(paths.EventsPath, 5)
	if err == nil {
		t.Error("missing file should error")
	}
}

func TestReadLastLines_Zero(t *testing.T) {
	result, err := ReadLastLines("/tmp/whatever", 0)
	if err != nil {
		t.Errorf("n=0 should not error: %v", err)
	}
	if result != nil {
		t.Error("n=0 should return nil")
	}
}

func TestFindStartEventByID(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	targetID := "ag_findme"
	events := []StartEvent{
		{TS: time.Now().UTC(), ID: "ag_other1", Phase: "start", Tool: "kubectl"},
		{TS: time.Now().UTC(), ID: targetID, Phase: "start", Tool: "terraform", CmdHash: "hash123"},
		{TS: time.Now().UTC(), ID: "ag_other2", Phase: "start", Tool: "helm"},
	}
	var sb strings.Builder
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		sb.Write(b)
		sb.WriteByte('\n')
	}
	os.WriteFile(paths.EventsPath, []byte(sb.String()), 0o644)

	found, err := FindStartEventByID(paths, targetID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found == nil {
		t.Fatal("should find event")
	}
	if found.Tool != "terraform" {
		t.Errorf("tool = %q, want terraform", found.Tool)
	}
	if found.CmdHash != "hash123" {
		t.Errorf("cmd_hash = %q, want hash123", found.CmdHash)
	}
}

func TestFindStartEventByID_NotFound(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	ev := StartEvent{TS: time.Now().UTC(), ID: "ag_other", Phase: "start", Tool: "kubectl"}
	b, _ := json.Marshal(ev)
	os.WriteFile(paths.EventsPath, append(b, '\n'), 0o644)

	found, err := FindStartEventByID(paths, "ag_nonexistent")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found != nil {
		t.Error("should not find non-existent event")
	}
}

func TestAppendHistory(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	rec := HistoryRecord{
		TS:         time.Now().UTC(),
		ID:         "ag_hist1",
		Tool:       "kubectl",
		Action:     "delete",
		ActionType: "destructive",
		Env:        "production",
		Decision:   DecisionDeny,
	}
	if err := AppendHistory(paths, rec); err != nil {
		t.Fatalf("append history: %v", err)
	}

	b, _ := os.ReadFile(paths.HistoryPath)
	if !strings.Contains(string(b), "ag_hist1") {
		t.Error("history record should be written")
	}
}
