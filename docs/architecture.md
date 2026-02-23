# AgentGate Architecture

## Overview

AgentGate is a local policy-enforcement shim for infrastructure and developer CLIs. It operates at two layers:

1. **CLI Shims** — PATH-based interception of `kubectl`, `terraform`, `helm`, `aws`, `gcloud`, `git`, `docker`
2. **MCP Proxy** — JSON-RPC interception between AI agents and MCP servers

Both layers evaluate commands against the same YAML policy engine and log events to the same append-only event log.

## Execution Flow

### CLI Shim Path

```
User/Agent runs: kubectl delete namespace prod
         │
         ▼
~/.agentgate/bin/kubectl (shim script)
         │
         ▼
agentgate __intercept kubectl -- delete namespace prod
         │
         ├── parser.go: ParseCommand() → CommandContext
         │     Tool=kubectl, Action=delete, Resource=namespace,
         │     ActionType=destructive, ResourceName=prod
         │
         ├── environment.go: DetectEnvironment() → production
         │     Reads kubeconfig context, terraform workspace,
         │     AWS profile, or gcloud project
         │
         ├── policy.go: EvaluatePolicies() → DecisionResult
         │     Matches against policies.yaml rules by priority
         │     Checks rate limits and require-plan rules
         │
         ├── bypass.go: ConsumeValidBypass()
         │     Checks for one-time allow tokens
         │
         ├── Decision applied:
         │     allow  → exec real binary
         │     warn   → exec + stderr warning
         │     confirm → interactive prompt (or deny if non-interactive)
         │     deny   → exit 77
         │
         └── logging.go: AppendEvent() → events.jsonl
```

### MCP Proxy Path

```
AI Agent (e.g., Claude Code)
         │
         ▼
agentgate mcp-proxy -- npx @modelcontextprotocol/server-filesystem /tmp
         │
         ├── Spawns real MCP server as child process
         ├── Pipes stdin/stdout bidirectionally
         │
         ▼
JSON-RPC message: {"method": "tools/call", "params": {"name": "delete_file", ...}}
         │
         ├── mcp/policy.go: EvaluateToolCall()
         │     Constructs CommandContext from MCP tool call
         │     Maps server name → Tool, tool name → Action
         │     Evaluates against same policy engine
         │
         ├── If DENIED:
         │     Returns {"result": {"isError": true, "content": [{"text": "AGENTGATE BLOCKED: ..."}]}}
         │     Agent receives denial as tool error
         │
         ├── If ALLOWED:
         │     Forwards to real MCP server
         │     Response forwarded back to agent
         │
         └── All events logged with source: "mcp"
```

## Directory Structure

```
agentgate/
├── cmd/agentgate/
│   └── main.go              # Entry point, routes __intercept vs CLI vs mcp-proxy
├── internal/
│   ├── agentgate/
│   │   ├── types.go          # Core types: CommandContext, Policy, Decision, etc.
│   │   ├── intercept.go      # Main interception flow, decision application
│   │   ├── parser.go         # 7 tool-specific CLI parsers
│   │   ├── environment.go    # Environment detection with 30s cache
│   │   ├── policy.go         # Policy matching engine, rate limits, require-plan
│   │   ├── evaluate.go       # Shared evaluation logic (used by explain + MCP)
│   │   ├── cli.go            # User-facing CLI commands
│   │   ├── explain.go        # Preflight simulation command
│   │   ├── config.go         # File I/O, path resolution
│   │   ├── logging.go        # JSONL event logging, rotation
│   │   ├── bypass.go         # One-time bypass tokens
│   │   ├── metrics.go        # Prometheus metrics collection
│   │   ├── starter_policies.go # Default policy YAML
│   │   └── util.go           # Pattern matching, hashing, helpers
│   └── mcp/
│       ├── types.go          # JSON-RPC 2.0 and MCP protocol types
│       ├── policy.go         # MCP → policy engine bridge
│       └── proxy.go          # Bidirectional JSON-RPC proxy
├── integrations/
│   └── claude-code/
│       └── mcp_proxy_example.json  # Ready-to-use MCP config
├── templates/
│   └── starter-policies.yaml      # Template for init
├── docs/
│   ├── architecture.md            # This file
│   ├── mcp-proxy.md               # MCP proxy guide
│   ├── testing.md                 # Test guide
│   ├── guardrails-research.md     # Incident research
│   ├── investor-product-strategy.md
│   └── metrics-and-alerting.md
└── Makefile
```

