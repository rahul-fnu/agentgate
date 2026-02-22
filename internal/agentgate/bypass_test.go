package agentgate

import (
	"os"
	"testing"
	"time"
)

func TestIssueAndConsumeBypass(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	cmdHash := CommandHash("kubectl", "kubectl delete ns prod", "production")
	rec, err := IssueBypass(paths, "ag_test123", cmdHash, 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if rec.TokenID == "" {
		t.Error("token_id should not be empty")
	}
	if rec.CmdHash != cmdHash {
		t.Error("cmd_hash mismatch")
	}

	// Consume should succeed
	tokenID, ok, err := ConsumeValidBypass(paths, cmdHash, time.Now())
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if !ok {
		t.Error("should find valid bypass")
	}
	if tokenID != rec.TokenID {
		t.Errorf("token_id = %q, want %q", tokenID, rec.TokenID)
	}
}

func TestConsumeBypass_DoubleConsume(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	cmdHash := CommandHash("kubectl", "kubectl delete ns prod", "production")
	_, err := IssueBypass(paths, "ag_test123", cmdHash, 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	// First consume succeeds
	_, ok, _ := ConsumeValidBypass(paths, cmdHash, time.Now())
	if !ok {
		t.Error("first consume should succeed")
	}

	// Second consume should fail
	_, ok, _ = ConsumeValidBypass(paths, cmdHash, time.Now())
	if ok {
		t.Error("double consume should fail")
	}
}

func TestConsumeBypass_Expired(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	cmdHash := CommandHash("kubectl", "kubectl delete ns prod", "production")
	_, err := IssueBypass(paths, "ag_test123", cmdHash, 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	// Try consuming after expiry
	_, ok, _ := ConsumeValidBypass(paths, cmdHash, time.Now().Add(10*time.Minute))
	if ok {
		t.Error("expired bypass should not be consumed")
	}
}

func TestConsumeBypass_WrongHash(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	cmdHash := CommandHash("kubectl", "kubectl delete ns prod", "production")
	_, err := IssueBypass(paths, "ag_test123", cmdHash, 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	wrongHash := CommandHash("kubectl", "kubectl get pods", "production")
	_, ok, _ := ConsumeValidBypass(paths, wrongHash, time.Now())
	if ok {
		t.Error("wrong hash should not match")
	}
}

func TestConsumeBypass_NoFile(t *testing.T) {
	paths := testPaths(t)
	_, ok, err := ConsumeValidBypass(paths, "nonexistent", time.Now())
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if ok {
		t.Error("no file should return false")
	}
}
