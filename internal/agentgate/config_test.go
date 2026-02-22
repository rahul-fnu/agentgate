package agentgate

import (
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Mode != ModeObserve {
		t.Errorf("default mode = %q, want observe", cfg.Mode)
	}
	if cfg.UnknownEnvDefaultDecision != DecisionWarn {
		t.Errorf("default unknown_env = %q, want warn", cfg.UnknownEnvDefaultDecision)
	}
	if len(cfg.EnvironmentPatterns) == 0 {
		t.Error("should have default environment patterns")
	}
	if _, ok := cfg.EnvironmentPatterns["production"]; !ok {
		t.Error("should have production patterns")
	}
}

func TestLoadConfig_Missing(t *testing.T) {
	paths := testPaths(t)
	cfg, err := LoadConfig(paths)
	if err != nil {
		t.Fatalf("missing config should not error: %v", err)
	}
	if cfg.Mode != ModeObserve {
		t.Error("missing config should return defaults")
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	cfg := DefaultConfig()
	cfg.Mode = ModeEnforce
	if err := SaveConfig(paths, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadConfig(paths)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Mode != ModeEnforce {
		t.Errorf("loaded mode = %q, want enforce", loaded.Mode)
	}
}

func TestLoadConfig_InvalidMode(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)
	os.WriteFile(paths.ConfigPath, []byte("mode: invalid\n"), 0o644)

	cfg, err := LoadConfig(paths)
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if cfg.Mode != ModeObserve {
		t.Errorf("invalid mode should default to observe, got %q", cfg.Mode)
	}
}

func TestLoadConfig_MalformedYAML(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)
	os.WriteFile(paths.ConfigPath, []byte("{{invalid yaml"), 0o644)

	_, err := LoadConfig(paths)
	if err == nil {
		t.Error("malformed YAML should error")
	}
}

func TestEnsureConfig(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)

	cfg, err := EnsureConfig(paths)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if cfg.Mode != ModeObserve {
		t.Error("should create default config")
	}

	// File should now exist
	if _, err := os.Stat(paths.ConfigPath); err != nil {
		t.Error("config file should be created")
	}
}

func TestLoadPolicies_Missing(t *testing.T) {
	paths := testPaths(t)
	policies, err := LoadPolicies(paths)
	if err != nil {
		t.Fatalf("missing policies should not error: %v", err)
	}
	if len(policies) != 0 {
		t.Error("should return empty slice")
	}
}

func TestLoadPolicies_StarterPolicies(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)
	os.WriteFile(paths.PoliciesPath, []byte(starterPoliciesYAML), 0o644)

	policies, err := LoadPolicies(paths)
	if err != nil {
		t.Fatalf("load starter policies: %v", err)
	}
	if len(policies) < 20 {
		t.Errorf("expected 20+ starter policies, got %d", len(policies))
	}
}

func TestLoadPolicies_MalformedYAML(t *testing.T) {
	paths := testPaths(t)
	os.MkdirAll(paths.Root, 0o755)
	os.WriteFile(paths.PoliciesPath, []byte("{{bad yaml"), 0o644)

	_, err := LoadPolicies(paths)
	if err == nil {
		t.Error("malformed YAML should error")
	}
}

func TestEnsureDirs(t *testing.T) {
	paths := testPaths(t)
	if err := EnsureDirs(paths); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
	if _, err := os.Stat(paths.Root); err != nil {
		t.Error("root dir should exist")
	}
	if _, err := os.Stat(paths.BinDir); err != nil {
		t.Error("bin dir should exist")
	}
}