## Data Directory (`~/.agentgate/`)

```
~/.agentgate/
├── bin/                    # Shim scripts (one per tool)
│   ├── kubectl
│   ├── terraform
│   ├── helm
│   ├── aws
│   ├── gcloud
│   ├── git
│   └── docker
├── config.yaml             # Mode (observe/enforce), patterns
├── policies.yaml           # YAML policy rules
├── events.jsonl            # Append-only event log; also used for rate-limit/require-plan checks
├── events.jsonl.1          # Rotated files (50MB threshold)
├── bypasses.jsonl           # Bypass token records
└── cache.json               # Environment detection cache (30s TTL)
```

## Policy Matching

Policies are evaluated in this order:

1. All policies are checked for match against the `CommandContext`
2. Matching policies are sorted by `priority` (highest wins)
3. Ties broken by restrictiveness: `deny > confirm > warn > allow`
4. Non-wildcard patterns use **substring matching** (`delete` matches `force-delete`)
5. Wildcard patterns use `filepath.Match` semantics (`*prod*` matches `production`)

### Match Fields

| Field | CLI Source | MCP Source |
|-------|-----------|------------|
| `tool` | CLI tool name | MCP server name |
| `action` | Parsed action verb | MCP tool name |
| `action_type` | Classified: read/write/destructive/other | Classified from tool name |
| `environment` | Detected from kubeconfig/workspace/profile | Not available (empty) |
| `resource` | Parsed resource type | N/A |
| `resource_name` | Parsed resource name | N/A |
| `namespace` | `-n` / `--namespace` flag | N/A |
| `flags` | Parsed CLI flags | N/A |
| `raw_contains` | Full command string | MCP argument values |

## Risk Scoring

Risk score = `base(10)` + `action_type` + `environment` + `decision`

| Factor | Value |
|--------|-------|
| Action: read | +5 |
| Action: write | +30 |
| Action: destructive | +55 |
| Action: other | +15 |
| Env: production | +20 |
| Env: unknown | +15 |
| Decision: warn | +5 |
| Decision: confirm | +15 |
| Decision: deny | +25 |

Capped at 99. Example: `kubectl delete ns prod` in production with deny = 10 + 55 + 20 + 25 = 99.

## Fail-Open Design

AgentGate is intentionally permissive on errors:

- Config parse failures → use defaults
- Policy parse failures → no policies (allow all)
- Environment detection failures → `unknown` environment
- MCP message parse failures → forward unchanged
- Binary not found → exit 0 with warning

This ensures AgentGate never blocks legitimate work due to its own bugs.

## Stderr Contract

Every intercepted CLI command produces structured stderr output:

```
AGENTGATE_ACTIVE=true
AGENTGATE_DECISION=deny
AGENTGATE_POLICY=no-prod-namespace-delete
AGENTGATE_RISK=99
AGENTGATE_ENVIRONMENT=production
AGENTGATE_COMMAND_ID=ag_a1b2c3d4e5f6
AGENTGATE_CONFIRM_REQUIRED=false
AGENTGATE_SUGGESTION=Deleting namespaces in production is blocked.
```

MCP proxy produces:

```
AGENTGATE_MCP_PROXY=true
AGENTGATE_MCP_SESSION=ag_session123
AGENTGATE_MCP_SERVER=filesystem
AGENTGATE_MCP_BLOCKED: tool=delete_file policy=block-rm risk=90
```
