package agentgate

import (
	"testing"
	"time"
)

func TestMatchPattern_Substring(t *testing.T) {
	if !MatchPattern("delete", "delete") {
		t.Error("exact match should work")
	}
	if !MatchPattern("delete", "force-delete") {
		t.Error("substring match should work")
	}
	if MatchPattern("delete", "get") {
		t.Error("should not match unrelated")
	}
}

func TestMatchPattern_Wildcard(t *testing.T) {
	if !MatchPattern("*prod*", "production") {
		t.Error("*prod* should match production")
	}
	if !MatchPattern("*prod*", "my-prod-cluster") {
		t.Error("*prod* should match my-prod-cluster")
	}
	if MatchPattern("*prod*", "staging") {
		t.Error("*prod* should not match staging")
	}
}

func TestMatchPattern_Empty(t *testing.T) {
	if MatchPattern("", "anything") {
		t.Error("empty pattern should not match")
	}
}

func TestMatchPattern_CaseInsensitive(t *testing.T) {
	if !MatchPattern("Delete", "delete") {
		t.Error("should be case insensitive")
	}
	if !MatchPattern("*PROD*", "production") {
		t.Error("wildcard should be case insensitive")
	}
}

func TestNormalize(t *testing.T) {
	if Normalize("  Hello World  ") != "hello world" {
		t.Error("should lowercase and trim")
	}
	if Normalize("") != "" {
		t.Error("empty should stay empty")
	}
}

func TestParseWindow(t *testing.T) {
	if ParseWindow("5m", time.Hour) != 5*time.Minute {
		t.Error("should parse 5m")
	}
	if ParseWindow("2h", time.Hour) != 2*time.Hour {
		t.Error("should parse 2h")
	}
	if ParseWindow("invalid", time.Hour) != time.Hour {
		t.Error("invalid should use fallback")
	}
}

func TestParseLastDuration(t *testing.T) {
	d, err := ParseLastDuration("7d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 7*24*time.Hour {
		t.Errorf("7d = %v, want 168h", d)
	}

	d, err = ParseLastDuration("24h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 24*time.Hour {
		t.Errorf("24h = %v", d)
	}

	d, err = ParseLastDuration("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 24*time.Hour {
		t.Errorf("empty = %v, want 24h", d)
	}

	_, err = ParseLastDuration("abc")
	if err == nil {
		t.Error("should error on invalid")
	}
}

func TestCommandHash(t *testing.T) {
	h1 := CommandHash("kubectl", "kubectl delete ns prod", "production")
	h2 := CommandHash("kubectl", "kubectl delete ns prod", "production")
	h3 := CommandHash("kubectl", "kubectl delete ns prod", "staging")

	if h1 != h2 {
		t.Error("same inputs should produce same hash")
	}
	if h1 == h3 {
		t.Error("different inputs should produce different hash")
	}
	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64", len(h1))
	}
}

func TestBuildRawCommand(t *testing.T) {
	got := BuildRawCommand("kubectl", []string{"delete", "namespace", "prod"})
	if got != "kubectl delete namespace prod" {
		t.Errorf("got %q", got)
	}

	got = BuildRawCommand("kubectl", []string{"apply", "-f", "file with spaces.yaml"})
	if got != `kubectl apply -f "file with spaces.yaml"` {
		t.Errorf("got %q", got)
	}
}

func TestNewCommandID(t *testing.T) {
	id := NewCommandID()
	if len(id) < 3 || id[:3] != "ag_" {
		t.Errorf("id should start with ag_, got %q", id)
	}
	id2 := NewCommandID()
	if id == id2 {
		t.Error("ids should be unique")
	}
}
