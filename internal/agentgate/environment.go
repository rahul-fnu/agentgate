package agentgate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type cacheValue struct {
	Value     string    `json:"value"`
	ExpiresAt time.Time `json:"expires_at"`
}

type cacheFile map[string]cacheValue

func DetectEnvironment(paths Paths, cfg Config, ctx *CommandContext) {
	var indicator string
	var reason string
	switch ctx.Tool {
	case "kubectl", "helm":
		indicator, reason = detectKubeIndicator(paths, ctx)
	case "terraform":
		indicator, reason = detectTerraformIndicator(paths, ctx)
	case "aws":
		indicator, reason = detectAWSIndicator(ctx)
	case "gcloud":
		indicator, reason = detectGCloudIndicator(paths, ctx)
	default:
		indicator = ""
	}

	env, pattern := classifyEnvironment(cfg, indicator)
	if env == "" {
		ctx.Environment = "unknown"
		if reason == "" {
			ctx.EnvReason = "no environment indicator matched"
		} else {
			ctx.EnvReason = reason
		}
		return
	}
	ctx.Environment = env
	if reason == "" {
		reason = fmt.Sprintf("indicator matched pattern %q", pattern)
	} else {
		reason = fmt.Sprintf("%s; pattern matched %q", reason, pattern)
	}
	ctx.EnvReason = reason
}

func classifyEnvironment(cfg Config, indicator string) (string, string) {
	value := Normalize(indicator)
	if value == "" {
		return "", ""
	}
	for env, patterns := range cfg.EnvironmentPatterns {
		for _, p := range patterns {
			if MatchPattern(p, value) {
				return env, p
			}
		}
	}
	return "", ""
}

func detectKubeIndicator(paths Paths, ctx *CommandContext) (string, string) {
	context := ctx.Flags["context"]
	if context == "" {
		context = currentKubeContext(paths)
	}
	parts := []string{}
	if context != "" {
		parts = append(parts, context)
	}
	if ctx.Namespace != "" {
		parts = append(parts, ctx.Namespace)
	}
	if len(parts) == 0 {
		return "", "kube context unavailable"
	}
	return strings.Join(parts, ":"), "kube context/namespace inspected"
}

func detectTerraformIndicator(paths Paths, ctx *CommandContext) (string, string) {
	wd := ctx.WorkingDir
	if wd == "" {
		wd = "."
	}
	ws := terraformWorkspace(paths, wd)
	if ws == "" {
		return wd, "terraform working directory inspected"
	}
	return wd + ":" + ws, "terraform workspace/working directory inspected"
}

func detectAWSIndicator(ctx *CommandContext) (string, string) {
	profile := ctx.Flags["profile"]
	if profile == "" {
		profile = os.Getenv("AWS_PROFILE")
	}
	if profile == "" {
		return "", "AWS profile unavailable"
	}
	return profile, "aws profile inspected"
}

func detectGCloudIndicator(paths Paths, ctx *CommandContext) (string, string) {
	project := ctx.Flags["project"]
	if project == "" {
		project = gcloudProject(paths)
	}
	if project == "" {
		return "", "gcloud project unavailable"
	}
	return project, "gcloud project inspected"
}

func currentKubeContext(paths Paths) string {
	if v, ok := cacheGet(paths, "kube_current_context"); ok {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	kubePath := os.Getenv("KUBECONFIG")
	if kubePath == "" {
		kubePath = filepath.Join(home, ".kube", "config")
	}
	b, err := os.ReadFile(kubePath)
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := yaml.Unmarshal(b, &payload); err != nil {
		return ""
	}
	v, _ := payload["current-context"].(string)
	if v != "" {
		cacheSet(paths, "kube_current_context", v, 30*time.Second)
	}
	return v
}

func terraformWorkspace(paths Paths, wd string) string {
	key := "tf_workspace:" + wd
	if v, ok := cacheGet(paths, key); ok {
		return v
	}
	b, err := os.ReadFile(filepath.Join(wd, ".terraform", "environment"))
	if err != nil {
		return ""
	}
	ws := strings.TrimSpace(string(b))
	if ws != "" {
		cacheSet(paths, key, ws, 30*time.Second)
	}
	return ws
}

func gcloudProject(paths Paths) string {
	if v, ok := cacheGet(paths, "gcloud_project"); ok {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	cfg := filepath.Join(home, ".config", "gcloud", "configurations", "config_default")
	b, err := os.ReadFile(cfg)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "project") {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 {
				p := strings.TrimSpace(parts[1])
				cacheSet(paths, "gcloud_project", p, 30*time.Second)
				return p
			}
		}
	}
	return ""
}

func cacheGet(paths Paths, key string) (string, bool) {
	c, err := readCache(paths)
	if err != nil {
		return "", false
	}
	entry, ok := c[key]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.ExpiresAt) {
		return "", false
	}
	return entry.Value, true
}

func cacheSet(paths Paths, key, value string, ttl time.Duration) {
	c, err := readCache(paths)
	if err != nil {
		c = cacheFile{}
	}
	c[key] = cacheValue{Value: value, ExpiresAt: time.Now().Add(ttl)}
	_ = writeCache(paths, c)
}

func readCache(paths Paths) (cacheFile, error) {
	b, err := os.ReadFile(paths.CachePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cacheFile{}, nil
		}
		return nil, err
	}
	var c cacheFile
	if err := json.Unmarshal(b, &c); err != nil {
		return cacheFile{}, nil
	}
	return c, nil
}

func writeCache(paths Paths, c cacheFile) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(paths.CachePath, b, 0o644)
}
