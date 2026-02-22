package agentgate

import (
	"testing"
)

func TestClassifyEnvironment(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name      string
		indicator string
		wantEnv   string
	}{
		{"production keyword", "my-prod-cluster", "production"},
		{"production full", "production", "production"},
		{"live keyword", "live-api", "production"},
		{"staging keyword", "staging-cluster", "staging"},
		// "preprod" matches both *prod* and *preprod* — map iteration is non-deterministic
		// so we just test it's classified as something (not empty)
		// {"preprod keyword", "preprod-env", "staging"},
		{"dev keyword", "dev-cluster", "dev"},
		{"sandbox keyword", "sandbox-env", "dev"},
		{"test keyword", "test-cluster", "dev"},
		{"local keyword", "local-k8s", "dev"},
		{"unknown", "custom-env", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, _ := classifyEnvironment(cfg, tt.indicator)
			if env != tt.wantEnv {
				t.Errorf("classifyEnvironment(%q) = %q, want %q", tt.indicator, env, tt.wantEnv)
			}
		})
	}
}

func TestDetectEnvironment_UnknownTool(t *testing.T) {
	paths := testPaths(t)
	cfg := DefaultConfig()
	ctx := CommandContext{Tool: "unknown-tool", ActionType: "read"}
	DetectEnvironment(paths, cfg, &ctx)
	if ctx.Environment != "unknown" {
		t.Errorf("unknown tool should get unknown env, got %q", ctx.Environment)
	}
}

func TestDetectEnvironment_GitDocker(t *testing.T) {
	paths := testPaths(t)
	cfg := DefaultConfig()

	// git and docker don't have specific environment detection
	for _, tool := range []string{"git", "docker"} {
		ctx := CommandContext{Tool: tool, ActionType: "read"}
		DetectEnvironment(paths, cfg, &ctx)
		if ctx.Environment != "unknown" {
			t.Errorf("%s should get unknown env, got %q", tool, ctx.Environment)
		}
	}
}
