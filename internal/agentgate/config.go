package agentgate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	eventsRotateSize = 50 * 1024 * 1024
	eventsRotateKeep = 3
)

var tools = []string{"kubectl", "terraform", "helm", "aws", "gcloud", "git", "docker"}

type Paths struct {
	Root             string
	BinDir           string
	ConfigPath       string
	PoliciesPath     string
	EventsPath       string
	BypassesPath     string
	CachePath        string
	EventsLock       string
	MCPOriginalsPath string
}

func ResolvePaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home: %w", err)
	}
	root := filepath.Join(home, ".agentgate")
	return Paths{
		Root:             root,
		BinDir:           filepath.Join(root, "bin"),
		ConfigPath:       filepath.Join(root, "config.yaml"),
		PoliciesPath:     filepath.Join(root, "policies.yaml"),
		EventsPath:       filepath.Join(root, "events.jsonl"),
		BypassesPath:     filepath.Join(root, "bypasses.jsonl"),
		CachePath:        filepath.Join(root, "cache.json"),
		EventsLock:       filepath.Join(root, ".events.lock"),
		MCPOriginalsPath: filepath.Join(root, "mcp_originals.json"),
	}, nil
}

func DefaultConfig() Config {
	return Config{
		Mode:                      ModeObserve,
		UnknownEnvDefaultDecision: DecisionWarn,
		EnvironmentPatterns: map[string][]string{
			"production": {"*prod*", "*production*", "*live*"},
			"staging":    {"*stage*", "*staging*", "*preprod*"},
			"dev":        {"*dev*", "*sandbox*", "*test*", "*local*"},
		},
	}
}

func EnsureDirs(paths Paths) error {
	if err := os.MkdirAll(paths.Root, 0o755); err != nil {
		return err
	}
	return os.MkdirAll(paths.BinDir, 0o755)
}

func LoadConfig(paths Paths) (Config, error) {
	cfg := DefaultConfig()
	b, err := os.ReadFile(paths.ConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return DefaultConfig(), fmt.Errorf("parse config: %w", err)
	}
	if cfg.Mode != ModeObserve && cfg.Mode != ModeEnforce {
		cfg.Mode = ModeObserve
	}
	if cfg.UnknownEnvDefaultDecision == "" {
		cfg.UnknownEnvDefaultDecision = DecisionWarn
	}
	if len(cfg.EnvironmentPatterns) == 0 {
		cfg.EnvironmentPatterns = DefaultConfig().EnvironmentPatterns
	}
	return cfg, nil
}

func SaveConfig(paths Paths, cfg Config) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(paths.ConfigPath, b, 0o644)
}

func EnsureConfig(paths Paths) (Config, error) {
	cfg, err := LoadConfig(paths)
	if err != nil {
		return Config{}, err
	}
	if _, err := os.Stat(paths.ConfigPath); errors.Is(err, os.ErrNotExist) {
		if err := SaveConfig(paths, cfg); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

func LoadPolicies(paths Paths) ([]Policy, error) {
	b, err := os.ReadFile(paths.PoliciesPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var pf PolicyFile
	if err := yaml.Unmarshal(b, &pf); err != nil {
		return nil, fmt.Errorf("parse policies: %w", err)
	}
	for i := range pf.Policies {
		m := &pf.Policies[i].Match
		if len(m.MCPServer) > 0 {
			fmt.Fprintf(os.Stderr, "agentgate: policy %q uses deprecated mcp_server field; use tool instead\n", pf.Policies[i].Name)
			m.Tool = append(m.Tool, m.MCPServer...)
			m.MCPServer = nil
		}
		if len(m.MCPTool) > 0 {
			fmt.Fprintf(os.Stderr, "agentgate: policy %q uses deprecated mcp_tool field; use action instead\n", pf.Policies[i].Name)
			m.Action = append(m.Action, m.MCPTool...)
			m.MCPTool = nil
		}
		if len(m.MCPArgsContain) > 0 {
			fmt.Fprintf(os.Stderr, "agentgate: policy %q uses deprecated mcp_args_contain field; use raw_contains instead\n", pf.Policies[i].Name)
			m.RawContains = append(m.RawContains, m.MCPArgsContain...)
			m.MCPArgsContain = nil
		}
	}
	return pf.Policies, nil
}
