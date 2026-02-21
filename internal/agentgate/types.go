package agentgate

import "time"

type Mode string

const (
	ModeObserve Mode = "observe"
	ModeEnforce Mode = "enforce"
)

type Decision string

const (
	DecisionAllow   Decision = "allow"
	DecisionWarn    Decision = "warn"
	DecisionConfirm Decision = "confirm"
	DecisionDeny    Decision = "deny"
)

type ParseStatus string

const (
	ParseStatusParsed  ParseStatus = "parsed"
	ParseStatusPartial ParseStatus = "partial"
	ParseStatusUnknown ParseStatus = "unknown"
)

type CommandContext struct {
	Tool         string            `json:"tool" yaml:"tool"`
	Action       string            `json:"action" yaml:"action"`
	ActionType   string            `json:"action_type" yaml:"action_type"`
	Resource     string            `json:"resource" yaml:"resource"`
	ResourceName string            `json:"resource_name" yaml:"resource_name"`
	Namespace    string            `json:"namespace" yaml:"namespace"`
	Flags        map[string]string `json:"flags" yaml:"flags"`
	RawCommand   string            `json:"raw_command" yaml:"raw_command"`
	RawArgs      []string          `json:"raw_args" yaml:"raw_args"`
	Environment  string            `json:"environment" yaml:"environment"`
	EnvReason    string            `json:"env_reason" yaml:"env_reason"`
	WorkingDir   string            `json:"working_dir" yaml:"working_dir"`
	Timestamp    time.Time         `json:"timestamp" yaml:"timestamp"`
	Interactive  bool              `json:"interactive" yaml:"interactive"`
	ParseStatus  ParseStatus       `json:"parse_status" yaml:"parse_status"`
}

type PolicyFile struct {
	Policies []Policy `yaml:"policies"`
}

type Policy struct {
	Name        string           `yaml:"name"`
	Priority    int              `yaml:"priority"`
	Decision    Decision         `yaml:"decision"`
	Suggestion  string           `yaml:"suggestion"`
	Match       PolicyMatch      `yaml:"match"`
	RateLimit   *RateLimitRule   `yaml:"rate_limit"`
	RequirePlan *RequirePlanRule `yaml:"require_plan"`
}

type RateLimitRule struct {
	Limit  int    `yaml:"limit"`
	Window string `yaml:"window"`
}

type RequirePlanRule struct {
	Window string `yaml:"window"`
}

type PolicyMatch struct {
	Tool        []string `yaml:"tool"`
	Environment []string `yaml:"environment"`
	Action      []string `yaml:"action"`
	ActionType  []string `yaml:"action_type"`
	Resource    []string `yaml:"resource"`
	Namespace   []string `yaml:"namespace"`
	Flags       []string `yaml:"flags"`
	RawContains []string `yaml:"raw_contains"`
}

type Config struct {
	Mode                      Mode                `yaml:"mode"`
	UnknownEnvDefaultDecision Decision            `yaml:"unknown_env_default_decision"`
	EnvironmentPatterns       map[string][]string `yaml:"environment_patterns"`
}

type DecisionResult struct {
	Decision   Decision
	PolicyName string
	Suggestion string
	Risk       int
}

type HistoryRecord struct {
	TS         time.Time `json:"ts"`
	ID         string    `json:"id"`
	Tool       string    `json:"tool"`
	Action     string    `json:"action"`
	ActionType string    `json:"action_type"`
	Env        string    `json:"env"`
	WorkingDir string    `json:"working_dir"`
	Decision   Decision  `json:"decision"`
}
